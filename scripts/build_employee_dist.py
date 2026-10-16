#!/usr/bin/env python3
"""Generate ASCENDING employee installation bundles from one private config."""

import argparse
import json
import os
import plistlib
import re
import shutil
import subprocess
import sys
import tempfile
import uuid
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
PROFILE_NAME = "CCWB"
BINARY_NAMES = {
    "macos-arm64": "credential-process-darwin-arm64",
    "macos-intel": "credential-process-darwin-amd64",
    "windows": "credential-process-windows-amd64.exe",
}
GO_TARGETS = {
    "macos-arm64": ("darwin", "arm64", "1"),
    "macos-intel": ("darwin", "amd64", "1"),
    "windows": ("windows", "amd64", "0"),
}
META_KEYS = {
    "profile_identifier", "profile_uuid", "profile_display_name",
    "payload_identifier", "payload_uuid", "payload_display_name",
}
PROFILE_KEYS = {
    "auth_type", "sso_enabled", "aws_region", "idc_region", "idc_start_url",
    "idc_account_id", "idc_permission_set_name",
}


def require_https_url(value, key):
    """Reject incomplete HTTPS addresses before they reach an employee bundle."""
    try:
        parsed = urlsplit(value)
        parsed.port  # A malformed port raises ValueError.
    except ValueError as error:
        raise ValueError(f"{key} must be a valid HTTPS URL with a host") from error
    if (parsed.scheme != "https" or not parsed.hostname or parsed.username is not None
            or parsed.password is not None or any(char.isspace() for char in value)):
        raise ValueError(f"{key} must be a valid HTTPS URL with a host")


def read_config(path):
    """Return runtime config, desktop policy, metadata and Bedrock region."""
    with Path(path).open(encoding="utf-8") as stream:
        data = json.load(stream)
    if not isinstance(data, dict) or set(data) != {"profiles", "desktop"}:
        raise ValueError("config.json must contain only profiles and desktop")
    profiles = data["profiles"]
    if not isinstance(profiles, dict) or set(profiles) != {PROFILE_NAME}:
        raise ValueError(f"config.json must contain one {PROFILE_NAME} profile")
    profile = profiles[PROFILE_NAME]
    if not isinstance(profile, dict) or set(profile) != PROFILE_KEYS:
        raise ValueError("profile must contain only the supported IDC fields")
    if profile["auth_type"] != "idc" or profile["sso_enabled"] is not False:
        raise ValueError("employee bundle requires IDC with sso_enabled=false")
    for key in ("aws_region", "idc_region", "idc_start_url", "idc_account_id", "idc_permission_set_name"):
        value = profile[key]
        if not isinstance(value, str) or not value.strip() or "REPLACE_WITH_" in value:
            raise ValueError(f"{key} must be a nonempty, non-example string")
    region = profile["aws_region"]
    for key in ("aws_region", "idc_region"):
        if not re.fullmatch(r"[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+", profile[key]):
            raise ValueError(f"{key} is not a valid AWS region name")
    if not re.fullmatch(r"[0-9]{12}", profile["idc_account_id"]):
        raise ValueError("idc_account_id must be a 12-digit AWS account ID")
    require_https_url(profile["idc_start_url"], "idc_start_url")
    desktop = data["desktop"]
    if not isinstance(desktop, dict) or not {"models", "mobileconfig"} <= set(desktop) or set(desktop) - {"models", "mobileconfig", "mcp_servers"}:
        raise ValueError("desktop must contain only models, mobileconfig and optional mcp_servers")
    models = desktop["models"]
    if not isinstance(models, list) or len(models) != 2:
        raise ValueError("desktop.models needs exactly Sonnet and Haiku")
    tiers = set()
    for model in models:
        allowed = {"name", "labelOverride", "anthropicFamilyTier", "supports1m", "isFamilyDefault"}
        if not isinstance(model, dict) or not {"name", "anthropicFamilyTier"} <= set(model) or set(model) - allowed:
            raise ValueError("desktop.models contains an invalid entry")
        arn = model["name"]
        pattern = rf"arn:(?:aws|aws-us-gov|aws-cn):bedrock:{re.escape(region)}:[0-9]{{12}}:application-inference-profile/[^\s/]+"
        if not isinstance(arn, str) or not re.fullmatch(pattern, arn):
            raise ValueError("desktop model must be an AIP ARN in the configured Bedrock region")
        tier = model["anthropicFamilyTier"]
        if tier not in {"sonnet", "haiku"} or tier in tiers:
            raise ValueError("desktop.models must contain one Sonnet and one Haiku")
        tiers.add(tier)
        if "labelOverride" in model and not isinstance(model["labelOverride"], str):
            raise ValueError("labelOverride must be a string")
        for key in ("supports1m", "isFamilyDefault"):
            if key in model and not isinstance(model[key], bool):
                raise ValueError(f"{key} must be a boolean")
    metadata = desktop["mobileconfig"]
    if not isinstance(metadata, dict) or set(metadata) != META_KEYS:
        raise ValueError("desktop.mobileconfig identity fields are incomplete")
    for key, value in metadata.items():
        if not isinstance(value, str) or not value.strip() or "example" in value.lower() or "REPLACE_WITH_" in value:
            raise ValueError(f"{key} has an empty or example value")
    for key in ("profile_uuid", "payload_uuid"):
        uuid.UUID(metadata[key])
        if metadata[key].startswith("00000000-0000-4000-8000-00000000000"):
            raise ValueError(f"{key} still contains an example UUID")
    mcp_servers = desktop.get("mcp_servers", [])
    if not isinstance(mcp_servers, list):
        raise ValueError("desktop.mcp_servers must be a list of remote MCP servers")
    seen_names = set()
    for server in mcp_servers:
        if not isinstance(server, dict) or set(server) != {"name", "transport", "url"}:
            raise ValueError("desktop.mcp_servers entries need exactly name, transport and url")
        name = server["name"]
        if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]*", name):
            raise ValueError(f"desktop.mcp_servers entry has an invalid server name: {name!r}")
        if name in seen_names:
            raise ValueError(f"duplicate desktop.mcp_servers server name: {name}")
        seen_names.add(name)
        if server["transport"] != "http":
            raise ValueError(f"desktop.mcp_servers transport must be http: {name}")
        url = server["url"]
        if not isinstance(url, str) or "REPLACE_WITH_" in url:
            raise ValueError(f"desktop.mcp_servers url must be a non-example HTTPS URL: {name}")
        require_https_url(url, f"desktop.mcp_servers url for {name}")
    mdm = {
        "inferenceProvider": "bedrock",
        "inferenceCredentialKind": "vendor-profile",
        "inferenceBedrockProfile": PROFILE_NAME,
        "inferenceBedrockRegion": region,
        "inferenceModels": json.dumps(models, ensure_ascii=False, separators=(",", ":")),
    }
    if mcp_servers:
        mdm["managedMcpServers"] = json.dumps(mcp_servers, ensure_ascii=False, separators=(",", ":"))
    return {"profiles": profiles}, mdm, metadata, region


def write_mobileconfig(path, mdm, metadata):
    payload = {
        "PayloadType": "com.anthropic.claudefordesktop",
        "PayloadIdentifier": metadata["payload_identifier"],
        "PayloadUUID": metadata["payload_uuid"],
        "PayloadVersion": 1,
        "PayloadDisplayName": metadata["payload_display_name"],
        **mdm,
    }
    policy = {
        "PayloadContent": [payload],
        "PayloadDisplayName": metadata["profile_display_name"],
        "PayloadIdentifier": metadata["profile_identifier"],
        "PayloadType": "Configuration",
        "PayloadUUID": metadata["profile_uuid"],
        "PayloadVersion": 1,
        "PayloadScope": "User",
    }
    with Path(path).open("wb") as stream:
        plistlib.dump(policy, stream, sort_keys=False)


def write_reg(path, mdm):
    lines = ["Windows Registry Editor Version 5.00", "", r"[HKEY_CURRENT_USER\SOFTWARE\Policies\Claude]"]
    for key, value in mdm.items():
        escaped = str(value).replace("\\", "\\\\").replace('"', '\\"')
        lines.append(f'"{key}"="{escaped}"')
    Path(path).write_text("\r\n".join(lines) + "\r\n", encoding="utf-16")


def render_template(source, target, region):
    content = Path(source).read_text(encoding="utf-8-sig")
    if content.count("__BEDROCK_REGION__") != 1:
        raise ValueError(f"template needs one Bedrock region marker: {source}")
    encoding = "utf-8-sig" if Path(source).suffix == ".ps1" else "utf-8"
    Path(target).write_text(content.replace("__BEDROCK_REGION__", region), encoding=encoding)
    if Path(source).suffix == ".sh":
        Path(target).chmod(0o755)


def build_bundle(config_path, template_dir, dist_dir, binary_dir):
    """Stage both platforms and replace an older bundle only when complete."""
    runtime, mdm, metadata, region = read_config(config_path)
    template_dir, binary_dir, dist_dir = map(Path, (template_dir, binary_dir, dist_dir))
    for platform, name in BINARY_NAMES.items():
        source = binary_dir / name
        if not source.is_file() or source.stat().st_size == 0:
            raise FileNotFoundError(f"missing nonempty {platform} binary: {source}")
    templates = {"macos": ("install.sh", "README-macOS.md"), "windows": ("install.ps1", "README-Windows.md")}
    for platform, files in templates.items():
        for name in files:
            if not (template_dir / platform / name).is_file():
                raise FileNotFoundError(f"missing template: {platform}/{name}")
    dist_dir.parent.mkdir(parents=True, exist_ok=True)
    output = dist_dir
    with tempfile.TemporaryDirectory(prefix=f".building-{dist_dir.name}-", dir=dist_dir.parent) as staging:
        stage = Path(staging)
        mac, win = stage / "macos", stage / "windows"
        mac.mkdir()
        win.mkdir()
        for target, platform in ((mac, "macos"), (win, "windows")):
            for name in templates[platform]:
                render_template(template_dir / platform / name, target / name, region)
            config_file = target / "config.json"
            config_file.write_text(json.dumps(runtime, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
            config_file.chmod(0o600)
        for platform, name in BINARY_NAMES.items():
            target = mac if platform.startswith("macos") else win
            shutil.copy2(binary_dir / name, target / name)
            (target / name).chmod(0o755)
        write_mobileconfig(mac / "Claude-CCWB.mobileconfig", mdm, metadata)
        write_reg(win / "Claude-CCWB.reg", mdm)
        for directory in (mac, win, stage):
            directory.chmod(0o700)
        previous = None
        if output.exists():
            previous = dist_dir.with_name(f".{dist_dir.name}-previous-{uuid.uuid4().hex}")
            output.rename(previous)
        try:
            stage.rename(output)
        except OSError:
            if previous is not None:
                previous.rename(output)
            raise
        if previous is not None:
            try:
                shutil.rmtree(previous)
            except OSError as error:
                print(f"Warning: new bundle published, but the old bundle needs manual cleanup: {previous} ({error})", file=sys.stderr)
    return output


def source_build_metadata(go_source):
    """Identify the Go source that produced the employee binaries."""
    try:
        version = subprocess.check_output(
            ["git", "describe", "--tags", "--always", "--dirty"],
            cwd=go_source, stderr=subprocess.DEVNULL, text=True,
        ).strip()
        commit = subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"],
            cwd=go_source, stderr=subprocess.DEVNULL, text=True,
        ).strip()
        return version, commit
    except (OSError, subprocess.CalledProcessError):
        return "dev", "unknown"


def compile_binaries(go_source, binary_dir):
    """Compile only credential-process, using the local Go tree as build input."""
    go_source = Path(go_source)
    if not (go_source / "go.mod").is_file() or not (go_source / "cmd" / "credential-process").is_dir():
        raise FileNotFoundError(f"not a credential-process Go source tree: {go_source}")
    version, commit = source_build_metadata(go_source)
    for platform, name in BINARY_NAMES.items():
        goos, goarch, cgo = GO_TARGETS[platform]
        env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED=cgo)
        ldflags = "" if goos == "windows" else "-s -w "
        ldflags += f"-X ccwb-go/internal/version.Version={version} -X ccwb-go/internal/version.Commit={commit}"
        print(f"Building {platform}...", file=sys.stderr)
        subprocess.run(
            ["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(Path(binary_dir) / name), "./cmd/credential-process"],
            cwd=go_source, env=env, check=True,
        )


def main(argv=None):
    parser = argparse.ArgumentParser(description="Generate the ASCENDING employee self-service installation dist")
    parser.add_argument("--config", type=Path, default=ROOT / "deployment/src/config/config.json")
    parser.add_argument("--dist", type=Path, default=ROOT / "dist")
    source = parser.add_mutually_exclusive_group()
    source.add_argument("--go-source", type=Path, default=ROOT / "source/go")
    source.add_argument("--binaries", type=Path)
    args = parser.parse_args(argv)
    try:
        if args.binaries:
            output = build_bundle(args.config, ROOT / "deployment/src", args.dist, args.binaries)
        else:
            with tempfile.TemporaryDirectory(prefix="ascending-ccwb-binaries-") as temp:
                compile_binaries(args.go_source, Path(temp))
                output = build_bundle(args.config, ROOT / "deployment/src", args.dist, temp)
    except (OSError, ValueError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"Employee bundle not published: {error}", file=sys.stderr)
        return 1
    print(f"Employee bundle generated: {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

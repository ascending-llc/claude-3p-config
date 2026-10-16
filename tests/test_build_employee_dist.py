"""Tests for the ASCENDING employee bundle builder."""

import importlib.util
import json
import plistlib
import shutil
import subprocess
import tempfile
import unittest
import codecs
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("employee_dist", ROOT / "scripts" / "build_employee_dist.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def valid_config(region="eu-west-1"):
    return {
        "profiles": {
            "CCWB": {
                "auth_type": "idc",
                "sso_enabled": False,
                "aws_region": region,
                "idc_region": region,
                "idc_start_url": "https://example.awsapps.com/start",
                "idc_account_id": "123456789012",
                "idc_permission_set_name": "BedrockAccess",
            }
        },
        "desktop": {
            "models": [
                {
                    "name": f"arn:aws:bedrock:{region}:123456789012:application-inference-profile/sonnet",
                    "anthropicFamilyTier": "sonnet",
                },
                {
                    "name": f"arn:aws:bedrock:{region}:123456789012:application-inference-profile/haiku",
                    "anthropicFamilyTier": "haiku",
                },
            ],
            "mobileconfig": {
                "profile_identifier": "com.ascending.claude.ccwb",
                "profile_uuid": "11111111-1111-4111-8111-111111111111",
                "profile_display_name": "Claude Bedrock",
                "payload_identifier": "com.ascending.claude.ccwb.payload",
                "payload_uuid": "22222222-2222-4222-8222-222222222222",
                "payload_display_name": "Claude Bedrock",
            },
        },
    }


class EmployeeDistTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.config = self.root / "config.json"
        self.binaries = self.root / "binaries"
        self.binaries.mkdir()
        for name in MODULE.BINARY_NAMES.values():
            (self.binaries / name).write_bytes(f"test binary: {name}".encode())
        self.dist = self.root / "dist"

    def write_config(self, data=None):
        self.config.write_text(json.dumps(data or valid_config()), encoding="utf-8")

    def build(self):
        return MODULE.build_bundle(self.config, ROOT / "deployment" / "src", self.dist, self.binaries)

    def test_generates_two_platform_directories_from_one_config(self):
        self.write_config()
        output = self.build()
        self.assertEqual(output, self.dist)
        self.assertEqual({p.name for p in output.iterdir()}, {"macos", "windows"})
        macos_readme = (output / "macos" / "README-macOS.md").read_text(encoding="utf-8")
        windows_readme = (output / "windows" / "README-Windows.md").read_text(encoding="utf-8")
        self.assertIn("--logout --profile CCWB", macos_readme)
        self.assertIn("--logout --profile CCWB", windows_readme)
        self.assertEqual({p.name for p in (output / "macos").iterdir()}, {
            "install.sh", "README-macOS.md", "config.json", "Claude-CCWB.mobileconfig",
            "credential-process-darwin-arm64", "credential-process-darwin-amd64",
        })
        self.assertEqual({p.name for p in (output / "windows").iterdir()}, {
            "install.ps1", "README-Windows.md", "config.json", "Claude-CCWB.reg",
            "credential-process-windows-amd64.exe",
        })
        runtime = json.loads((output / "macos" / "config.json").read_text())
        self.assertEqual(runtime, {"profiles": valid_config()["profiles"]})
        self.assertEqual((output / "macos" / "config.json").read_bytes(), (output / "windows" / "config.json").read_bytes())
        macos_install = (output / "macos" / "install.sh").read_text()
        windows_install = (output / "windows" / "install.ps1").read_text(encoding="utf-8-sig")
        self.assertIn('aws_region="eu-west-1"', macos_install)
        self.assertIn('profile_name="CCWB"', macos_install)
        self.assertIn("$awsRegion = 'eu-west-1'", windows_install)
        self.assertIn("$profileName = 'CCWB'", windows_install)
        self.assertTrue((output / "windows" / "install.ps1").read_bytes().startswith(codecs.BOM_UTF8))
        with (output / "macos" / "Claude-CCWB.mobileconfig").open("rb") as stream:
            policy = plistlib.load(stream)
        self.assertEqual(policy["PayloadContent"][0]["inferenceBedrockRegion"], "eu-west-1")
        self.assertEqual(policy["PayloadContent"][0]["inferenceBedrockProfile"], "CCWB")
        reg_text = (output / "windows" / "Claude-CCWB.reg").read_text(encoding="utf-16")
        self.assertIn('"inferenceBedrockRegion"="eu-west-1"', reg_text)
        self.assertIn('"inferenceBedrockProfile"="CCWB"', reg_text)

    def test_mcp_servers_are_embedded_in_mobileconfig_and_reg(self):
        server = {
            "name": "jarvis-registry",
            "transport": "http",
            "url": "https://jarvis.example.com/gateway/proxy/mcpgw/mcp",
        }
        config = valid_config()
        config["desktop"]["mcp_servers"] = [server]
        self.write_config(config)
        output = self.build()
        with (output / "macos" / "Claude-CCWB.mobileconfig").open("rb") as stream:
            policy = plistlib.load(stream)
        self.assertEqual(policy["PayloadContent"][0]["managedMcpServers"], json.dumps([server], separators=(",", ":")))
        reg_text = (output / "windows" / "Claude-CCWB.reg").read_text(encoding="utf-16")
        self.assertIn(
            '"managedMcpServers"="[{\\"name\\":\\"jarvis-registry\\",\\"transport\\":\\"http\\",'
            '\\"url\\":\\"https://jarvis.example.com/gateway/proxy/mcpgw/mcp\\"}]"',
            reg_text,
        )

    def test_managed_mcp_servers_omitted_without_configured_servers(self):
        for servers in (None, []):
            with self.subTest(servers=servers):
                config = valid_config()
                if servers is not None:
                    config["desktop"]["mcp_servers"] = servers
                self.write_config(config)
                output = self.build()
                with (output / "macos" / "Claude-CCWB.mobileconfig").open("rb") as stream:
                    policy = plistlib.load(stream)
                self.assertNotIn("managedMcpServers", policy["PayloadContent"][0])
                self.assertNotIn("managedMcpServers", (output / "windows" / "Claude-CCWB.reg").read_text(encoding="utf-16"))

    def test_mcp_servers_must_be_a_list(self):
        config = valid_config()
        config["desktop"]["mcp_servers"] = {"name": "jarvis-registry"}
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()

    def test_mcp_server_entry_with_unsupported_field_is_rejected(self):
        config = valid_config()
        config["desktop"]["mcp_servers"] = [{
            "name": "jarvis-registry",
            "transport": "http",
            "url": "https://jarvis.example.com/gateway/proxy/mcpgw/mcp",
            "command": "npx",
        }]
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()

    def test_mcp_server_with_non_http_transport_is_rejected(self):
        config = valid_config()
        config["desktop"]["mcp_servers"] = [{
            "name": "jarvis-registry",
            "transport": "stdio",
            "url": "https://jarvis.example.com/gateway/proxy/mcpgw/mcp",
        }]
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()

    def test_mcp_server_with_plain_http_url_is_rejected(self):
        config = valid_config()
        config["desktop"]["mcp_servers"] = [{
            "name": "jarvis-registry",
            "transport": "http",
            "url": "http://jarvis.example.com/gateway/proxy/mcpgw/mcp",
        }]
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()

    def test_mcp_server_with_example_url_is_rejected(self):
        config = valid_config()
        config["desktop"]["mcp_servers"] = [{
            "name": "jarvis-registry",
            "transport": "http",
            "url": "https://REPLACE_WITH_MCP_GATEWAY_URL/mcp",
        }]
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()

    def test_mcp_servers_with_duplicate_names_are_rejected(self):
        config = valid_config()
        config["desktop"]["mcp_servers"] = [
            {"name": "jarvis-registry", "transport": "http", "url": "https://jarvis.example.com/mcp"},
            {"name": "jarvis-registry", "transport": "http", "url": "https://other.example.com/mcp"},
        ]
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()

    def test_mcp_server_with_invalid_name_is_rejected(self):
        for name in ("", "jarvis registry", "jarvis/registry"):
            with self.subTest(name=name):
                config = valid_config()
                config["desktop"]["mcp_servers"] = [{
                    "name": name,
                    "transport": "http",
                    "url": "https://jarvis.example.com/gateway/proxy/mcpgw/mcp",
                }]
                self.write_config(config)
                with self.assertRaises(ValueError):
                    self.build()

    def test_invalid_input_preserves_previous_output(self):
        self.write_config()
        output = self.build()
        marker = output / "keep-me"
        marker.write_text("old")
        config = valid_config()
        config["profiles"]["CCWB"]["monitoring_enabled"] = True
        self.write_config(config)
        with self.assertRaises(ValueError):
            self.build()
        self.assertEqual(marker.read_text(), "old")

    def test_missing_binary_preserves_previous_output(self):
        self.write_config()
        output = self.build()
        marker = output / "keep-me"
        marker.write_text("old")
        (self.binaries / MODULE.BINARY_NAMES["windows"]).unlink()
        with self.assertRaises(FileNotFoundError):
            self.build()
        self.assertEqual(marker.read_text(), "old")

    def test_rebuild_replaces_stale_files(self):
        self.write_config()
        output = self.build()
        (output / "stale").write_text("old")
        self.build()
        self.assertFalse((output / "stale").exists())

    def test_multisegment_aws_region_is_accepted(self):
        self.write_config(valid_config("ap-southeast-2"))
        output = self.build()
        self.assertIn('aws_region="ap-southeast-2"', (output / "macos" / "install.sh").read_text())

    def test_publish_failure_restores_previous_bundle(self):
        self.write_config()
        output = self.build()
        marker = output / "keep-me"
        marker.write_text("old")
        original = Path.rename

        def fail_stage_rename(path, target):
            if path.name.startswith(".building-") and target == output:
                raise OSError("simulated publish failure")
            return original(path, target)

        with patch.object(Path, "rename", fail_stage_rename):
            with self.assertRaises(OSError):
                self.build()
        self.assertEqual(marker.read_text(), "old")

    def test_compiler_metadata_uses_go_source_revision(self):
        if not shutil.which("git"):
            self.skipTest("git is unavailable")
        repo = self.root / "go-source"
        repo.mkdir()
        subprocess.run(["git", "init", "-q", str(repo)], check=True)
        (repo / "go.mod").write_text("module example.com/test\n", encoding="utf-8")
        subprocess.run(["git", "add", "go.mod"], cwd=repo, check=True)
        subprocess.run(["git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "test"], cwd=repo, check=True)
        subprocess.run(["git", "tag", "v1.2.3"], cwd=repo, check=True)
        version, commit = MODULE.source_build_metadata(repo)
        self.assertEqual(version, "v1.2.3")
        self.assertRegex(commit, r"^[0-9a-f]{7,}$")

    def test_command_uses_deployment_src_templates(self):
        self.write_config()
        exit_code = MODULE.main([
            "--config", str(self.config), "--binaries", str(self.binaries),
            "--dist", str(self.dist),
        ])
        self.assertEqual(exit_code, 0)
        self.assertEqual({p.name for p in self.dist.iterdir()}, {"macos", "windows"})

    def test_command_uses_local_go_source_by_default(self):
        self.write_config()

        def fake_compile(go_source, binary_dir):
            self.assertEqual(Path(go_source), ROOT / "source" / "go")
            for name in MODULE.BINARY_NAMES.values():
                (Path(binary_dir) / name).write_bytes(b"test binary")

        with patch.object(MODULE, "compile_binaries", side_effect=fake_compile):
            exit_code = MODULE.main(["--config", str(self.config), "--dist", str(self.dist)])
        self.assertEqual(exit_code, 0)
        self.assertEqual({p.name for p in self.dist.iterdir()}, {"macos", "windows"})


if __name__ == "__main__":
    unittest.main()

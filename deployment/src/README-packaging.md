# ASCENDING Employee Bundle Packaging Guide

`config.example/config.json.example` is a redacted template. The real `config/config.json` must come through an organization-approved private channel and is ignored by Git. Do not put tokens, access keys, or personal employee credentials in it. The configuration allows one `ASCENDING-CCWB` IAM Identity Center profile and two AIP models, Sonnet and Haiku. Quota and monitoring features are not supported.

The optional `desktop.mcp_servers` list configures remote HTTP MCP servers. Each entry needs `name`, `transport: http`, and an HTTPS `url`. Packaging serializes the list as a JSON string in the `managedMcpServers` key of both the `.mobileconfig` and `.reg` policies.

If `jarvis-registry` is configured, employees must also open **Customize → Connectors**, select `jarvis-registry`, click **Connect**, and complete browser authorization after installing the policy and restarting Claude Desktop. Deploying the policy does not complete OAuth authorization. This connection flow was verified on the current Mac on October 16, 2026, but has not been verified on an employee Windows computer. Both employee guides include the steps.

From the repository root, run:

```bash
python3 scripts/build_employee_dist.py
```

The default configuration path is `deployment/src/config/config.json`; use `deployment/src/config.example/config.json.example` as a starting point. The default Go source is this repository's `source/go`, so a neighboring reference repository is not needed. The build machine needs Python 3.9+ and Go 1.24+. The first build may download Go module dependencies. Building the macOS Intel binary from source also requires a local C toolchain that supports that architecture.

The packager builds only `credential-process` for macOS arm64, macOS amd64, and Windows amd64 from the selected Go source; it does not build monitoring helper programs. To use binaries built through another trusted process, pass `--binaries <directory>`. That directory must contain `credential-process-darwin-arm64`, `credential-process-darwin-amd64`, and `credential-process-windows-amd64.exe`. Use `--go-source <directory>` to override the Go source, `--config <file>` to read a different private configuration, or `--dist <directory>` to change the output directory.

Successful packaging creates `macos/` and `windows/` directly under `dist/`. Each directory contains an installer, an English guide, an IDC-only runtime configuration, and the corresponding Claude Desktop policy. macOS has binaries for both supported architectures; Windows has one. The packager fills in the Bedrock Region in the installer templates and generates the `.mobileconfig` and `.reg` policies from the configuration. The Windows `.reg` file uses UTF-16 encoding for Registry Editor 5.00.

Packaging prepares the complete output in a temporary directory before replacing an older bundle. If input validation or a build fails, the old bundle remains unchanged; after a successful replacement, the old bundle is removed. The output contains organization account, IDC entry point, and model configuration. Distribute it to employees only through a private channel; do not add it to Git. This process does not create cloud resources, sign in on behalf of employees, or install system policies. Installation on an employee Windows computer has not yet been verified.

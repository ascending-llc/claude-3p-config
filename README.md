# Claude on Amazon Bedrock, configuration only

This repository sets up **Claude Desktop** and the **Claude Code CLI** to use Claude models on Amazon Bedrock in your own AWS account. People sign in through **AWS IAM Identity Center**, which can federate to Entra ID or another identity provider. Nothing is installed except configuration:

| Piece | What it does |
| --- | --- |
| Claude Desktop policy | Uses Desktop's built-in IAM Identity Center sign-in, calls Bedrock through your application inference profiles (AIPs), and adds managed MCP connectors |
| Claude Code policy | Points Claude Code at Bedrock through the `CCWB` AWS CLI profile, routes every model alias to your AIPs, and runs `aws sso login` when credentials expire |
| AWS CLI profile | The `[profile CCWB]` and `[sso-session ccwb]` sections that Claude Code signs in with |

The generator `ccwb-policy` renders all three from one `config/config.json`:

| Platform | Output | Status |
| --- | --- | --- |
| macOS | `dist/macos/Claude-CCWB.mobileconfig`, `dist/macos/aws-config-CCWB.ini` | Tested end to end |
| Windows | `dist/windows/Claude-CCWB.reg`, `dist/windows/aws-config-CCWB.ini` | Tested end to end |

If you'd rather run a central gateway than configure each machine, the Claude apps gateway is the alternative. It isn't covered here.

## Quick start

You need Go 1.24 or later.

```bash
cp config/config.example.json config/config.json   # then fill in every REPLACE_WITH_ value
make policies                                       # or: go run ./cmd/ccwb-policy
```

Then follow the guide for your platform:

- [Admin guide](docs/admin-guide.md): AWS prerequisites, the config fields, generating and distributing the policies
- [macOS user guide](docs/macos-user-guide.md): install and sign in on a Mac
- [Windows user guide](docs/windows-user-guide.md): install and sign in on Windows
- [Test plan](docs/test-plan.md): what was tested, and the checklist for new platforms or changes
- [Jarvis Registry static OAuth client](docs/jarvis-static-oauth-client.md): what the Jarvis Registry MCP server needs for Claude Desktop sign-in
- [Jarvis Registry static OAuth client](docs/jarvis-static-oauth-client.md): what the Jarvis Registry MCP server needs for Claude Desktop sign-in

Without Go, copy the committed examples in [`examples/`](examples/) and replace each `REPLACE_WITH_` value by hand. The examples are rendered from [`config/config.example.json`](config/config.example.json).

`config/config.json` and `dist/` are git-ignored. They hold your account ID, AIP ARNs and sign-in URL, which aren't secrets but are internal, so share them through a private channel.

## Repository layout

```
cmd/ccwb-policy/      command-line entry point
policy/               config validation and rendering of every artifact
internal/plist/       XML property list encoder for the .mobileconfig
config/               config.example.json (committed) and config.json (yours, ignored)
examples/             artifacts rendered from config.example.json
docs/                 admin guide, user guides, test plan, Jarvis Registry OAuth brief
```

## Development

You also need Python 3.14 for the pre-commit hooks (see `.python-version`). Run `pre-commit install -t pre-commit` once, then `make all` (tests and golangci-lint) before each commit. After changing the config schema or rendering, run `make examples` and commit the updated `examples/`; a test fails while they're stale. See [CLAUDE.md](CLAUDE.md) for conventions.

Upstream inspiration: [guidance-for-claude-code-with-amazon-bedrock](https://github.com/aws-solutions-library-samples/guidance-for-claude-code-with-amazon-bedrock).

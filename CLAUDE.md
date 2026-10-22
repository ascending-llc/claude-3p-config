# claude-3p-config

Generates the managed policies that point Claude Desktop and the Claude Code CLI at Amazon Bedrock, signed in through AWS IAM Identity Center. Configuration only: no binaries are shipped. `cmd/ccwb-policy` renders `config/config.json` into a macOS `.mobileconfig`, a Windows `.reg` and an `~/.aws/config` snippet. See README.md for the user-facing overview.

## Keep these in sync

A change to the config schema or to any rendered key isn't done until all of these match:

- `config/config.example.json`, then `make examples` to re-render `examples/` (`TestExamplesAreCurrent` fails while they're stale)
- `policy/testdata/valid.json` and the golden files: `go test ./policy -update`, then review the golden diff before committing
- `docs/admin-guide.md` (field table and "What the policies set"), the user guides, and `docs/test-plan.md` if behavior people check changes

`ProfileName` (`CCWB`) and `SSOSessionName` (`ccwb`) are shared by the Claude Code policy and the AWS snippet; never change one without the other.

## Facts the design depends on

Verified on macOS on 2026-10-21. Windows is generated but untested, so say so wherever Windows behavior is described until it has been tested.

- Claude Desktop reads the `com.anthropic.claudefordesktop` payload, Claude Code the `com.anthropic.claudecode` payload; both work in one per-user (`PayloadScope` `User`) profile.
- Claude Code's `availableModels` restricts aliases, not raw model IDs: `--model us.anthropic.…` still reaches Bedrock. Don't describe the allowlist as a security boundary; IAM or an SCP is.
- `modelOverrides` keys must be Anthropic model IDs; without them Claude Code doesn't recognize an AIP ARN.
- Claude Desktop runs managed-MCP OAuth sign-in on the device. Its automatic client registration always listens on `127.0.0.1:53280`, inside the Windows dynamic port range that WSL2 and Hyper-V reserve from, so the policy always sets a pre-registered `clientId` and a `callbackPort` below 49152. Desktop rejects `callbackPort` without `clientId`, drops any `managedMcpServers` entry it can't parse, and logs why in `main.log`.
- On Windows both policies go under `HKLM`: Claude Code ignores some managed-only keys in `HKCU`, and Desktop ignores user policy when machine policy exists.
- When unsure about a Claude Desktop or Claude Code setting, check the current docs (code.claude.com, claude.com/docs/third-party) instead of guessing; both products change quickly.

## Go conventions

- Standard library only. Adding a third-party module needs a strong reason.
- No package folders that exist purely for organization. `policy` owns validation and rendering; `internal/plist` is internal because it's an implementation detail of the `.mobileconfig` output.
- Exported names read naturally with their package qualifier, no stutter (`policy.Config`, not `policy.PolicyConfig`).
- Interfaces are defined at the consumer, only when there's a real need for them.
- Declarations follow `decorder`: one `type (...)` block, then `const`, then `var`, then funcs.
- All new or changed exported identifiers need godoc comments.

### Errors

- Use `fmt.Errorf("...: %s", err.Error())` for context-only wrapping. Use `%w` only with an exported `Err*` sentinel that callers would branch on.
- Validation errors are prefixed with the JSON field path (`models[0].aip_arn: ...`) and all of them are reported at once. Keep that when adding fields.

### Testing

- Rendering is pure input → output: cover it with golden files under `policy/testdata/golden` plus targeted assertions, such as `plutil -lint` on macOS and decoding the `.reg`.
- Validation is table-driven: mutate a `Config` loaded from `testdata/valid.json`, and assert on the path-qualified message.
- The command is tested end to end through `run` with a temp output directory.
- Prefer `testdata/` fixtures over inline literals. Don't add production seams only to reach rare error branches.

## Tooling

- Run `pre-commit install -t pre-commit` once. Use the `Makefile` targets: `make all` (test + lint) is the bar for done; `make fmt` fixes formatting and import grouping.
- pre-commit builds its Python hooks (tartufo) with `python3.14`, matching `.python-version`. tartufo scans staged changes for secrets; add a false positive to `tartufo.toml` with a `reason`, never by disabling the hook.
- CI (`.github/workflows/test-and-lint.yaml`) runs `make test`, `make lint` and `make tartufo` on Linux, and `make test` on macOS for the `plutil -lint` check.
- golangci-lint must pass. Suppress a false positive in `.golangci.yaml` under `linters.exclusions.rules`, scoped to `path` and `text`, with a comment explaining why; never a blanket `//nolint` or a disabled linter.
- `config/config.json` and `dist/` hold real account details and are git-ignored; never commit them or paste their contents into examples or tests.
- `.reg` files are UTF-16LE with CRLF line endings and marked `binary` in `.gitattributes`; read them with `iconv -f UTF-16 -t UTF-8`.

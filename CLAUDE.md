# claude-3p-config

Generates the managed policies that point Claude Desktop and the Claude Code CLI at Amazon Bedrock, signed in through AWS IAM Identity Center, plus the Codex `requirements.toml` that does the same through Bedrock Runtime when `codex` is selected. Products are selected on the command line with `claude`, `codex` or both. Configuration only: no binaries are shipped. `cmd/ccwb-policy` renders `config/config.json` into macOS `.mobileconfig` profiles, a Windows `.reg`, a Codex `requirements.toml` (also embedded in a macOS profile), and an `~/.aws/config` snippet. See README.md for the user-facing overview.

## Keep these in sync

A change to the config schema or to any rendered key isn't done until all of these match:

- `config/config.example.json`, then `make examples` to re-render `examples/` (`TestExamplesAreCurrent` fails while they're stale)
- `policy/testdata/valid.json`, `cmd/ccwb-policy/testdata/config.json` and the golden files: `go test ./policy -update`, then review the golden diff before committing
- `docs/admin-guide.md` (field table and "What the policies set"), the user guides, and `docs/test-plan.md` if behavior people check changes

`ProfileName` (`CCWB`) and `SSOSessionName` (`ccwb`) are shared by the Claude Code policy, the Codex requirements and the AWS snippet; never change one without the other.

## Facts the design depends on

Verified on macOS on 2026-10-21 and on Windows 10 on 2026-10-22. Note in `docs/test-plan.md` when a change to the schema or the rendered keys hasn't been re-tested on a platform yet.

- Claude Desktop reads the `com.anthropic.claudefordesktop` payload, Claude Code the `com.anthropic.claudecode` payload; both work in one per-user (`PayloadScope` `User`) profile.
- Claude Code's `availableModels` restricts aliases, not raw model IDs: `--model us.anthropic.…` still reaches Bedrock. Don't describe the allowlist as a security boundary; IAM or an SCP is.
- `modelOverrides` keys must be Anthropic model IDs; without them Claude Code doesn't recognize an AIP ARN.
- Claude Desktop runs managed-MCP OAuth sign-in on the device. Its automatic client registration always listens on `127.0.0.1:53280`, inside the Windows dynamic port range that WSL2 and Hyper-V reserve from, so the policy always sets the Jarvis static `clientId` `jarvis-registry-claude-desktop`, scope `mcp-proxy-ops` and `callbackPort` 47280. Every managed connector must be a Jarvis Registry MCP endpoint. Desktop rejects `callbackPort` without `clientId`, drops any `managedMcpServers` entry it can't parse, and logs why in `main.log`.
- On Windows both policies go under `HKLM`: Claude Code ignores some managed-only keys in `HKCU`, and Desktop ignores user policy when machine policy exists.
- Codex (0.166.0) reads admin-enforced `requirements.toml` from `/etc/codex/requirements.toml` or MDM `com.openai.codex:requirements_toml_base64` on macOS, and from `%ProgramData%\OpenAI\Codex\requirements.toml` on Windows. It ignores `CODEX_HOME\managed_config.toml` on Windows (verified 2026-11-02), so the generator writes no `managed_config.toml` at all.
- Codex's `aws.auth_refresh` accepts only `command = "aws"` in 0.165.0-alpha.1.2 and re-runs the command when AWS credentials expire. It runs the command without a shell, so `aws` must be on the `PATH` Codex itself sees. The macOS ChatGPT app (26.1027, verified 2026-11-02) gets that `PATH` by running `$SHELL -ilc` at launch, falling back to `/usr/bin:/bin:/usr/sbin:/sbin` if the shell fails or times out. The CLI talks to a long-running `codex app-server` daemon that must be restarted (`codex app-server daemon restart`) after the profile is installed or changed, or it reports `modelProviderPolicyConflict`. The generator sets it to `aws sso login --profile CCWB` with a five-minute timeout for the browser sign-in.
- Codex's `requirements.toml` accepts `model_provider` and `model_providers`; a required provider entry replaces the user's and is validated like a `config.toml` Bedrock override. So the provider, profile, Region and `auth_refresh` go in the requirements. The defaults users may change (model, effort) go in its `[models.new_thread]` table, which the CLI and the ChatGPT app apply to new threads without enforcing them (verified on Windows 2026-11-02).
- Codex has no supported model allowlist key. IAM/SCP is the model boundary — don't describe the Codex config as a whitelist.
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
- Validation errors are prefixed with the JSON field path (`claude.models.sonnet.aip_arn: ...`) and all of them are reported at once. Keep that when adding fields.

### Testing

Follow the mainland China and US testing split in `docs/test-plan.md` before any client testing. The implementer runs offline checks, and US testers handle sign-in and inference.

- Rendering is pure input → output: cover it with golden files under `policy/testdata/golden` plus targeted assertions, such as `plutil -lint` on macOS and decoding the `.reg`.
- Validation is table-driven: mutate a `Config` loaded from `testdata/valid.json`, and assert on the path-qualified message.
- The command is tested end to end through `run` with a temp output directory.
- Prefer `testdata/` fixtures over inline literals. Don't add production seams only to reach rare error branches.

## Tooling

- Run `pre-commit install -t pre-commit` once. Use the `Makefile` targets: `make all` (test + lint) is the bar for done; `make fmt` fixes formatting and import grouping.
- The gelato pre-commit hook scans staged changes for secrets; `make gelato` runs it by hand. Its config is the `[tool.tartufo]` table of `tartufo.toml`, the file and table names gelato reads. Add a false positive there with a `reason` (`exclude-entropy-patterns` for generated content that changes, `exclude-signatures` for a single finding), never by disabling the hook. CI doesn't scan for secrets; the local hook is the check.
- CI (`.github/workflows/test-and-lint.yaml`) runs `make test` and `make lint` on Linux, with pre-commit on Python from `.python-version`, and `make test` on macOS for the `plutil -lint` check.
- golangci-lint must pass. Suppress a false positive in `.golangci.yaml` under `linters.exclusions.rules`, scoped to `path` and `text`, with a comment explaining why; never a blanket `//nolint` or a disabled linter.
- `config/config.json` and `dist/` hold real account details and are git-ignored; never commit them or paste their contents into examples or tests.
- `.reg` files are UTF-16LE with CRLF line endings and marked `binary` in `.gitattributes`; read them with `iconv -f UTF-16 -t UTF-8`.

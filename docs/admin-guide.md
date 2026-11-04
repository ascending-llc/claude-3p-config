# Admin guide

This guide is for whoever prepares the configuration: AWS setup, `config/config.json`, generating the policies and handing them to people or to IT.

## 1. AWS prerequisites

- **IAM Identity Center** in your organization, with the identity provider you use (such as Entra ID) connected.
- **A permission set** assigned to the people who'll use Claude, in the account that holds the AIPs. Both Claude Desktop and Claude Code sign in with it.
- **An application inference profile (AIP) per model tier** in that account and in your Bedrock region: one for a Sonnet model, one for a Haiku model and, optionally, one for an Opus model. All Claude traffic goes through them, so cost is tracked per AIP. Bedrock doesn't attribute cost per person; per-person activity shows up as the caller's role session (`…/<user email>`) in CloudTrail or Bedrock invocation logs.

### Permissions, and why IAM is the real model boundary

Give the permission set what the [Claude Code Bedrock docs](https://code.claude.com/docs/en/amazon-bedrock#iam-configuration) list (`bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream`, `bedrock:ListInferenceProfiles`, `bedrock:GetInferenceProfile`), narrowed to your AIPs and the foundation models they route to.

The generated Claude Code policy limits the `/model` picker to the tiers you configured, but that allowlist only covers model aliases. Testing showed that `claude --model us.anthropic.claude-sonnet-5` still sends the request to Bedrock. Only IAM or a service control policy (SCP) can stop people from calling models outside your AIPs, so deny direct model access there. In the tested organization, an SCP blocked those requests even for an administrator role.

## 2. Fill in the config

```bash
cp config/config.example.json config/config.json
```

| Field | Value |
| --- | --- |
| `aws.account_id` | The 12-digit account that holds the AIPs |
| `aws.bedrock_region` | The region the AIPs are in |
| `aws.sso_start_url` | Your access portal URL, such as `https://example.awsapps.com/start` |
| `aws.sso_region` | The IAM Identity Center region |
| `aws.sso_role_name` | The permission set name |
| `models[].tier` | `opus`, `sonnet` or `haiku`; one model per tier. Sonnet and Haiku are required, Opus is optional |
| `models[].aip_arn` | The AIP ARN; must match `account_id` and `bedrock_region` |
| `models[].anthropic_model_id` | The Anthropic model ID the AIP routes to (see below) |
| `models[].label` | Optional name Claude Desktop shows; defaults to the capitalized tier, such as `Sonnet` |
| `default_tier` | Optional tier both apps start with; defaults to `sonnet`. Must be a configured tier |
| `codex.enabled` | Optional. Set to `true` to render Codex managed configuration; omitted or `false` keeps the existing Claude-only output |
| `codex.default_model` | A Global (`global.openai.…`) or US Geo (`us.openai.…`) Bedrock Runtime inference profile ID, such as `global.openai.gpt-6.1-terra`. The generator checks only the form: check the model's AWS model card that the profile can be called from `aws.bedrock_region` (US Geo profiles also work from the Canadian Regions), and grant it in IAM |
| `codex.reasoning_effort` | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra` or `persistent`. Each model supports only some of these; check the model card |
| `codex.mcp_servers[]` | Optional remote MCP connections, independent of Claude Desktop. Each entry has `name` and `url`; omit the list or use an empty array to generate no MCP configuration |
| `codex.mcp_servers[].name` | Server name: letters, digits, `-` and `_`, starting with a letter or digit. Names must be unique within the Codex list |
| `codex.mcp_servers[].url` | HTTPS MCP endpoint. Claude Desktop's `oauth` configuration is not accepted; Codex handles sign-in separately |
| `desktop.mcp_servers[]` | Optional remote MCP servers: `name`, `url` (final https address) and, for a server that needs sign-in, `oauth` |
| `desktop.mcp_servers[].oauth.client_id` | The public OAuth client registered for Claude Desktop at the server's authorization server (see below) |
| `desktop.mcp_servers[].oauth.scope` | Optional space-separated scopes Desktop requests. Without it, Desktop requests every scope the server advertises |
| `desktop.mcp_servers[].oauth.callback_port` | Optional loopback port Desktop receives the sign-in on; defaults to `47280`. Must be between 1024 and 49151 |
| `profile.identifier` | Reverse-DNS identifier. **Keep it stable**: a profile with the same identifier replaces the installed one |
| `profile.display_name`, `profile.organization` | Shown in System Settings |

To find the AIPs and the models behind them:

```bash
aws bedrock list-inference-profiles --type-equals APPLICATION --region <bedrock_region>
aws bedrock get-inference-profile --inference-profile-identifier <aip_arn> --query 'models[].modelArn'
```

Turn the foundation model ID into `anthropic_model_id` by dropping the `anthropic.` prefix and any `-v1:0` suffix. For example, `anthropic.claude-sonnet-5` becomes `claude-sonnet-5`, `anthropic.claude-opus-5-5` becomes `claude-opus-5-5`, and `anthropic.claude-haiku-4-5-20251001-v1:0` becomes `claude-haiku-4-5-20251001`. Claude Code needs this mapping (`modelOverrides`) to recognize the AIP: without it the `/model` picker shows the raw ARN, features may be detected wrongly, and Sonnet 5 loses its 1M-token context window.

### Opus and the default model

Without an Opus model, the `opus` alias isn't allowed in Claude Code and Claude Desktop doesn't offer Opus. If you don't provision an Opus AIP, delete the `opus` entry from the example config. Opus costs more per token than Sonnet, and on Bedrock every token is billed, so set `default_tier` to `opus` only as a deliberate choice. The Opus tier and `default_tier` haven't been tested end to end yet; see the [test plan](test-plan.md).

### MCP connectors with OAuth

Claude Desktop signs each person in to an OAuth connector itself. It opens the browser and receives the result on a temporary listener at `http://127.0.0.1:<callback_port>/callback`. The policy always gives Desktop a client registered in advance and a callback port of 47280 by default, because Desktop's own automatic client registration doesn't work reliably on Windows:

- Automatic registration always listens on port 53280. On Windows that port is inside the dynamic port range (49152–65535 by default), from which WSL2, Docker Desktop and Hyper-V reserve blocks of ports. When a block covers 53280, **Connect** shows only a brief "connection failed" message, and Desktop's `main.log` records `listen EACCES: permission denied 127.0.0.1:53280`. The blocks move after each reboot, so the same machine can work one day and fail the next.
- Desktop accepts a different port only together with a client ID.

Port 47280 is below the dynamic range, so Windows never reserves it, and it's in a block IANA hasn't assigned. The generator rejects ports outside 1024–49151.

For each OAuth server, register **one** client at its authorization server for every device to share:

- A public client with no secret: `token_endpoint_auth_method` `none`, protected by PKCE (S256). A secret in a policy file would be readable on every device, so Desktop doesn't need one.
- Redirect URI `http://127.0.0.1:47280/callback`, or your `callback_port`.
- Grant types `authorization_code` and `refresh_token`, so Desktop can renew the sign-in in the background.

If the server supports dynamic client registration (its OAuth metadata lists a `registration_endpoint`), one request registers the client and returns its `client_id`:

```bash
curl -sS -X POST <registration_endpoint> -H 'Content-Type: application/json' -d '{
  "client_name": "Claude Desktop",
  "redirect_uris": ["http://127.0.0.1:47280/callback"],
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"],
  "token_endpoint_auth_method": "none"}'
```

Confirm with the server's operators that the client is kept permanently. Some servers delete dynamically registered clients after a while or on redeploy, and every device's connector then fails until you register a new client and redeploy the policy. For the Jarvis Registry, a static client built into the server is planned; see [Jarvis Registry: a static OAuth client for Claude Desktop](jarvis-static-oauth-client.md).

Claude Desktop doesn't follow HTTP redirects from the MCP URL or its OAuth metadata, so use final addresses.

### Codex MCP connections

Add a separate `mcp_servers` field inside the `codex` object, for example:

```json
{
  "mcp_servers": [
    {
      "name": "jarvis-registry",
      "url": "https://jarvis.ascendingdc.com/gateway/proxy/mcpgw/mcp"
    }
  ]
}
```

For each server, the generator writes `[mcp_servers.<name>]`, `url` and `enabled = true`. This configures the connection only; each user must still sign in through Codex when the server requires OAuth. Claude Desktop's static OAuth client ID, scopes and callback port are not copied to Codex.

Connection configuration and allowlists serve different purposes: `config.toml` defines connections, while `mcp_servers` in `requirements.toml` restricts which configured connections may be enabled. An allowlist does not create connections. This generator does not add an MCP allowlist automatically. If your organization applies one separately, it must allow both the server name and URL or the connection will be disabled. See the [OpenAI MCP documentation](https://learn.chatgpt.com/docs/extend/mcp) and [managed configuration guide](https://learn.chatgpt.com/docs/enterprise/managed-configuration).

## 3. Generate

```bash
make policies        # or: go run ./cmd/ccwb-policy -config config/config.json -out dist
```

The command validates every field first and lists all problems, each with its field path. It writes:

```
dist/macos/Claude-CCWB.mobileconfig    one profile, two payloads: Claude Desktop and Claude Code
dist/macos/aws-config-CCWB.ini         ~/.aws/config snippet
dist/windows/Claude-CCWB.reg           both policies under HKLM
dist/windows/aws-config-CCWB.ini       same snippet, for %USERPROFILE%\.aws\config

dist/macos/Codex-CCWB.mobileconfig     Codex requirements and optional MCP connections
dist/macos/codex-requirements-CCWB.toml
dist/windows/codex-requirements-CCWB.toml

dist/macos/codex-config-CCWB.toml       only generated when Codex MCP is configured
dist/windows/codex-config-CCWB.toml     the same MCP configuration snippet
```

On a Mac, run `plutil -lint` for both `.mobileconfig` files; each should print `OK`.

## 4. Distribute

The repository doesn't distribute anything. Pick what fits:

- **Self-install.** Send the `dist/macos` or `dist/windows` folder through a private channel and point people to the [macOS](macos-user-guide.md) or [Windows](windows-user-guide.md) user guide.
- **MDM on macOS** (Jamf, Kandji, Intune and similar). Upload `Claude-CCWB.mobileconfig` as a custom profile. Both payloads were tested installed per user (`PayloadScope` `User`), so deploy it through the user channel. MDM tools usually sign profiles; a self-installed unsigned profile shows as "Unverified", which is expected. The AWS snippet still has to reach each person's `~/.aws/config`, through a script or the user guide.
- **Windows management** (Intune, Group Policy). Import `Claude-CCWB.reg` or set the same values. They live under `HKEY_LOCAL_MACHINE`, so this needs administrator rights. Claude Code ignores some managed-only keys in the per-user `HKCU` hive, and Claude Desktop ignores user policy whenever machine policy exists.
- **Codex on macOS.** Deploy `Codex-CCWB.mobileconfig` through the user channel. It retains `requirements_toml_base64` under `com.openai.codex` and adds `config_toml_base64` when MCP is configured, so no user file changes are needed. For file-based deployment, install the requirements as `/etc/codex/requirements.toml` and merge the optional `codex-config-CCWB.toml` into each user's `~/.codex/config.toml`.
- **Codex on Windows.** Deploy the requirements as `%ProgramData%\OpenAI\Codex\requirements.toml`, writable only by administrators. Merge the optional MCP snippet into the target user's `%USERPROFILE%\.codex\config.toml`, or into their custom `CODEX_HOME`. The generator only writes the snippet; deployment tooling or the user must merge it. Renaming it to `managed_config.toml` does not make it load automatically on Windows.

Back up the configuration before merging the MCP snippet. Merge by server name: append new tables, or update `url` and `enabled` in existing HTTP tables while preserving other settings. Do not duplicate table headers or replace the whole user file. Repeated deployment should produce the same result. If `CODEX_HOME` is customized, target its `config.toml`. Removing a server later also requires removing its previously merged table. The generator does not clean old artifacts; generate into a clean output directory before distribution.

When converting an existing stdio table to HTTP, first remove stdio-specific fields such as `command`, `args`, `env`, `env_vars` and `cwd`, then add `url`. Preserve applicable general settings such as timeouts and tool restrictions, and leave other servers intact. Do not keep both `command` and `url`: the tested client rejects this with `url is not supported for stdio` and fails to load the configuration.

To change models or settings later, update the config, regenerate and reinstall. On macOS the new profile replaces the old one as long as `profile.identifier` hasn't changed.

## 5. What the policies set

**Claude Desktop** (`com.anthropic.claudefordesktop` payload, or `HKLM\SOFTWARE\Policies\Claude`):

| Key | Value |
| --- | --- |
| `inferenceProvider` | `bedrock` |
| `inferenceCredentialKind` | `interactive`: Desktop's own IAM Identity Center sign-in |
| `inferenceBedrockRegion`, `inferenceBedrockSso*` | From `aws.*` |
| `inferenceModels` | JSON list of the AIPs with labels and tiers. The `default_tier` model comes first, which makes it Desktop's default |
| `managedMcpServers` | JSON list of `desktop.mcp_servers`; omitted when empty. Each `oauth` becomes Desktop's `clientId`, `scope` and `callbackPort`, with the port always set |

**Claude Code** (`com.anthropic.claudecode` payload, or the JSON `Settings` value under `HKLM\SOFTWARE\Policies\ClaudeCode`):

| Key | Value |
| --- | --- |
| `env` | `CLAUDE_CODE_USE_BEDROCK=1`, `AWS_PROFILE=CCWB`, `AWS_REGION`, `ANTHROPIC_MODEL` set to the `default_tier` AIP, and `ANTHROPIC_DEFAULT_<TIER>_MODEL` set to each configured tier's AIP |
| `awsAuthRefresh` | `aws sso login --profile CCWB`, run only after credentials have expired |
| `modelOverrides` | Each `anthropic_model_id` mapped to its AIP |
| `availableModels`, `enforceAvailableModels` | Only the configured tiers' aliases, including for the Default option. Without this, Claude Code's Bedrock default is Opus through a public inference profile, which bypasses your AIPs |

Because these are managed settings, they apply to **every** Claude Code session for that user on that machine, and users can't override them.

**Codex** still gets its Bedrock policy through `requirements.toml`:

- **Enforced; users can't override it:** it pins the built-in `amazon-bedrock-runtime` provider and replaces any user definition of it with the `CCWB` profile, the Bedrock Region, and an `auth_refresh` command that re-runs `aws sso login --profile CCWB` when credentials expire, the Codex counterpart of Claude Code's `awsAuthRefresh`. It also allows web search only as `disabled`, since hosted web search isn't available on Bedrock Runtime.
- **Starting values; users can change them:** `[models.new_thread]` sets `codex.default_model` and `codex.reasoning_effort`. The CLI and the ChatGPT app apply them to each new thread unless the user picks another model.

When MCP is configured, the generator also writes `codex-config-CCWB.toml`. The macOS profile embeds the requirements and MCP configuration under separate Base64 keys; Windows and file-based deployments must merge the MCP snippet. MCP input does not change the Bedrock provider, default model, AWS profile or Region.

The generator does not write `managed_config.toml`: the Windows client tested for this project ignores that file. On macOS, `config_toml_base64` provisions MCP connections; the default model and reasoning effort remain in `[models.new_thread]` in the requirements.

Codex runs `auth_refresh` without a shell and only accepts the bare command `aws`, so the AWS CLI must be on the `PATH` that Codex itself sees:

- **CLI and IDE extension:** the `PATH` of the terminal or editor that started them.
- **ChatGPT app on macOS:** at launch the app runs the user's login shell as `$SHELL -ilc` (interactive and login), copies its environment, and passes it to its bundled Codex. So `aws` must be on the `PATH` after the shell's startup files run: for zsh `~/.zshenv`, `~/.zprofile`, `~/.zshrc` and `~/.zlogin`; for bash only the first of `~/.bash_profile`, `~/.bash_login` and `~/.profile`, which doesn't read `~/.bashrc` unless it sources it. If that shell exits with an error or takes longer than about 10 seconds, the app falls back to its own `PATH` (`/usr/bin:/bin:/usr/sbin:/sbin`), which doesn't include `/usr/local/bin` or `/opt/homebrew/bin`. Check with `$SHELL -ilc 'command -v aws'` and test C10 in the [test plan](test-plan.md). This is the app's behavior, observed in version 26.1027, not something Codex documents.

After installing or changing the Codex profile, quit the ChatGPT app and run `codex app-server daemon restart`. The CLI connects to a long-running background Codex server that keeps the settings it started with, and fails with `modelProviderPolicyConflict` (provider `openai`) when those predate the profile.

Codex has no model allowlist requirement, so IAM/SCP must restrict actual model access. See section 4 for the macOS and Windows deployment paths.

## 6. Sign-in lifetime

Claude Desktop keeps its own IAM Identity Center session; Claude Code uses the AWS CLI's cache in `~/.aws/sso/cache`. Signing out of one doesn't sign out the other. How often people sign in again depends on the IAM Identity Center session duration and the permission set's session duration; check both before rollout and mention the result in your internal docs.

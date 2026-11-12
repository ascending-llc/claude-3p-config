# Admin guide

This guide is for whoever prepares the configuration: AWS setup, `config/config.json`, generating the policies and handing them to people or to IT.

## 1. AWS prerequisites

- **IAM Identity Center** in your organization, with the identity provider you use (such as Entra ID) connected.
- **A permission set** assigned to the people who'll use the selected products, in the AWS account that runs their Bedrock requests. Claude Desktop, Claude Code and Codex use this account and permission set.
- **For Claude, an application inference profile (AIP) per model tier** in that account and in your Bedrock region. Provision one for Sonnet, one for Haiku and, optionally, one for Opus. All Claude traffic goes through them, so cost is tracked per AIP. Per-person activity shows up as the caller's role session (`…/<user email>`) in CloudTrail or Bedrock invocation logs.
- **For Codex, a Global or US Geo system inference profile** (`global.openai.…` or `us.openai.…`) permitted in IAM. The Bedrock Responses API used by Codex rejects application inference profiles with a 400 response, so Codex requires a system profile and its spend cannot be tagged per AIP. Use invocation logs or CloudTrail to attribute activity by role session, or separate accounts or permission sets to separate usage.

### Permissions, and why IAM is the real model boundary

Give the permission set what the [Claude Code Bedrock docs](https://code.claude.com/docs/en/amazon-bedrock#iam-configuration) list (`bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream`, `bedrock:ListInferenceProfiles`, `bedrock:GetInferenceProfile`), narrowed to your AIPs and the foundation models they route to.

The generated Claude Code policy limits the `/model` picker to the tiers you configured, but that allowlist only covers model aliases. Testing showed that `claude --model us.anthropic.claude-sonnet-5` still sends the request to Bedrock. Only IAM or a service control policy (SCP) can stop people from calling models outside your AIPs, so deny direct model access there. In the tested organization, an SCP blocked those requests even for an administrator role.

## 2. Fill in the config

```bash
cp config/config.example.json config/config.json
```

Choose products on the command line. Shared fields are always required and validated. A product's section is required and validated only when that product is selected. Unselected sections may be omitted. The entire file is decoded strictly, so an unknown field or a field with the wrong JSON type fails even in an unselected section.

Existing deployment configs must be rewritten by hand using the new example.

### Shared fields

| Field | Value |
| --- | --- |
| `aws.account_id` | The 12-digit AWS account used by the selected products |
| `aws.bedrock_region` | The region used for Bedrock requests and Claude AIPs |
| `aws.sso_start_url` | Your access portal URL, such as `https://example.awsapps.com/start` |
| `aws.sso_region` | The IAM Identity Center region |
| `aws.sso_role_name` | The permission set name |
| `macos_profile.identifier_prefix` | Reverse-DNS prefix, such as `com.example.ccwb`. Keep it stable so updates replace the installed profiles |
| `macos_profile.organization` | Organization shown in System Settings |

### Claude fields

| Field | Value |
| --- | --- |
| `claude.models` | Object keyed by `opus`, `sonnet` or `haiku`. Sonnet and Haiku are required, Opus is optional |
| `claude.models.<tier>.aip_arn` | The AIP ARN, matching `aws.account_id` and `aws.bedrock_region` |
| `claude.models.<tier>.anthropic_model_id` | The Anthropic model ID the AIP routes to (see below) |
| `claude.models.<tier>.label` | Optional name Claude Desktop shows, defaulting to the capitalized tier |
| `claude.default_tier` | Optional starting tier for Claude Desktop and Claude Code, defaulting to `sonnet`. Must be configured |
| `claude.desktop_mcp_servers[]` | Optional Jarvis Registry MCP endpoints, each with `name` and `url` (the final HTTPS address) |

### Codex fields

| Field | Value |
| --- | --- |
| `codex.default_model` | A Global (`global.openai.…`) or US Geo (`us.openai.…`) Bedrock Runtime inference profile ID, such as `global.openai.gpt-6.1-terra`. The generator checks only the form: check the model's AWS model card that the profile can be called from `aws.bedrock_region` (US Geo profiles also work from the Canadian Regions), and grant it in IAM |
| `codex.reasoning_effort` | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra` or `persistent`. Each model supports only some of these; check the model card |

To find the AIPs and the models behind them:

```bash
aws bedrock list-inference-profiles --type-equals APPLICATION --region <bedrock_region>
aws bedrock get-inference-profile --inference-profile-identifier <aip_arn> --query 'models[].modelArn'
```

Turn the foundation model ID into `anthropic_model_id` by dropping the `anthropic.` prefix and any `-v1:0` suffix. For example, `anthropic.claude-sonnet-5` becomes `claude-sonnet-5`, `anthropic.claude-opus-5-5` becomes `claude-opus-5-5`, and `anthropic.claude-haiku-4-5-20251001-v1:0` becomes `claude-haiku-4-5-20251001`. Claude Code needs this mapping (`modelOverrides`) to recognize the AIP: without it the `/model` picker shows the raw ARN, features may be detected wrongly, and Sonnet 5 loses its 1M-token context window.

### Opus and the default model

Without an Opus model, the `opus` alias isn't allowed in Claude Code and Claude Desktop doesn't offer Opus. If you don't provision an Opus AIP, delete the `opus` entry from `claude.models`. Opus costs more per token than Sonnet, and on Bedrock every token is billed, so set `claude.default_tier` to `opus` only as a deliberate choice. The Opus tier and `claude.default_tier` haven't been tested end to end yet; see the [test plan](test-plan.md).

### MCP connectors with OAuth

Every `claude.desktop_mcp_servers` entry must point to a Jarvis Registry MCP endpoint. The policy always supplies the public static client `jarvis-registry-claude-desktop`, scope `mcp-proxy-ops` and callback port `47280`. Claude Desktop opens the browser and receives the sign-in result at `http://127.0.0.1:47280/callback`. This connector configuration supports Jarvis Registry's authorization server only and requires sign-in.

Desktop's automatic registration uses port 53280. On Windows, that port falls in the dynamic range from which WSL2, Docker Desktop and Hyper-V reserve blocks. If reserved, **Connect** fails and `main.log` records `listen EACCES: permission denied 127.0.0.1:53280`. The fixed client and port 47280 keep the callback below that range.

Before deploying the new policies, confirm that `jarvis.ascendingdc.com` runs an auth-server containing the static client introduced in Jarvis Registry commit `0cfbd576`. The temporary client registered on 2026-10-22 expires around 2026-11-22.

Claude Desktop doesn't follow HTTP redirects from the MCP URL or its OAuth metadata, so use final addresses.

## 3. Generate

```bash
make policies PRODUCTS="claude codex"
# Equivalent command, with flags before products
go run ./cmd/ccwb-policy -config config/config.json -out dist claude codex
```

Use `claude` for Claude Desktop and Claude Code, `codex` for Codex, or both. A Codex-only config can omit the `claude` section entirely. Products are required, duplicates are ignored, and unknown products are rejected. `make examples` selects both products automatically, and `-example` rejects product arguments.

The command validates the shared fields and selected sections before writing, and reports all field validation errors together. With both products selected it writes the following files.

```
dist/macos/Claude-CCWB.mobileconfig    one profile, two payloads: Claude Desktop and Claude Code
dist/macos/aws-config-CCWB.ini         ~/.aws/config snippet
dist/windows/Claude-CCWB.reg           both policies under HKLM
dist/windows/aws-config-CCWB.ini       same snippet, for %USERPROFILE%\.aws\config

dist/macos/Codex-CCWB.mobileconfig     the Codex requirements, for macOS MDM
dist/macos/codex-requirements-CCWB.toml
dist/windows/codex-requirements-CCWB.toml
```

Claude produces its `.mobileconfig` and `.reg`. Codex produces its `.mobileconfig` and the two TOML copies. Both selections include the two AWS snippets, for four files with Claude, five with Codex and seven with both.

Use an empty output directory for packaging. Files left by a previous run remain in `dist/` even when that product is omitted in the next run.

On a Mac, run `plutil -lint` for both `.mobileconfig` files. Each should print `OK`. Follow the [test plan](test-plan.md#before-you-start) for the mainland China and US testing split before testing clients.

## 4. Distribute

The repository doesn't distribute anything. Pick what fits:

- **Self-install.** Send the `dist/macos` or `dist/windows` folder through a private channel and point people to the [macOS](macos-user-guide.md) or [Windows](windows-user-guide.md) user guide.
- **MDM on macOS** (Jamf, Kandji, Intune and similar). Upload `Claude-CCWB.mobileconfig` as a custom profile. Both payloads were tested installed per user (`PayloadScope` `User`), so deploy it through the user channel. MDM tools usually sign profiles; a self-installed unsigned profile shows as "Unverified", which is expected. The AWS snippet still has to reach each person's `~/.aws/config`, through a script or the user guide.
- **Windows management** (Intune, Group Policy). Import `Claude-CCWB.reg` or set the same values. They live under `HKEY_LOCAL_MACHINE`, so this needs administrator rights. Claude Code ignores some managed-only keys in the per-user `HKCU` hive, and Claude Desktop ignores user policy whenever machine policy exists.
- **Codex on macOS.** Deploy `Codex-CCWB.mobileconfig` through the user channel. It writes the requirements as Base64 TOML (`requirements_toml_base64`) under the `com.openai.codex` preference domain. Without MDM, install `codex-requirements-CCWB.toml` as `/etc/codex/requirements.toml`.
- **Codex on Windows.** For self-service setup, [build, sign and test `ccwb-codex-setup.exe`](codex-setup-guide.md) with your config built in, then give it to employees. It requests one administrator prompt when installation or permission repair is needed. For managed deployment, install AWS CLI v2 and deploy `codex-requirements-CCWB.toml` as `%ProgramData%\OpenAI\Codex\requirements.toml` through Intune, Group Policy file deployment or a similar tool. The setup program skips the administrator step only when the CLI is present, the file matches byte for byte, and both the folder and file have the required owners and permissions; see [Managed fleets](codex-setup-guide.md#managed-fleets). Codex has no registry or Group Policy settings.

To change models or settings later, update the config, regenerate and reinstall. The macOS identifiers are `<prefix>.claude` and `<prefix>.codex`, and retain their identity while `macos_profile.identifier_prefix` is unchanged. For the initial upgrade to this schema, remove older Claude and Codex profiles first. Their display names are the same, but their identifiers changed, so macOS would otherwise keep both.

## 5. What the policies set

**Claude Desktop** (`com.anthropic.claudefordesktop` payload, or `HKLM\SOFTWARE\Policies\Claude`):

| Key | Value |
| --- | --- |
| `inferenceProvider` | `bedrock` |
| `inferenceCredentialKind` | `interactive`: Desktop's own IAM Identity Center sign-in |
| `inferenceBedrockRegion`, `inferenceBedrockSso*` | From `aws.*` |
| `inferenceModels` | JSON list of the AIPs with labels and tiers. The `claude.default_tier` model comes first, which makes it Desktop's default |
| `managedMcpServers` | JSON list of `claude.desktop_mcp_servers`, omitted when empty. Every entry has fixed `oauth` with `clientId` `jarvis-registry-claude-desktop`, `scope` `mcp-proxy-ops` and `callbackPort` `47280` |

**Claude Code** (`com.anthropic.claudecode` payload, or the JSON `Settings` value under `HKLM\SOFTWARE\Policies\ClaudeCode`):

| Key | Value |
| --- | --- |
| `env` | `CLAUDE_CODE_USE_BEDROCK=1`, `AWS_PROFILE=CCWB`, `AWS_REGION`, `ANTHROPIC_MODEL` set to the `claude.default_tier` AIP, and `ANTHROPIC_DEFAULT_<TIER>_MODEL` set to each configured tier's AIP |
| `awsAuthRefresh` | `aws sso login --profile CCWB`, run only after credentials have expired |
| `modelOverrides` | Each `anthropic_model_id` mapped to its AIP |
| `availableModels`, `enforceAvailableModels` | Only the configured tiers' aliases, including for the Default option. Without this, Claude Code's Bedrock default is Opus through a public inference profile, which bypasses your AIPs |

Because these are managed settings, they apply to **every** Claude Code session for that user on that machine, and users can't override them.

**Codex** gets one file, `requirements.toml`:

- **Enforced; users can't override it:** it pins the built-in `amazon-bedrock-runtime` provider and replaces any user definition of it with the `CCWB` profile, the Bedrock Region, and an `auth_refresh` command that re-runs `aws sso login --profile CCWB` when credentials expire, the Codex counterpart of Claude Code's `awsAuthRefresh`. It also allows web search only as `disabled`, since hosted web search isn't available on Bedrock Runtime.
- **Starting values; users can change them:** `[models.new_thread]` sets `codex.default_model` and `codex.reasoning_effort`. The CLI and the ChatGPT app apply them to each new thread unless the user picks another model.

The generator doesn't write `managed_config.toml`, Codex's older file for defaults: Codex ignores it on Windows, and its own documentation describes it as being phased out in favor of `requirements.toml`.

Codex runs `auth_refresh` without a shell and only accepts the bare command `aws`, so the AWS CLI must be on the `PATH` that Codex itself sees:

- **CLI and IDE extension:** the `PATH` of the terminal or editor that started them.
- **ChatGPT app on macOS:** at launch the app runs the user's login shell as `$SHELL -ilc` (interactive and login), copies its environment, and passes it to its bundled Codex. So `aws` must be on the `PATH` after the shell's startup files run: for zsh `~/.zshenv`, `~/.zprofile`, `~/.zshrc` and `~/.zlogin`; for bash only the first of `~/.bash_profile`, `~/.bash_login` and `~/.profile`, which doesn't read `~/.bashrc` unless it sources it. If that shell exits with an error or takes longer than about 10 seconds, the app falls back to its own `PATH` (`/usr/bin:/bin:/usr/sbin:/sbin`), which doesn't include `/usr/local/bin` or `/opt/homebrew/bin`. Check with `$SHELL -ilc 'command -v aws'` and test C10 in the [test plan](test-plan.md). This is the app's behavior, observed in version 26.1027, not something Codex documents.

After installing or changing the Codex profile, quit the ChatGPT app and run `codex app-server daemon restart`. The CLI connects to a long-running background Codex server that keeps the settings it started with, and fails with `modelProviderPolicyConflict` (provider `openai`) when those predate the profile.

Codex has no model allowlist requirement, so IAM/SCP must restrict actual model access. See section 4 for the macOS and Windows deployment paths.

## 6. Sign-in lifetime

Claude Desktop keeps its own IAM Identity Center session; Claude Code uses the AWS CLI's cache in `~/.aws/sso/cache`. Signing out of one doesn't sign out the other. How often people sign in again depends on the IAM Identity Center session duration and the permission set's session duration; check both before rollout and mention the result in your internal docs.

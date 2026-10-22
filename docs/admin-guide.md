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
| `desktop.mcp_servers[]` | Optional remote MCP servers: `name`, `url` (final https address) and `oauth` |
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

For an `oauth: true` server, Claude Desktop registers itself with the server's authorization server and receives the sign-in result at `http://127.0.0.1:53280/callback`. The authorization server must accept that redirect URI. Claude Desktop doesn't follow HTTP redirects from the MCP URL or its OAuth metadata, so use final addresses.

## 3. Generate

```bash
make policies        # or: go run ./cmd/ccwb-policy -config config/config.json -out dist
```

The command validates every field first and lists all problems, each with its field path. It writes:

```
dist/macos/Claude-CCWB.mobileconfig    one profile, two payloads: Claude Desktop and Claude Code
dist/macos/aws-config-CCWB.ini         ~/.aws/config snippet
dist/windows/Claude-CCWB.reg           both policies under HKLM (untested)
dist/windows/aws-config-CCWB.ini       same snippet, for %USERPROFILE%\.aws\config
```

On a Mac, `plutil -lint dist/macos/Claude-CCWB.mobileconfig` should print `OK`.

## 4. Distribute

The repository doesn't distribute anything. Pick what fits:

- **Self-install.** Send the `dist/macos` or `dist/windows` folder through a private channel and point people to the [macOS](macos-user-guide.md) or [Windows](windows-user-guide.md) user guide.
- **MDM on macOS** (Jamf, Kandji, Intune and similar). Upload `Claude-CCWB.mobileconfig` as a custom profile. Both payloads were tested installed per user (`PayloadScope` `User`), so deploy it through the user channel. MDM tools usually sign profiles; a self-installed unsigned profile shows as "Unverified", which is expected. The AWS snippet still has to reach each person's `~/.aws/config`, through a script or the user guide.
- **Windows management** (Intune, Group Policy). Import `Claude-CCWB.reg` or set the same values. They live under `HKEY_LOCAL_MACHINE`, so this needs administrator rights. Claude Code ignores some managed-only keys in the per-user `HKCU` hive, and Claude Desktop ignores user policy whenever machine policy exists.

To change models or settings later, update the config, regenerate and reinstall. On macOS the new profile replaces the old one as long as `profile.identifier` hasn't changed.

## 5. What the policies set

**Claude Desktop** (`com.anthropic.claudefordesktop` payload, or `HKLM\SOFTWARE\Policies\Claude`):

| Key | Value |
| --- | --- |
| `inferenceProvider` | `bedrock` |
| `inferenceCredentialKind` | `interactive`: Desktop's own IAM Identity Center sign-in |
| `inferenceBedrockRegion`, `inferenceBedrockSso*` | From `aws.*` |
| `inferenceModels` | JSON list of the AIPs with labels and tiers. The `default_tier` model comes first, which makes it Desktop's default |
| `managedMcpServers` | JSON list of `desktop.mcp_servers`; omitted when empty |

**Claude Code** (`com.anthropic.claudecode` payload, or the JSON `Settings` value under `HKLM\SOFTWARE\Policies\ClaudeCode`):

| Key | Value |
| --- | --- |
| `env` | `CLAUDE_CODE_USE_BEDROCK=1`, `AWS_PROFILE=CCWB`, `AWS_REGION`, `ANTHROPIC_MODEL` set to the `default_tier` AIP, and `ANTHROPIC_DEFAULT_<TIER>_MODEL` set to each configured tier's AIP |
| `awsAuthRefresh` | `aws sso login --profile CCWB`, run only after credentials have expired |
| `modelOverrides` | Each `anthropic_model_id` mapped to its AIP |
| `availableModels`, `enforceAvailableModels` | Only the configured tiers' aliases, including for the Default option. Without this, Claude Code's Bedrock default is Opus through a public inference profile, which bypasses your AIPs |

Because these are managed settings, they apply to **every** Claude Code session for that user on that machine, and users can't override them.

## 6. Sign-in lifetime

Claude Desktop keeps its own IAM Identity Center session; Claude Code uses the AWS CLI's cache in `~/.aws/sso/cache`. Signing out of one doesn't sign out the other. How often people sign in again depends on the IAM Identity Center session duration and the permission set's session duration; check both before rollout and mention the result in your internal docs.

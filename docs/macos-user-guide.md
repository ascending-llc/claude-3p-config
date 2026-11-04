# macOS user guide

This sets up Claude Desktop, the Claude Code CLI and, optionally, Codex (the ChatGPT desktop app, Codex CLI and IDE extension) on your Mac to use your organization's models on Amazon Bedrock. You sign in with your organization account through AWS IAM Identity Center. Nothing is installed except configuration profiles and a few lines in `~/.aws/config`.

You need these files from whoever maintains your organization's configuration, or from `dist/macos/` if you generated them yourself:

- `Claude-CCWB.mobileconfig`: the configuration profile for Claude Desktop and Claude Code
- `aws-config-CCWB.ini`: the AWS CLI profile Claude Code signs in with
- `Codex-CCWB.mobileconfig`: optional, separate managed configuration for local Codex/ChatGPT Work

If your organization's IT installs the profile for you through device management, skip step 2.

## Before you start

- Claude Desktop, and the Claude Code CLI if you use it
- AWS CLI v2 (`aws --version`); install it with `brew install awscli` or [AWS's installer](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- Access to the AWS account and permission set your organization assigned for Claude

Tested on macOS 26.7.1 with Claude Desktop 2.26454.2, Claude Code 2.1.306 and AWS CLI 2.37.12.

## 1. Add the AWS profile

Open `~/.aws/config` in a text editor (create it if needed) and append the contents of `aws-config-CCWB.ini`. It adds two sections:

```ini
[sso-session ccwb]
...
[profile CCWB]
...
```

If the file already has a `[profile CCWB]` or `[sso-session ccwb]` section from an earlier setup, replace it instead of adding a second one.

## 2. Install the configuration profile

1. Remove any earlier Claude Desktop profile first: **System Settings → General → Device Management**, select it, and click **−**.
2. Double-click `Claude-CCWB.mobileconfig`. macOS says the profile was downloaded.
3. Open **System Settings → General → Device Management**, select the profile, and click **Install**. It shows as "Unverified" unless your organization signed it; that's expected.

To check it worked, run `ls "/Library/Managed Preferences/$USER/"`. You should see `com.anthropic.claudefordesktop.plist` and `com.anthropic.claudecode.plist`.

## 3. Sign in to Claude Desktop

1. Quit Claude Desktop completely (**Claude → Quit**) and open it again.
2. Choose to sign in with AWS. Your browser opens the AWS access portal, which may first send you to your organization's sign-in page.
3. Check that the code shown in the browser matches the one in Claude Desktop, click **Confirm and continue**, then **Allow**.

Claude Desktop then shows the models your organization configured, such as Sonnet, Haiku and possibly Opus.

**If the browser page hangs after "Confirm and continue"**, copy the page's address into another browser, such as Safari, and approve it there. Some Chrome setups never send the approval.

## 4. Connect MCP connectors

If your organization added connectors such as `jarvis-registry`, open **Customize → Connectors** in Claude Desktop, find the connector, click **Connect**, and finish the sign-in in your browser. This sign-in is separate from the AWS one, and the connector's tools aren't available until it's done.

## 5. Use Claude Code

Open a **new** terminal and run `claude`. The first request runs `aws sso login --profile CCWB`, which opens your browser to sign in. After that, Claude Code reuses the session until it expires and signs you in again when needed.

Run `/status` to check. You should see:

- `API provider: Amazon Bedrock`
- `Model:` the ARN of your organization's default model AIP (usually Sonnet)
- `Setting sources:` including `Enterprise managed settings (plist)`

Good to know:

- **Every `claude` session on this Mac now uses Bedrock**, including ones that used a Claude subscription before. Remove the profile to switch back.
- **AWS credentials exported in your shell win.** If `AWS_ACCESS_KEY_ID` or `AWS_SESSION_TOKEN` is set, Claude Code uses them instead of the `CCWB` profile. Unset them, or start a new terminal.
- **`/model` offers only your organization's models.** Web search isn't available on Bedrock.

## 6. Use Codex with Bedrock

Only do this when your organization has approved Codex on Bedrock and provided `Codex-CCWB.mobileconfig`.

1. Double-click `Codex-CCWB.mobileconfig`, then install **Codex on Amazon Bedrock (CCWB)** under **System Settings → General → Device Management**. This is separate from the Claude profile, so either one can be rolled back independently.
2. Run `aws sso login --profile CCWB` and confirm `aws sts get-caller-identity --profile CCWB` succeeds.
3. Quit the ChatGPT/Codex desktop app and any Codex CLI or IDE extension, run `codex app-server daemon restart` in Terminal, then reopen them.
4. Start a new local Codex task, check `/status` for provider `amazon-bedrock-runtime`, then send `Reply with exactly: bedrock-ok`.

The managed profile selects the Bedrock provider and default model. When AWS credentials expire, Codex re-runs the SSO sign-in automatically (`auth_refresh`) and continues once you complete it, like Claude Code. IAM or an SCP—not the client model picker—decides which models are actually allowed. You do not need to sign out of ChatGPT.

If your administrator configured Codex MCP, the connection settings are included in `config_toml_base64` in the same Codex profile; no user file merge is needed. After restarting, run `codex mcp list` to check for the server. For OAuth, run `codex mcp login jarvis-registry` (substitute the server name as needed) and complete browser sign-in, or authenticate in the client's MCP settings. MCP sign-in is separate from AWS and Claude Desktop. An `enabled` entry confirms configuration only, not successful authentication or tool calls.

For file-based deployment instead of a profile, IT must merge the server tables from `codex-config-CCWB.toml` into `~/.codex/config.toml`, or the file in a custom `CODEX_HOME`. Preserve existing content; update `url` and `enabled` in matching HTTP tables without duplicating headers or replacing the whole file.

If a matching table previously used `command` to start a local stdio server, remove its stdio-specific fields (`command`, `args`, `env`, `env_vars`, `cwd`) before switching to HTTP. Preserve applicable general settings and other servers. Keeping both `command` and `url` prevents configuration loading.

## Sign out

- **Claude Desktop:** sign out in the app.
- **Claude Code:** `aws sso logout`.

The two sign-ins are independent; signing out of one doesn't affect the other.

## Uninstall

1. **System Settings → General → Device Management**: select the Claude profile and click **−**. If Codex was enabled, remove **Codex on Amazon Bedrock (CCWB)** separately. Quit and reopen the affected apps.
2. Remove the `[sso-session ccwb]` and `[profile CCWB]` sections from `~/.aws/config`.

If you manually merged the Codex MCP snippet, also remove the server tables added by that deployment. Removing the profile alone does not remove connections from user files. Preserve other servers, `auth.json`, plugins and project settings.

## Troubleshooting

| Problem | What to do |
| --- | --- |
| The device page hangs after "Confirm and continue" | Approve it in another browser (see step 3) |
| Claude Desktop doesn't offer AWS sign-in | Check the profile is installed (step 2), then quit and reopen Claude Desktop. Desktop's log is `~/Library/Logs/Claude-3p/main.log` |
| A connector is missing or won't connect | Quit and reopen Claude Desktop. If it still fails, send the error message to your administrator; never share sign-in codes or tokens |
| Claude Code: `AWS authentication failed` | Run `aws sso login --profile CCWB`, then retry. If it persists, ask your administrator to check your permission set |
| Claude Code: `403 … not authorized … with an explicit deny` | You asked for a model outside your organization's AIPs, for example with `--model`. Use `/model` to pick one of your organization's models |
| `/status` doesn't list `Enterprise managed settings (plist)` | The profile isn't installed for your user; repeat step 2 |
| Codex `/status` doesn't show `amazon-bedrock-runtime` | Check that the separate Codex profile is installed and that the client was fully restarted |
| A Codex MCP server is missing or disabled | Check that the Codex profile contains `config_toml_base64` and the client was restarted. For file-based deployment, check the merge target. Any organizational MCP allowlist must also allow the server name and URL |
| Codex lists the MCP server, but its tools are unavailable | Complete that server's MCP OAuth sign-in and check connection errors; AWS sign-in does not replace MCP sign-in |
| Codex CLI: `This chat uses model provider "openai", but your organization requires "amazon-bedrock-runtime"` | Quit the ChatGPT app, run `codex app-server daemon restart`, then start `codex` again |
| The ChatGPT app can't renew the AWS sign-in because it can't find `aws` | The app looks for `aws` on your login shell's `PATH`. Run `$SHELL -ilc 'command -v aws'` in Terminal; if it prints nothing, add the AWS CLI's folder to `PATH` in your shell startup file, then quit and reopen the app |
| Codex gets `AccessDenied` although STS works | STS proves identity only. Send the model ID, Region, error and Request ID to the AWS administrator; never send credentials |

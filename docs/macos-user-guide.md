# macOS user guide

This sets up your selected Claude or Codex clients on your Mac to use your organization's models on Amazon Bedrock. You sign in with your organization account through AWS IAM Identity Center. The setup uses configuration profiles and a few lines in `~/.aws/config`.

Follow the [test plan](test-plan.md#before-you-start) for implementation testing before installing policies or signing in.

Use the files for your selected products from whoever maintains your organization's configuration, or from `dist/macos/` if you generated them yourself:

- `Claude-CCWB.mobileconfig`: the configuration profile for Claude Desktop and Claude Code
- `aws-config-CCWB.ini`: the shared AWS CLI profile Claude Code and Codex sign in with
- `Codex-CCWB.mobileconfig`: managed configuration for local Codex/ChatGPT Work when Codex is selected

For a Codex-only setup, complete step 1 and then go directly to step 6. For Claude, follow steps 1–5 for the clients you use. If you selected both products, also complete step 6.

If your organization's IT installs the profile for you through device management, skip step 2.

## Before you start

- **Claude users.** Install Claude Desktop, Claude Code, or both, according to which clients you use.
- **Codex users.** Install your chosen local client, such as the ChatGPT desktop app, Codex CLI or IDE extension.
- **Claude Code and Codex users.** Install AWS CLI v2 with `brew install awscli` or AWS's installer, and check `aws --version`. [AWS installer](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- Access to the AWS account and permission set your organization assigned for the selected products

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

1. In **System Settings → General → Device Management**, remove the Claude profiles from an earlier setup before installing the new version. Their display names are the same, but the identifiers changed, so macOS would otherwise keep both. Select each old Claude profile and click **−**. Have IT replace profiles managed by your organization. When also upgrading Codex, remove its older profiles as described in step 6.
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

1. Remove any Codex profile from an earlier setup under **System Settings → General → Device Management**, even if its name matches the new one. The identifier changed. Have IT replace managed profiles. Then double-click `Codex-CCWB.mobileconfig` and install **Codex on Amazon Bedrock (CCWB)** under **System Settings → General → Device Management**. This is separate from the Claude profile, so either one can be rolled back independently.
2. Run `aws sso login --profile CCWB` and confirm `aws sts get-caller-identity --profile CCWB` succeeds.
3. Quit the ChatGPT/Codex desktop app and any Codex CLI or IDE extension, run `codex app-server daemon restart` in Terminal, then reopen them.
4. Start a new local Codex task, check `/status` for provider `amazon-bedrock-runtime`, then send `Reply with exactly: bedrock-ok`.

The managed profile selects the Bedrock provider and default model. When AWS credentials expire, Codex re-runs the SSO sign-in automatically (`auth_refresh`) and continues once you complete it, like Claude Code. IAM or an SCP—not the client model picker—decides which models are actually allowed. You do not need to sign out of ChatGPT.

## Sign out

- **Claude Desktop:** sign out in the app.
- **Claude Code:** `aws sso logout`.

The two sign-ins are independent; signing out of one doesn't affect the other.

## Uninstall

1. In **System Settings → General → Device Management**, remove the installed Claude and Codex profiles for the products you are uninstalling. Include any older profiles left from an earlier setup, even if they share the same display names. Have IT remove managed profiles. Quit and reopen the affected apps.
2. Remove the `[sso-session ccwb]` and `[profile CCWB]` sections from `~/.aws/config` only after all clients using them have been retired. Keep these shared sections while either Claude Code or Codex still uses them.

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
| Codex CLI: `This chat uses model provider "openai", but your organization requires "amazon-bedrock-runtime"` | Quit the ChatGPT app, run `codex app-server daemon restart`, then start `codex` again |
| The ChatGPT app can't renew the AWS sign-in because it can't find `aws` | The app looks for `aws` on your login shell's `PATH`. Run `$SHELL -ilc 'command -v aws'` in Terminal; if it prints nothing, add the AWS CLI's folder to `PATH` in your shell startup file, then quit and reopen the app |
| Codex gets `AccessDenied` although STS works | STS proves identity only. Send the model ID, Region, error and Request ID to the AWS administrator; never send credentials |

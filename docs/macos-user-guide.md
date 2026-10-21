# macOS user guide

This sets up Claude Desktop and the Claude Code CLI on your Mac to use your organization's Claude models on Amazon Bedrock. You sign in with your organization account through AWS IAM Identity Center. Nothing is installed except a configuration profile and a few lines in `~/.aws/config`.

You need two files from whoever maintains your organization's configuration, or from `dist/macos/` if you generated them yourself:

- `Claude-CCWB.mobileconfig`: the configuration profile for Claude Desktop and Claude Code
- `aws-config-CCWB.ini`: the AWS CLI profile Claude Code signs in with

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

Claude Desktop then shows the Sonnet and Haiku models from your organization.

**If the browser page hangs after "Confirm and continue"**, copy the page's address into another browser, such as Safari, and approve it there. Some Chrome setups never send the approval.

## 4. Connect MCP connectors

If your organization added connectors such as `jarvis-registry`, open **Customize → Connectors** in Claude Desktop, find the connector, click **Connect**, and finish the sign-in in your browser. This sign-in is separate from the AWS one, and the connector's tools aren't available until it's done.

## 5. Use Claude Code

Open a **new** terminal and run `claude`. The first request runs `aws sso login --profile CCWB`, which opens your browser to sign in. After that, Claude Code reuses the session until it expires and signs you in again when needed.

Run `/status` to check. You should see:

- `API provider: Amazon Bedrock`
- `Model:` your Sonnet AIP's ARN
- `Setting sources:` including `Enterprise managed settings (plist)`

Good to know:

- **Every `claude` session on this Mac now uses Bedrock**, including ones that used a Claude subscription before. Remove the profile to switch back.
- **AWS credentials exported in your shell win.** If `AWS_ACCESS_KEY_ID` or `AWS_SESSION_TOKEN` is set, Claude Code uses them instead of the `CCWB` profile. Unset them, or start a new terminal.
- **`/model` offers only Sonnet and Haiku.** Web search isn't available on Bedrock.

## Sign out

- **Claude Desktop:** sign out in the app.
- **Claude Code:** `aws sso logout`.

The two sign-ins are independent; signing out of one doesn't affect the other.

## Uninstall

1. **System Settings → General → Device Management**: select the profile and click **−**. Quit and reopen Claude Desktop.
2. Remove the `[sso-session ccwb]` and `[profile CCWB]` sections from `~/.aws/config`.

## Troubleshooting

| Problem | What to do |
| --- | --- |
| The device page hangs after "Confirm and continue" | Approve it in another browser (see step 3) |
| Claude Desktop doesn't offer AWS sign-in | Check the profile is installed (step 2), then quit and reopen Claude Desktop. Desktop's log is `~/Library/Logs/Claude-3p/main.log` |
| A connector is missing or won't connect | Quit and reopen Claude Desktop. If it still fails, send the error message to your administrator; never share sign-in codes or tokens |
| Claude Code: `AWS authentication failed` | Run `aws sso login --profile CCWB`, then retry. If it persists, ask your administrator to check your permission set |
| Claude Code: `403 … not authorized … with an explicit deny` | You asked for a model outside your organization's AIPs, for example with `--model`. Use `/model` to pick Sonnet or Haiku |
| `/status` doesn't list `Enterprise managed settings (plist)` | The profile isn't installed for your user; repeat step 2 |

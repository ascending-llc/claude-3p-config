# Windows user guide

> **Untested.** The Windows files are generated from the same settings that were tested on macOS, but nobody has run this setup on Windows yet. Follow [the test plan](test-plan.md) on a test machine before rolling it out, and report what differs from this guide.

This sets up Claude Desktop and the Claude Code CLI on Windows to use your organization's Claude models on Amazon Bedrock, signed in through AWS IAM Identity Center.

You need two files from whoever maintains your organization's configuration, or from `dist/windows/` if you generated them yourself:

- `Claude-CCWB.reg`: the policies for Claude Desktop and Claude Code
- `aws-config-CCWB.ini`: the AWS CLI profile Claude Code signs in with

If your organization's IT deploys the policies for you through Intune or Group Policy, skip step 2.

## Before you start

- Claude Desktop, and the Claude Code CLI if you use it
- AWS CLI v2: `winget install Amazon.AWSCLI`, then open a new terminal and check `aws --version`
- **Administrator rights** for step 2. Both policies live under `HKEY_LOCAL_MACHINE`: Claude Code ignores some managed-only settings in the per-user hive, and Claude Desktop ignores per-user policy whenever machine policy exists.

## 1. Add the AWS profile

Open `%USERPROFILE%\.aws\config` in a text editor (create the folder and file if needed) and append the contents of `aws-config-CCWB.ini`. If the file already has a `[profile CCWB]` or `[sso-session ccwb]` section, replace it instead of adding a second one.

## 2. Import the policies

Double-click `Claude-CCWB.reg` and approve the administrator prompt. Or, in a terminal opened with **Run as administrator**:

```powershell
reg import Claude-CCWB.reg
```

This writes:

- Claude Desktop's settings as values under `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Claude`
- Claude Code's settings as one JSON value named `Settings` under `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\ClaudeCode`

## 3. Sign in to Claude Desktop

Quit Claude Desktop completely (including from the system tray) and open it again. Choose to sign in with AWS, then in the browser check that the code matches, click **Confirm and continue**, and then **Allow**.

If the page hangs after "Confirm and continue", copy its address into another browser and approve it there.

## 4. Connect MCP connectors

If your organization added connectors such as `jarvis-registry`, open **Customize → Connectors**, click **Connect** on the connector, and finish the sign-in in your browser.

## 5. Use Claude Code

Open a **new** terminal and run `claude`. The first request runs `aws sso login --profile CCWB` and opens your browser. Run `/status` and check for `API provider: Amazon Bedrock`, the ARN of your organization's default model AIP (usually Sonnet) as the model, and a managed settings source from the registry.

As on macOS, every Claude Code session for this machine now uses Bedrock, and AWS credentials set as environment variables take precedence over the `CCWB` profile.

## Sign out

Sign out in Claude Desktop. For Claude Code, run `aws sso logout`.

## Uninstall

In a terminal opened with **Run as administrator**:

```powershell
reg delete "HKLM\SOFTWARE\Policies\ClaudeCode" /v Settings /f
```

For Claude Desktop, delete the values that `Claude-CCWB.reg` lists under `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Claude`, for example in Registry Editor. Delete the whole key only if nothing else manages Claude Desktop on this machine. Then remove the `[sso-session ccwb]` and `[profile CCWB]` sections from `%USERPROFILE%\.aws\config`.

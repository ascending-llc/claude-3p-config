# Windows user guide

This sets up Claude Desktop and the Claude Code CLI on Windows to use your organization's Claude models on Amazon Bedrock, signed in through AWS IAM Identity Center.

You need two files from whoever maintains your organization's configuration, or from `dist/windows/` if you generated them yourself:

- `Claude-CCWB.reg`: the policies for Claude Desktop and Claude Code
- `aws-config-CCWB.ini`: the AWS CLI profile Claude Code signs in with

If your organization's IT deploys the policies for you through Intune or Group Policy, skip step 2.

## Before you start

- Claude Desktop, and the Claude Code CLI if you use it
- AWS CLI v2: `winget install Amazon.AWSCLI`, then open a new terminal and check `aws --version`
- **Administrator rights** for step 2. Both policies live under `HKEY_LOCAL_MACHINE`: Claude Code ignores some managed-only settings in the per-user hive, and Claude Desktop ignores per-user policy whenever machine policy exists.

Tested on Windows 10 Home 22H2 (build 19045) with Claude Desktop 3.1623.4 from the Microsoft Store, Claude Code 2.1.308 and AWS CLI 2.38.4.

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

Quit Claude Desktop completely and open it again. Closing the window only hides it in the system tray, so use the menu at the top left (**File → Exit**), or right-click the Claude icon in the system tray and choose **Quit**. Choose to sign in with AWS, then in the browser check that the code matches, click **Confirm and continue**, and then **Allow**.

If the page hangs after "Confirm and continue", copy its address into another browser and approve it there.

## 4. Connect MCP connectors

If your organization added connectors such as `jarvis-registry`, open **Customize → Connectors**, click **Connect** on the connector, and finish the sign-in in your browser. This sign-in is separate from the AWS one, and the connector's tools aren't available until it's done.

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

## Troubleshooting

Claude Desktop's log is `main.log` in `%LOCALAPPDATA%\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Local\Claude-3p\logs` (the Microsoft Store install). Its messages often explain what the app shows only briefly.

| Problem | What to do |
| --- | --- |
| Claude Desktop doesn't offer AWS sign-in | Check that step 2 worked, then quit Claude Desktop completely (see step 3) and open it again |
| A connector is missing | Quit and reopen Claude Desktop. If it's still missing, send your administrator the `managedMcpServers` lines from `main.log`; Desktop leaves out a connector whose settings it can't read and logs why |
| **Connect** shows only a brief "connection failed" message | Look in `main.log` for `listen EACCES` or a port that's in use. Desktop receives the connector sign-in on a local port (47280 unless your organization chose another), and another program on your computer may hold it. Send the line to your administrator; never share sign-in codes or tokens |
| Claude Code: `AWS authentication failed` | Run `aws sso login --profile CCWB`, then retry. If it persists, ask your administrator to check your permission set |
| Claude Code: `403 … not authorized … with an explicit deny` | You asked for a model outside your organization's AIPs, for example with `--model`. Use `/model` to pick one of your organization's models |
| `/status` doesn't list a managed settings source | The policies weren't imported under `HKEY_LOCAL_MACHINE`; repeat step 2 from a terminal opened with **Run as administrator** |

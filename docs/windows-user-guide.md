# Windows user guide

This sets up your selected Claude or Codex clients on Windows to use your organization's models on Amazon Bedrock, signed in through AWS IAM Identity Center.

Follow the [test plan](test-plan.md#before-you-start) for implementation testing before installing policies or signing in.

For Codex, get `ccwb-codex-setup.exe` from your IT team. For Claude or a manual Codex installation, get the files for your selected products from IT, or from `dist/windows/` if you generated them yourself:

- `Claude-CCWB.reg`: the policies for Claude Desktop and Claude Code
- `aws-config-CCWB.ini`: the shared AWS CLI profile Claude Code and Codex sign in with
- `codex-requirements-CCWB.toml`: for Codex when selected. It enforces the Bedrock Runtime provider, AWS profile and Region, and sets the starting model

For a Codex-only setup using the program, go directly to [Use Codex with Bedrock](#6-use-codex-with-bedrock); it adds the AWS profile for you. For Claude, follow sections 1–5 for the clients you use. If you selected both products, also complete the Codex section.

If your organization's IT deploys the policies for you through Intune or Group Policy, skip [Import the policies](#2-import-the-policies).

## Before you start

- **Claude users.** Install Claude Desktop, Claude Code, or both, according to which clients you use.
- **Codex users.** Install your chosen local client, such as the ChatGPT desktop app, Codex CLI or IDE extension.
- **Claude Code and manual Codex installations.** Install AWS CLI v2 with `winget install Amazon.AWSCLI`, then open a new terminal and check `aws --version`. The Codex setup program installs AWS CLI v2 if it is missing.
- **Administrator approval** is needed to import Claude policies and install the Codex system configuration. For the Codex setup program, IT can enter administrator credentials when prompted; start it as your normal Windows user. Claude policies live under `HKEY_LOCAL_MACHINE`. Claude Code ignores some managed-only settings in the per-user hive, and Claude Desktop ignores per-user policy whenever machine policy exists.

The earlier manual setup was tested on Windows 10 Home 22H2 (build 19045) with Claude Desktop 3.1623.4 from the Microsoft Store, Claude Code 2.1.308 and AWS CLI 2.38.4. See the [test results](test-plan.md#results) for the setup program's validation status.

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

## 6. Use Codex with Bedrock

Only do this when your organization has approved Codex on Bedrock. Your IT team builds the program with your organization's settings; you do not need Go or signing tools.

1. Get `ccwb-codex-setup.exe` from your IT team.
2. Double-click it as your normal Windows user. If **Windows protected your PC** appears, choose **More info → Run anyway**, but only for a file supplied by your IT team. If Smart App Control blocks it or no override is available, ask IT for a signed copy.
3. Check the summary. Press Enter at `Proceed? [Y/n]` to continue, or enter `n` to cancel. If existing settings differ, check the old and new values before agreeing to replace them.
4. Approve the administrator prompt if one appears. If you are not an administrator, someone from IT enters their credentials. Keep running the original program under your own Windows account so the AWS profile belongs to you.
5. At `Sign in to AWS now to check the settings? [Y/n]`, press Enter to sign in through your browser, or enter `n` to skip it. If you sign in, check the reported AWS role and any warnings; finishing the installation alone does not prove sign-in succeeded.
6. Quit the ChatGPT app from the system tray (**right-click → Quit**), then open it from the **Start menu**. It normally starts when you sign in to Windows, so it may be running even with no window open. Close and reopen the Codex CLI or IDE extension too if you use them.
7. If the ChatGPT app asks you to sign in, use your ChatGPT, Google or other account. **Don't choose Amazon Bedrock** there: that option selects a different provider, which the managed settings block with "Cannot select `amazon-bedrock` because requirements set `model_provider` to `amazon-bedrock-runtime`". In a new local task, check the settings or `/status` for `amazon-bedrock-runtime` and your configured model, reasoning effort and Region. Follow the [Codex tests](test-plan.md#codex) for a model smoke test, subject to the testing location restrictions.

When AWS credentials expire, Codex re-runs the SSO sign-in automatically (`auth_refresh`) and continues once you complete it. IAM or an SCP—not the client model picker—enforces the real model allowlist. You do not need to sign out of ChatGPT.

### Manual alternative (administrators)

For a manual pilot, install AWS CLI v2 and [add the AWS profile](#1-add-the-aws-profile) first. Then:

1. In a PowerShell opened with **Run as administrator**:

   ```powershell
   New-Item -ItemType Directory -Force "$env:ProgramData\OpenAI\Codex" | Out-Null
   Copy-Item codex-requirements-CCWB.toml "$env:ProgramData\OpenAI\Codex\requirements.toml"
   ```

   Have IT secure the file and directory as described in the [managed-fleet instructions](codex-setup-guide.md#managed-fleets).
2. Back in a normal, newly opened terminal under the employee's account, run `aws sso login --profile CCWB` and `aws sts get-caller-identity --profile CCWB`.
3. Finish with the tray restart and client checks described above. The [Codex tests](test-plan.md#codex) include the `Reply with exactly: bedrock-ok` smoke test for US-based testers.

## Sign out

Sign out in Claude Desktop. For Claude Code, run `aws sso logout`.

## Uninstall

Follow the instructions for the products you are uninstalling.

For Claude Code, open a terminal with **Run as administrator** and run the following command.

```powershell
reg delete "HKLM\SOFTWARE\Policies\ClaudeCode" /v Settings /f
```

For Claude Desktop, delete the values that `Claude-CCWB.reg` lists under `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Claude`, for example in Registry Editor. Delete the whole key only if nothing else manages Claude Desktop on this machine.

For Codex, have IT back up and remove `%ProgramData%\OpenAI\Codex\requirements.toml`, then restart the client. Do not remove the user's `config.toml`, `auth.json`, plugins or project settings.

The setup program leaves `.bak-*` backups next to an existing `%USERPROFILE%\.aws\config` or `%ProgramData%\OpenAI\Codex\requirements.toml` that it replaces. Have IT review these before restoring or deleting them; restoring a whole AWS config can also revert unrelated profiles.

Remove the `[sso-session ccwb]` and `[profile CCWB]` sections from `%USERPROFILE%\.aws\config` only after all clients using them have been retired. Keep these shared sections while either Claude Code or Codex still uses them.

## Troubleshooting

Claude Desktop's log is `main.log` in `%LOCALAPPDATA%\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Local\Claude-3p\logs` (the Microsoft Store install). Its messages often explain what the app shows only briefly.

| Problem | What to do |
| --- | --- |
| Claude Desktop doesn't offer AWS sign-in | Check that [policy import](#2-import-the-policies) worked, then [quit and reopen Claude Desktop](#3-sign-in-to-claude-desktop) |
| A connector is missing | Quit and reopen Claude Desktop. If it's still missing, send your administrator the `managedMcpServers` lines from `main.log`; Desktop leaves out a connector whose settings it can't read and logs why |
| **Connect** shows only a brief "connection failed" message | Look in `main.log` for `listen EACCES` or a port that's in use. Desktop receives the connector sign-in on a local port (fixed at 47280), and another program on your computer may hold it. Send the line to your administrator; never share sign-in codes or tokens |
| Claude Code: `AWS authentication failed` | Run `aws sso login --profile CCWB`, then retry. If it persists, ask your administrator to check your permission set |
| Claude Code: `403 … not authorized … with an explicit deny` | You asked for a model outside your organization's AIPs, for example with `--model`. Use `/model` to pick one of your organization's models |
| `/status` doesn't list a managed settings source | The policies weren't imported under `HKEY_LOCAL_MACHINE`; repeat [policy import](#2-import-the-policies) from a terminal opened with **Run as administrator** |
| Codex `/status` doesn't show `amazon-bedrock-runtime` | Check that `%ProgramData%\OpenAI\Codex\requirements.toml` exists, then fully restart the client |
| The ChatGPT app shows "Cannot select `amazon-bedrock` …" | You chose Amazon Bedrock in the app's sign-in screen. Sign in with an account instead; see [Use Codex with Bedrock](#6-use-codex-with-bedrock) |
| Codex gets `AccessDenied` although STS works | STS proves identity only. Send the model ID, Region, error and Request ID to the AWS administrator; never send credentials |
| **Windows protected your PC** | For a file supplied by your IT team, use **More info → Run anyway** if available. Otherwise ask IT for a signed copy |
| **Smart App Control blocked this app** | Ask IT for a signed copy |
| `This program was built without config/config.json, so it has no settings. Ask your IT team for a correctly built copy.` | Forward the message to IT and request a rebuilt program |
| `The settings built into this program are invalid:` | Forward the message and the listed field errors to IT; the built-in config must be corrected and the program rebuilt |
| Exit 3 / `Ask your IT team to install what's still missing:` | Send IT the preceding error and the missing items listed below it. The AWS profile may already be configured; IT can complete installation and rerun the program |
| `AWS CLI version 1 is installed. Codex needs version 2, which version 1 can't sit beside. Remove "AWS Command Line Interface" in Settings → Apps, then run this program again.` | Have IT remove version 1, then rerun the program to install version 2 |

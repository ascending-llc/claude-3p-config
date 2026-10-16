# ASCENDING Claude Desktop: Windows Employee Self-Service Installation Guide

This directory connects Claude Desktop on your Windows computer to models provided by your organization through Amazon Bedrock. There are three steps: install this project's credential program, complete IAM Identity Center authorization yourself, and import the Claude Desktop registry policy for your Windows account. Completing the first step does not complete the other two.

## Directory contents

| File | Purpose |
| --- | --- |
| `install.ps1` | Installs the program and `config.json`, and adds only the current user's `CCWB` AWS profile. |
| `credential-process-windows-amd64.exe` | This project's credential program for Windows x64. The script installs it as `credential-process.exe`. |
| `config.json` | Contains the organization's IAM Identity Center entry point, AWS account, permission set, and regions. It does not contain your sign-in token or static keys. |
| `Claude-CCWB.reg` | Writes the Bedrock, model, and `vendor-profile` settings for Claude Desktop to the current user's `HKCU\SOFTWARE\Policies\Claude`. Do not put this file in the Claude Desktop application folder. |

Obtain the complete `windows` directory through an organization-approved private channel. This release supports Windows x64 only and requires 64-bit PowerShell. Do not send your sign-in code, tokens, or AWS credentials to coworkers. Your account also needs the corresponding IAM Identity Center assignment.

## Step 1: Install the program and AWS profile

Open 64-bit PowerShell in the `windows` directory you received and run this as your own Windows user:

```powershell
.\install.ps1
```

If script execution is blocked, check the policies in the same window:

```powershell
Get-ExecutionPolicy -List
```

If `MachinePolicy` or `UserPolicy` is not `Undefined`, do not bypass the organization policy; ask its owner for an approved installation method. If both are `Undefined` and the effective policy is `Restricted`, verify that the bundle came from an approved channel and that its files are intact. You may then set `RemoteSigned` for this PowerShell window only and rerun the script:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy RemoteSigned
.\install.ps1
```

If the effective policy is already `RemoteSigned` but the downloaded script is reported as unsigned, verify its source and contents before running `Unblock-File -Path .\install.ps1`, then rerun the script. A `Process` setting expires when the window closes. Do not set the policy to `Bypass` or change the machine-wide `LocalMachine` policy. If `AllSigned` is required or another security policy still blocks execution, stop rather than bypassing it. See [Microsoft's Windows PowerShell execution policy documentation](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.security/set-executionpolicy?view=powershell-5.1).

Do not select “Run as administrator.” The script rejects elevation to avoid installing into an administrator's user directory. It reads the `.exe` and `config.json` from its own directory, installs them in `%USERPROFILE%\claude-code-with-bedrock\`, and adds only this separate section to `%USERPROFILE%\.aws\config`. The path below is illustrative; do not copy it manually:

```ini
[profile CCWB]
region = __BEDROCK_REGION__
credential_process = "C:\Users\alice\claude-code-with-bedrock\credential-process.exe" --profile CCWB
```

The script checks target files, AWS profiles with the same name, static credentials, and machine-wide Claude policies before writing. It stops on conflicts without overwriting or cleaning them up. If it modifies an existing AWS configuration, it keeps a timestamped backup in the same directory and prints its path; it stops if the original file permissions cannot be preserved. It does not write the registry, sign you in, invoke a model, or change PowerShell execution policy.

## Step 2: Sign in yourself

In PowerShell, run:

```powershell
& "$env:USERPROFILE\claude-code-with-bedrock\credential-process.exe" --login --profile CCWB
```

Follow the terminal instructions to open the authorization page and complete your organization's IAM Identity Center sign-in. If your organization uses Entra ID, federation occurs through the sign-in flow; the bundle does not need your Entra ID secrets. If you cannot see the assigned AWS account or permission set, check your account assignment. Do not borrow a coworker's credentials.

### Sign out and sign in again

To repeat authorization on this computer, quit Claude Desktop completely and run in PowerShell:

```powershell
& "$env:USERPROFILE\claude-code-with-bedrock\credential-process.exe" --logout --profile CCWB
```

This clears only local sign-in and session caches. It does not remove the credential program, `config.json`, AWS profile, or Claude Desktop user policy, and it does not revoke a remote session. Then run the `--login` command above again. The device authorization flow should appear; follow its prompts.

## Step 3: Import the Claude Desktop user policy

First check whether a Claude policy already exists for the current user:

```powershell
reg.exe query "HKCU\SOFTWARE\Policies\Claude"
```

If the key does not exist, run:

```powershell
reg.exe import .\Claude-CCWB.reg
```

If the key exists, export a backup with Registry Editor and review its current values before importing. Do not overwrite settings from an unknown source without checking them. Quit and reopen Claude Desktop after importing. `HKCU` affects only the current Windows user and can be changed by that user, so it supports this self-service deployment but is not equivalent to an immutable organization-managed policy.

## Connect Jarvis MCP

If your organization includes `jarvis-registry` in the user policy, import `Claude-CCWB.reg`, reopen Claude Desktop, then go to **Customize → Connectors**. Find `jarvis-registry`, click **Connect**, complete browser authorization, and return to Claude Desktop to confirm it is connected. MCP authorization is separate from the IAM Identity Center sign-in above. Importing the policy alone does not authorize MCP, so its tools may not appear in chats before connection.

If the bundle should include this service but `jarvis-registry` is missing from Connectors, quit and reopen Claude Desktop, then confirm that the current user's policy was imported. If a machine-wide `HKLM\SOFTWARE\Policies\Claude` policy exists, Claude Desktop uses it in preference to `HKCU`. If the service appears but cannot connect, record the error shown in the app and ask IT or the project maintainers to check the MCP service and authorization configuration. Do not share authorization codes or tokens. This step has not yet been verified on an employee Windows computer. See the [Claude Desktop on 3P Managed MCP documentation](https://claude.com/docs/third-party/claude-desktop/extensions#managed-mcp-servers-admin).

## Existing machine-wide policies and rollback

If the script reports an existing `HKLM\SOFTWARE\Policies\Claude` key, do not import the `.reg` file yet. Claude Desktop uses the machine-wide `HKLM` policy in preference to the current user's `HKCU` policy. On your own computer, export that key with Registry Editor and confirm that it belongs entirely to a previous Claude configuration you installed and contains nothing else you need. Remove it yourself and rerun the installer only if you are certain and have permission on that computer. Otherwise, leave it in place. This script does not bypass Windows policy precedence.

For rollback, first read the installer's “Added in this run” summary:

- If the AWS profile was added in this run, open `%USERPROFILE%\.aws\config` in a text editor and remove only `[profile CCWB]` and its `region` and `credential_process` lines. Before restoring a backup, compare it with the current file so you do not overwrite other settings added later.
- If the program or `config.json` was added in this run, remove only that individual file. Do not delete your entire user directory or `%USERPROFILE%\.aws`.
- For the registry settings imported in this run, export a backup of `HKCU\SOFTWARE\Policies\Claude` first, then remove only the values listed in `Claude-CCWB.reg`. Delete the entire user-level key only if it did not exist before and no other settings have been added since. Do not delete an `HKLM` policy whose ownership you cannot confirm.

If the script reports that the AWS configuration is “missing from its original path,” do not retry immediately. Record the original path, backup path, and preserved temporary file, then review and restore the configuration manually. Do not treat the temporary file as a verified final configuration.

A “ready” message from the script confirms only that local files and the AWS profile have been configured. It does not verify sign-in, registry import, or Bedrock permissions. Installation has not yet been verified on an employee Windows computer. The `.exe`, `.reg`, and employee directory are generated by `python3 scripts/build_employee_dist.py`; generating them does not prove they work on an employee computer.

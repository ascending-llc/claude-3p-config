# ASCENDING Claude Desktop: macOS Employee Installation Guide

This directory connects Claude Desktop to models provided by your organization through Amazon Bedrock. There are three separate steps: install this project's credential program, complete IAM Identity Center authorization yourself, and install the macOS managed configuration. Completing the first step does not complete the other two.

## Directory contents

| File | Purpose |
| --- | --- |
| `install.sh` | Installs the program in the current user's home directory and adds the `ASCENDING-CCWB` AWS profile. |
| `credential-process-darwin-arm64`, `credential-process-darwin-amd64` | One binary each for Apple Silicon and Intel; the script installs only the matching one. |
| `config.json` | Contains the organization's IAM Identity Center entry point, AWS account, permission set, and regions. It does not contain your sign-in token or static keys. |
| `Claude-CCWB.mobileconfig` | Tells Claude Desktop to use Bedrock, the `ASCENDING-CCWB` profile, and the selected models. This is a macOS managed configuration, not a file to place in the Claude Desktop application folder. |

Obtain the complete `macos` directory through an organization-approved private channel. Do not send your sign-in code, tokens, or AWS credentials to coworkers. Your account must have the required IAM Identity Center assignment, and Claude Desktop must be installed on your Mac. This directory supports Apple Silicon and Intel Macs.

## Step 1: Install the program and AWS profile

In Terminal, change to the `macos` directory you received and run:

```bash
bash install.sh
```

Run this as your own user. Do not use `sudo`.

The script installs the binary matching your Mac at `~/claude-code-with-bedrock/credential-process`, places `config.json` in the same directory, and adds the following separate section to `~/.aws/config`. The path below is illustrative; do not copy it manually:

```ini
[profile ASCENDING-CCWB]
region = __BEDROCK_REGION__
credential_process = "/Users/alice/claude-code-with-bedrock/credential-process" --profile ASCENDING-CCWB
```

The script fills in the path from the current user's home directory. It does not overwrite other AWS profiles, `~/.aws/credentials`, or existing Claude Desktop managed policies. If an existing configuration with the same name differs, the script stops and identifies the file. If it changes an existing `~/.aws/config`, it prints the backup path; keep that information. Repeating the installation without content changes does not duplicate the AWS section.

## Step 2: Sign in yourself

In Terminal, run:

```bash
"$HOME/claude-code-with-bedrock/credential-process" --login --profile ASCENDING-CCWB
```

Follow the terminal instructions to open the authorization page and complete your organization's sign-in. If your organization uses Entra ID, federation occurs through the configured sign-in flow; the bundle does not need your Entra ID secrets. If authorization fails or you cannot see the assigned AWS account or permission set, ask IT to check your IAM Identity Center assignment. Do not change a coworker's credentials file.

### Sign out and sign in again

To repeat authorization on this Mac, quit Claude Desktop completely and run:

```bash
"$HOME/claude-code-with-bedrock/credential-process" --logout --profile ASCENDING-CCWB
```

This clears only local sign-in and session caches. It does not remove the credential program, `config.json`, AWS profile, or Claude Desktop managed policy, and it does not revoke a remote session. Then run the `--login` command above again. The device authorization flow should appear; follow its prompts.

## Step 3: Install the Claude Desktop managed configuration

Double-click `Claude-CCWB.mobileconfig` in this directory, then review and install it through macOS System Settings. If your organization manages this configuration through MDM, follow the process provided by IT. Quit and reopen Claude Desktop after installation.

If another Claude Desktop third-party managed policy was installed previously, check its source in System Settings first. An old policy may conflict with this one. `install.sh` does not remove old policies. Ask IT to handle an MDM policy that you cannot remove yourself; do not try to resolve it by deleting the Claude Desktop application folder.

## Connect Jarvis MCP

If your organization includes `jarvis-registry` in the managed configuration, install `Claude-CCWB.mobileconfig`, reopen Claude Desktop, then go to **Customize → Connectors**. Find `jarvis-registry`, click **Connect**, complete browser authorization, and return to Claude Desktop to confirm it is connected. MCP authorization is separate from the IAM Identity Center sign-in above. Adding the managed configuration alone does not authorize MCP, so its tools may not appear in chats before connection.

If the bundle should include this service but `jarvis-registry` is missing from Connectors, quit and reopen Claude Desktop, then confirm that `Claude-CCWB.mobileconfig` is installed and active. If the service appears but cannot connect, record the error shown in the app and ask IT or the project maintainers to check the MCP service and authorization configuration. Do not share authorization codes or tokens. See the [Claude Desktop on 3P Managed MCP documentation](https://claude.com/docs/third-party/claude-desktop/extensions#managed-mcp-servers-admin).

## Errors and rollback

- If the script reports a conflicting AWS profile, static credentials section, or target file, stop and give the reported path to IT or the project maintainers. The script does not overwrite it automatically.
- If it reports `AWS_CONFIG_FILE` or `AWS_SHARED_CREDENTIALS_FILE`, this installer supports only the default `~/.aws` location. First confirm whether you use custom AWS configuration paths.
- If macOS security policy blocks the binary, ask IT about organization signing or approval requirements. The script does not remove the quarantine attribute from downloaded files.
- For rollback, first read the script's “Added in this run” summary. If the AWS profile was added in this run, open `~/.aws/config` in a text editor and remove only `[profile ASCENDING-CCWB]` and its `region` and `credential_process` lines. If the binary or `config.json` was added in this run, remove only that individual file. Finally, remove the `.mobileconfig` installed in this run through macOS settings. Do not delete other AWS profiles, `~/.aws/credentials`, or the entire `~/.aws` directory. Before restoring a `~/.aws/config` backup, compare it with the current file so you do not overwrite settings added by other programs since installation.

A “ready” message from the script confirms only that the local files and AWS profile have been configured. It does not verify sign-in, policy installation, or Bedrock permissions. Installation has not yet been verified on another employee's Mac. The Windows directory is distributed separately and has not yet been verified on an employee Windows computer.

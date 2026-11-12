# Codex setup program: build, sign and distribute

This guide is for your organization's IT administrator. You review the source, build `ccwb-codex-setup.exe` with your organization's settings, sign it with your own key when needed, test it, and give it to employees. This repository supplies source; your IT team builds and distributes the executable.

Clone the repository, then run the commands below from its root:

```sh
git clone https://github.com/ascending-llc/coding-agent-3p.git
cd coding-agent-3p
```

For implementation testing, follow the [location-specific testing rules](test-plan.md#before-you-start). Mainland China implementers check the source and build artifacts without AWS sign-in or model requests; US-based testers perform the sign-in and Codex smoke tests.

## 1. What the program does

Use this table when reviewing the source before building:

| Side effect | Implementation |
| --- | --- |
| Merges `[sso-session ccwb]` and `[profile CCWB]` into the original user's `%USERPROFILE%\.aws\config`. Backs up an existing file as `.bak-*` before writing and asks before replacing existing CCWB sections. Other profiles are preserved. | [awsconfig.go](../cmd/ccwb-codex-setup/awsconfig.go) |
| Checks for AWS CLI v2. If it is missing, downloads `https://awscli.amazonaws.com/AWSCLIV2.msi`, verifies its Amazon signature and installs it with `msiexec`. An installed AWS CLI v1 stops setup instead of being replaced. | [steps_windows.go](../cmd/ccwb-codex-setup/steps_windows.go), [elevated_windows.go](../cmd/ccwb-codex-setup/elevated_windows.go) |
| Writes `%ProgramData%\OpenAI\Codex\requirements.toml`, backing up an existing file as `.bak-*` and asking before replacing different contents. Secures the directory and file so ordinary users can read but not change the settings. | [steps_windows.go](../cmd/ccwb-codex-setup/steps_windows.go), [elevated_windows.go](../cmd/ccwb-codex-setup/elevated_windows.go) |
| Requests one administrator (UAC) prompt per run only if the AWS CLI needs installing or the Codex settings need writing or securing. The user's AWS config is written before elevation, even if a different administrator approves UAC. | [main.go](../cmd/ccwb-codex-setup/main.go), [steps_windows.go](../cmd/ccwb-codex-setup/steps_windows.go) |
| If the user agrees, runs `aws sso login --profile CCWB` and `aws sts get-caller-identity --profile CCWB` as the original user. Prints the role ARN and warns if the account differs from the built-in account. | [steps_windows.go](../cmd/ccwb-codex-setup/steps_windows.go) |
| Runs `codex app-server daemon restart` only if `codex` is on `PATH` and its CLI daemon is already running. The desktop app must be quit and reopened separately. | [steps_windows.go](../cmd/ccwb-codex-setup/steps_windows.go) |

The MSI download is the program's only direct network request. Optional sign-in and identity checking use the AWS CLI's own network access. The elevated process re-reads the built-in config; its command line contains only which installation steps to perform.

Start reading at [main.go](../cmd/ccwb-codex-setup/main.go) for the flow, [config/embed.go](../config/embed.go) for embedding, and [policy/](../policy/) for validation and rendering. [go.mod](../go.mod) has no `require` block: the Go program uses only the standard library. The separate [signing/Dockerfile](../signing/Dockerfile) is used only for signing; it adds the jsign jar to a pinned Java image and checks the jar's SHA-256.

## 2. Prerequisites

- A build machine running Windows, macOS or Linux, with Git and Go at least the version in [go.mod](../go.mod).
- Your organization's AWS prerequisites from the [admin guide](admin-guide.md#1-aws-prerequisites), including access to the configured model.
- A Windows test machine with the Codex CLI and/or ChatGPT desktop app installed. Prefer a standard user account with an IT administrator available to approve UAC.
- For signing, the tools and key access described in [section 5](#5-sign-it).

On Windows, install Git and Go, then open a new terminal:

```powershell
winget install --id Git.Git
winget install --id GoLang.Go
```

The setup program can install a missing AWS CLI. It does not install the Codex clients.

## 3. Write the config

Copy `config/config.example.json` to `config/config.json`:

```sh
cp config/config.example.json config/config.json
```

In Windows PowerShell, use:

```powershell
Copy-Item config\config.example.json config\config.json
```

Fill in `aws.*` and `codex.*` using the [AWS field table](admin-guide.md#shared-fields) and [Codex field table](admin-guide.md#codex-fields). The setup program needs only these two sections. You may remove `claude` and `macos_profile`; if present, their values are not validated for setup. The JSON is still decoded strictly: an unknown key or the wrong JSON type anywhere is an error.

Settings are checked when the program runs, not when it is compiled. A successful build does not prove the config is valid; [test-run it](#6-test-run-it) before distribution. Keep your real config out of Git.

## 4. Build

On macOS or Linux:

```sh
make codex-setup
```

On Windows PowerShell, where `make` is not required:

```powershell
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
go build -trimpath -o dist\ccwb-codex-setup.exe .\cmd\ccwb-codex-setup
```

Both produce `dist/ccwb-codex-setup.exe`, a 64-bit Windows executable that also runs on Windows 11 on Arm through x64 emulation. Employees need neither Go nor a separate config file.

Rebuild after changing `config/config.json` or pulling new source. The first line printed by the program identifies its Git revision; `+modified` means it was built with local source changes. Re-sign after rebuilding.

`make codex-setup` refuses to build without `config/config.json`. Direct `go build` can build without it, so fresh clones and CI still compile; that executable reports the missing config when run. [S8](test-plan.md#codex-setup-program-windows) checks both the missing-config and invalid-config cases on Windows.

## 5. Sign it

### 5.1 Do you need to sign?

| How employees get the executable | Recommendation |
| --- | --- |
| Intune, Configuration Manager or a file share on managed devices | Signing is optional unless your application-control policy requires it. An internal-CA certificate can be sufficient where that root is trusted; sign if AppLocker or WDAC allows applications by publisher. |
| Browser download, email or chat attachment | Use a publicly trusted certificate to help establish publisher trust. Unsigned downloads normally show a SmartScreen warning; a new signing certificate can still need to build reputation. |
| Unmanaged Windows 11 devices | Use a publicly trusted certificate. Enforcing Smart App Control can block an unsigned, unknown program without a per-app override. |

SmartScreen checks downloaded files carrying Mark of the Web. [Smart App Control](https://support.microsoft.com/en-gb/windows/security/threat-malware-protection/smart-app-control-frequently-asked-questions) is a separate check; Windows can turn it off automatically on enterprise-managed devices, so check its actual state. Ask IT for a signed copy if it blocks the program rather than instructing employees to turn it off.

A signature identifies your company in **Properties → Digital Signatures** and detects changes to the executable, including its built-in config. It does not prove the settings are correct; you still need the test run.

### 5.2 Find out where your key is

- If the certificate appears in `certmgr.msc` under **Personal → Certificates**, including through a USB token's driver, use [the Windows certificate store](#53-key-in-the-windows-certificate-store).
- If it is in Azure Artifact Signing, use [Artifact Signing](#54-azure-artifact-signing).
- For another cloud service, use [other cloud keys](#55-other-cloud-key-services).

If you have no signing certificate, Artifact Signing is a straightforward option for an eligible organization. Public Trust is available only in supported countries, and identity validation can take a few business days; check Microsoft's [setup prerequisites](https://learn.microsoft.com/azure/artifact-signing/quickstart) before choosing it.

Publicly trusted code-signing keys issued under the rules effective June 1, 2023 must be protected in certified hardware, such as a USB token, smart card, HSM or cloud signing service. Do not expect a public CA to deliver an exportable private key as a `.pfx`. An internal CA can issue an exportable PFX, but it is trusted only where that CA's root is trusted. See the [CA/Browser Forum requirements](https://cabforum.org/working-groups/code-signing/requirements/).

### 5.3 Key in the Windows certificate store

Use Windows PowerShell 5.1's built-in `Set-AuthenticodeSignature`; no signing tool installation is needed. Replace `<thumbprint>` with your certificate's thumbprint:

```powershell
Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert | Format-Table Subject, Thumbprint, NotAfter
$cert = Get-Item 'Cert:\CurrentUser\My\<thumbprint>'
Set-AuthenticodeSignature -FilePath dist\ccwb-codex-setup.exe -Certificate $cert -HashAlgorithm SHA256 -TimestampServer http://timestamp.digicert.com
```

Use the timestamp URL supplied by your CA, or a public timestamp service such as the one above. For this cmdlet, use an [`http://` timestamp URL](https://learn.microsoft.com/powershell/module/microsoft.powershell.security/set-authenticodesignature#-timestampserver): an `https://` URL can leave the file signed without a timestamp. Use an RSA code-signing certificate for this path; ECDSA rejection is tracked in [PowerShell issue 19436](https://github.com/PowerShell/PowerShell/issues/19436).

For an internal-CA PFX, import it first, replacing the file path:

```powershell
$password = Read-Host 'PFX password' -AsSecureString
Import-PfxCertificate -FilePath 'C:\path\company-signing.pfx' -CertStoreLocation Cert:\CurrentUser\My -Password $password
Remove-Variable password
```

A USB token or smart card may ask for its PIN when signing. Its driver must expose the certificate and private-key operation to Windows.

### 5.4 Azure Artifact Signing

#### One-time setup in your Azure account

Follow Microsoft's [Artifact Signing quickstart](https://learn.microsoft.com/azure/artifact-signing/quickstart):

1. Create an Artifact Signing account. Its region determines the endpoint, `https://<region>.codesigning.azure.net`. Creating the resources requires an appropriate role such as Contributor.
2. Create and complete a Public Trust organization identity validation. The person doing this needs the **Artifact Signing Identity Verifier** role.
3. Create a Public Trust certificate profile using that identity.
4. Give the signing person the **Artifact Signing Certificate Profile Signer** role on the profile.

Owner or Contributor does not automatically grant the signing role; see Microsoft's [role assignments](https://learn.microsoft.com/azure/artifact-signing/tutorial-assign-roles). These resources belong to your organization. Azure sign-in authorizes executable signing; it is separate from the AWS SSO sign-in used by Codex. Artifact Signing certificates are valid for three days, so always timestamp the signature.

#### Sign on Windows

Install Microsoft's documented client tools and Azure CLI, then open a new PowerShell and sign in:

```powershell
winget install -e --id Microsoft.AzureCLI
winget install -e --id Microsoft.Azure.ArtifactSigningClientTools
az login
```

The signing plugin uses your Azure CLI sign-in. Create `dist\metadata.json` with your endpoint, account name and profile name, replacing the angle-bracket values:

```json
{
  "Endpoint": "https://<region>.codesigning.azure.net",
  "CodeSigningAccountName": "<account>",
  "CertificateProfileName": "<certificate-profile>"
}
```

Use the compatible `signtool.exe` installed by the client tools, together with their `Azure.CodeSigning.Dlib.dll`. Locate the installed tools:

```powershell
Get-ChildItem 'C:\Program Files (x86)\Microsoft\ArtifactSigningClientTools' -Recurse -Include signtool.exe,Azure.CodeSigning.Dlib.dll | Select-Object -ExpandProperty FullName
```

Use the matching x64 SignTool and plugin. Replace the SignTool path below with the installed path and check that the plugin path matches your installation, then sign:

```powershell
$signTool = 'C:\path\from-client-tools\signtool.exe'
$dlib = 'C:\Program Files (x86)\Microsoft\ArtifactSigningClientTools\bin\Azure.CodeSigning.Dlib.dll'
& $signTool sign /v /fd SHA256 /tr http://timestamp.acs.microsoft.com /td SHA256 `
  /dlib $dlib `
  /dmdf dist\metadata.json dist\ccwb-codex-setup.exe
if ($LASTEXITCODE -ne 0) { throw 'Signing failed.' }
```

The exact SignTool path remains to be recorded by Windows test S10 and substituted here. See the [pending acceptance evidence](test-plan.md#codex-setup-program-windows) and Microsoft's [signing integration instructions](https://learn.microsoft.com/azure/artifact-signing/how-to-signing-integrations).

#### Sign on macOS or Linux

Install Azure CLI and either Docker or Podman, then run `az login`. Docker Desktop's free small-business tier requires fewer than 250 employees **and** less than $10 million in annual revenue; commercial use outside that tier needs a paid subscription. Check its [subscription terms](https://docs.docker.com/subscription-billing/desktop-license/). Podman can run the same image for free. On macOS, the chosen container engine's virtual machine must be running.

Build the image and obtain an access token, then replace `<region>`, `<account>` and `<certificate-profile>`:

```sh
(
  set -e
  docker build -t ccwb-jsign signing
  AZURE_CODESIGNING_TOKEN="$(az account get-access-token --resource https://codesigning.azure.net --query accessToken -o tsv)"
  export AZURE_CODESIGNING_TOKEN
  docker run --rm -e AZURE_CODESIGNING_TOKEN -v "$PWD/dist:/work" ccwb-jsign \
    --storetype TRUSTEDSIGNING --keystore '<region>.codesigning.azure.net' \
    --storepass env:AZURE_CODESIGNING_TOKEN --alias '<account>/<certificate-profile>' \
    --tsaurl http://timestamp.acs.microsoft.com/ --tsmode RFC3161 \
    --name 'CCWB Codex Setup' /work/ccwb-codex-setup.exe
)
```

The subshell stops at the first failed step and returns its exit status. The token obtained here exists only in that subshell; any existing token in the calling shell stays unchanged.

For Podman, replace both `docker` commands with `podman`. The environment-variable name is passed with `-e`; the token value is not placed on the command line or in the image. These jsign options also underpin `jarvis-registry-cli/scripts/sign-windows.sh`.

The container is for cloud keys only; use the Windows certificate-store path for a USB token. The Dockerfile pins its Java base image by digest and jsign by version and SHA-256. It does not contain your executable, config or signing credentials. CI does not build or run it; [S11](test-plan.md#codex-setup-program-windows) validates Docker and Podman signing separately.

### 5.5 Other cloud key services

For Azure Key Vault, AWS KMS, Google Cloud KMS, DigiCert KeyLocker, SSL.com eSigner and other supported services, use the same container with the `--storetype`, `--keystore`, `--storepass` and `--alias` documented for your service in the [jsign manual](https://ebourg.github.io/jsign/). Use its environment-variable or file-based credential options rather than putting secret values in command arguments.

### 5.6 Check the signature

On Windows:

```powershell
Get-AuthenticodeSignature dist\ccwb-codex-setup.exe | Format-List Status, SignerCertificate, TimeStamperCertificate
```

Before distribution, expect `Status` to be `Valid` on an employee's machine and a populated `TimeStamperCertificate`. A test signature made with the untrusted self-signed certificate in S9 checks the commands and timestamp only; it is not a trusted release signature.

### 5.7 Sign every build

Re-sign after every rebuild, always with a timestamp. Sign before the test run below, so you test the file employees receive. A valid signature and a timestamp are both required when distributing a signed build; check both after signing.

## 6. Test-run it

Use a Windows machine you control, ideally signed in as a standard user. Follow [S1–S8](test-plan.md#codex-setup-program-windows) for the detailed acceptance cases and testing-location limits.

1. **Dry run.** Double-click the executable. Check its first-line revision and the summary's account, regions, SSO details, model and effort. At `Proceed? [Y/n]`, answer `n`. It prints `Cancelled. Nothing was changed.` and exits 1. This is a normal interactive cancellation, not a command-line mode.
2. **Check config errors.** A missing embedded config prints `This program was built without config/config.json, so it has no settings. Ask your IT team for a correctly built copy.` Invalid settings print `The settings built into this program are invalid:` followed by validation errors and `Ask your IT team for a correctly built copy.` Fix the config, rebuild and re-sign.
3. **Full run.** Run it again and answer `y`. Approve UAC if requested. At `Sign in to AWS now to check the settings? [Y/n]`, complete the optional AWS sign-in if your testing location permits it. Check that the role ARN and account are correct, that the original user's `.aws\config` has exactly one `[profile CCWB]` and one `[sso-session ccwb]`, and that `%ProgramData%\OpenAI\Codex\requirements.toml` exists. In a new terminal, `aws --version` must show `aws-cli/2`.
4. **Rerun.** Without changing settings or permissions, there is no UAC prompt. The program prints `.aws\config already has the CCWB settings.` and `The Codex settings file is already current.` Optional sign-in can still be offered.

On Windows the console waits at `Press Enter to close.` before exiting. When run from PowerShell, inspect `$LASTEXITCODE` after closing it:

| Exit code | Meaning |
| --- | --- |
| 0 | The setup flow finished without a fatal error or missing requested administrator work. Optional sign-in and restart warnings are separate; this code alone does not prove authentication or model access. |
| 1 | Missing or invalid built-in config, initial cancellation, AWS CLI v1, or another setup error. |
| 2 | Arguments were supplied; ordinary use takes no arguments because settings are built in. |
| 3 | The administrator step was declined or failed, or requested work is still missing. The user-level AWS config step has already run; inspect the error and any missing-component list. |

If a user declines replacing existing AWS sections or Codex settings, those settings are left unchanged; do not interpret completion as proof that they match this build. Resolve that choice before handing out the executable.

## 7. Smoke-test Codex

US-based testers run [C1 and C2](test-plan.md#codex) in the Codex CLI. `/status` must show `amazon-bedrock-runtime` and the intended model, reasoning effort and Region; C2 checks a `bedrock-ok` response. C2 makes a real model request.

Then quit the ChatGPT app from the system tray and open it from the Start menu before running C2 there. Closing its window is not enough: the app has its own server and may already be running from Windows startup. If the app asks for an account sign-in, follow the [Windows user guide](windows-user-guide.md#6-use-codex-with-bedrock); AWS authentication is separate.

Do not treat successful AWS STS output as proof that the configured model is available. Record the exact checks completed in the test plan.

## 8. Distribute

Send the tested executable through an internal channel and point employees to [section 6 of the Windows user guide](windows-user-guide.md#6-use-codex-with-bedrock). It embeds your AWS account ID, SSO start URL, role and other config values. Handle it like `config/config.json` and `dist/`: those values are internal, and the files stay out of Git.

### Managed fleets

You can deploy AWS CLI v2 and the generated `dist/windows/codex-requirements-CCWB.toml` as `%ProgramData%\OpenAI\Codex\requirements.toml` through Intune or Group Policy. Setup skips elevation only when the AWS CLI is available and the Codex file matches the embedded requirements byte for byte **and** both directory and file pass its permission checks:

- The `Codex` directory is owned by Administrators, has inheritance disabled, and has exactly three explicit allow rules: SYSTEM and Administrators full control, Users read and execute, inheritable by files and subdirectories.
- `requirements.toml` is owned by Administrators, SYSTEM or TrustedInstaller. Its inheritance is enabled and its only rules are the three inherited grants above, with no additional explicit grants or denies.

The [permission checks](../cmd/ccwb-codex-setup/elevate.go) compare well-known SIDs and rights rather than localized names. Ordinary users can read the requirements but cannot edit them. If the content matches but permissions do not, setup still requests UAC to secure them. IT should also keep the parent `OpenAI` directory under a trusted owner; the program warns if that owner can replace the `Codex` directory.

### Updates

After changing config or source, rebuild, re-sign if applicable, test, and redistribute. On a configured machine, different CCWB sections or Codex requirements are shown as old and new values and require confirmation before replacement. Existing files are backed up next to the originals as `.bak-*`.

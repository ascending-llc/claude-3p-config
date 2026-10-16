# ASCENDING Claude Desktop Windows employee installer.
# Installs only the credential program, config.json, and named AWS profile.
# It does not sign in or import registry settings.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$profileName = 'ASCENDING-CCWB'
$awsRegion = '__BEDROCK_REGION__'
$binaryWritten = $false
$configWritten = $false
$awsWritten = $false
$backupPath = $null
$temporaryPath = $null
$awsConfig = $null
$replacementAttempted = $false
$preserveTemporary = $false

function Assert-OrdinaryPath {
    param(
        [Parameter(Mandatory = $true)][string] $Path,
        [Parameter(Mandatory = $true)][bool] $Directory
    )

    if (-not (Test-Path -LiteralPath $Path)) { return }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Path is a symlink or reparse point; refusing to write: $Path"
    }
    if ($item.PSIsContainer -ne $Directory) {
        throw "Unexpected path type: $Path"
    }
}

function Assert-SameOrMissingFile {
    param(
        [Parameter(Mandatory = $true)][string] $Source,
        [Parameter(Mandatory = $true)][string] $Target
    )

    Assert-OrdinaryPath -Path $Target -Directory $false
    if (-not (Test-Path -LiteralPath $Target)) { return }
    $sourceHash = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
    $targetHash = (Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash
    if ($sourceHash -ne $targetHash) {
        throw "Existing file differs; it will not be overwritten: $Target"
    }
}

function Read-StrictUtf8 {
    param([Parameter(Mandatory = $true)][string] $Path)

    [byte[]] $bytes = [System.IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -ge 2 -and
        (($bytes[0] -eq 0xFF -and $bytes[1] -eq 0xFE) -or
         ($bytes[0] -eq 0xFE -and $bytes[1] -eq 0xFF))) {
        throw "AWS file is UTF-16; cannot safely append configuration: $Path"
    }
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and
        $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
        throw "AWS file has a UTF-8 BOM; review its encoding manually: $Path"
    }
    $utf8 = [System.Text.UTF8Encoding]::new($false, $true)
    $content = $utf8.GetString($bytes)
    if ($content.IndexOf([char] 0) -ge 0) {
        throw "AWS file contains a null character; cannot parse it safely: $Path"
    }
    return $content
}

function Get-ProfileState {
    param(
        [Parameter(Mandatory = $true)][AllowEmptyString()][string] $Content,
        [Parameter(Mandatory = $true)][string] $SectionName,
        [Parameter(Mandatory = $true)][string] $Region,
        [Parameter(Mandatory = $true)][string] $Command
    )

    $count = 0
    $active = $false
    $unexpected = $false
    $regionCount = 0
    $commandCount = 0
    $regionValue = $null
    $commandValue = $null
    foreach ($line in [regex]::Split($Content, "`r`n|`n|`r")) {
        if ($line -match '^[ \t]*\[([^\]]+)\][ \t]*(?:[#;].*)?$') {
            $active = ($Matches[1].Trim() -ceq $SectionName)
            if ($active) { $count++ }
            continue
        }
        if (-not $active -or $line -match '^[ \t]*(?:$|[#;])') { continue }
        if ($line -match '^[ \t]*([^= \t]+)[ \t]*=[ \t]*(.*)$') {
            $key = $Matches[1].ToLowerInvariant()
            $value = $Matches[2].Trim()
        } else {
            $unexpected = $true
            continue
        }
        switch ($key) {
            'region' {
                $regionCount++
                $regionValue = $value
            }
            'credential_process' {
                $commandCount++
                $commandValue = $value
            }
            default { $unexpected = $true }
        }
    }
    if ($count -eq 0) { return 'missing' }
    if ($count -eq 1 -and $regionCount -eq 1 -and $commandCount -eq 1 -and
        $regionValue -ceq $Region -and $commandValue -ceq $Command -and
        -not $unexpected) {
        return 'same'
    }
    return 'conflict'
}

function Assert-NoStaticCredentials {
    param(
        [Parameter(Mandatory = $true)][AllowEmptyString()][string] $Content,
        [Parameter(Mandatory = $true)][string] $Name
    )

    foreach ($line in [regex]::Split($Content, "`r`n|`n|`r")) {
        if ($line -match '^[ \t]*\[([^\]]+)\][ \t]*(?:[#;].*)?$') {
            $section = $Matches[1].Trim()
            if ($section -ceq $Name -or $section -ceq "profile $Name") {
                throw "AWS credentials file contains a section with the same name: $Name"
            }
        }
    }
}

try {
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'This installer supports Windows only.'
    }
    $architecture = if ($env:PROCESSOR_ARCHITEW6432) {
        $env:PROCESSOR_ARCHITEW6432
    } else {
        $env:PROCESSOR_ARCHITECTURE
    }
    if (-not [Environment]::Is64BitOperatingSystem -or $architecture -ne 'AMD64') {
        throw 'This bundle supports Windows x64 only.'
    }
    if (-not [Environment]::Is64BitProcess) {
        throw 'Run the installer in 64-bit PowerShell to read machine-wide registry policies correctly.'
    }
    $currentIdentity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $currentPrincipal = [System.Security.Principal.WindowsPrincipal]::new($currentIdentity)
    if ($currentPrincipal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Run as the employee in a regular 64-bit PowerShell session, not as administrator.'
    }
    if ($null -ne [Environment]::GetEnvironmentVariable('AWS_CONFIG_FILE') -or
        $null -ne [Environment]::GetEnvironmentVariable('AWS_SHARED_CREDENTIALS_FILE')) {
        throw 'AWS_CONFIG_FILE or AWS_SHARED_CREDENTIALS_FILE is set; this installer supports only the default location.'
    }
    if (Test-Path -LiteralPath 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Claude') {
        throw 'A machine-wide Claude policy (HKLM) exists and takes precedence over the current user policy; review it first.'
    }

    $employeeHome = $env:USERPROFILE
    if ([string]::IsNullOrWhiteSpace($employeeHome) -or
        -not [System.IO.Path]::IsPathRooted($employeeHome) -or
        -not [System.IO.Directory]::Exists($employeeHome)) {
        throw 'USERPROFILE is not a valid current user directory.'
    }
    $packageDirectory = $PSScriptRoot
    $sourceBinary = Join-Path $packageDirectory 'credential-process-windows-amd64.exe'
    $sourceConfig = Join-Path $packageDirectory 'config.json'
    foreach ($sourcePath in @($sourceBinary, $sourceConfig)) {
        Assert-OrdinaryPath -Path $sourcePath -Directory $false
        if (-not [System.IO.File]::Exists($sourcePath) -or
            (Get-Item -LiteralPath $sourcePath).Length -eq 0) {
            throw "The bundle is missing a nonempty file: $sourcePath"
        }
    }

    $installDirectory = Join-Path $employeeHome 'claude-code-with-bedrock'
    $targetBinary = Join-Path $installDirectory 'credential-process.exe'
    $targetConfig = Join-Path $installDirectory 'config.json'
    $awsDirectory = Join-Path $employeeHome '.aws'
    $awsConfig = Join-Path $awsDirectory 'config'
    $awsCredentials = Join-Path $awsDirectory 'credentials'
    $expectedCommand = '"' + $targetBinary + '" --profile ' + $profileName

    Assert-OrdinaryPath -Path $installDirectory -Directory $true
    Assert-OrdinaryPath -Path $awsDirectory -Directory $true
    Assert-SameOrMissingFile -Source $sourceBinary -Target $targetBinary
    Assert-SameOrMissingFile -Source $sourceConfig -Target $targetConfig
    Assert-OrdinaryPath -Path $awsConfig -Directory $false
    Assert-OrdinaryPath -Path $awsCredentials -Directory $false

    if ([System.IO.File]::Exists($awsCredentials)) {
        [string] $credentialsContent = Read-StrictUtf8 -Path $awsCredentials
        Assert-NoStaticCredentials -Content $credentialsContent -Name $profileName
    }
    $awsContent = ''
    $profileState = 'missing'
    if ([System.IO.File]::Exists($awsConfig)) {
        $awsContent = [string] (Read-StrictUtf8 -Path $awsConfig)
        $profileState = Get-ProfileState -Content $awsContent -SectionName "profile $profileName" -Region $awsRegion -Command $expectedCommand
    }
    if ($profileState -eq 'conflict') {
        throw "AWS configuration already has a different [profile $profileName]: $awsConfig"
    }

    # Write only after all preflight checks pass; do not change Claude Desktop registry policies.
    [System.IO.Directory]::CreateDirectory($installDirectory) | Out-Null
    [System.IO.Directory]::CreateDirectory($awsDirectory) | Out-Null
    if (-not [System.IO.File]::Exists($targetBinary)) {
        [System.IO.File]::Copy($sourceBinary, $targetBinary, $false)
        $binaryWritten = $true
    }
    if (-not [System.IO.File]::Exists($targetConfig)) {
        [System.IO.File]::Copy($sourceConfig, $targetConfig, $false)
        $configWritten = $true
    }

    if ($profileState -eq 'missing') {
        $lineBreak = "`r`n"
        $separator = if ($awsContent.Length -eq 0) {
            ''
        } elseif ($awsContent.EndsWith("`n") -or $awsContent.EndsWith("`r")) {
            $lineBreak
        } else {
            $lineBreak + $lineBreak
        }
        $newSection = "[profile $profileName]${lineBreak}region = $awsRegion${lineBreak}" +
            "credential_process = $expectedCommand${lineBreak}"
        $newContent = $awsContent + $separator + $newSection
        $utf8 = [System.Text.UTF8Encoding]::new($false, $true)
        $suffix = [Guid]::NewGuid().ToString('N').Substring(0, 8)
        $temporaryPath = Join-Path $awsDirectory (".config.ascending-ccwb.$suffix")
        [System.IO.File]::WriteAllText($temporaryPath, $newContent, $utf8)
        if ([System.IO.File]::Exists($awsConfig)) {
            $timestamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
            $backupPath = Join-Path $awsDirectory ("config.ascending-ccwb.backup.$timestamp.$suffix")
            $replacementAttempted = $true
            [System.IO.File]::Replace($temporaryPath, $awsConfig, $backupPath, $false)
        } else {
            $replacementAttempted = $true
            [System.IO.File]::Move($temporaryPath, $awsConfig)
        }
        $temporaryPath = $null
        $replacementAttempted = $false
        $awsWritten = $true
    }

    Write-Output "Local files are ready: ${targetBinary}, ${targetConfig}"
    Write-Output "AWS profile is ready: $profileName ($awsConfig)"
    Write-Output "Added in this run: credential program=$binaryWritten, config.json=$configWritten, AWS profile=$awsWritten."
    if ($backupPath -and [System.IO.File]::Exists($backupPath)) {
        Write-Output "Original AWS configuration backup: $backupPath"
    }
    Write-Output "Next, run this yourself: & `"$targetBinary`" --login --profile $profileName"
    Write-Output 'Then import Claude-CCWB.reg yourself and reopen Claude Desktop.'
} catch {
    [Console]::Error.WriteLine("Installation incomplete: $($_.Exception.Message)")
    [Console]::Error.WriteLine("Added in this run: credential program=$binaryWritten, config.json=$configWritten, AWS profile=$awsWritten.")
    if ($backupPath -and [System.IO.File]::Exists($backupPath)) {
        [Console]::Error.WriteLine("Original AWS configuration backup: $backupPath")
    }
    if ($replacementAttempted -and $awsConfig -and
        -not [System.IO.File]::Exists($awsConfig)) {
        $preserveTemporary = $true
        [Console]::Error.WriteLine("Warning: AWS configuration is missing from its original path: $awsConfig; review the backup and temporary file before retrying or overwriting anything.")
        if ($temporaryPath -and [System.IO.File]::Exists($temporaryPath)) {
            [Console]::Error.WriteLine("Temporary file preserved for review: $temporaryPath")
        }
    }
    [Console]::Error.WriteLine('Review only the paths added in this run. Do not delete other AWS profiles, credentials, or registry policies.')
    exit 1
} finally {
    if ($temporaryPath -and [System.IO.File]::Exists($temporaryPath)) {
        if (-not $preserveTemporary) {
            try {
                [System.IO.File]::Delete($temporaryPath)
            } catch {
                [Console]::Error.WriteLine("Cannot remove temporary file; review it manually: $temporaryPath; $($_.Exception.Message)")
            }
        }
    }
}

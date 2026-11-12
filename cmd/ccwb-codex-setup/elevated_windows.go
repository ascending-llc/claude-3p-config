package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ascending-llc/claude-3p-config/policy"
)

const (
	awsCLIInstallerURL    = "https://awscli.amazonaws.com/AWSCLIV2.msi"
	awsCLIDownloadTimeout = 5 * time.Minute
	msiRestartRequired    = 3010
)

func runElevated(args []string, cfg []byte) int {
	if len(args) != 2 || args[0] != "-elevated" {
		return exitElevatedConfig
	}

	steps, err := parseInstallSteps(args[1])
	if err != nil {
		return exitElevatedConfig
	}

	s, err := policy.ParseCodexSetup(bytes.NewReader(cfg))
	if err != nil {
		return exitElevatedConfig
	}

	code := 0
	if steps.awsCLI {
		code = installAWSCLI()
	}

	if steps.requirements {
		fileCode, fileErr := installRequirements(s)
		if fileErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, fileErr)
		}

		if code == 0 {
			code = fileCode
		}
	}

	return code
}

func requirementsPath() (string, error) {
	base := os.Getenv("ProgramData")
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("ProgramData must be an absolute path")
	}

	return filepath.Join(base, "OpenAI", "Codex", "requirements.toml"), nil
}

func powershellCommand(script string) *exec.Cmd {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)

	// PowerShell 7 can pass its module paths through Go to Windows PowerShell
	// 5.1, whose built-in cmdlets then fail to load. Let it rebuild its defaults.
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "PSModulePath") {
			cmd.Env = append(cmd.Env, entry)
		}
	}

	return cmd
}

func readACL(path string) (pathACL, error) {
	script := `$ErrorActionPreference = 'Stop'; $acl = Get-Acl -LiteralPath ` + powershellQuote(path) + `;
'owner=' + $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value;
'protected=' + $acl.AreAccessRulesProtected;
foreach ($r in $acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
  'rule={0} {1} {2} inh={3} inherited={4}' -f $r.IdentityReference.Value, $r.AccessControlType, [int]$r.FileSystemRights, $r.InheritanceFlags, $r.IsInherited
}`

	cmd := powershellCommand(script)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return pathACL{}, fmt.Errorf("reading permissions for %s: %s: %s", path, err.Error(), strings.TrimSpace(stderr.String()))
	}

	acl, err := parseACL(output)
	if err != nil {
		return pathACL{}, fmt.Errorf("parsing permissions for %s: %s: %s", path, err.Error(), strings.TrimSpace(string(output)))
	}

	return acl, nil
}

func installAWSCLI() int {
	dir, err := os.MkdirTemp("", "ccwb-awscli-*")
	if err != nil {
		return exitDownload
	}

	defer func() { _ = os.RemoveAll(dir) }()

	msi := filepath.Join(dir, "AWSCLIV2.msi")
	if downloadErr := downloadAWSCLI(msi); downloadErr != nil {
		return exitDownload
	}

	script := `$ErrorActionPreference = 'Stop'; $s = Get-AuthenticodeSignature -LiteralPath ` + powershellQuote(msi) + `;
$s.Status;
if ($null -ne $s.SignerCertificate) { $s.SignerCertificate.SubjectName.Format($true) }`

	output, err := powershellCommand(script).Output()
	if err != nil || !validAWSSignature(output) {
		return exitSignature
	}

	err = exec.Command("msiexec.exe", "/i", msi, "/qn", "/norestart", "/l*v", filepath.Join(dir, "awscli-install.log")).Run()
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == msiRestartRequired {
		return 0
	}

	return exitInstall
}

func downloadAWSCLI(path string) error {
	client := &http.Client{Timeout: awsCLIDownloadTimeout}

	response, err := client.Get(awsCLIInstallerURL)
	if err != nil {
		return err
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading the AWS CLI: HTTP %d", response.StatusCode)
	}

	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(file, response.Body); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}

func installRequirements(s policy.CodexSetup) (int, error) {
	path, err := requirementsPath()
	if err != nil {
		return exitPermissions, err
	}

	dir := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		return exitPermissions, mkdirErr
	}

	if lockErr := lockRequirementsFolder(dir); lockErr != nil {
		return exitPermissions, lockErr
	}

	existing, err := os.ReadFile(filepath.Clean(path))
	if err != nil && !os.IsNotExist(err) {
		return exitWrite, err
	}

	if err == nil {
		backup := path + ".bak-" + time.Now().Format("20060102-150405")
		if backupErr := backupRequirements(backup, existing); backupErr != nil {
			return exitWrite, backupErr
		}
	}

	if writeErr := writeRequirements(path, s.Requirements()); writeErr != nil {
		return exitWrite, writeErr
	}

	// Ownership also grants permission to change the file's ACL. The replacement
	// inherits the locked directory's rules, but must have a trusted owner too.
	if ownerErr := runICACLS(path, "/setowner", "*"+administratorSID); ownerErr != nil {
		return exitPermissions, ownerErr
	}

	acl, err := readACL(path)
	if err != nil {
		return exitPermissions, err
	}

	if !acl.fileLocked() {
		return exitPermissions, fmt.Errorf("the Codex settings file permissions do not match the required lock: %+v", acl)
	}

	return 0, nil
}

func lockRequirementsFolder(dir string) error {
	if err := runICACLS(dir, "/setowner", "*"+administratorSID); err != nil {
		return err
	}

	// Establish explicit access before removing any existing or inherited
	// grants. Fresh directories may have only inherited permissions.
	if err := runICACLS(dir, "/grant:r", "*"+systemSID+":(OI)(CI)F", "*"+administratorSID+":(OI)(CI)F", "*"+usersSID+":(OI)(CI)RX"); err != nil {
		return err
	}

	acl, err := readACL(dir)
	if err != nil {
		return err
	}

	// /grant:r replaces only each named SID's grants. Remove other explicit
	// grants and deny rules, so the lock also repairs an existing custom ACL.
	removed := make(map[string]bool)
	for _, rule := range acl.rules {
		if rule.inherited || (rule.allow && (rule.sid == systemSID || rule.sid == administratorSID || rule.sid == usersSID)) {
			continue
		}

		option := "/remove:d"
		if rule.allow {
			option = "/remove:g"
		}

		key := option + rule.sid
		if removed[key] {
			continue
		}

		if commandErr := runICACLS(dir, option, "*"+rule.sid); commandErr != nil {
			return commandErr
		}

		removed[key] = true
	}

	if commandErr := runICACLS(dir, "/inheritance:r"); commandErr != nil {
		return commandErr
	}

	acl, err = readACL(dir)
	if err != nil {
		return err
	}

	if !acl.directoryLocked() {
		return fmt.Errorf("the Codex folder permissions do not match the required lock: %+v", acl)
	}

	return nil
}

func runICACLS(path string, args ...string) error {
	output, err := exec.Command("icacls", append([]string{path}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls %q %v: %s: %s", path, args, err.Error(), strings.TrimSpace(string(output)))
	}

	return nil
}

func backupRequirements(path string, data []byte) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return err
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(path)

		return err
	}

	return nil
}

func writeRequirements(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ccwb-requirements-*")
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(file.Name()) }()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()

		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	return os.Rename(file.Name(), path)
}

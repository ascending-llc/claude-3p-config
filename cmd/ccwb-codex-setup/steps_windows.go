package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ascending-llc/claude-3p-config/policy"
)

const (
	awsV1Message  = `AWS CLI version 1 is installed. Codex needs version 2, which version 1 can't sit beside. Remove "AWS Command Line Interface" in Settings → Apps, then run this program again.`
	reopenMessage = `Quit the ChatGPT app from the system tray (right-click → Quit), then open it again. If it asks you to sign in, use your ChatGPT, Google or other account; don't choose Amazon Bedrock.`
)

func finishSteps(in *bufio.Scanner, stdout, stderr io.Writer, s policy.CodexSetup) int {
	defer func() { _, _ = fmt.Fprintln(stdout, reopenMessage) }()

	aws, major, err := findAWSCLI()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return 1
	}

	if major == 1 {
		_, _ = fmt.Fprintln(stderr, awsV1Message)

		return 1
	}

	requirements, err := prepareRequirements(in, stdout, stderr, s)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return 1
	}

	requested := installSteps{requirements: requirements, awsCLI: major == 0}
	code := 0

	if requested != (installSteps{}) {
		code = elevateAndCheck(requested, s, stderr)

		aws, major, err = findAWSCLI()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
		}
	}

	if err == nil && major == 2 {
		checkAWSSignIn(aws, in, stdout, stderr, s)
	}

	restartCodex(stdout, stderr)

	return code
}

func findAWSCLI() (string, int, error) {
	path, err := exec.LookPath("aws")
	if err == nil {
		return awsCLIVersion(path)
	}

	if errors.Is(err, exec.ErrDot) {
		return "", 0, fmt.Errorf("finding the AWS CLI: %s", err.Error())
	}

	for _, candidate := range []struct{ root, relative string }{
		{os.Getenv("ProgramFiles"), `Amazon\AWSCLIV2\aws.exe`},
		{os.Getenv("LOCALAPPDATA"), `Programs\Amazon\AWSCLIV2\aws.exe`},
	} {
		if candidate.root == "" {
			continue
		}

		path = filepath.Join(candidate.root, candidate.relative)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", 0, fmt.Errorf("finding the AWS CLI: %s", err.Error())
		}

		return awsCLIVersion(path)
	}

	return "", 0, nil
}

func awsCLIVersion(path string) (string, int, error) {
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("checking the AWS CLI version: %s", err.Error())
	}

	major, err := parseAWSVersion(output)
	if err != nil {
		return "", 0, err
	}

	if major != 1 && major != 2 {
		return "", 0, fmt.Errorf("unsupported AWS CLI version %d; install AWS CLI version 2", major)
	}

	return path, major, nil
}

func prepareRequirements(in *bufio.Scanner, stdout, stderr io.Writer, s policy.CodexSetup) (bool, error) {
	path, err := requirementsPath()
	if err != nil {
		return false, err
	}

	warnParentOwner(filepath.Dir(filepath.Dir(path)), stderr)

	existing, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return true, nil
	}

	if err != nil {
		return false, fmt.Errorf("reading the Codex settings file: %s", err.Error())
	}

	wanted := s.Requirements()
	if !bytes.Equal(existing, wanted) {
		if _, err = fmt.Fprintf(stdout, "Current Codex settings:\n%s\nNew Codex settings:\n%s\n", existing, wanted); err != nil {
			return false, err
		}

		replace, confirmErr := confirm(in, stdout, "Replace it? [Y/n]")
		if confirmErr != nil {
			return false, confirmErr
		}

		if !replace {
			_, confirmErr = fmt.Fprintln(stdout, "The Codex settings file was left unchanged.")
		}

		return replace, confirmErr
	}

	locked, err := requirementsLocked(path)
	if err != nil {
		return false, err
	}

	if locked {
		_, err := fmt.Fprintln(stdout, "The Codex settings file is already current.")

		return false, err
	}

	return true, nil
}

func warnParentOwner(dir string, stderr io.Writer) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}

	acl, err := readACL(dir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Warning: could not check the OpenAI folder's owner: %s\n", err.Error())

		return
	}

	if !trustedOwner(acl.owner) {
		_, _ = fmt.Fprintln(stderr, "Warning: the OpenAI folder is not owned by Administrators, SYSTEM or TrustedInstaller. Its owner can replace the Codex folder. Ask your IT team to check it.")
	}
}

func elevateAndCheck(requested installSteps, s policy.CodexSetup, stderr io.Writer) int {
	childCode, err := startElevated(requested)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
	} else if childCode != 0 {
		_, _ = fmt.Fprintln(stderr, childFailureMessage(childCode))
	}

	ready := checkInstalled(requested, s, stderr)

	missing := remainingSteps(requested, ready)
	if missing != (installSteps{}) {
		_, _ = fmt.Fprintln(stderr, "Ask your IT team to install what's still missing:")
		if missing.requirements {
			_, _ = fmt.Fprintln(stderr, "the Codex settings file")
		}

		if missing.awsCLI {
			_, _ = fmt.Fprintln(stderr, "the AWS CLI")
		}
	}

	if err != nil || childCode != 0 || missing != (installSteps{}) {
		return 3
	}

	return 0
}

func startElevated(steps installSteps) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding this setup program: %s", err.Error())
	}

	err = powershellCommand(elevationScript(exe, steps)).Run()
	if err == nil {
		return 0, nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}

	return 0, fmt.Errorf("starting the administrator step: %s", err.Error())
}

func checkInstalled(requested installSteps, s policy.CodexSetup, stderr io.Writer) installSteps {
	var ready installSteps

	if requested.awsCLI {
		_, major, err := findAWSCLI()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
		}

		ready.awsCLI = err == nil && major == 2
	}

	if requested.requirements {
		var err error

		ready.requirements, err = requirementsCurrent(s)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
		}
	}

	return ready
}

func requirementsCurrent(s policy.CodexSetup) (bool, error) {
	path, err := requirementsPath()
	if err != nil {
		return false, err
	}

	content, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("checking the Codex settings file: %s", err.Error())
	}

	if !bytes.Equal(content, s.Requirements()) {
		return false, nil
	}

	return requirementsLocked(path)
}

// A locked directory does not override a file's independent permissions.
func requirementsLocked(path string) (bool, error) {
	acl, err := readACL(filepath.Dir(path))
	if err != nil {
		return false, err
	}

	if !acl.directoryLocked() {
		return false, nil
	}

	acl, err = readACL(path)
	if err != nil {
		return false, err
	}

	return acl.fileLocked(), nil
}

func checkAWSSignIn(aws string, in *bufio.Scanner, stdout, stderr io.Writer, s policy.CodexSetup) {
	login, err := confirm(in, stdout, "Sign in to AWS now to check the settings? [Y/n]")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return
	}

	if !login {
		return
	}

	cmd := exec.Command(aws, "sso", "login", "--profile", policy.ProfileName)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr

	if err = cmd.Run(); err != nil {
		_, _ = fmt.Fprintf(stderr, "AWS sign-in failed: %s. Check the start URL, Region and role name.\n", err.Error())

		return
	}

	identity := exec.Command(aws, "sts", "get-caller-identity", "--profile", policy.ProfileName, "--query", "[Account,Arn]", "--output", "text")
	identity.Stderr = stderr

	output, err := identity.Output()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Checking the AWS identity failed: %s. Check the start URL, Region and role name.\n", err.Error())

		return
	}

	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		_, _ = fmt.Fprintln(stderr, "The AWS identity response did not contain an account and role ARN.")

		return
	}

	_, _ = fmt.Fprintf(stdout, "AWS role: %s\n", fields[1])
	if fields[0] != s.AWS.AccountID {
		_, _ = fmt.Fprintf(stderr, "Warning: signed in to AWS account %s, but the configured account is %s.\n", fields[0], s.AWS.AccountID)
	}
}

func restartCodex(stdout, stderr io.Writer) {
	codex, err := exec.LookPath("codex")
	if err != nil {
		return
	}

	// A spawned daemon inherits output handles; pipes would never reach EOF.
	if err := exec.Command(codex, "app-server", "daemon", "version").Run(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			_, _ = fmt.Fprintf(stderr, "Warning: could not check the Codex background service: %s\n", err.Error())
		}

		return
	}

	if err := exec.Command(codex, "app-server", "daemon", "restart").Run(); err != nil {
		_, _ = fmt.Fprintf(stderr, "Warning: could not restart the Codex background service: %s\n", err.Error())

		return
	}

	_, _ = fmt.Fprintln(stdout, "Restarted the Codex background service.")
}

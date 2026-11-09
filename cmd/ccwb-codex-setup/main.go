// Command ccwb-codex-setup sets up Codex on Amazon Bedrock for the current
// Windows user, using the config/config.json it was built with. Double-click
// it to run it.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/ascending-llc/claude-3p-config/config"
	"github.com/ascending-llc/claude-3p-config/policy"
)

func main() {
	code := run(os.Args[1:], config.JSON(), os.Stdin, os.Stdout, os.Stderr)
	if runtime.GOOS == "windows" {
		_, _ = fmt.Fprintln(os.Stdout, "Press Enter to close.")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}

	os.Exit(code)
}

func run(args []string, cfg []byte, stdin io.Reader, stdout, stderr io.Writer) int {
	info, _ := debug.ReadBuildInfo()
	_, _ = fmt.Fprintf(stdout, "ccwb-codex-setup %s: set up Codex on Amazon Bedrock (CCWB profile)\n", revision(info))

	if len(args) != 0 {
		_, _ = fmt.Fprintln(stderr, "ccwb-codex-setup takes no arguments; its settings are built in. Double-click it to run it.")

		return 2
	}

	if len(cfg) == 0 {
		_, _ = fmt.Fprintln(stderr, "This program was built without config/config.json, so it has no settings. Ask your IT team for a correctly built copy.")

		return 1
	}

	s, err := policy.ParseCodexSetup(bytes.NewReader(cfg))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "The settings built into this program are invalid:\n%s\nAsk your IT team for a correctly built copy.\n", err.Error())

		return 1
	}

	_, _ = fmt.Fprintf(stdout, `
AWS account ID: %s
Bedrock region: %s
SSO start URL: %s
SSO region: %s
SSO role name: %s
Codex default model: %s
Codex reasoning effort: %s

`, s.AWS.AccountID, s.AWS.BedrockRegion, s.AWS.SSOStartURL, s.AWS.SSORegion,
		s.AWS.SSORoleName, s.Codex.DefaultModel, s.Codex.ReasoningEffort)

	in := bufio.NewScanner(stdin)

	proceed, err := confirm(in, stdout, "Proceed? [Y/n]")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())

		return 1
	}

	if !proceed {
		_, _ = fmt.Fprintln(stdout, "Cancelled. Nothing was changed.")

		return 1
	}

	path, err := awsConfigPath()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())

		return 1
	}

	if err := updateAWSConfig(path, in, stdout, s); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())

		return 1
	}

	return 0
}

func revision(info *debug.BuildInfo) string {
	if info == nil {
		return "unknown"
	}

	var rev string

	var modified bool

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			rev = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if rev == "" {
		return "unknown"
	}

	rev = rev[:min(12, len(rev))]
	if modified {
		rev += "+modified"
	}

	return rev
}

func confirm(in *bufio.Scanner, out io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprint(out, prompt+" "); err != nil {
		return false, fmt.Errorf("writing confirmation: %s", err.Error())
	}

	if !in.Scan() {
		if err := in.Err(); err != nil {
			return false, fmt.Errorf("reading confirmation: %s", err.Error())
		}

		return false, nil
	}

	answer := strings.ToLower(strings.TrimSpace(in.Text()))

	return answer == "" || answer == "y" || answer == "yes", nil
}

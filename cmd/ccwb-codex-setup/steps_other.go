//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"runtime"

	"github.com/ascending-llc/claude-3p-config/policy"
)

func finishSteps(_ *bufio.Scanner, stdout, _ io.Writer, _ policy.CodexSetup) int {
	_, _ = fmt.Fprintf(stdout, "Skipping the Windows-only steps on %s.\n", runtime.GOOS)

	return 0
}

func runElevated(_ []string, _ []byte) int {
	return 2
}

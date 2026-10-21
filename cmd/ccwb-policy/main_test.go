package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesBothPlatforms(t *testing.T) {
	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-config", "testdata/config.json", "-out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	for _, name := range []string{
		"macos/Claude-CCWB.mobileconfig",
		"macos/aws-config-CCWB.ini",
		"windows/Claude-CCWB.reg",
		"windows/aws-config-CCWB.ini",
	} {
		path := filepath.Join(out, filepath.FromSlash(name))

		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing %s: %s", name, err.Error())

			continue
		}

		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}

		if !strings.Contains(stdout.String(), "wrote "+path) {
			t.Errorf("stdout does not report %s:\n%s", path, stdout.String())
		}
	}
}

// TestExamplesAreCurrent keeps the committed examples/ in step with the
// generator and config/config.example.json.
func TestExamplesAreCurrent(t *testing.T) {
	out := t.TempDir()
	repo := filepath.Join("..", "..")

	var stdout, stderr bytes.Buffer

	args := []string{"-example", "-config", filepath.Join(repo, exampleConfig), "-out", out}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		got, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}

		want, err := os.ReadFile(filepath.Join(repo, exampleOut, e.Name()))
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("%s/%s is stale; run make examples", exampleOut, e.Name())
		}
	}
}

func TestRunFailures(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(invalid, []byte(`{"aws": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		wantErr  string
		args     []string
		wantCode int
	}{
		{name: "missing config", args: []string{"-config", "testdata/missing.json"}, wantCode: 1, wantErr: "reading config"},
		{name: "invalid config", args: []string{"-config", invalid}, wantCode: 1, wantErr: "aws.account_id: must be a 12-digit AWS account ID"},
		{name: "unknown flag", args: []string{"-nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			args := append([]string{"-out", t.TempDir()}, tt.args...)
			if code := run(args, &stdout, &stderr); code != tt.wantCode {
				t.Errorf("exit code %d, want %d", code, tt.wantCode)
			}

			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

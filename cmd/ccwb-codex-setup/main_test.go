package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ascending-llc/claude-3p-config/policy"
)

type writeFunc func([]byte) (int, error)

var update = flag.Bool("update", false, "rewrite testdata/awsconfig expected output")

func TestMergeAWSConfig(t *testing.T) {
	snippet := setupFixture(t).AWSConfig()
	tests := []string{
		"empty", "new-crlf", "other-profiles-lf", "other-profiles-crlf", "bom",
		"outdated-ccwb", "only-sso-session", "only-profile", "already-current",
		"current-with-bom", "duplicate-ccwb", "spaced-header", "case-sensitive", "no-final-newline",
		"quoted-profile",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			existing := readFixture(t, "awsconfig/"+name+".in")

			newline := "\n"
			if name == "new-crlf" || name == "other-profiles-lf" {
				newline = "\r\n"
			}

			got, old, changed := mergeAWSConfig(existing, snippet, newline)

			wantPath := filepath.Join("testdata", "awsconfig", name+".want")
			if *update {
				writeTestFile(t, wantPath, got)
			}

			want := readTestFile(t, wantPath)
			if !bytes.Equal(got, want) {
				t.Errorf("merged config:\ngot  %q\nwant %q", got, want)
			}

			if changed != !bytes.Equal(existing, want) {
				t.Errorf("changed = %t, input equals expected = %t", changed, bytes.Equal(existing, want))
			}

			if bytes.HasPrefix(got, []byte{0xef, 0xbb, 0xbf}) {
				t.Error("output contains a leading BOM")
			}

			for _, header := range []string{"[profile CCWB]", "[sso-session ccwb]"} {
				if count := bytes.Count(got, []byte(header)); count != 1 {
					t.Errorf("%s appears %d times, want once", header, count)
				}
			}

			if (name == "already-current" || name == "current-with-bom") && old != "" {
				t.Errorf("unchanged target sections reported as replaced: %q", old)
			}

			if name == "outdated-ccwb" && (!strings.Contains(old, "https://old.example.com/start") || !strings.Contains(old, "region = us-west-2")) {
				t.Errorf("old does not include both replaced sections: %q", old)
			}

			if name == "quoted-profile" {
				for _, header := range []string{`[profile "my profile"]`, `[services "my services"]`, `[profile "team]dev"]`, "[profile work]"} {
					if strings.Contains(old, header) {
						t.Errorf("non-target section included in replaced text: %s", header)
					}
				}
			}

			again, _, changedAgain := mergeAWSConfig(got, snippet, newline)
			if changedAgain || !bytes.Equal(got, again) {
				t.Errorf("second merge changed the result: %q", again)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		input      string
		existing   string
		want       string
		args       []string
		output     []string
		errors     []string
		code       int
		backup     bool
		noQuestion bool
		noReplace  bool
	}{
		{name: "new config", config: "config.json", input: "\n", want: "empty.want"},
		{name: "case insensitive yes", config: "config.json", input: "YeS\n", want: "empty.want"},
		{
			name: "missing built-in config", existing: "outdated-ccwb.in", code: 1, noQuestion: true,
			errors: []string{"This program was built without config/config.json, so it has no settings. Ask your IT team for a correctly built copy."},
		},
		{
			name: "invalid config", config: "invalid-config.json", existing: "outdated-ccwb.in", code: 1, noQuestion: true,
			errors: []string{"The settings built into this program are invalid:", "aws.account_id:", "codex.reasoning_effort:", "Ask your IT team for a correctly built copy."},
		},
		{
			name: "argument rejected", config: "config.json", args: []string{"dropped.json"}, existing: "outdated-ccwb.in", code: 2, noQuestion: true,
			errors: []string{"ccwb-codex-setup takes no arguments; its settings are built in. Double-click it to run it."},
		},
		{
			name: "decline proceed", config: "config.json", input: "n\n", existing: "outdated-ccwb.in", code: 1,
			output: []string{"Cancelled. Nothing was changed."},
		},
		{
			name: "closed input", config: "config.json", existing: "outdated-ccwb.in", code: 1,
			output: []string{"Cancelled. Nothing was changed."},
		},
		{
			name: "unrecognized answer", config: "config.json", input: "maybe\n", code: 1,
			output: []string{"Cancelled. Nothing was changed."},
		},
		{
			name: "replace with shared input", config: "config.json", input: "y\ny\n", existing: "outdated-ccwb.in", want: "outdated-ccwb.want", backup: true,
			output: []string{"Replace them? [Y/n]", "https://old.example.com/start", "https://example.awsapps.com/start"},
		},
		{name: "decline replacement", config: "config.json", input: "y\nn\n", existing: "outdated-ccwb.in", output: []string{"Replace them? [Y/n]", "unchanged"}},
		{name: "closed replacement input", config: "config.json", input: "y\n", existing: "outdated-ccwb.in", output: []string{"unchanged"}},
		{
			name: "already current", config: "config.json", input: "y\n", existing: "already-current.in", noReplace: true,
			output: []string{`.aws\config already has the CCWB settings.`},
		},
		{name: "BOM only", config: "config.json", input: "y\n", existing: "current-with-bom.in", want: "current-with-bom.want", backup: true, noReplace: true},
		{name: "append to other profiles", config: "config.json", input: "y\n", existing: "other-profiles-lf.in", want: "other-profiles-lf.want", backup: true, noReplace: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".aws", "config")
			t.Setenv("AWS_CONFIG_FILE", path)

			var cfg, before []byte

			var beforeTime time.Time

			if tt.config != "" {
				cfg = readFixture(t, tt.config)
			}

			if tt.existing != "" {
				before = readFixture(t, "awsconfig/"+tt.existing)
				writeTestFile(t, path, before)

				beforeTime = time.Unix(946684800, 0)
				if err := os.Chtimes(path, beforeTime, beforeTime); err != nil {
					t.Fatal(err)
				}
			}

			var stdout, stderr bytes.Buffer
			if code := run(tt.args, cfg, strings.NewReader(tt.input), &stdout, &stderr); code != tt.code {
				t.Fatalf("exit = %d, want %d; stdout: %s; stderr: %s", code, tt.code, stdout.String(), stderr.String())
			}

			assertContains(t, stdout.String(), tt.output)
			assertContains(t, stderr.String(), tt.errors)

			if len(tt.errors) == 0 && stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %s", stderr.String())
			}

			if tt.noQuestion && strings.Contains(stdout.String(), "[Y/n]") {
				t.Errorf("asked a question before accepting config/arguments: %s", stdout.String())
			}

			if tt.noReplace && strings.Contains(stdout.String(), "Replace them?") {
				t.Errorf("asked to replace unchanged target sections: %s", stdout.String())
			}

			if !tt.noQuestion {
				assertSummary(t, stdout.String())
			}

			assertRunFiles(t, path, before, beforeTime, tt.want, tt.backup)
		})
	}
}

func TestRunPreservesChangesDuringConfirmation(t *testing.T) {
	for _, removed := range []bool{false, true} {
		name := "modified"
		if removed {
			name = "deleted"
		}

		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			t.Setenv("AWS_CONFIG_FILE", path)

			original := readFixture(t, "awsconfig/outdated-ccwb.in")
			writeTestFile(t, path, original)

			external := append(bytes.Clone(original), []byte("\n[profile added-elsewhere]\nregion = eu-west-1\n")...)
			changed := false

			var stdout, stderr bytes.Buffer

			output := writeFunc(func(p []byte) (int, error) {
				if !changed && bytes.Contains(p, []byte("Replace them?")) {
					changed = true

					if removed {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					} else {
						writeTestFile(t, path, external)
					}
				}

				return stdout.Write(p)
			})

			code := run(nil, readFixture(t, "config.json"), strings.NewReader("y\ny\n"), output, &stderr)
			if !changed || code != 1 || !strings.Contains(stderr.String(), "changed") {
				t.Fatalf("conflict not reported: changed=%t, exit=%d, stderr=%q", changed, code, stderr.String())
			}

			wantFiles := 0
			if !removed {
				wantFiles = 1

				if got := readTestFile(t, path); !bytes.Equal(got, external) {
					t.Errorf("external change overwritten: %q", got)
				}
			}

			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}

			if len(entries) != wantFiles {
				t.Errorf("conflict created a config, backup or temporary file: %v", entries)
			}
		})
	}
}

func TestEnsureAWSConfigUnchanged(t *testing.T) {
	tests := []struct {
		name     string
		original string
		current  string
		existed  bool
		exists   bool
		wantErr  bool
	}{
		{name: "unchanged", original: "same", current: "same", existed: true, exists: true},
		{name: "still absent"},
		{name: "changed", original: "before", current: "after", existed: true, exists: true, wantErr: true},
		{name: "deleted", original: "before", existed: true, wantErr: true},
		{name: "created", current: "new", exists: true, wantErr: true},
		{name: "empty file created", exists: true, wantErr: true},
		{name: "empty file deleted", existed: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if tt.exists {
				writeTestFile(t, path, []byte(tt.current))
			}

			err := ensureAWSConfigUnchanged(path, []byte(tt.original), tt.existed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v, want error=%t", err, tt.wantErr)
			}
		})
	}

	t.Run("read failure", func(t *testing.T) {
		if err := ensureAWSConfigUnchanged(t.TempDir(), nil, false); err == nil {
			t.Fatal("a directory was treated as an absent or unchanged file")
		}
	})
}

func (f writeFunc) Write(p []byte) (int, error) {
	return f(p)
}

func TestRunFileErrors(t *testing.T) {
	for _, name := range []string{"directory as config", "file as parent"} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir()
			if name == "file as parent" {
				parent := filepath.Join(path, "parent")
				writeTestFile(t, parent, []byte("keep me"))
				path = filepath.Join(parent, "config")
			}

			t.Setenv("AWS_CONFIG_FILE", path)

			var stdout, stderr bytes.Buffer
			if code := run(nil, readFixture(t, "config.json"), strings.NewReader("y\n"), &stdout, &stderr); code != 1 || stderr.Len() == 0 {
				t.Fatalf("file failure: exit=%d, stderr=%q", code, stderr.String())
			}
		})
	}
}

func TestRunInputError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	t.Setenv("AWS_CONFIG_FILE", path)

	var stdout, stderr bytes.Buffer

	inputErr := errors.New("cannot read confirmation")
	if code := run(nil, readFixture(t, "config.json"), iotest.ErrReader(inputErr), &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}

	assertContains(t, stderr.String(), []string{inputErr.Error()})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("config created after input error: %v", err)
	}
}

func TestAWSConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "default", want: filepath.Join(home, ".aws", "config")},
		{name: "explicit", env: filepath.Join(home, "custom"), want: filepath.Join(home, "custom")},
		{name: "tilde", env: "~/custom/config", want: filepath.Join(home, "custom", "config")},
		{name: "tilde home", env: "~", want: home},
		{name: "clean", env: home + string(filepath.Separator) + "unused/../config", want: filepath.Join(home, "config")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_CONFIG_FILE", tt.env)

			got, err := awsConfigPath()
			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Errorf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRevision(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{name: "nil", want: "unknown"},
		{name: "no settings", info: &debug.BuildInfo{}, want: "unknown"},
		{name: "revision", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "1234567890abcdef"}}}, want: "1234567890ab"},
		{name: "modified", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: "1234567890abcdef"}}}, want: "1234567890ab+modified"},
		{name: "clean", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "1234567890abcdef"}, {Key: "vcs.modified", Value: "false"}}}, want: "1234567890ab"},
		{name: "short", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}}, want: "abc"},
		{name: "no revision", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}}, want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := revision(tt.info); got != tt.want {
				t.Errorf("revision = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBackupAWSConfigDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.bak-20261106-120000")
	original := []byte("original backup\r\n")
	writeTestFile(t, path, original)

	if err := backupAWSConfig(path, []byte("replacement")); err == nil {
		t.Fatal("overwriting an existing backup succeeded")
	}

	if got := readTestFile(t, path); !bytes.Equal(got, original) {
		t.Errorf("existing backup was changed: %q", got)
	}
}

func TestWriteAWSConfigCleansUpFailedRename(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "config")
	child := filepath.Join(path, "keep")
	original := []byte("keep this file")
	writeTestFile(t, child, original)

	err := writeAWSConfig(path, []byte("new config"))
	if err == nil || !strings.Contains(err.Error(), "replacing the AWS config:") {
		t.Fatalf("expected rename failure, got %v", err)
	}

	if got := readTestFile(t, child); !bytes.Equal(got, original) {
		t.Errorf("original directory contents changed: %q", got)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Name() != "config" {
		t.Errorf("temporary files left after failed rename: %v", entries)
	}
}

func setupFixture(t *testing.T) policy.CodexSetup {
	t.Helper()

	setup, err := policy.ParseCodexSetup(bytes.NewReader(readFixture(t, "config.json")))
	if err != nil {
		t.Fatal(err)
	}

	return setup
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	return readTestFile(t, filepath.Join("testdata", name))
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, text string, parts []string) {
	t.Helper()

	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Errorf("output %q does not contain %q", text, part)
		}
	}
}

func assertSummary(t *testing.T, text string) {
	t.Helper()

	setup := setupFixture(t)
	assertContains(t, text, []string{
		"ccwb-codex-setup ", ": set up Codex on Amazon Bedrock (CCWB profile)",
		setup.AWS.AccountID, setup.AWS.BedrockRegion, setup.AWS.SSOStartURL,
		setup.AWS.SSORegion, setup.AWS.SSORoleName, setup.Codex.DefaultModel,
		setup.Codex.ReasoningEffort, "Proceed? [Y/n]",
	})

	if !strings.HasPrefix(text, "ccwb-codex-setup ") {
		t.Errorf("first line is not the version banner: %q", text)
	}

	if strings.Contains(text, "Press Enter to close.") {
		t.Error("run must never pause")
	}
}

func assertRunFiles(t *testing.T, path string, before []byte, beforeTime time.Time, wantName string, backup bool) {
	t.Helper()

	want := before
	if wantName != "" {
		want = readFixture(t, "awsconfig/"+wantName)
		if before == nil && runtime.GOOS == "windows" {
			want = bytes.ReplaceAll(want, []byte("\n"), []byte("\r\n"))
		}
	}

	if want == nil {
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Errorf("created configuration directory on cancelled run: %v", err)
		}

		return
	}

	got := readTestFile(t, path)
	if !bytes.Equal(got, want) {
		t.Errorf("config:\ngot  %q\nwant %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if wantName == "" && !info.ModTime().Equal(beforeTime) {
		t.Errorf("unchanged file was rewritten: modification time = %s", info.ModTime())
	}

	if runtime.GOOS != "windows" && before == nil {
		if info.Mode().Perm() != 0o600 {
			t.Errorf("config permissions = %o, want 600", info.Mode().Perm())
		}

		dir, statErr := os.Stat(filepath.Dir(path))
		if statErr != nil {
			t.Fatal(statErr)
		}

		if dir.Mode().Perm() != 0o700 {
			t.Errorf("directory permissions = %o, want 700", dir.Mode().Perm())
		}
	}

	backups, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}

	wantCount := 0
	if backup {
		wantCount = 1
	}

	if len(backups) != wantCount {
		t.Fatalf("backups = %v, want %d", backups, wantCount)
	}

	if backup {
		if data := readTestFile(t, backups[0]); !bytes.Equal(data, before) {
			t.Errorf("backup differs from original: %q", data)
		}

		stamp := strings.TrimPrefix(backups[0], path+".bak-")
		if _, parseErr := time.ParseInLocation("20060102-150405", stamp, time.Local); parseErr != nil {
			t.Errorf("backup name: %s", parseErr)
		}
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1+wantCount {
		t.Errorf("unexpected files left after update: %v", entries)
	}
}

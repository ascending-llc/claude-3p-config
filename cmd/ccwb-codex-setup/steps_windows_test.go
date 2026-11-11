package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRequirements(t *testing.T) {
	setup := setupFixture(t)
	outdated := setup
	outdated.AWS.BedrockRegion = "us-west-2"

	for _, tt := range []struct {
		name       string
		input      string
		existing   []byte
		want       bool
		wantPrompt bool
	}{
		{name: "missing", input: "next\n", want: true},
		{name: "replace", input: "y\nnext\n", existing: outdated.Requirements(), want: true, wantPrompt: true},
		{name: "keep", input: "n\nnext\n", existing: outdated.Requirements(), wantPrompt: true},
		{name: "matching but unlocked", input: "next\n", existing: setup.Requirements(), want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ProgramData", t.TempDir())

			path, err := requirementsPath()
			if err != nil {
				t.Fatal(err)
			}

			if tt.existing != nil {
				writeTestFile(t, path, tt.existing)
			}

			in := bufio.NewScanner(strings.NewReader(tt.input))

			var stdout, stderr bytes.Buffer

			got, err := prepareRequirements(in, &stdout, &stderr, setup)
			if err != nil || got != tt.want {
				t.Fatalf("prepare = %v, %v; want %v, nil", got, err, tt.want)
			}

			if prompted := strings.Contains(stdout.String(), "Replace it? [Y/n]"); prompted != tt.wantPrompt {
				t.Errorf("replacement prompt = %v, want %v; output %q", prompted, tt.wantPrompt, stdout.String())
			}

			if !in.Scan() || in.Text() != "next" {
				t.Errorf("remaining input = %q, want next", in.Text())
			}

			if tt.existing == nil {
				if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					t.Errorf("preparation created a settings file: %v", statErr)
				}
			} else if content := readTestFile(t, path); !bytes.Equal(content, tt.existing) {
				t.Errorf("preparation modified existing settings: %q", content)
			}

			current, err := requirementsCurrent(setup)
			if err != nil || current {
				t.Errorf("current = %v, %v; want false, nil", current, err)
			}
		})
	}
}

func TestPrepareCurrentRequirements(t *testing.T) {
	requireElevatedWindows(t)
	t.Setenv("ProgramData", t.TempDir())

	setup := setupFixture(t)
	if code := installRequirements(setup); code != 0 {
		t.Fatalf("install returned %d", code)
	}

	in := bufio.NewScanner(strings.NewReader("next\n"))

	var stdout, stderr bytes.Buffer

	install, err := prepareRequirements(in, &stdout, &stderr, setup)
	if err != nil || install {
		t.Fatalf("prepare = %v, %v; want false, nil", install, err)
	}

	if stdout.String() != "The Codex settings file is already current.\n" {
		t.Errorf("output = %q", stdout.String())
	}

	if !in.Scan() || in.Text() != "next" {
		t.Errorf("preparation consumed unexpected input: %q", in.Text())
	}

	current, err := requirementsCurrent(setup)
	if err != nil || !current {
		t.Errorf("current = %v, %v; want true, nil", current, err)
	}
}

func TestRequirementsRepairsFilePermissions(t *testing.T) {
	requireElevatedWindows(t)

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "explicit Users write", args: []string{"/grant:r", "*" + usersSID + ":W"}},
		{name: "disabled inheritance", args: []string{"/inheritance:d", "/grant", "*" + usersSID + ":W"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ProgramData", t.TempDir())

			setup := setupFixture(t)
			if code := installRequirements(setup); code != 0 {
				t.Fatalf("initial install returned %d", code)
			}

			path, err := requirementsPath()
			if err != nil {
				t.Fatal(err)
			}

			if permissionErr := runICACLS(path, tt.args...); permissionErr != nil {
				t.Fatal(permissionErr)
			}

			directory, err := readACL(filepath.Dir(path))
			if err != nil || !directory.directoryLocked() {
				t.Fatalf("file-only change altered the folder lock: %+v, %v", directory, err)
			}

			current, err := requirementsCurrent(setup)
			if err != nil || current {
				t.Fatalf("unsafe file reported current: %t, %v", current, err)
			}

			in := bufio.NewScanner(strings.NewReader("next\n"))

			var stdout, stderr bytes.Buffer

			repair, err := prepareRequirements(in, &stdout, &stderr, setup)
			if err != nil || !repair {
				t.Fatalf("file permission repair was not requested: %t, %v", repair, err)
			}

			if strings.Contains(stdout.String(), "[Y/n]") || strings.Contains(stdout.String(), "already current") {
				t.Errorf("unexpected prompt or success message: %q", stdout.String())
			}

			if !in.Scan() || in.Text() != "next" {
				t.Errorf("permission repair consumed confirmation input: %q", in.Text())
			}

			if code := installRequirements(setup); code != 0 {
				t.Fatalf("file permission repair returned %d", code)
			}

			assertRequirementsLocked(t, filepath.Dir(path))

			current, err = requirementsCurrent(setup)
			if err != nil || !current {
				t.Errorf("repaired settings not current: %t, %v", current, err)
			}

			backups, err := filepath.Glob(path + ".bak-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups = %v, %v; want one", backups, err)
			}

			if !bytes.Equal(readTestFile(t, backups[0]), setup.Requirements()) {
				t.Error("permission repair did not preserve the original settings in its backup")
			}
		})
	}
}

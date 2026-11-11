package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRequirements(t *testing.T) {
	requireElevatedWindows(t)
	t.Setenv("ProgramData", t.TempDir())

	setup := setupFixture(t)
	if code := installRequirements(setup); code != 0 {
		t.Fatalf("initial install returned %d", code)
	}

	path, err := requirementsPath()
	if err != nil {
		t.Fatal(err)
	}

	original := readTestFile(t, path)
	if !bytes.Equal(original, setup.Requirements()) {
		t.Fatalf("requirements = %q, want %q", original, setup.Requirements())
	}

	assertRequirementsLocked(t, filepath.Dir(path))

	setup.Codex.ReasoningEffort = "low"
	if code := installRequirements(setup); code != 0 {
		t.Fatalf("replacement returned %d", code)
	}

	if got := readTestFile(t, path); !bytes.Equal(got, setup.Requirements()) {
		t.Errorf("updated requirements = %q, want %q", got, setup.Requirements())
	}

	backups, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}

	if len(backups) != 1 {
		t.Fatalf("backups = %v, want one", backups)
	}

	if got := readTestFile(t, backups[0]); !bytes.Equal(got, original) {
		t.Errorf("backup = %q, want original %q", got, original)
	}

	assertRequirementsLocked(t, filepath.Dir(path))
}

func TestRequirementsRepairsExtraGrant(t *testing.T) {
	requireElevatedWindows(t)
	t.Setenv("ProgramData", t.TempDir())

	path, err := requirementsPath()
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}

	if grantErr := runICACLS(dir, "/grant", "*S-1-1-0:(OI)(CI)W"); grantErr != nil {
		t.Fatal(grantErr)
	}

	acl, err := readACL(dir)
	if err != nil {
		t.Fatal(err)
	}

	if acl.directoryLocked() {
		t.Fatal("folder with an extra Everyone grant was reported as locked")
	}

	if code := installRequirements(setupFixture(t)); code != 0 {
		t.Fatalf("repair returned %d", code)
	}

	assertRequirementsLocked(t, dir)
}

func TestRequirementsPathRejectsInvalidBase(t *testing.T) {
	for _, base := range []string{"", "relative"} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("ProgramData", base)

			if _, err := requirementsPath(); err == nil {
				t.Fatal("accepted a missing or relative ProgramData path")
			}
		})
	}
}

func TestElevatedRejectsInvalidInput(t *testing.T) {
	for _, args := range [][]string{nil, {"-elevated"}, {"-elevated", "unknown"}, {"-elevated", "requirements", "extra"}} {
		if code := runElevated(args, readFixture(t, "config.json")); code != exitElevatedConfig {
			t.Errorf("runElevated(%v) = %d, want %d", args, code, exitElevatedConfig)
		}
	}

	for _, cfg := range [][]byte{nil, readFixture(t, "invalid-config.json")} {
		if code := runElevated([]string{"-elevated", "requirements"}, cfg); code != exitElevatedConfig {
			t.Errorf("invalid config returned %d, want %d", code, exitElevatedConfig)
		}
	}
}

func TestRequirementsBackupDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requirements.toml.bak-existing")
	original := []byte("the existing backup\n")
	writeTestFile(t, path, original)

	if err := backupRequirements(path, []byte("new backup\n")); err == nil {
		t.Fatal("overwrote an existing backup")
	}

	if got := readTestFile(t, path); !bytes.Equal(got, original) {
		t.Errorf("backup changed: %q", got)
	}
}

func TestRequirementsWriteCleansUpFailedRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.toml")

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := writeRequirements(path, []byte("new settings\n")); err == nil {
		t.Fatal("replaced a directory with a settings file")
	}

	leftovers, err := filepath.Glob(filepath.Join(dir, ".ccwb-requirements-*"))
	if err != nil {
		t.Fatal(err)
	}

	if len(leftovers) != 0 {
		t.Errorf("temporary files remain after failure: %v", leftovers)
	}
}

func requireElevatedWindows(t *testing.T) {
	t.Helper()

	groups, err := exec.Command("whoami", "/groups").Output()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(groups), "S-1-16-12288") {
		if os.Getenv("CCWB_REQUIRE_ELEVATED_TESTS") == "1" {
			t.Fatal("Windows CI must run the requirements permissions tests in an elevated process")
		}

		t.Skip("the requirements permissions test needs an elevated Windows process")
	}
}

func assertRequirementsLocked(t *testing.T, dir string) {
	t.Helper()

	acl, err := readACL(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !acl.directoryLocked() {
		t.Fatalf("unexpected folder permissions: %+v", acl)
	}

	acl, err = readACL(filepath.Join(dir, "requirements.toml"))
	if err != nil {
		t.Fatal(err)
	}

	if !acl.fileLocked() {
		t.Fatalf("unexpected file permissions: %+v", acl)
	}
}

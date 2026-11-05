package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ascending-llc/claude-3p-config/policy"
)

func TestRunWritesBothPlatforms(t *testing.T) {
	tests := []struct {
		name     string
		products []string
		mutate   func(*policy.Config)
		want     []string
	}{
		{
			name:     "both products",
			products: []string{"claude", "codex"},
			want: []string{
				"macos/Claude-CCWB.mobileconfig", "macos/Codex-CCWB.mobileconfig",
				"macos/aws-config-CCWB.ini", "macos/codex-requirements-CCWB.toml",
				"windows/Claude-CCWB.reg", "windows/aws-config-CCWB.ini", "windows/codex-requirements-CCWB.toml",
			},
		},
		{
			name:     "Claude ignores invalid Codex values",
			products: []string{"claude"},
			mutate:   func(cfg *policy.Config) { cfg.Codex.ReasoningEffort = "hihg" },
			want: []string{
				"macos/Claude-CCWB.mobileconfig", "macos/aws-config-CCWB.ini",
				"windows/Claude-CCWB.reg", "windows/aws-config-CCWB.ini",
			},
		},
		{
			name:     "Codex without Claude config",
			products: []string{"codex"},
			mutate:   func(cfg *policy.Config) { cfg.Claude = nil },
			want: []string{
				"macos/Codex-CCWB.mobileconfig", "macos/aws-config-CCWB.ini", "macos/codex-requirements-CCWB.toml",
				"windows/aws-config-CCWB.ini", "windows/codex-requirements-CCWB.toml",
			},
		},
		{
			name:     "duplicate products",
			products: []string{"codex", "claude", "codex", "claude"},
			want: []string{
				"macos/Claude-CCWB.mobileconfig", "macos/Codex-CCWB.mobileconfig",
				"macos/aws-config-CCWB.ini", "macos/codex-requirements-CCWB.toml",
				"windows/Claude-CCWB.reg", "windows/aws-config-CCWB.ini", "windows/codex-requirements-CCWB.toml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := "testdata/config.json"
			if tt.mutate != nil {
				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}

				var cfg policy.Config
				if decodeErr := json.Unmarshal(data, &cfg); decodeErr != nil {
					t.Fatal(decodeErr)
				}

				tt.mutate(&cfg)

				data, err = json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}

				configPath = filepath.Join(t.TempDir(), "config.json")
				if err := os.WriteFile(configPath, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			out := t.TempDir()
			args := append([]string{"-config", configPath, "-out", out}, tt.products...)

			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
			}

			assertWrittenFiles(t, out, stdout.String(), tt.want)
		})
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

	wantFiles := []string{
		"Claude-CCWB.example.mobileconfig", "Claude-CCWB.example.reg", "Codex-CCWB.example.mobileconfig",
		"aws-config-CCWB.example.ini", "codex-requirements-CCWB.example.toml",
	}
	assertWrittenFiles(t, out, stdout.String(), wantFiles)

	for _, name := range wantFiles {
		got, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}

		want, err := os.ReadFile(filepath.Join(repo, exampleOut, name))
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("%s/%s is stale; run make examples", exampleOut, name)
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
		{name: "missing config", args: []string{"-config", "testdata/missing.json", "claude"}, wantCode: 1, wantErr: "reading config"},
		{name: "invalid config", args: []string{"-config", invalid, "claude"}, wantCode: 1, wantErr: "aws.account_id: must be a 12-digit AWS account ID"},
		{name: "selected section missing", args: []string{"-config", invalid, "codex"}, wantCode: 1, wantErr: "codex: is required when codex is selected"},
		{name: "unknown flag", args: []string{"-nope"}, wantCode: 2, wantErr: "flag provided but not defined"},
		{name: "no products", wantCode: 2, wantErr: "select at least one product"},
		{name: "unknown product", args: []string{"gemini"}, wantCode: 2, wantErr: `unknown product "gemini"`},
		{name: "example with product", args: []string{"-example", "claude"}, wantCode: 2, wantErr: "-example does not accept products"},
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

			if tt.wantCode == 2 && !strings.Contains(stderr.String(), "Usage: ccwb-policy") {
				t.Errorf("stderr does not include usage: %s", stderr.String())
			}

			if stdout.Len() != 0 {
				t.Errorf("failed command reported output files: %s", stdout.String())
			}
		})
	}
}

func assertWrittenFiles(t *testing.T, out, stdout string, want []string) {
	t.Helper()

	var got []string

	err := filepath.WalkDir(out, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		relative, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}

		got = append(got, filepath.ToSlash(relative))

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.Size() == 0 {
			t.Errorf("%s is empty", path)
		}

		if count := strings.Count(stdout, "wrote "+path+"\n"); count != 1 {
			t.Errorf("stdout reports %s %d times, want once", path, count)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("output files = %v, want %v", got, want)
	}
}

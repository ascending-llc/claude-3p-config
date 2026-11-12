package main

import (
	"strings"
	"testing"
)

func TestInstallSteps(t *testing.T) {
	for _, tt := range []struct {
		argument string
		want     installSteps
	}{
		{"requirements", installSteps{requirements: true}},
		{"awscli", installSteps{awsCLI: true}},
		{"requirements,awscli", installSteps{requirements: true, awsCLI: true}},
	} {
		t.Run(tt.argument, func(t *testing.T) {
			got, err := parseInstallSteps(tt.argument)
			if err != nil || got != tt.want {
				t.Fatalf("parseInstallSteps(%q) = %+v, %v; want %+v", tt.argument, got, err, tt.want)
			}

			if got.argument() != tt.argument {
				t.Errorf("argument = %q, want %q", got.argument(), tt.argument)
			}
		})
	}

	for _, value := range []string{"", "unknown", "requirements,unknown", "awscli,awscli", "requirements,requirements", "awscli,requirements", ",awscli", "requirements,", " awscli", "AWSCLI"} {
		if _, err := parseInstallSteps(value); err == nil {
			t.Errorf("parseInstallSteps(%q) succeeded", value)
		}
	}

	if value := (installSteps{}).argument(); value != "" {
		t.Errorf("empty task list = %q", value)
	}
}

func TestRemainingSteps(t *testing.T) {
	for _, tt := range []struct {
		name      string
		requested installSteps
		ready     installSteps
		want      installSteps
	}{
		{"all missing", installSteps{true, true}, installSteps{}, installSteps{true, true}},
		{"all ready", installSteps{true, true}, installSteps{true, true}, installSteps{}},
		{"only CLI succeeded", installSteps{true, true}, installSteps{awsCLI: true}, installSteps{requirements: true}},
		{"only settings succeeded", installSteps{true, true}, installSteps{requirements: true}, installSteps{awsCLI: true}},
		{"settings declined CLI ready", installSteps{awsCLI: true}, installSteps{awsCLI: true}, installSteps{}},
		{"settings declined CLI missing", installSteps{awsCLI: true}, installSteps{}, installSteps{awsCLI: true}},
		{"nothing requested", installSteps{}, installSteps{}, installSteps{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := remainingSteps(tt.requested, tt.ready); got != tt.want {
				t.Errorf("remainingSteps = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPowerShellQuoting(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"", "''"},
		{`C:\Users\O'Brien\Downloads\x.exe`, `'C:\Users\O''Brien\Downloads\x.exe'`},
		{`C:\a $name; (folder)\x.exe`, `'C:\a $name; (folder)\x.exe'`},
		{"'quoted'", "'''quoted'''"},
	} {
		if got := powershellQuote(tt.value); got != tt.want {
			t.Errorf("powershellQuote(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}

	want := `try { $p = Start-Process -FilePath 'C:\Users\O''Brien\Downloads\x.exe' -ArgumentList '-elevated','requirements,awscli' -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ErrorAction Stop } catch { exit 1223 }; exit $p.ExitCode`
	got := elevationScript(`C:\Users\O'Brien\Downloads\x.exe`, installSteps{true, true})

	if got != want {
		t.Errorf("elevation script:\ngot  %s\nwant %s", got, want)
	}
}

func TestParseAWSVersion(t *testing.T) {
	for _, tt := range []struct {
		output string
		want   int
	}{
		{"aws-cli/2.38.4 Python/3.14.6 Windows/11 script-exe/AMD64\r\n", 2},
		{"aws-cli/1.42.0 Python/3.11 Windows/10 botocore/1.40.0\n", 1},
		{"  aws-cli/2.39.6 Python/3.14.6\n", 2},
		{"aws-cli/3.0.0", 3},
		{"", 0},
		{"command not found", 0},
		{"aws-cli/2", 0},
		{"aws-cli/two.0", 0},
		{"aws-cli/0.1", 0},
		{"aws-cli/999999999999999999999999.0", 0},
	} {
		got, err := parseAWSVersion([]byte(tt.output))
		if got != tt.want || (err != nil) != (tt.want == 0) {
			t.Errorf("parseAWSVersion(%q) = %d, %v; want %d", tt.output, got, err, tt.want)
		}
	}
}

func TestValidAWSSignature(t *testing.T) {
	valid := string(readFixture(t, "windows/aws-signature.txt"))

	for _, tt := range []struct {
		name   string
		output string
		want   bool
	}{
		{"recorded signer", valid, true},
		{"CRLF", strings.ReplaceAll(valid, "\n", "\r\n"), true},
		{"unquoted organization", strings.ReplaceAll(valid, `"Amazon Web Services, Inc."`, awsCLISigner), true},
		{"old signer", strings.ReplaceAll(valid, awsCLISigner, "Amazon.com Services LLC"), false},
		{"different organization", strings.Replace(valid, `O="Amazon Web Services, Inc."`, "O=Example Inc.", 1), false},
		{"wrong status", strings.Replace(valid, "Valid", "NotTrusted", 1), false},
		{"missing organization", "Valid\nCN=Amazon Web Services, Inc.\n", false},
		{"duplicate organization", valid + "O=Amazon Web Services, Inc.\n", false},
		{"unclosed quotes", "Valid\nO=\"Amazon Web Services, Inc.\n", false},
		{"empty", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := validAWSSignature([]byte(tt.output)); got != tt.want {
				t.Errorf("validAWSSignature = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestDirectoryACL(t *testing.T) {
	locked := string(readFixture(t, "windows/locked-acl.txt"))

	for _, tt := range []struct {
		name    string
		output  string
		locked  bool
		wantErr bool
	}{
		{"recorded lock", locked, true, false},
		{"CRLF", strings.ReplaceAll(locked, "\n", "\r\n"), true, false},
		{"flag order", strings.ReplaceAll(locked, "ContainerInherit, ObjectInherit", "ObjectInherit, ContainerInherit"), true, false},
		{"inherited only", strings.ReplaceAll(strings.Replace(locked, "protected=True", "protected=False", 1), "inherited=False", "inherited=True"), false, false},
		{"wrong owner", strings.Replace(locked, "owner="+administratorSID, "owner="+usersSID, 1), false, false},
		{"SYSTEM directory owner", strings.Replace(locked, "owner="+administratorSID, "owner="+systemSID, 1), false, false},
		{"extra grant", locked + "rule=S-1-1-0 Allow 2032127 inh=None inherited=False\n", false, false},
		{"users can write", strings.Replace(locked, "1179817", "1179959", 1), false, false},
		{"plain RX missing synchronize", strings.Replace(locked, "1179817", "131241", 1), false, false},
		{"deny", strings.Replace(locked, "Allow", "Deny", 1), false, false},
		{"no inheritance flags", strings.Replace(locked, "ContainerInherit, ObjectInherit", "None", 1), false, false},
		{"duplicate SID", strings.Replace(locked, "rule="+systemSID, "rule="+administratorSID, 1), false, false},
		{"negative rights", strings.Replace(locked, "1179817", "-1610612736", 1), false, false},
		{"missing rules", "owner=" + administratorSID + "\nprotected=True\n", false, false},
		{"missing metadata", "", false, true},
		{"duplicate owner", locked + "owner=" + administratorSID, false, true},
		{"duplicate protection", locked + "protected=True", false, true},
		{"invalid SID", strings.Replace(locked, systemSID, "SYSTEM", 1), false, true},
		{"invalid mask", strings.Replace(locked, "1179817", "ReadAndExecute", 1), false, true},
		{"invalid allow", strings.Replace(locked, "Allow", "Unknown", 1), false, true},
		{"invalid protection", strings.Replace(locked, "protected=True", "protected=1", 1), false, true},
		{"invalid inheritance", strings.Replace(locked, "inherited=False", "inherited=1", 1), false, true},
		{"invalid flags", strings.Replace(locked, "ContainerInherit, ObjectInherit", "Unknown", 1), false, true},
		{"missing flags", strings.Replace(locked, " inh=ContainerInherit, ObjectInherit inherited=False", "", 1), false, true},
		{"unexpected output", locked + "unexpected line\n", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			acl, err := parseACL([]byte(tt.output))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseACL error = %v, want error %t", err, tt.wantErr)
			}

			if got := acl.directoryLocked(); got != tt.locked {
				t.Errorf("locked = %t, want %t", got, tt.locked)
			}
		})
	}
}

func TestFileACL(t *testing.T) {
	locked := string(readFixture(t, "windows/file-acl.txt"))

	for _, tt := range []struct {
		name   string
		output string
		locked bool
	}{
		{"inherited lock", locked, true},
		{"SYSTEM owner", strings.Replace(locked, "owner="+administratorSID, "owner="+systemSID, 1), true},
		{"TrustedInstaller owner", strings.Replace(locked, "owner="+administratorSID, "owner="+trustedInstallerSID, 1), true},
		{"CRLF", strings.ReplaceAll(locked, "\n", "\r\n"), true},
		{"explicit Users write", locked + "rule=" + usersSID + " Allow 1179959 inh=None inherited=False\n", false},
		{"explicit Everyone write", locked + "rule=S-1-1-0 Allow 1179959 inh=None inherited=False\n", false},
		{"inheritance disabled", strings.Replace(locked, "protected=False", "protected=True", 1), false},
		{"non-administrator owner", strings.Replace(locked, "owner="+administratorSID, "owner=S-1-5-21-1-2-3-1001", 1), false},
		{"duplicate SID", strings.Replace(locked, "rule="+systemSID, "rule="+administratorSID, 1), false},
		{"Users can write", strings.Replace(locked, "1179817", "1179959", 1), false},
		{"explicit rules", strings.ReplaceAll(locked, "inherited=True", "inherited=False"), false},
		{"deny rule", strings.Replace(locked, "Allow", "Deny", 1), false},
		{"directory inheritance flags", strings.Replace(locked, "inh=None", "inh=ContainerInherit, ObjectInherit", 1), false},
		{"missing grants", "owner=" + administratorSID + "\nprotected=False\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			acl, err := parseACL([]byte(tt.output))
			if err != nil {
				t.Fatal(err)
			}

			if got := acl.fileLocked(); got != tt.locked {
				t.Errorf("fileLocked = %t, want %t", got, tt.locked)
			}

			if acl.directoryLocked() {
				t.Error("file permissions were accepted as a locked directory")
			}
		})
	}
}

func TestChildFailureMessage(t *testing.T) {
	for _, tt := range []struct {
		want string
		code int
	}{
		{"invalid tasks or built-in settings", 10},
		{"download failed", 11},
		{"signature could not be verified", 12},
		{"installation failed", 13},
		{"folder could not be created or secured", 14},
		{"file could not be written", 15},
		{"administrator prompt was declined", 1223},
		{"exit code 42", 42},
	} {
		if got := childFailureMessage(tt.code); !strings.Contains(got, tt.want) {
			t.Errorf("childFailureMessage(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}

	if got := childFailureMessage(0); got != "" {
		t.Errorf("successful child message = %q", got)
	}
}

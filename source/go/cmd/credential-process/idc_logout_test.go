package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccwb-go/internal/config"
	"ccwb-go/internal/federation"
	"ccwb-go/internal/otel"
	"ccwb-go/internal/storage"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
)

func logoutTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(home, ".aws", "credentials"))
	return home
}

func captureLogoutStderr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	code := fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

func TestRunIDCLogoutClearsTargetProfileState(t *testing.T) {
	home := logoutTestHome(t)
	const target = "idc-target"
	const other = "idc-other"
	startURL := "https://d-1234567890.awsapps.com/start"
	tokenPath, err := ssocreds.StandardCachedTokenFilepath(startURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(`{"accessToken":"target"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	otherTokenPath, err := ssocreds.StandardCachedTokenFilepath("https://d-0987654321.awsapps.com/start")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherTokenPath, []byte(`{"accessToken":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{target, other} {
		if err := storage.SaveRefreshToken(profile, "session", "refresh-"+profile); err != nil {
			t.Fatal(err)
		}
		if err := storage.SaveMonitoringToken(profile, "session", "monitor-"+profile, map[string]interface{}{"exp": float64(time.Now().Add(time.Hour).Unix())}); err != nil {
			t.Fatal(err)
		}
		if err := otel.WriteCachedHeaders(profile, map[string]string{"profile": profile}, time.Now().Add(time.Hour).Unix()); err != nil {
			t.Fatal(err)
		}
		if err := storage.SaveToCredentialsFile(&federation.AWSCredentials{Version: 1, AccessKeyID: profile, SecretAccessKey: "secret", SessionToken: "session"}, profile); err != nil {
			t.Fatal(err)
		}
	}
	quotaDir := filepath.Join(home, "claude-code-with-bedrock")
	if err := os.MkdirAll(quotaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{target, other} {
		if err := storage.SaveQuotaState(profile); err != nil {
			t.Fatal(err)
		}
	}
	app := &credentialApp{profile: target, cfg: &config.ProfileConfig{AuthType: "idc", IDCStartURL: startURL, CredentialStorage: "session"}}
	for attempt := 1; attempt <= 2; attempt++ {
		code, stderr := captureLogoutStderr(t, app.runIDCLogout)
		if code != 0 || !strings.Contains(stderr, `Logged out profile "idc-target". Run --login to sign in again.`) {
			t.Fatalf("attempt %d: code=%d stderr=%q", attempt, code, stderr)
		}
	}
	for _, path := range []string{
		tokenPath,
		filepath.Join(home, ".claude-code-session", target+"-refresh.json"),
		filepath.Join(home, ".claude-code-session", target+"-monitoring.json"),
		filepath.Join(home, ".claude-code-session", target+"-otel-headers.json"),
		filepath.Join(home, ".claude-code-session", target+"-otel-headers.raw"),
		filepath.Join(quotaDir, ".quota-state-"+target+".json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("target cache still exists: %s (err=%v)", path, err)
		}
	}
	for _, path := range []string{
		otherTokenPath,
		filepath.Join(home, ".claude-code-session", other+"-refresh.json"),
		filepath.Join(home, ".claude-code-session", other+"-monitoring.json"),
		filepath.Join(home, ".claude-code-session", other+"-otel-headers.json"),
		filepath.Join(home, ".claude-code-session", other+"-otel-headers.raw"),
		filepath.Join(quotaDir, ".quota-state-"+other+".json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("other cache missing: %s (err=%v)", path, err)
		}
	}
	if creds, err := storage.ReadFromCredentialsFile(target); err != nil || creds != nil {
		t.Errorf("target credentials still present: creds=%+v err=%v", creds, err)
	}
	if creds, err := storage.ReadFromCredentialsFile(other); err != nil || creds == nil || creds.AccessKeyID != other {
		t.Errorf("other credentials changed: creds=%+v err=%v", creds, err)
	}
}

func TestRunIDCLogoutRejectsNonIDCProfile(t *testing.T) {
	logoutTestHome(t)
	app := &credentialApp{profile: "oidc", cfg: &config.ProfileConfig{AuthType: "oidc"}}
	code, stderr := captureLogoutStderr(t, app.runIDCLogout)
	if code == 0 || !strings.Contains(stderr, "--logout only supported for IAM Identity Center") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunIDCLogoutRejectsMissingStartURL(t *testing.T) {
	logoutTestHome(t)
	app := &credentialApp{profile: "idc", cfg: &config.ProfileConfig{AuthType: "idc"}}
	code, stderr := captureLogoutStderr(t, app.runIDCLogout)
	if code == 0 || !strings.Contains(stderr, "idc_start_url") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunIDCLogoutReportsTokenRemovalFailure(t *testing.T) {
	home := logoutTestHome(t)
	startURL := "https://d-1234567890.awsapps.com/start"
	tokenPath, err := ssocreds.StandardCachedTokenFilepath(startURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tokenPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tokenPath, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveRefreshToken("idc", "session", "refresh"); err != nil {
		t.Fatal(err)
	}
	app := &credentialApp{profile: "idc", cfg: &config.ProfileConfig{AuthType: "idc", IDCStartURL: startURL, CredentialStorage: "session"}}
	code, stderr := captureLogoutStderr(t, app.runIDCLogout)
	if code == 0 || !strings.Contains(stderr, "logout incomplete") || !strings.Contains(stderr, "SSO token") || strings.Contains(stderr, "Logged out profile") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude-code-session", "idc-refresh.json")); !os.IsNotExist(err) {
		t.Fatalf("refresh token was not cleared after SSO token failure: %v", err)
	}
}

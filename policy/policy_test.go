package policy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

const validConfig = "testdata/valid.json"

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

func TestRenderMatchesGolden(t *testing.T) {
	cfg, err := Load(validConfig, Options{})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string][]byte{
		"Claude-CCWB.mobileconfig": artifacts.Mobileconfig,
		"Claude-CCWB.reg":          artifacts.Reg,
		"aws-config-CCWB.ini":      artifacts.AWSConfig,
	} {
		path := filepath.Join("testdata", "golden", name)
		if *update {
			if err := os.WriteFile(path, got, 0o600); err != nil {
				t.Fatal(err)
			}

			continue
		}

		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from %s; run go test ./policy -update after checking the change", name, path)
		}
	}
}

func TestMobileconfigIsValidProfile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil is only available on macOS")
	}

	artifacts := mustRender(t, validConfig)
	path := filepath.Join(t.TempDir(), "profile.mobileconfig")

	if err := os.WriteFile(path, artifacts.Mobileconfig, 0o600); err != nil {
		t.Fatal(err)
	}

	if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil -lint: %s: %s", err.Error(), out)
	}

	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		t.Fatal(err)
	}

	var profile struct {
		PayloadContent []map[string]any `json:"PayloadContent"`
	}

	if err := json.Unmarshal(out, &profile); err != nil {
		t.Fatal(err)
	}

	if len(profile.PayloadContent) != 2 {
		t.Fatalf("got %d payloads, want 2", len(profile.PayloadContent))
	}

	desktop, claudeCode := profile.PayloadContent[0], profile.PayloadContent[1]
	if desktop["PayloadType"] != "com.anthropic.claudefordesktop" || claudeCode["PayloadType"] != "com.anthropic.claudecode" {
		t.Fatalf("unexpected payload types %v and %v", desktop["PayloadType"], claudeCode["PayloadType"])
	}

	serversJSON, ok := desktop["managedMcpServers"].(string)
	if !ok {
		t.Fatalf("managedMcpServers = %v, want a JSON string", desktop["managedMcpServers"])
	}

	var servers []map[string]any
	if err := json.Unmarshal([]byte(serversJSON), &servers); err != nil {
		t.Fatalf("managedMcpServers is not JSON: %s", err.Error())
	}

	if got := servers[0]["url"]; got != `https://mcp.example.com/mcp?team=a&b="c"` {
		t.Errorf("MCP URL did not survive plist escaping: %v", got)
	}

	env, ok := claudeCode["env"].(map[string]any)
	if !ok || env["AWS_PROFILE"] != ProfileName {
		t.Errorf("Claude Code env = %v, want AWS_PROFILE %s", claudeCode["env"], ProfileName)
	}
}

func TestRegFile(t *testing.T) {
	reg := mustRender(t, validConfig).Reg

	if !bytes.HasPrefix(reg, []byte{0xFF, 0xFE}) {
		t.Fatal("missing UTF-16LE byte order mark")
	}

	units := make([]uint16, (len(reg)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(reg[2+2*i:])
	}

	text := string(utf16.Decode(units))
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Error("found a line ending that is not CRLF")
	}

	lines := strings.Split(text, "\r\n")
	if lines[0] != "Windows Registry Editor Version 5.00" {
		t.Errorf("first line = %q", lines[0])
	}

	values := map[string]string{}
	key := ""
	valuePattern := regexp.MustCompile(`^"((?:[^"\\]|\\.)*)"="((?:[^"\\]|\\.)*)"$`)
	unescape := strings.NewReplacer(`\\`, `\`, `\"`, `"`)

	for _, line := range lines[1:] {
		switch m := valuePattern.FindStringSubmatch(line); {
		case strings.HasPrefix(line, "["):
			key = strings.Trim(line, "[]")
		case m != nil:
			values[key+`\`+unescape.Replace(m[1])] = unescape.Replace(m[2])
		case line != "":
			t.Errorf("unparseable line %q", line)
		}
	}

	if got := values[desktopRegKey+`\inferenceCredentialKind`]; got != "interactive" {
		t.Errorf("inferenceCredentialKind = %q", got)
	}

	var servers []desktopMCPServer
	if err := json.Unmarshal([]byte(values[desktopRegKey+`\managedMcpServers`]), &servers); err != nil {
		t.Fatalf("managedMcpServers: %s", err.Error())
	}

	wantOAuth := desktopOAuth{ClientID: "claude-desktop-ccwb", Scope: "mcp-proxy-ops", CallbackPort: defaultCallbackPort}
	if len(servers) != 2 || servers[0].OAuth == nil || *servers[0].OAuth != wantOAuth || servers[1].OAuth != nil {
		t.Errorf("managedMcpServers = %+v, want OAuth %+v on the first server only", servers, wantOAuth)
	}

	var settings struct {
		Env            map[string]string `json:"env"`
		ModelOverrides map[string]string `json:"modelOverrides"`
	}

	if err := json.Unmarshal([]byte(values[claudeCodeRegKey+`\Settings`]), &settings); err != nil {
		t.Fatalf("Claude Code Settings: %s", err.Error())
	}

	if got := settings.ModelOverrides["claude-haiku-4-5-20251001"]; !strings.HasSuffix(got, "/haiku00001") {
		t.Errorf("haiku override = %q", got)
	}
}

func TestDesktopSettingsDefaults(t *testing.T) {
	cfg, err := Load(validConfig, Options{})
	if err != nil {
		t.Fatal(err)
	}

	cfg.Desktop.MCPServers = nil

	settings, err := desktopSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := settings["managedMcpServers"]; ok {
		t.Error("managedMcpServers is set without any configured MCP servers")
	}

	var models []desktopModel
	if err := json.Unmarshal([]byte(settings["inferenceModels"]), &models); err != nil {
		t.Fatal(err)
	}

	want := []desktopModel{
		{Name: cfg.Models[0].AIPARN, LabelOverride: "Sonnet", AnthropicFamilyTier: TierSonnet},
		{Name: cfg.Models[2].AIPARN, LabelOverride: "Opus", AnthropicFamilyTier: TierOpus},
		{Name: cfg.Models[1].AIPARN, LabelOverride: "Haiku", AnthropicFamilyTier: TierHaiku},
	}
	if !slices.Equal(models, want) {
		t.Errorf("inferenceModels = %+v, want %+v", models, want)
	}
}

func TestMCPOAuthCallbackPort(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		wantPort int
	}{
		{name: "default", wantPort: defaultCallbackPort},
		{name: "configured", port: 40000, wantPort: 40000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{})
			if err != nil {
				t.Fatal(err)
			}

			cfg.Desktop.MCPServers[0].OAuth.CallbackPort = tt.port

			settings, err := desktopSettings(cfg)
			if err != nil {
				t.Fatal(err)
			}

			var servers []desktopMCPServer
			if err := json.Unmarshal([]byte(settings["managedMcpServers"]), &servers); err != nil {
				t.Fatal(err)
			}

			if got := servers[0].OAuth.CallbackPort; got != tt.wantPort {
				t.Errorf("callbackPort = %d, want %d", got, tt.wantPort)
			}
		})
	}
}

func TestDefaultTierAndOptionalOpus(t *testing.T) {
	tests := []struct {
		name          string
		defaultTier   string
		wantDefault   string
		wantDesktop   []string
		wantAvailable []string
		withoutOpus   bool
	}{
		{
			name:          "sonnet by default",
			wantDesktop:   []string{TierSonnet, TierOpus, TierHaiku},
			wantDefault:   TierSonnet,
			wantAvailable: []string{TierOpus, TierSonnet, TierHaiku},
		},
		{
			name:          "opus as default",
			defaultTier:   TierOpus,
			wantDesktop:   []string{TierOpus, TierSonnet, TierHaiku},
			wantDefault:   TierOpus,
			wantAvailable: []string{TierOpus, TierSonnet, TierHaiku},
		},
		{
			name:          "without opus",
			withoutOpus:   true,
			wantDesktop:   []string{TierSonnet, TierHaiku},
			wantDefault:   TierSonnet,
			wantAvailable: []string{TierSonnet, TierHaiku},
		},
		{
			name:          "haiku as default without opus",
			defaultTier:   TierHaiku,
			withoutOpus:   true,
			wantDesktop:   []string{TierHaiku, TierSonnet},
			wantDefault:   TierHaiku,
			wantAvailable: []string{TierSonnet, TierHaiku},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{})
			if err != nil {
				t.Fatal(err)
			}

			cfg.DefaultTier = tt.defaultTier
			if tt.withoutOpus {
				cfg.Models = cfg.Models[:2]
			}

			desktop, err := desktopSettings(cfg)
			if err != nil {
				t.Fatal(err)
			}

			var models []desktopModel
			if err := json.Unmarshal([]byte(desktop["inferenceModels"]), &models); err != nil {
				t.Fatal(err)
			}

			gotTiers := make([]string, 0, len(models))
			for _, m := range models {
				gotTiers = append(gotTiers, m.AnthropicFamilyTier)
			}

			if !slices.Equal(gotTiers, tt.wantDesktop) {
				t.Errorf("Desktop tiers = %v, want %v", gotTiers, tt.wantDesktop)
			}

			settings := claudeCodeSettings(cfg)
			env, _ := settings["env"].(map[string]string)
			wantModel, _ := cfg.model(tt.wantDefault)

			if env["ANTHROPIC_MODEL"] != wantModel.AIPARN {
				t.Errorf("ANTHROPIC_MODEL = %q, want the %s AIP", env["ANTHROPIC_MODEL"], tt.wantDefault)
			}

			if _, ok := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; ok == tt.withoutOpus {
				t.Errorf("ANTHROPIC_DEFAULT_OPUS_MODEL set = %v, want %v", ok, !tt.withoutOpus)
			}

			if got, _ := settings["availableModels"].([]string); !slices.Equal(got, tt.wantAvailable) {
				t.Errorf("availableModels = %v, want %v", got, tt.wantAvailable)
			}

			if got, _ := settings["modelOverrides"].(map[string]string); len(got) != len(tt.wantAvailable) {
				t.Errorf("modelOverrides = %v, want one entry per configured tier", got)
			}
		})
	}
}

func TestParseRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		mutate  func(c *Config)
		name    string
		wantErr []string
	}{
		{name: "short account ID", mutate: func(c *Config) { c.AWS.AccountID = "12345" }, wantErr: []string{"aws.account_id: must be a 12-digit AWS account ID"}},
		{name: "bad region", mutate: func(c *Config) { c.AWS.SSORegion = "useast1" }, wantErr: []string{"aws.sso_region: must be an AWS region name"}},
		{name: "http start URL", mutate: func(c *Config) { c.AWS.SSOStartURL = "http://example.awsapps.com/start" }, wantErr: []string{"aws.sso_start_url: must be an https URL"}},
		{name: "role name with space", mutate: func(c *Config) { c.AWS.SSORoleName = "Claude User" }, wantErr: []string{"aws.sso_role_name: must be a permission set name"}},
		{name: "no haiku model", mutate: func(c *Config) { c.Models = c.Models[:1] }, wantErr: []string{"models: must include a haiku model"}},
		{name: "duplicate tier", mutate: func(c *Config) { c.Models[1].Tier = TierSonnet }, wantErr: []string{`models[1].tier: "sonnet" is listed more than once`, "models: must include a haiku model"}},
		{name: "unknown tier", mutate: func(c *Config) { c.Models[0].Tier = "fable" }, wantErr: []string{`models[0].tier: must be "opus", "sonnet" or "haiku"`}},
		{
			name: "default tier without its model",
			mutate: func(c *Config) {
				c.Models = c.Models[:2]
				c.DefaultTier = TierOpus
			},
			wantErr: []string{`default_tier: must be the tier of a configured model, got "opus"`},
		},
		{name: "unknown default tier", mutate: func(c *Config) { c.DefaultTier = "Sonnet" }, wantErr: []string{`default_tier: must be the tier of a configured model, got "Sonnet"`}},
		{
			name:    "system inference profile instead of an AIP",
			mutate:  func(c *Config) { c.Models[0].AIPARN = "us.anthropic.claude-sonnet-5" },
			wantErr: []string{"models[0].aip_arn: must be an application inference profile ARN"},
		},
		{
			name: "AIP in another region",
			mutate: func(c *Config) {
				c.Models[0].AIPARN = "arn:aws:bedrock:us-west-2:123456789012:application-inference-profile/abc"
			},
			wantErr: []string{"models[0].aip_arn: is in us-west-2, but aws.bedrock_region is us-east-1"},
		},
		{
			name: "AIP in another account",
			mutate: func(c *Config) {
				c.Models[1].AIPARN = "arn:aws:bedrock:us-east-1:210987654321:application-inference-profile/abc"
			},
			wantErr: []string{"models[1].aip_arn: is in account 210987654321, but aws.account_id is 123456789012"},
		},
		{
			name:    "model ID of another tier",
			mutate:  func(c *Config) { c.Models[0].AnthropicModelID = "claude-haiku-4-5" },
			wantErr: []string{"models[0].anthropic_model_id: must be an Anthropic model ID of the sonnet tier"},
		},
		{name: "http MCP URL", mutate: func(c *Config) { c.Desktop.MCPServers[0].URL = "http://mcp.example.com" }, wantErr: []string{"desktop.mcp_servers[0].url: must be an https URL"}},
		{name: "bad MCP name", mutate: func(c *Config) { c.Desktop.MCPServers[0].Name = "-registry" }, wantErr: []string{"desktop.mcp_servers[0].name: must be letters"}},
		{
			name:    "duplicate MCP name",
			mutate:  func(c *Config) { c.Desktop.MCPServers[1].Name = "example-registry" },
			wantErr: []string{`desktop.mcp_servers[1].name: "example-registry" is listed more than once`},
		},
		{name: "OAuth without client ID", mutate: func(c *Config) { c.Desktop.MCPServers[0].OAuth.ClientID = "" }, wantErr: []string{"desktop.mcp_servers[0].oauth.client_id: must be a client ID without spaces"}},
		{name: "OAuth client ID with space", mutate: func(c *Config) { c.Desktop.MCPServers[0].OAuth.ClientID = "claude desktop" }, wantErr: []string{"desktop.mcp_servers[0].oauth.client_id: must be a client ID without spaces"}},
		{
			name:    "callback port in the Windows dynamic range",
			mutate:  func(c *Config) { c.Desktop.MCPServers[0].OAuth.CallbackPort = 53280 },
			wantErr: []string{"desktop.mcp_servers[0].oauth.callback_port: must be between 1024 and 49151, below the dynamic port range Windows reserves ports from, got 53280"},
		},
		{name: "privileged callback port", mutate: func(c *Config) { c.Desktop.MCPServers[0].OAuth.CallbackPort = 80 }, wantErr: []string{"desktop.mcp_servers[0].oauth.callback_port: must be between 1024 and 49151"}},
		{name: "malformed scope", mutate: func(c *Config) { c.Desktop.MCPServers[0].OAuth.Scope = "mcp-proxy-ops  servers-read" }, wantErr: []string{"desktop.mcp_servers[0].oauth.scope: must be space-separated OAuth scopes"}},
		{name: "bad identifier", mutate: func(c *Config) { c.Profile.Identifier = "claude" }, wantErr: []string{"profile.identifier: must be a reverse-DNS identifier"}},
		{name: "empty organization", mutate: func(c *Config) { c.Profile.Organization = " " }, wantErr: []string{"profile.organization: must not be empty"}},
		{
			name:    "placeholder left in",
			mutate:  func(c *Config) { c.AWS.SSORoleName = "REPLACE_WITH_PERMISSION_SET_NAME" },
			wantErr: []string{`aws.sso_role_name: replace the placeholder "REPLACE_WITH_PERMISSION_SET_NAME"`},
		},
		{
			name: "every failure is reported",
			mutate: func(c *Config) {
				c.AWS.AccountID = "1"
				c.Profile.DisplayName = ""
			},
			wantErr: []string{"aws.account_id:", "profile.display_name: must not be empty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{})
			if err != nil {
				t.Fatal(err)
			}

			tt.mutate(&cfg)

			mutated, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}

			_, err = Parse(bytes.NewReader(mutated), Options{})
			if err == nil {
				t.Fatal("Parse succeeded, want an error")
			}

			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	raw, err := os.ReadFile(validConfig)
	if err != nil {
		t.Fatal(err)
	}

	valid := string(raw)
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "unknown field", input: strings.Replace(valid, `"organization": "Example"`, `"organization": "Example", "extra": true`, 1), wantErr: `unknown field "extra"`},
		{name: "unknown MCP field", input: strings.Replace(valid, `"name": "static-tools"`, `"name": "static-tools", "transport": "sse"`, 1), wantErr: `unknown field "transport"`},
		{name: "unknown OAuth field", input: strings.Replace(valid, `"scope": "mcp-proxy-ops"`, `"scope": "mcp-proxy-ops", "client_secret": "x"`, 1), wantErr: `unknown field "client_secret"`},
		{
			name:    "OAuth as a boolean, the former automatic registration",
			input:   regexp.MustCompile(`"oauth": \{[^}]*\}`).ReplaceAllString(valid, `"oauth": true`),
			wantErr: "oauth of type policy.OAuth",
		},
		{name: "trailing data", input: valid + `{}`, wantErr: "unexpected data after the JSON object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.input == valid {
				t.Fatal("test input was not modified; the fixture changed")
			}

			_, err := Parse(strings.NewReader(tt.input), Options{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPlaceholders(t *testing.T) {
	const example = "../config/config.example.json"

	if _, err := Load(example, Options{AllowPlaceholders: true}); err != nil {
		t.Fatalf("example config with placeholders allowed: %s", err.Error())
	}

	_, err := Load(example, Options{})
	if err == nil {
		t.Fatal("example config loaded without AllowPlaceholders")
	}

	for _, field := range []string{"aws.account_id", "aws.sso_start_url", "aws.sso_role_name", "models[0].aip_arn", "models[1].aip_arn", "models[2].aip_arn", "desktop.mcp_servers[0].url", "desktop.mcp_servers[0].oauth.client_id"} {
		if !strings.Contains(err.Error(), field+": replace the placeholder") {
			t.Errorf("error does not flag %s:\n%s", field, err.Error())
		}
	}
}

func TestStableUUID(t *testing.T) {
	a, b := stableUUID("com.example.claude"), stableUUID("com.example.claude.desktop")
	if a != stableUUID("com.example.claude") {
		t.Error("stableUUID is not deterministic")
	}

	if a == b {
		t.Error("different names produced the same UUID")
	}

	format := regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-8[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$`)
	if !format.MatchString(a) {
		t.Errorf("%s is not an uppercase version 8 UUID", a)
	}
}

func mustRender(t *testing.T, path string) Artifacts {
	t.Helper()

	cfg, err := Load(path, Options{})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	return artifacts
}

package policy

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf16"
)

const validConfig = "testdata/valid.json"

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

func TestRenderMatchesGolden(t *testing.T) {
	cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string][]byte{
		"Claude-CCWB.mobileconfig":     artifacts.Mobileconfig,
		"Claude-CCWB.reg":              artifacts.Reg,
		"aws-config-CCWB.ini":          artifacts.AWSConfig,
		"Codex-CCWB.mobileconfig":      artifacts.CodexMobileconfig,
		"codex-requirements-CCWB.toml": artifacts.CodexRequirements,
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

	cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	cfg.MacOSProfile.IdentifierPrefix = "com.example.custom"

	artifacts, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "profile.mobileconfig")

	if writeErr := os.WriteFile(path, artifacts.Mobileconfig, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if out, lintErr := exec.Command("plutil", "-lint", path).CombinedOutput(); lintErr != nil {
		t.Fatalf("plutil -lint: %s: %s", lintErr.Error(), out)
	}

	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		t.Fatal(err)
	}

	var profile struct {
		PayloadUUID        string           `json:"PayloadUUID"`
		PayloadIdentifier  string           `json:"PayloadIdentifier"`
		PayloadDisplayName string           `json:"PayloadDisplayName"`
		PayloadContent     []map[string]any `json:"PayloadContent"`
	}

	if err := json.Unmarshal(out, &profile); err != nil {
		t.Fatal(err)
	}

	if len(profile.PayloadContent) != 2 {
		t.Fatalf("got %d payloads, want 2", len(profile.PayloadContent))
	}

	if profile.PayloadIdentifier != "com.example.custom.claude" || profile.PayloadDisplayName != "Claude on Amazon Bedrock (CCWB)" {
		t.Errorf("profile identity = %q / %q", profile.PayloadIdentifier, profile.PayloadDisplayName)
	}

	if profile.PayloadUUID != stableUUID("com.example.custom.claude") {
		t.Errorf("profile UUID = %q, want the UUID derived from the custom prefix", profile.PayloadUUID)
	}

	desktop, claudeCode := profile.PayloadContent[0], profile.PayloadContent[1]
	if desktop["PayloadIdentifier"] != "com.example.custom.claude.desktop" || claudeCode["PayloadIdentifier"] != "com.example.custom.claude.claudecode" {
		t.Errorf("payload identifiers = %v / %v", desktop["PayloadIdentifier"], claudeCode["PayloadIdentifier"])
	}

	if desktop["PayloadUUID"] != stableUUID("com.example.custom.claude.desktop") || claudeCode["PayloadUUID"] != stableUUID("com.example.custom.claude.claudecode") {
		t.Errorf("payload UUIDs = %v / %v, want UUIDs derived from the custom prefix", desktop["PayloadUUID"], claudeCode["PayloadUUID"])
	}

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

func TestCodexMobileconfigIsValidProfile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil is only available on macOS")
	}

	cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	cfg.MacOSProfile.IdentifierPrefix = "com.example.custom"

	artifacts, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "codex.mobileconfig")

	if writeErr := os.WriteFile(path, artifacts.CodexMobileconfig, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if out, lintErr := exec.Command("plutil", "-lint", path).CombinedOutput(); lintErr != nil {
		t.Fatalf("plutil -lint: %s: %s", lintErr.Error(), out)
	}

	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		t.Fatal(err)
	}

	var profile struct {
		PayloadUUID        string           `json:"PayloadUUID"`
		PayloadIdentifier  string           `json:"PayloadIdentifier"`
		PayloadDisplayName string           `json:"PayloadDisplayName"`
		PayloadScope       string           `json:"PayloadScope"`
		PayloadContent     []map[string]any `json:"PayloadContent"`
	}

	if err := json.Unmarshal(out, &profile); err != nil {
		t.Fatal(err)
	}

	if len(profile.PayloadContent) != 1 || profile.PayloadContent[0]["PayloadType"] != "com.openai.codex" {
		t.Fatalf("PayloadContent = %v, want one com.openai.codex payload", profile.PayloadContent)
	}

	if profile.PayloadIdentifier != "com.example.custom.codex" || profile.PayloadDisplayName != "Codex on Amazon Bedrock (CCWB)" {
		t.Errorf("profile identity = %q / %q", profile.PayloadIdentifier, profile.PayloadDisplayName)
	}

	if profile.PayloadContent[0]["PayloadIdentifier"] != "com.example.custom.codex.managed" {
		t.Errorf("payload identifier = %v", profile.PayloadContent[0]["PayloadIdentifier"])
	}

	if profile.PayloadUUID != stableUUID("com.example.custom.codex") || profile.PayloadContent[0]["PayloadUUID"] != stableUUID("com.example.custom.codex.managed") {
		t.Errorf("profile and payload UUIDs = %q / %v, want UUIDs derived from the custom prefix", profile.PayloadUUID, profile.PayloadContent[0]["PayloadUUID"])
	}

	if profile.PayloadScope != "User" {
		t.Errorf("PayloadScope = %q, want User", profile.PayloadScope)
	}

	// Codex decodes the key as Base64 TOML; it must round-trip to the file
	// that Windows and non-MDM macOS installs use. config_toml_base64 would
	// carry the legacy managed config, which this generator no longer writes.
	if _, ok := profile.PayloadContent[0]["config_toml_base64"]; ok {
		t.Error("payload has config_toml_base64; the defaults belong in the requirements")
	}

	encoded, _ := profile.PayloadContent[0]["requirements_toml_base64"].(string)

	got, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil {
		t.Fatalf("requirements_toml_base64: %s", decodeErr.Error())
	}

	if !bytes.Equal(got, artifacts.CodexRequirements) {
		t.Errorf("requirements_toml_base64 decodes to\n%s\nwant\n%s", got, artifacts.CodexRequirements)
	}
}

func TestProductSelection(t *testing.T) {
	tests := []struct {
		mutate     func(*Config)
		name       string
		products   []Product
		wantErr    []string
		wantClaude bool
		wantCodex  bool
	}{
		{name: "both products", products: []Product{ProductClaude, ProductCodex}, wantClaude: true, wantCodex: true},
		{name: "duplicate products", products: []Product{ProductCodex, ProductClaude, ProductCodex}, wantClaude: true, wantCodex: true},
		{name: "claude only", products: []Product{ProductClaude}, wantClaude: true},
		{name: "codex only", products: []Product{ProductCodex}, wantCodex: true},
		{name: "codex without claude section", products: []Product{ProductCodex}, mutate: func(c *Config) { c.Claude = nil }, wantCodex: true},
		{name: "claude without codex section", products: []Product{ProductClaude}, mutate: func(c *Config) { c.Codex = nil }, wantClaude: true},
		{name: "unselected invalid codex", products: []Product{ProductClaude}, mutate: func(c *Config) { c.Codex.ReasoningEffort = "hihg" }, wantClaude: true},
		{name: "unselected invalid claude", products: []Product{ProductCodex}, mutate: func(c *Config) { c.Claude.Models = nil }, wantCodex: true},
		{name: "missing selected claude", products: []Product{ProductClaude}, mutate: func(c *Config) { c.Claude = nil }, wantErr: []string{"claude: is required when claude is selected"}},
		{name: "missing selected codex", products: []Product{ProductCodex}, mutate: func(c *Config) { c.Codex = nil }, wantErr: []string{"codex: is required when codex is selected"}},
		{name: "no products", wantErr: []string{"products: must select at least one of claude or codex"}},
		{name: "unknown product", products: []Product{"gemini"}, wantErr: []string{`products: unknown product "gemini"`}},
		{name: "shared AWS is validated for claude", products: []Product{ProductClaude}, mutate: func(c *Config) { c.AWS.AccountID = "" }, wantErr: []string{"aws.account_id:"}},
		{name: "shared profile is validated for codex", products: []Product{ProductCodex}, mutate: func(c *Config) { c.MacOSProfile.IdentifierPrefix = "" }, wantErr: []string{"macos_profile.identifier_prefix:"}},
		{
			name:     "missing sections join shared errors",
			products: []Product{ProductClaude, ProductCodex},
			mutate: func(c *Config) {
				c.AWS.AccountID = ""
				c.Claude = nil
				c.Codex = nil
			},
			wantErr: []string{"aws.account_id:", "claude: is required when claude is selected", "codex: is required when codex is selected"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
			if err != nil {
				t.Fatal(err)
			}

			if tt.mutate != nil {
				tt.mutate(&cfg)
			}

			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err = Parse(bytes.NewReader(data), Options{Products: tt.products})
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("Parse succeeded, want an error")
				}

				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err.Error(), want)
					}
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if (cfg.Claude != nil) != tt.wantClaude || (cfg.Codex != nil) != tt.wantCodex {
				t.Fatalf("parsed sections: Claude = %v, Codex = %v", cfg.Claude, cfg.Codex)
			}

			artifacts, err := Render(cfg)
			if err != nil {
				t.Fatal(err)
			}

			if (len(artifacts.Mobileconfig) > 0) != tt.wantClaude || (len(artifacts.Reg) > 0) != tt.wantClaude {
				t.Errorf("Claude artifacts do not match selection %v", tt.products)
			}

			if (len(artifacts.CodexMobileconfig) > 0) != tt.wantCodex || (len(artifacts.CodexRequirements) > 0) != tt.wantCodex {
				t.Errorf("Codex artifacts do not match selection %v", tt.products)
			}

			if len(artifacts.AWSConfig) == 0 {
				t.Error("shared AWS config is missing")
			}
		})
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

	if len(servers) != 2 {
		t.Fatalf("got %d MCP servers, want 2", len(servers))
	}

	wantOAuth := desktopOAuth{ClientID: "jarvis-registry-claude-desktop", Scope: "mcp-proxy-ops", CallbackPort: 47280}
	for _, server := range servers {
		if server.OAuth == nil || *server.OAuth != wantOAuth {
			t.Errorf("server %q OAuth = %+v, want %+v", server.Name, server.OAuth, wantOAuth)
		}
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
	cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	cfg.Claude.DesktopMCPServers = nil

	settings, err := desktopSettings(cfg.AWS, *cfg.Claude)
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
		{Name: cfg.Claude.Models[TierSonnet].AIPARN, LabelOverride: "Sonnet", AnthropicFamilyTier: TierSonnet},
		{Name: cfg.Claude.Models[TierOpus].AIPARN, LabelOverride: "Opus", AnthropicFamilyTier: TierOpus},
		{Name: cfg.Claude.Models[TierHaiku].AIPARN, LabelOverride: "Haiku", AnthropicFamilyTier: TierHaiku},
	}
	if !slices.Equal(models, want) {
		t.Errorf("inferenceModels = %+v, want %+v", models, want)
	}
}

func TestMCPUsesJarvisOAuth(t *testing.T) {
	cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	settings, err := desktopSettings(cfg.AWS, *cfg.Claude)
	if err != nil {
		t.Fatal(err)
	}

	var servers []desktopMCPServer
	if err := json.Unmarshal([]byte(settings["managedMcpServers"]), &servers); err != nil {
		t.Fatal(err)
	}

	if len(servers) != len(cfg.Claude.DesktopMCPServers) {
		t.Fatalf("got %d MCP servers, want %d", len(servers), len(cfg.Claude.DesktopMCPServers))
	}

	wantOAuth := desktopOAuth{ClientID: "jarvis-registry-claude-desktop", Scope: "mcp-proxy-ops", CallbackPort: 47280}
	for _, server := range servers {
		if server.OAuth == nil || *server.OAuth != wantOAuth {
			t.Errorf("server %q OAuth = %+v, want %+v", server.Name, server.OAuth, wantOAuth)
		}
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
			cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
			if err != nil {
				t.Fatal(err)
			}

			cfg.Claude.DefaultTier = tt.defaultTier
			if tt.withoutOpus {
				delete(cfg.Claude.Models, TierOpus)
			}

			desktop, err := desktopSettings(cfg.AWS, *cfg.Claude)
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

			settings := claudeCodeSettings(cfg.AWS, *cfg.Claude)
			env, _ := settings["env"].(map[string]string)
			wantModel, _ := cfg.Claude.model(tt.wantDefault)

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
		{name: "no haiku model", mutate: func(c *Config) { delete(c.Claude.Models, TierHaiku) }, wantErr: []string{"claude.models: must include a haiku model"}},
		{name: "no sonnet model", mutate: func(c *Config) { delete(c.Claude.Models, TierSonnet) }, wantErr: []string{"claude.models: must include a sonnet model"}},
		{
			name:    "unknown tier",
			mutate:  func(c *Config) { c.Claude.Models["fable"] = c.Claude.Models[TierSonnet] },
			wantErr: []string{`claude.models.fable: must be "opus", "sonnet" or "haiku"`},
		},
		{
			name: "default tier without its model",
			mutate: func(c *Config) {
				delete(c.Claude.Models, TierOpus)
				c.Claude.DefaultTier = TierOpus
			},
			wantErr: []string{`claude.default_tier: must be the tier of a configured model, got "opus"`},
		},
		{name: "unknown default tier", mutate: func(c *Config) { c.Claude.DefaultTier = "Sonnet" }, wantErr: []string{`claude.default_tier: must be the tier of a configured model, got "Sonnet"`}},
		{
			name: "system inference profile instead of an AIP",
			mutate: func(c *Config) {
				model := c.Claude.Models[TierSonnet]
				model.AIPARN = "us.anthropic.claude-sonnet-5"
				c.Claude.Models[TierSonnet] = model
			},
			wantErr: []string{"claude.models.sonnet.aip_arn: must be an application inference profile ARN"},
		},
		{
			name: "AIP in another region",
			mutate: func(c *Config) {
				model := c.Claude.Models[TierSonnet]
				model.AIPARN = "arn:aws:bedrock:us-west-2:123456789012:application-inference-profile/abc"
				c.Claude.Models[TierSonnet] = model
			},
			wantErr: []string{"claude.models.sonnet.aip_arn: is in us-west-2, but aws.bedrock_region is us-east-1"},
		},
		{
			name: "AIP in another account",
			mutate: func(c *Config) {
				model := c.Claude.Models[TierHaiku]
				model.AIPARN = "arn:aws:bedrock:us-east-1:210987654321:application-inference-profile/abc"
				c.Claude.Models[TierHaiku] = model
			},
			wantErr: []string{"claude.models.haiku.aip_arn: is in account 210987654321, but aws.account_id is 123456789012"},
		},
		{
			name: "model ID of another tier",
			mutate: func(c *Config) {
				model := c.Claude.Models[TierSonnet]
				model.AnthropicModelID = "claude-haiku-4-5"
				c.Claude.Models[TierSonnet] = model
			},
			wantErr: []string{"claude.models.sonnet.anthropic_model_id: must be an Anthropic model ID of the sonnet tier"},
		},
		{name: "http MCP URL", mutate: func(c *Config) { c.Claude.DesktopMCPServers[0].URL = "http://mcp.example.com" }, wantErr: []string{"claude.desktop_mcp_servers[0].url: must be an https URL"}},
		{name: "bad MCP name", mutate: func(c *Config) { c.Claude.DesktopMCPServers[0].Name = "-registry" }, wantErr: []string{"claude.desktop_mcp_servers[0].name: must be letters"}},
		{
			name:    "duplicate MCP name",
			mutate:  func(c *Config) { c.Claude.DesktopMCPServers[1].Name = "example-registry" },
			wantErr: []string{`claude.desktop_mcp_servers[1].name: "example-registry" is listed more than once`},
		},
		{
			name:    "codex model that is not an OpenAI inference profile",
			mutate:  func(c *Config) { c.Codex.DefaultModel = "openai.gpt-6.1-sol" },
			wantErr: []string{"codex.default_model: must be a global. or us. Bedrock Runtime inference profile ID of an OpenAI model"},
		},
		{
			name:    "codex model of another provider",
			mutate:  func(c *Config) { c.Codex.DefaultModel = "global.anthropic.claude-sonnet-5" },
			wantErr: []string{"codex.default_model: must be a global. or us. Bedrock Runtime inference profile ID of an OpenAI model"},
		},
		{name: "unknown codex reasoning effort", mutate: func(c *Config) { c.Codex.ReasoningEffort = "hihg" }, wantErr: []string{`codex.reasoning_effort: must be one of none, minimal, low, medium, high, xhigh, max, ultra, persistent, got "hihg"`}},
		{name: "bad identifier", mutate: func(c *Config) { c.MacOSProfile.IdentifierPrefix = "claude" }, wantErr: []string{"macos_profile.identifier_prefix: must be a reverse-DNS identifier"}},
		{name: "empty organization", mutate: func(c *Config) { c.MacOSProfile.Organization = " " }, wantErr: []string{"macos_profile.organization: must not be empty"}},
		{
			name:    "placeholder left in",
			mutate:  func(c *Config) { c.AWS.SSORoleName = "REPLACE_WITH_PERMISSION_SET_NAME" },
			wantErr: []string{`aws.sso_role_name: replace the placeholder "REPLACE_WITH_PERMISSION_SET_NAME"`},
		},
		{
			name: "every failure is reported",
			mutate: func(c *Config) {
				c.AWS.AccountID = "1"
				c.MacOSProfile.Organization = ""
			},
			wantErr: []string{"aws.account_id:", "macos_profile.organization: must not be empty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
			if err != nil {
				t.Fatal(err)
			}

			tt.mutate(&cfg)

			mutated, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}

			_, err = Parse(bytes.NewReader(mutated), Options{Products: []Product{ProductClaude, ProductCodex}})
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

func TestModelValidationOrder(t *testing.T) {
	cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	cfg.Claude.Models = map[string]Model{"zeta": {}, "alpha": {}}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Parse(bytes.NewReader(data), Options{Products: []Product{ProductClaude}})
	if err == nil {
		t.Fatal("Parse succeeded, want invalid model errors")
	}

	messages := strings.Split(err.Error(), "\n")

	wantPaths := []string{
		`claude.models.alpha: must be "opus", "sonnet" or "haiku"`,
		`claude.models.zeta: must be "opus", "sonnet" or "haiku"`,
		"claude.models: must include a sonnet model", "claude.models: must include a haiku model",
	}
	if len(messages) != len(wantPaths)+1 {
		t.Fatalf("got errors %q, want %d model errors", err.Error(), len(wantPaths))
	}

	for i, path := range wantPaths {
		if !strings.HasPrefix(messages[i+1], path) {
			t.Errorf("error %d = %q, want prefix %q", i, messages[i+1], path)
		}
	}
}

func TestParseAcceptsCodexModels(t *testing.T) {
	for _, model := range []string{"global.openai.gpt-6-astra", "us.openai.gpt-6-luna", "global.openai.gpt-6.1-terra", "us.openai.gpt-6.1-sol"} {
		t.Run(model, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
			if err != nil {
				t.Fatal(err)
			}

			cfg.Codex.DefaultModel = model

			mutated, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := Parse(bytes.NewReader(mutated), Options{Products: []Product{ProductClaude, ProductCodex}}); err != nil {
				t.Errorf("Parse: %s", err.Error())
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
		name     string
		input    string
		wantErr  string
		products []Product
	}{
		{name: "unknown field", input: strings.Replace(valid, `"organization": "Example"`, `"organization": "Example", "extra": true`, 1), wantErr: `unknown field "extra"`, products: []Product{ProductClaude, ProductCodex}},
		{name: "unknown MCP field", input: strings.Replace(valid, `"name": "static-tools"`, `"name": "static-tools", "transport": "sse"`, 1), wantErr: `unknown field "transport"`, products: []Product{ProductClaude, ProductCodex}},
		{name: "removed OAuth field", input: strings.Replace(valid, `"name": "static-tools"`, `"name": "static-tools", "oauth": {}`, 1), wantErr: `unknown field "oauth"`, products: []Product{ProductClaude}},
		{name: "removed enabled field", input: strings.Replace(valid, `"codex": {`, `"codex": {"enabled": true,`, 1), wantErr: `unknown field "enabled"`, products: []Product{ProductCodex}},
		{name: "unknown unselected codex field", input: strings.Replace(valid, `"reasoning_effort"`, `"reasoning_efort"`, 1), wantErr: `unknown field "reasoning_efort"`, products: []Product{ProductClaude}},
		{name: "unknown unselected claude field", input: strings.Replace(valid, `"anthropic_model_id"`, `"anthropic_model"`, 1), wantErr: `unknown field "anthropic_model"`, products: []Product{ProductCodex}},
		{name: "wrong type in unselected codex", input: strings.Replace(valid, `"reasoning_effort": "high"`, `"reasoning_effort": 1`, 1), wantErr: "cannot unmarshal number", products: []Product{ProductClaude}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.input == valid {
				t.Fatal("test input was not modified; the fixture changed")
			}

			_, err := Parse(strings.NewReader(tt.input), Options{Products: tt.products})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseRequiresEndOfInput(t *testing.T) {
	raw, err := os.ReadFile(validConfig)
	if err != nil {
		t.Fatal(err)
	}

	valid := string(raw)

	tests := []struct {
		reader  io.Reader
		name    string
		wantErr bool
	}{
		{name: "end of input", reader: strings.NewReader(valid)},
		{name: "trailing whitespace", reader: strings.NewReader(valid + " \t\r\n")},
		{name: "extra closing brace", reader: strings.NewReader(valid + "}"), wantErr: true},
		{name: "extra closing bracket", reader: strings.NewReader(valid + "]"), wantErr: true},
		{name: "second object", reader: strings.NewReader(valid + "{}"), wantErr: true},
		{name: "trailing null", reader: strings.NewReader(valid + "null"), wantErr: true},
		{name: "trailing garbage", reader: strings.NewReader(valid + "garbage"), wantErr: true},
		{name: "read failure", reader: io.MultiReader(strings.NewReader(valid), iotest.ErrReader(io.ErrUnexpectedEOF)), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.reader, Options{Products: []Product{ProductClaude, ProductCodex}})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "unexpected data after the JSON object") {
					t.Fatalf("got %v, want a trailing-input error", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("valid config rejected: %s", err.Error())
			}
		})
	}
}

func TestPlaceholders(t *testing.T) {
	const example = "../config/config.example.json"

	if _, err := Load(example, Options{Products: []Product{ProductClaude, ProductCodex}, AllowPlaceholders: true}); err != nil {
		t.Fatalf("example config with placeholders allowed: %s", err.Error())
	}

	_, err := Load(example, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err == nil {
		t.Fatal("example config loaded without AllowPlaceholders")
	}

	for _, field := range []string{"aws.account_id", "aws.sso_start_url", "aws.sso_role_name", "macos_profile.organization", "claude.models.sonnet.aip_arn", "claude.models.haiku.aip_arn", "claude.models.opus.aip_arn", "claude.desktop_mcp_servers[0].url"} {
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

	cfg, err := Load(path, Options{Products: []Product{ProductClaude, ProductCodex}})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	return artifacts
}

func TestParseCodexSetup(t *testing.T) {
	tests := []struct {
		mutate  func(*Config)
		name    string
		wantErr []string
	}{
		{name: "full config"},
		{name: "invalid claude ignored", mutate: func(c *Config) { c.Claude.Models = nil }},
		{name: "invalid macos profile ignored", mutate: func(c *Config) { c.MacOSProfile = MacOSProfile{} }},
		{name: "missing codex", mutate: func(c *Config) { c.Codex = nil }, wantErr: []string{"codex: is required"}},
		{name: "AWS placeholder", mutate: func(c *Config) { c.AWS.AccountID = "REPLACE_WITH_ACCOUNT_ID" }, wantErr: []string{"aws.account_id: replace the placeholder"}},
		{name: "Codex placeholder", mutate: func(c *Config) { c.Codex.DefaultModel = "REPLACE_WITH_MODEL" }, wantErr: []string{"codex.default_model: replace the placeholder"}},
		{
			name: "multiple errors",
			mutate: func(c *Config) {
				c.AWS.AccountID = "bad"
				c.Codex.ReasoningEffort = "bad"
			},
			wantErr: []string{"aws.account_id:", "codex.reasoning_effort:"},
		},
		{
			name: "missing codex joins AWS errors",
			mutate: func(c *Config) {
				c.AWS.AccountID = "bad"
				c.Codex = nil
			},
			wantErr: []string{"aws.account_id:", "codex: is required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(validConfig, Options{Products: []Product{ProductClaude, ProductCodex}})
			if err != nil {
				t.Fatal(err)
			}

			if tt.mutate != nil {
				tt.mutate(&cfg)
			}

			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}

			got, err := ParseCodexSetup(bytes.NewReader(raw))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("ParseCodexSetup succeeded, want an error")
				}

				lines := strings.Split(err.Error(), "\n")
				if lines[0] != "invalid config:" || len(lines) != len(tt.wantErr)+1 {
					t.Fatalf("error = %q, want exactly %d validation errors", err.Error(), len(tt.wantErr))
				}

				for i, want := range tt.wantErr {
					if !strings.HasPrefix(lines[i+1], want) {
						t.Errorf("error %q does not start with %q", lines[i+1], want)
					}
				}

				if cfg.Codex == nil && lines[len(lines)-1] != "codex: is required" {
					t.Errorf("missing section error = %q", lines[len(lines)-1])
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.AWS != cfg.AWS || got.Codex != *cfg.Codex {
				t.Errorf("parsed setup = %+v, want original AWS and Codex values", got)
			}
		})
	}
}

func TestParseCodexSetupSections(t *testing.T) {
	raw, err := os.ReadFile(validConfig)
	if err != nil {
		t.Fatal(err)
	}

	var sections map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sections); err != nil {
		t.Fatal(err)
	}

	delete(sections, "macos_profile")
	delete(sections, "claude")

	for _, invalidClaude := range []bool{false, true} {
		if invalidClaude {
			sections["claude"] = json.RawMessage(`{"models": {}, "default_tier": "invalid"}`)
		}

		onlyNeeded, err := json.Marshal(sections)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := ParseCodexSetup(bytes.NewReader(onlyNeeded)); err != nil {
			t.Fatalf("config without macos_profile rejected: %s", err.Error())
		}
	}
}

func TestParseCodexSetupStrictDecoding(t *testing.T) {
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
		{name: "top level unknown", input: strings.Replace(valid, `{`, `{"extra": true,`, 1), wantErr: `unknown field "extra"`},
		{name: "AWS unknown", input: strings.Replace(valid, `"aws": {`, `"aws": {"extra": true,`, 1), wantErr: `unknown field "extra"`},
		{name: "Codex unknown", input: strings.Replace(valid, `"codex": {`, `"codex": {"extra": true,`, 1), wantErr: `unknown field "extra"`},
		{name: "ignored Claude unknown", input: strings.Replace(valid, `"claude": {`, `"claude": {"extra": true,`, 1), wantErr: `unknown field "extra"`},
		{name: "ignored macos unknown", input: strings.Replace(valid, `"macos_profile": {`, `"macos_profile": {"extra": true,`, 1), wantErr: `unknown field "extra"`},
		{name: "ignored nested model unknown", input: strings.Replace(valid, `"anthropic_model_id"`, `"anthropic_model"`, 1), wantErr: `unknown field "anthropic_model"`},
		{name: "ignored field wrong type", input: strings.Replace(valid, `"organization": "Example"`, `"organization": 123`, 1), wantErr: "cannot unmarshal number"},
		{name: "empty input", wantErr: "EOF"},
		{name: "second object", input: valid + `{}`, wantErr: "unexpected data after the JSON object"},
		{name: "trailing garbage", input: valid + `!`, wantErr: "unexpected data after the JSON object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.input == valid {
				t.Fatal("test input was not modified; the fixture changed")
			}

			_, err := ParseCodexSetup(strings.NewReader(tt.input))
			if err == nil || !strings.HasPrefix(err.Error(), "decoding config:") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want a decoding error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCodexSetupRenderMatchesGolden(t *testing.T) {
	raw, err := os.ReadFile(validConfig)
	if err != nil {
		t.Fatal(err)
	}

	setup, err := ParseCodexSetup(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string][]byte{
		"aws-config-CCWB.ini":          setup.AWSConfig(),
		"codex-requirements-CCWB.toml": setup.Requirements(),
	} {
		want, err := os.ReadFile(filepath.Join("testdata", "golden", name))
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the generator golden", name)
		}
	}
}

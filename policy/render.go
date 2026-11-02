package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/ascending-llc/claude-3p-config/internal/plist"
)

type (
	// Artifacts are the rendered files for one config.
	Artifacts struct {
		// Mobileconfig is a macOS configuration profile with one payload for
		// Claude Desktop and one for Claude Code.
		Mobileconfig []byte
		// Reg is a Windows .reg file, UTF-16LE with a byte order mark, that writes
		// both apps' policies under HKEY_LOCAL_MACHINE.
		Reg []byte
		// AWSConfig is the ~/.aws/config snippet that defines ProfileName.
		AWSConfig []byte
		// CodexMobileconfig is the macOS managed preferences profile for Codex.
		// It embeds the managed config and requirements TOML as Base64 strings.
		CodexMobileconfig []byte
		// CodexManagedConfig sets the deployment's default model and reasoning
		// effort, which users can change. Administrators install it as
		// managed_config.toml.
		CodexManagedConfig []byte
		// CodexRequirements enforces the Bedrock Runtime provider, its AWS
		// profile, Region and sign-in refresh, and disabled web search.
		// Administrators install it as requirements.toml.
		CodexRequirements []byte
	}

	// desktopModel is one entry of Claude Desktop's inferenceModels.
	desktopModel struct {
		Name                string `json:"name"`
		LabelOverride       string `json:"labelOverride"`
		AnthropicFamilyTier string `json:"anthropicFamilyTier"`
	}

	// desktopMCPServer is one entry of Claude Desktop's managedMcpServers.
	desktopMCPServer struct {
		OAuth     *desktopOAuth `json:"oauth,omitempty"`
		Name      string        `json:"name"`
		Transport string        `json:"transport"`
		URL       string        `json:"url"`
	}

	// desktopOAuth is the oauth object of a managedMcpServers entry.
	desktopOAuth struct {
		ClientID     string `json:"clientId"`
		Scope        string `json:"scope,omitempty"`
		CallbackPort int    `json:"callbackPort"`
	}
)

const (
	// ProfileName is the AWS CLI profile Claude Code signs in with. Claude
	// Code's policy and the AWS config snippet must agree on it.
	ProfileName = "CCWB"
	// SSOSessionName is the [sso-session] section the profile uses.
	SSOSessionName = "ccwb"

	// Registry keys of the Windows policies.
	desktopRegKey    = `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Claude`
	claudeCodeRegKey = `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\ClaudeCode`
)

// Render builds every artifact from a config returned by Load or Parse.
func Render(cfg Config) (Artifacts, error) {
	desktop, err := desktopSettings(cfg)
	if err != nil {
		return Artifacts{}, err
	}

	claudeCode := claudeCodeSettings(cfg)

	mobileconfig, err := renderMobileconfig(cfg.Profile, desktop, claudeCode)
	if err != nil {
		return Artifacts{}, err
	}

	claudeCodeJSON, err := compactJSON(claudeCode)
	if err != nil {
		return Artifacts{}, err
	}

	artifacts := Artifacts{
		Mobileconfig: mobileconfig,
		Reg:          renderReg(desktop, claudeCodeJSON),
		AWSConfig:    renderAWSConfig(cfg.AWS),
	}

	if cfg.codexEnabled() {
		artifacts.CodexManagedConfig = renderCodexManagedConfig(*cfg.Codex)
		artifacts.CodexRequirements = renderCodexRequirements(cfg.AWS)

		artifacts.CodexMobileconfig, err = renderCodexMobileconfig(
			cfg.Profile,
			artifacts.CodexManagedConfig,
			artifacts.CodexRequirements,
		)
		if err != nil {
			return Artifacts{}, err
		}
	}

	return artifacts, nil
}

// desktopSettings returns Claude Desktop's managed configuration. Every value
// is a string, because the configuration profile and the registry both store
// the JSON-valued keys as strings.
func desktopSettings(cfg Config) (map[string]string, error) {
	ordered := cfg.orderedModels()
	models := make([]desktopModel, 0, len(ordered))

	for _, m := range ordered {
		label := m.Label
		if label == "" {
			label = strings.ToUpper(m.Tier[:1]) + m.Tier[1:]
		}

		models = append(models, desktopModel{Name: m.AIPARN, LabelOverride: label, AnthropicFamilyTier: m.Tier})
	}

	modelsJSON, err := compactJSON(models)
	if err != nil {
		return nil, err
	}

	settings := map[string]string{
		"inferenceProvider":            "bedrock",
		"inferenceCredentialKind":      "interactive",
		"inferenceBedrockRegion":       cfg.AWS.BedrockRegion,
		"inferenceBedrockSsoStartUrl":  cfg.AWS.SSOStartURL,
		"inferenceBedrockSsoRegion":    cfg.AWS.SSORegion,
		"inferenceBedrockSsoAccountId": cfg.AWS.AccountID,
		"inferenceBedrockSsoRoleName":  cfg.AWS.SSORoleName,
		"inferenceModels":              modelsJSON,
	}

	if len(cfg.Desktop.MCPServers) > 0 {
		servers := make([]desktopMCPServer, 0, len(cfg.Desktop.MCPServers))
		for _, s := range cfg.Desktop.MCPServers {
			servers = append(servers, desktopMCPServer{Name: s.Name, Transport: "http", URL: s.URL, OAuth: oauthSettings(s.OAuth)})
		}

		serversJSON, err := compactJSON(servers)
		if err != nil {
			return nil, err
		}

		settings["managedMcpServers"] = serversJSON
	}

	return settings, nil
}

// oauthSettings returns the oauth object of a managedMcpServers entry, always
// with an explicit callback port so Desktop never falls back to 53280.
func oauthSettings(o *OAuth) *desktopOAuth {
	if o == nil {
		return nil
	}

	port := o.CallbackPort
	if port == 0 {
		port = defaultCallbackPort
	}

	return &desktopOAuth{ClientID: o.ClientID, Scope: o.Scope, CallbackPort: port}
}

// claudeCodeSettings returns Claude Code's managed settings. Claude Code
// reaches Bedrock through ProfileName, and every allowed model alias resolves
// to one of the configured AIPs. An unconfigured tier's alias isn't allowed.
func claudeCodeSettings(cfg Config) map[string]any {
	defaultModel, _ := cfg.model(cfg.defaultTier())
	env := map[string]string{
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"AWS_PROFILE":             ProfileName,
		"AWS_REGION":              cfg.AWS.BedrockRegion,
		"ANTHROPIC_MODEL":         defaultModel.AIPARN,
	}
	overrides := map[string]string{}
	available := []string{}

	for _, tier := range tiers {
		m, ok := cfg.model(tier)
		if !ok {
			continue
		}

		env["ANTHROPIC_DEFAULT_"+strings.ToUpper(tier)+"_MODEL"] = m.AIPARN
		overrides[m.AnthropicModelID] = m.AIPARN

		available = append(available, tier)
	}

	return map[string]any{
		"awsAuthRefresh":         "aws sso login --profile " + ProfileName,
		"env":                    env,
		"modelOverrides":         overrides,
		"availableModels":        available,
		"enforceAvailableModels": true,
	}
}

func renderMobileconfig(p Profile, desktop map[string]string, claudeCode map[string]any) ([]byte, error) {
	desktopPayload := payload(p.Identifier+".desktop", "com.anthropic.claudefordesktop", "Claude Desktop")
	for k, v := range desktop {
		desktopPayload[k] = v
	}

	claudeCodePayload := payload(p.Identifier+".claudecode", "com.anthropic.claudecode", "Claude Code")
	for k, v := range claudeCode {
		claudeCodePayload[k] = v
	}

	profile := map[string]any{
		"PayloadContent": []any{desktopPayload, claudeCodePayload},
		"PayloadDescription": "Points Claude Desktop and Claude Code at Amazon Bedrock, " +
			"signed in through AWS IAM Identity Center.",
		"PayloadDisplayName":  p.DisplayName,
		"PayloadIdentifier":   p.Identifier,
		"PayloadOrganization": p.Organization,
		"PayloadScope":        "User",
		"PayloadType":         "Configuration",
		"PayloadUUID":         stableUUID(p.Identifier),
		"PayloadVersion":      1,
	}

	out, err := plist.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("rendering mobileconfig: %s", err.Error())
	}

	return out, nil
}

func payload(identifier, payloadType, displayName string) map[string]any {
	return map[string]any{
		"PayloadDisplayName": displayName,
		"PayloadIdentifier":  identifier,
		"PayloadType":        payloadType,
		"PayloadUUID":        stableUUID(identifier),
		"PayloadVersion":     1,
	}
}

// stableUUID derives a UUID from name, so rebuilding a profile reproduces the
// same payload UUIDs. It is a version 8 (custom) UUID per RFC 9562, hashed
// with SHA-256.
func stableUUID(name string) string {
	sum := sha256.Sum256([]byte("claude-3p-config/" + name))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x80
	b[8] = b[8]&0x3f | 0x80

	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

// renderReg writes Claude Desktop's settings as one string value each and
// Claude Code's settings as JSON in the Settings value, both under HKLM.
// Desktop ignores user (HKCU) policy whenever machine policy exists, and
// Claude Code ignores some managed-only keys in HKCU.
func renderReg(desktop map[string]string, claudeCodeJSON string) []byte {
	keys := make([]string, 0, len(desktop))
	for k := range desktop {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	lines := []string{"Windows Registry Editor Version 5.00", "", "[" + desktopRegKey + "]"}
	for _, k := range keys {
		lines = append(lines, regValue(k, desktop[k]))
	}

	lines = append(lines, "", "["+claudeCodeRegKey+"]", regValue("Settings", claudeCodeJSON))
	text := strings.Join(lines, "\r\n") + "\r\n"

	units := utf16.Encode([]rune(text))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFF, 0xFE)

	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}

	return out
}

func regValue(name, value string) string {
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`)

	return `"` + escape.Replace(name) + `"="` + escape.Replace(value) + `"`
}

func renderAWSConfig(a AWS) []byte {
	return []byte(fmt.Sprintf(`# Claude on Amazon Bedrock. Add to ~/.aws/config (Windows: %%USERPROFILE%%\.aws\config).

[sso-session %s]
sso_start_url = %s
sso_region = %s
sso_registration_scopes = sso:account:access

[profile %s]
sso_session = %s
sso_account_id = %s
sso_role_name = %s
region = %s
`, SSOSessionName, a.SSOStartURL, a.SSORegion, ProfileName, SSOSessionName, a.AccountID, a.SSORoleName, a.BedrockRegion))
}

// renderCodexManagedConfig sets the starting model and reasoning effort.
// Codex applies these managed defaults at launch, but users can change them
// during a session, so everything that must hold lives in the requirements.
// web_search is set here too because Codex rejects a default that the
// requirements don't allow.
func renderCodexManagedConfig(c Codex) []byte {
	return []byte(fmt.Sprintf(`model = %q
model_reasoning_effort = %q
web_search = "disabled"
`, c.DefaultModel, c.ReasoningEffort))
}

// renderCodexRequirements pins Codex to its built-in Bedrock Runtime provider
// and replaces any user definition of that provider, so neither config.toml
// nor a command-line override can switch provider, AWS profile or Region.
// Hosted web search isn't available on Bedrock Runtime, so it is allowed only
// as disabled. Model authorization remains an AWS IAM/SCP responsibility.
//
// auth_refresh re-runs the SSO sign-in when credentials expire, like Claude
// Code's awsAuthRefresh. Codex accepts only command = "aws", runs it without a
// shell and looks it up on its own PATH; the five-minute timeout leaves room
// for the browser sign-in.
func renderCodexRequirements(a AWS) []byte {
	return []byte(fmt.Sprintf(`model_provider = "amazon-bedrock-runtime"
allowed_web_search_modes = ["disabled"]

[model_providers.amazon-bedrock-runtime.aws]
profile = %q
region = %q
auth_refresh = { command = "aws", args = ["sso", "login", "--profile", %q], timeout_ms = 300000 }
`, ProfileName, a.BedrockRegion, ProfileName))
}

func renderCodexMobileconfig(p Profile, managedConfig, requirements []byte) ([]byte, error) {
	identifier := p.Identifier + ".codex"
	codexPayload := payload(identifier+".managed", "com.openai.codex", "Codex")
	codexPayload["config_toml_base64"] = base64.StdEncoding.EncodeToString(managedConfig)
	codexPayload["requirements_toml_base64"] = base64.StdEncoding.EncodeToString(requirements)

	profile := map[string]any{
		"PayloadContent":      []any{codexPayload},
		"PayloadDescription":  "Configures Codex to use Amazon Bedrock Runtime through the CCWB AWS profile.",
		"PayloadDisplayName":  "Codex on Amazon Bedrock (CCWB)",
		"PayloadIdentifier":   identifier,
		"PayloadOrganization": p.Organization,
		"PayloadScope":        "User",
		"PayloadType":         "Configuration",
		"PayloadUUID":         stableUUID(identifier),
		"PayloadVersion":      1,
	}

	out, err := plist.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("rendering Codex mobileconfig: %s", err.Error())
	}

	return out, nil
}

// compactJSON encodes v without HTML escaping, so URLs and ARNs stay readable.
func compactJSON(v any) (string, error) {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("encoding JSON: %s", err.Error())
	}

	return strings.TrimSuffix(b.String(), "\n"), nil
}

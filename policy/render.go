package policy

import (
	"bytes"
	"crypto/sha256"
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

	return Artifacts{
		Mobileconfig: mobileconfig,
		Reg:          renderReg(desktop, claudeCodeJSON),
		AWSConfig:    renderAWSConfig(cfg.AWS),
	}, nil
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

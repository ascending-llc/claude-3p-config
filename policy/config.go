// Package policy turns one deployment config into the managed policies that
// point Claude Desktop and Claude Code at Amazon Bedrock through AWS IAM
// Identity Center, plus the AWS CLI profile Claude Code signs in with.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type (
	// Config is the deployment configuration, normally config/config.json.
	Config struct {
		AWS     AWS     `json:"aws"`
		Profile Profile `json:"profile"`
		Models  []Model `json:"models"`
		Desktop Desktop `json:"desktop"`
	}

	// AWS holds the account, regions and IAM Identity Center sign-in details.
	AWS struct {
		// AccountID is the AWS account that owns the application inference profiles.
		AccountID string `json:"account_id"`
		// BedrockRegion is the region Claude Desktop and Claude Code call Bedrock in.
		BedrockRegion string `json:"bedrock_region"`
		// SSOStartURL is the IAM Identity Center access portal URL.
		SSOStartURL string `json:"sso_start_url"`
		// SSORegion is the region of the IAM Identity Center instance.
		SSORegion string `json:"sso_region"`
		// SSORoleName is the permission set users sign in with.
		SSORoleName string `json:"sso_role_name"`
	}

	// Model maps one model tier to the application inference profile (AIP) that serves it.
	Model struct {
		// Tier is TierSonnet or TierHaiku.
		Tier string `json:"tier"`
		// AIPARN is the application inference profile ARN.
		AIPARN string `json:"aip_arn"`
		// AnthropicModelID is the Anthropic model ID the AIP routes to, such as claude-sonnet-5.
		AnthropicModelID string `json:"anthropic_model_id"`
		// Label is the name Claude Desktop shows. It defaults to the capitalized tier.
		Label string `json:"label,omitempty"`
	}

	// Desktop holds settings that apply to Claude Desktop only.
	Desktop struct {
		// MCPServers are the remote MCP servers provisioned as managed connectors.
		MCPServers []MCPServer `json:"mcp_servers,omitempty"`
	}

	// MCPServer is a remote MCP server reached over streamable HTTP.
	MCPServer struct {
		// Name is the connector name users see in Claude Desktop.
		Name string `json:"name"`
		// URL is the server's final https address. Claude Desktop doesn't follow redirects.
		URL string `json:"url"`
		// OAuth makes Claude Desktop run the server's MCP OAuth sign-in.
		OAuth bool `json:"oauth,omitempty"`
	}

	// Profile identifies the generated macOS configuration profile.
	Profile struct {
		// Identifier is the reverse-DNS profile identifier. Keep it stable so a
		// newer profile replaces the installed one instead of adding a second.
		Identifier string `json:"identifier"`
		// DisplayName is the profile name shown in System Settings.
		DisplayName string `json:"display_name"`
		// Organization is shown as the profile's publisher.
		Organization string `json:"organization"`
	}

	// Options controls how a config is validated.
	Options struct {
		// AllowPlaceholders accepts REPLACE_WITH_ values, for rendering the committed examples.
		AllowPlaceholders bool
	}

	validator struct {
		errs              []error
		allowPlaceholders bool
	}
)

const (
	// TierSonnet and TierHaiku are the model tiers, in the order the policies list them.
	TierSonnet = "sonnet"
	TierHaiku  = "haiku"

	// placeholderMarker marks values in config.example.json that a deployer must replace.
	placeholderMarker = "REPLACE_WITH_"
)

var (
	accountIDPattern  = regexp.MustCompile(`^[0-9]{12}$`)
	regionPattern     = regexp.MustCompile(`^[a-z]{2}(-gov)?-[a-z]+-[0-9]+$`)
	roleNamePattern   = regexp.MustCompile(`^[\w+=,.@-]{1,64}$`)
	modelIDPattern    = regexp.MustCompile(`^claude-[a-z0-9-]+$`)
	serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	aipARNPattern     = regexp.MustCompile(
		`^arn:aws(?:-us-gov|-cn)?:bedrock:([a-z0-9-]+):([0-9]{12}):application-inference-profile/[a-z0-9]+$`)
)

// Load reads and validates the config file at path.
func Load(path string, opts Options) (Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Config{}, fmt.Errorf("reading config: %s", err.Error())
	}

	cfg, err := Parse(bytes.NewReader(data), opts)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %s", path, err.Error())
	}

	return cfg, nil
}

// Parse decodes and validates a config. Unknown fields are rejected, and
// every validation failure is reported, each prefixed with its field path.
func Parse(r io.Reader, opts Options) (Config, error) {
	var cfg Config

	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decoding config: %s", err.Error())
	}

	if dec.More() {
		return Config{}, errors.New("decoding config: unexpected data after the JSON object")
	}

	v := validator{allowPlaceholders: opts.AllowPlaceholders}
	v.validate(cfg)

	if len(v.errs) > 0 {
		return Config{}, fmt.Errorf("invalid config:\n%s", errors.Join(v.errs...).Error())
	}

	return cfg, nil
}

// model returns the model for tier. Callers rely on validation having
// guaranteed exactly one model per tier.
func (c Config) model(tier string) Model {
	for _, m := range c.Models {
		if m.Tier == tier {
			return m
		}
	}

	return Model{}
}

func (v *validator) validate(cfg Config) {
	v.validateAWS(cfg.AWS)
	v.validateModels(cfg.Models, cfg.AWS)
	v.validateMCPServers(cfg.Desktop.MCPServers)
	v.field("profile.identifier", cfg.Profile.Identifier, pattern(identifierPattern, "a reverse-DNS identifier such as com.example.claude"))
	v.field("profile.display_name", cfg.Profile.DisplayName, nonEmpty)
	v.field("profile.organization", cfg.Profile.Organization, nonEmpty)
}

func (v *validator) validateAWS(a AWS) {
	v.field("aws.account_id", a.AccountID, pattern(accountIDPattern, "a 12-digit AWS account ID"))
	v.field("aws.bedrock_region", a.BedrockRegion, pattern(regionPattern, "an AWS region name"))
	v.field("aws.sso_start_url", a.SSOStartURL, httpsURL)
	v.field("aws.sso_region", a.SSORegion, pattern(regionPattern, "an AWS region name"))
	v.field("aws.sso_role_name", a.SSORoleName, pattern(roleNamePattern, "a permission set name"))
}

func (v *validator) validateModels(models []Model, a AWS) {
	if len(models) != 2 {
		v.fail("models", "must list exactly two models, one sonnet and one haiku")
	}

	seen := map[string]bool{}

	for i, m := range models {
		path := fmt.Sprintf("models[%d]", i)

		switch {
		case m.Tier != TierSonnet && m.Tier != TierHaiku:
			v.fail(path+".tier", fmt.Sprintf("must be %q or %q", TierSonnet, TierHaiku))
		case seen[m.Tier]:
			v.fail(path+".tier", fmt.Sprintf("%q is listed more than once", m.Tier))
		}

		seen[m.Tier] = true

		v.field(path+".aip_arn", m.AIPARN, aipARN(a))
		v.field(path+".anthropic_model_id", m.AnthropicModelID, func(id string) error {
			if !modelIDPattern.MatchString(id) || !strings.Contains(id, m.Tier) {
				return fmt.Errorf("must be an Anthropic model ID of the %s tier, such as claude-%s-5", m.Tier, m.Tier)
			}

			return nil
		})
	}
}

func (v *validator) validateMCPServers(servers []MCPServer) {
	seen := map[string]bool{}

	for i, s := range servers {
		path := fmt.Sprintf("desktop.mcp_servers[%d]", i)

		v.field(path+".name", s.Name, pattern(serverNamePattern, "letters, digits, '-' and '_', starting with a letter or digit"))

		if seen[s.Name] {
			v.fail(path+".name", fmt.Sprintf("%q is listed more than once", s.Name))
		}

		seen[s.Name] = true

		v.field(path+".url", s.URL, httpsURL)
	}
}

// field records an error for path unless value passes check. A placeholder
// passes when placeholders are allowed and fails otherwise.
func (v *validator) field(path, value string, check func(string) error) {
	if strings.Contains(value, placeholderMarker) {
		if !v.allowPlaceholders {
			v.fail(path, fmt.Sprintf("replace the placeholder %q", value))
		}

		return
	}

	if err := check(value); err != nil {
		v.fail(path, err.Error())
	}
}

func (v *validator) fail(path, msg string) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", path, msg))
}

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be empty")
	}

	return nil
}

func pattern(re *regexp.Regexp, want string) func(string) error {
	return func(s string) error {
		if !re.MatchString(s) {
			return fmt.Errorf("must be %s, got %q", want, s)
		}

		return nil
	}
}

func httpsURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(s, " \t\r\n") {
		return fmt.Errorf("must be an https URL with a host, got %q", s)
	}

	return nil
}

// aipARN checks an AIP ARN and, when they are not placeholders, that it
// belongs to the configured account and Bedrock region.
func aipARN(a AWS) func(string) error {
	return func(s string) error {
		m := aipARNPattern.FindStringSubmatch(s)
		if m == nil {
			return fmt.Errorf("must be an application inference profile ARN, got %q", s)
		}

		if region := m[1]; regionPattern.MatchString(a.BedrockRegion) && region != a.BedrockRegion {
			return fmt.Errorf("is in %s, but aws.bedrock_region is %s", region, a.BedrockRegion)
		}

		if account := m[2]; accountIDPattern.MatchString(a.AccountID) && account != a.AccountID {
			return fmt.Errorf("is in account %s, but aws.account_id is %s", account, a.AccountID)
		}

		return nil
	}
}

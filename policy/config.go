// Package policy turns one deployment config into the managed policies that
// point Claude Desktop and Claude Code at Amazon Bedrock through AWS IAM
// Identity Center and the shared AWS CLI profile. Codex requirements are
// rendered when codex is selected.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type (
	// Config is the deployment configuration, normally config/config.json.
	Config struct {
		// Claude is required when Claude policies are selected and nil after Parse otherwise.
		Claude *Claude `json:"claude,omitempty"`
		// Codex is required when Codex policies are selected and nil after Parse otherwise.
		Codex *Codex `json:"codex,omitempty"`
		// AWS contains the shared account and sign-in settings.
		AWS AWS `json:"aws"`
		// MacOSProfile identifies each product's macOS configuration profile.
		MacOSProfile MacOSProfile `json:"macos_profile"`
	}

	// AWS holds the account, regions and IAM Identity Center sign-in details.
	AWS struct {
		// AccountID is the AWS account users sign in to.
		AccountID string `json:"account_id"`
		// BedrockRegion is the region the selected clients call Bedrock in.
		BedrockRegion string `json:"bedrock_region"`
		// SSOStartURL is the IAM Identity Center access portal URL.
		SSOStartURL string `json:"sso_start_url"`
		// SSORegion is the region of the IAM Identity Center instance.
		SSORegion string `json:"sso_region"`
		// SSORoleName is the permission set users sign in with.
		SSORoleName string `json:"sso_role_name"`
	}

	// Claude holds settings shared by Claude Desktop and Claude Code.
	Claude struct {
		// Models maps each configured tier to its application inference profile.
		Models map[string]Model `json:"models"`
		// DefaultTier is the tier both clients start with. It defaults to TierSonnet.
		DefaultTier string `json:"default_tier,omitempty"`
		// DesktopMCPServers are Jarvis Registry connectors for Claude Desktop.
		DesktopMCPServers []MCPServer `json:"desktop_mcp_servers,omitempty"`
	}

	// Model describes the application inference profile (AIP) for one model tier.
	Model struct {
		// AIPARN is the application inference profile ARN.
		AIPARN string `json:"aip_arn"`
		// AnthropicModelID is the Anthropic model ID the AIP routes to, such as claude-sonnet-5.
		AnthropicModelID string `json:"anthropic_model_id"`
		// Label is the name Claude Desktop shows. It defaults to the capitalized tier.
		Label string `json:"label,omitempty"`
	}

	// Codex holds the managed defaults for Codex on Amazon Bedrock.
	Codex struct {
		// DefaultModel is one approved Bedrock Runtime inference profile ID.
		DefaultModel string `json:"default_model"`
		// ReasoningEffort is the default effort shared by the approved models.
		ReasoningEffort string `json:"reasoning_effort"`
	}

	// MCPServer is a Jarvis Registry MCP endpoint reached over streamable HTTP.
	MCPServer struct {
		// Name is the connector name users see in Claude Desktop.
		Name string `json:"name"`
		// URL is the server's final https address. Claude Desktop doesn't follow redirects.
		URL string `json:"url"`
	}

	// MacOSProfile supplies the shared identity of the macOS configuration profiles.
	MacOSProfile struct {
		// IdentifierPrefix is the reverse-DNS prefix used to derive each product's identifier.
		IdentifierPrefix string `json:"identifier_prefix"`
		// Organization is shown as the profile's publisher.
		Organization string `json:"organization"`
	}

	// Product identifies a client product whose policies can be generated.
	Product string

	// Options controls which products are selected and how their config is validated.
	Options struct {
		// Products lists the products to validate and render. At least one is required.
		Products []Product
		// AllowPlaceholders accepts REPLACE_WITH_ values, for rendering the committed examples.
		AllowPlaceholders bool
	}

	validator struct {
		errs              []error
		allowPlaceholders bool
	}
)

const (
	// TierOpus, TierSonnet and TierHaiku are the model tiers, in the order the
	// policies list them after the default tier. Opus is optional.
	TierOpus   = "opus"
	TierSonnet = "sonnet"
	TierHaiku  = "haiku"

	// placeholderMarker marks values in config.example.json that a deployer must replace.
	placeholderMarker = "REPLACE_WITH_"

	// ProductClaude selects both Claude Desktop and Claude Code.
	ProductClaude Product = "claude"
	// ProductCodex selects local Codex clients.
	ProductCodex Product = "codex"
)

var (
	// tiers lists every tier in policy order.
	tiers = []string{TierOpus, TierSonnet, TierHaiku}

	accountIDPattern = regexp.MustCompile(`^[0-9]{12}$`)
	regionPattern    = regexp.MustCompile(`^[a-z]{2}(-gov)?-[a-z]+-[0-9]+$`)
	roleNamePattern  = regexp.MustCompile(`^[\w+=,.@-]{1,64}$`)
	modelIDPattern   = regexp.MustCompile(`^claude-[a-z0-9-]+$`)
	// codexModelPattern matches the Global and US Geo cross-Region inference
	// profiles that Codex's amazon-bedrock-runtime provider accepts. Which of
	// them a user may invoke is for IAM/SCP to decide, not this list.
	codexModelPattern = regexp.MustCompile(`^(global|us)\.openai\.[a-z0-9]+([.-][a-z0-9]+)*$`)
	serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	// codexReasoningEfforts are the effort values Codex knows; each model
	// supports a subset.
	codexReasoningEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent"}
	aipARNPattern         = regexp.MustCompile(
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
	v.validate(cfg, opts.Products)

	if len(v.errs) > 0 {
		return Config{}, fmt.Errorf("invalid config:\n%s", errors.Join(v.errs...).Error())
	}

	if !slices.Contains(opts.Products, ProductClaude) {
		cfg.Claude = nil
	}

	if !slices.Contains(opts.Products, ProductCodex) {
		cfg.Codex = nil
	}

	return cfg, nil
}

// Valid reports whether p is a supported product.
func (p Product) Valid() bool {
	return p == ProductClaude || p == ProductCodex
}

// model returns the model for tier, and false when the tier isn't configured.
// Validation guarantees at most one model per tier, and always sonnet and haiku.
func (c Claude) model(tier string) (Model, bool) {
	m, ok := c.Models[tier]

	return m, ok
}

// defaultTier returns DefaultTier, or TierSonnet when it isn't set.
func (c Claude) defaultTier() string {
	if c.DefaultTier == "" {
		return TierSonnet
	}

	return c.DefaultTier
}

// orderedModels returns configured tier keys with the default tier first,
// then the others in tier order. Claude Desktop treats the first as its default.
func (c Claude) orderedModels() []string {
	def := c.defaultTier()
	models := []string{def}

	for _, tier := range tiers {
		if _, ok := c.model(tier); ok && tier != def {
			models = append(models, tier)
		}
	}

	return models
}

func (v *validator) validate(cfg Config, products []Product) {
	if len(products) == 0 {
		v.fail("products", "must select at least one of claude or codex")
	}

	for _, product := range products {
		if !product.Valid() {
			v.fail("products", fmt.Sprintf("unknown product %q; must be claude or codex", product))
		}
	}

	v.validateAWS(cfg.AWS)
	v.field("macos_profile.identifier_prefix", cfg.MacOSProfile.IdentifierPrefix, pattern(identifierPattern, "a reverse-DNS identifier such as com.example.ccwb"))
	v.field("macos_profile.organization", cfg.MacOSProfile.Organization, nonEmpty)

	if slices.Contains(products, ProductClaude) {
		if cfg.Claude == nil {
			v.fail("claude", "is required when claude is selected")
		} else {
			v.validateModels(cfg.Claude.Models, cfg.AWS)
			v.validateDefaultTier(*cfg.Claude)
			v.validateMCPServers(cfg.Claude.DesktopMCPServers)
		}
	}

	if slices.Contains(products, ProductCodex) {
		if cfg.Codex == nil {
			v.fail("codex", "is required when codex is selected")
		} else {
			v.validateCodex(*cfg.Codex)
		}
	}
}

func (v *validator) validateCodex(c Codex) {
	v.field("codex.default_model", c.DefaultModel, pattern(codexModelPattern,
		"a global. or us. Bedrock Runtime inference profile ID of an OpenAI model, such as global.openai.gpt-6.1-terra"))

	if !slices.Contains(codexReasoningEfforts, c.ReasoningEffort) {
		v.fail("codex.reasoning_effort", fmt.Sprintf("must be one of %s, got %q",
			strings.Join(codexReasoningEfforts, ", "), c.ReasoningEffort))
	}
}

func (v *validator) validateAWS(a AWS) {
	v.field("aws.account_id", a.AccountID, pattern(accountIDPattern, "a 12-digit AWS account ID"))
	v.field("aws.bedrock_region", a.BedrockRegion, pattern(regionPattern, "an AWS region name"))
	v.field("aws.sso_start_url", a.SSOStartURL, httpsURL)
	v.field("aws.sso_region", a.SSORegion, pattern(regionPattern, "an AWS region name"))
	v.field("aws.sso_role_name", a.SSORoleName, pattern(roleNamePattern, "a permission set name"))
}

func (v *validator) validateModels(models map[string]Model, a AWS) {
	for _, tier := range slices.Sorted(maps.Keys(models)) {
		m := models[tier]
		path := "claude.models." + tier

		if !slices.Contains(tiers, tier) {
			v.fail(path, fmt.Sprintf("must be %q, %q or %q", TierOpus, TierSonnet, TierHaiku))
		}

		v.field(path+".aip_arn", m.AIPARN, aipARN(a))
		v.field(path+".anthropic_model_id", m.AnthropicModelID, func(id string) error {
			if !modelIDPattern.MatchString(id) || !strings.Contains(id, tier) {
				return fmt.Errorf("must be an Anthropic model ID of the %s tier, such as claude-%s-5", tier, tier)
			}

			return nil
		})
	}

	for _, tier := range []string{TierSonnet, TierHaiku} {
		if _, ok := models[tier]; !ok {
			v.fail("claude.models", fmt.Sprintf("must include a %s model", tier))
		}
	}
}

func (v *validator) validateDefaultTier(cfg Claude) {
	if cfg.DefaultTier == "" {
		return
	}

	if _, ok := cfg.model(cfg.DefaultTier); !ok {
		v.fail("claude.default_tier", fmt.Sprintf("must be the tier of a configured model, got %q", cfg.DefaultTier))
	}
}

func (v *validator) validateMCPServers(servers []MCPServer) {
	seen := map[string]bool{}

	for i, s := range servers {
		path := fmt.Sprintf("claude.desktop_mcp_servers[%d]", i)

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

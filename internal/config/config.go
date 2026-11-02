// Package config defines the user-facing configuration model of saringan:
// data sources, censor rules and policies that group them together.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// CurrentVersion is the config schema version this build understands.
const CurrentVersion = 1

// DefaultFileNames are searched (in order) when --config is not provided.
var DefaultFileNames = []string{"saringan.yaml", "saringan.yml", ".saringan.yaml", ".saringan.yml"}

// Config is the root document.
type Config struct {
	Version  int               `yaml:"version" json:"version"`
	Sources  map[string]Source `yaml:"sources,omitempty" json:"sources,omitempty"`
	Rules    []Rule            `yaml:"rules,omitempty" json:"rules,omitempty"`
	Policies map[string]Policy `yaml:"policies,omitempty" json:"policies,omitempty"`
}

// Source describes one data integration. Only the fields relevant to the
// source `type` need to be filled in. String values support ${ENV_VAR}
// expansion.
type Source struct {
	Type string `yaml:"type" json:"type"` // file | http | postgres | mysql | env | stdin

	// file
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	Format string `yaml:"format,omitempty" json:"format,omitempty"` // json | yaml | csv | text (default: by extension, else text)

	// http
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Method  string            `yaml:"method,omitempty" json:"method,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty" json:"body,omitempty"`
	Auth    *Auth             `yaml:"auth,omitempty" json:"auth,omitempty"`

	// postgres / mysql
	DSN   string `yaml:"dsn,omitempty" json:"dsn,omitempty"`
	Query string `yaml:"query,omitempty" json:"query,omitempty"`

	// env
	Keys []string `yaml:"keys,omitempty" json:"keys,omitempty"`

	// common
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"` // e.g. "10s"
}

// Auth for http sources.
type Auth struct {
	Type     string `yaml:"type" json:"type"` // basic | bearer | header
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	Token    string `yaml:"token,omitempty" json:"token,omitempty"`
	Header   string `yaml:"header,omitempty" json:"header,omitempty"`
	Value    string `yaml:"value,omitempty" json:"value,omitempty"`
}

// Rule is a single censoring rule. Selectors (field, path, pattern) decide
// where the rule applies; the action decides how the value is censored.
//
//   - field: glob on the data field/key name, e.g. "ssn", "*token*", "card_*"
//   - path:  dot path with "*" wildcards, e.g. "user.credentials.password"
//   - pattern: regex applied to string values; when combined with field/path
//     it refines the selection, otherwise it applies to every string.
type Rule struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	Field   string `yaml:"field,omitempty" json:"field,omitempty"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty"`

	Action string `yaml:"action" json:"action"` // replace | mask | redact | hash | truncate | drop

	With      string `yaml:"with,omitempty" json:"with,omitempty"`             // replacement for action=replace (default *hidden*)
	MaskChar  string `yaml:"mask_char,omitempty" json:"mask_char,omitempty"`   // for action=mask (default *)
	KeepLeft  int    `yaml:"keep_left,omitempty" json:"keep_left,omitempty"`   // keep first N chars visible when masking
	KeepRight int    `yaml:"keep_right,omitempty" json:"keep_right,omitempty"` // keep last N chars visible when masking
	Keep      int    `yaml:"keep,omitempty" json:"keep,omitempty"`             // alias for keep_right (keep last N)

	CaseSensitive bool  `yaml:"case_sensitive,omitempty" json:"case_sensitive,omitempty"`
	Enabled       *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// IsEnabled reports whether the rule is active (default true).
func (r Rule) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

// Policy groups sources and rules. Everything a downstream AI agent is
// allowed to see passes through a policy.
type Policy struct {
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Sources     []string `yaml:"sources,omitempty" json:"sources,omitempty"`   // empty = all sources
	Rules       []string `yaml:"rules,omitempty" json:"rules,omitempty"`       // empty = all rules
	OnError     string   `yaml:"on_error,omitempty" json:"on_error,omitempty"` // allow (fail-open) | deny (fail-closed)
	Format      string   `yaml:"format,omitempty" json:"format,omitempty"`     // output format: json | text
}

var ( //nolint:gochecknoglobals
	validActions = map[string]bool{"replace": true, "mask": true, "redact": true, "hash": true, "truncate": true, "drop": true}
	validSources = map[string]bool{"file": true, "http": true, "postgres": true, "mysql": true, "env": true, "stdin": true}
	validOnError = map[string]bool{"": true, "allow": true, "deny": true}
	validFormats = map[string]bool{"": true, "json": true, "yaml": true, "csv": true, "text": true}
	validOutFmt  = map[string]bool{"": true, "json": true, "text": true}
)

// FindConfig returns the path of the first default config file that exists.
func FindConfig(startDir string) string {
	for _, name := range DefaultFileNames {
		p := filepath.Join(startDir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Load reads, expands ${ENV} placeholders and validates a config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

// Parse parses config bytes (already on disk).
func Parse(raw []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	// Expand ${ENV_VAR} placeholders after parsing so unset variables stay
	// visible as "${VAR}" (config remains valid before the env is configured).
	expandStrings(&c)
	if c.Sources == nil {
		c.Sources = map[string]Source{}
	}
	if c.Policies == nil {
		c.Policies = map[string]Policy{}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config back as YAML.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	header := "# saringan configuration — see https://github.com/bilhasry/saringan\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), data...), 0o600)
}

// Validate checks the whole configuration for consistency.
func (c *Config) Validate() error {
	var errs []string
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if c.Version != CurrentVersion {
		errs = append(errs, fmt.Sprintf("unsupported version %d (this build supports %d)", c.Version, CurrentVersion))
	}

	for name, s := range c.Sources {
		if err := validateSource(name, s); err != nil {
			errs = append(errs, err.Error())
		}
	}

	seen := map[string]bool{}
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Name == "" {
			r.Name = autoRuleName(i, *r)
		}
		if seen[r.Name] {
			errs = append(errs, fmt.Sprintf("duplicate rule name %q", r.Name))
		}
		seen[r.Name] = true
		if err := validateRule(*r); err != nil {
			errs = append(errs, err.Error())
		}
	}

	for name, p := range c.Policies {
		if !validOnError[p.OnError] {
			errs = append(errs, fmt.Sprintf("policy %q: on_error must be allow or deny, got %q", name, p.OnError))
		}
		if !validOutFmt[p.Format] {
			errs = append(errs, fmt.Sprintf("policy %q: format must be json or text, got %q", name, p.Format))
		}
		for _, sn := range p.Sources {
			if _, ok := c.Sources[sn]; !ok {
				errs = append(errs, fmt.Sprintf("policy %q references unknown source %q", name, sn))
			}
		}
		for _, rn := range p.Rules {
			if !seen[rn] {
				errs = append(errs, fmt.Sprintf("policy %q references unknown rule %q", name, rn))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateSource(name string, s Source) error {
	if !validSources[s.Type] {
		return fmt.Errorf("source %q: unknown type %q (want file|http|postgres|mysql|env|stdin)", name, s.Type)
	}
	switch s.Type {
	case "file":
		if s.Path == "" {
			return fmt.Errorf("source %q (file): path is required", name)
		}
	case "http":
		if s.URL == "" {
			return fmt.Errorf("source %q (http): url is required", name)
		}
		if s.Auth != nil && !validAuthType(s.Auth.Type) {
			return fmt.Errorf("source %q: auth.type must be basic|bearer|header, got %q", name, s.Auth.Type)
		}
	case "postgres", "mysql":
		if s.DSN == "" {
			return fmt.Errorf("source %q (%s): dsn is required", name, s.Type)
		}
		if s.Query == "" {
			return fmt.Errorf("source %q (%s): query is required", name, s.Type)
		}
	case "env":
		if len(s.Keys) == 0 {
			return fmt.Errorf("source %q (env): keys is required (list of variable names, or [\"*\"] for all)", name)
		}
	case "stdin":
		// nothing required
	}
	if !validFormats[s.Format] {
		return fmt.Errorf("source %q: unknown format %q (want json|yaml|csv|text)", name, s.Format)
	}
	return nil
}

func validAuthType(t string) bool {
	switch t {
	case "", "basic", "bearer", "header":
		return true
	}
	return false
}

func validateRule(r Rule) error {
	if !validActions[r.Action] {
		return fmt.Errorf("rule %q: unknown action %q (want replace|mask|redact|hash|truncate|drop)", r.Name, r.Action)
	}
	if r.Field == "" && r.Path == "" && r.Pattern == "" {
		return fmt.Errorf("rule %q: needs at least one of field, path or pattern", r.Name)
	}
	if r.Pattern != "" {
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("rule %q: bad pattern: %w", r.Name, err)
		}
	}
	if r.Path != "" && strings.HasPrefix(r.Path, ".") {
		return fmt.Errorf("rule %q: path must not start with '.'", r.Name)
	}
	if r.MaskChar != "" && len([]rune(r.MaskChar)) != 1 {
		return fmt.Errorf("rule %q: mask_char must be a single character", r.Name)
	}
	if r.Action == "truncate" && r.Keep <= 0 && r.KeepLeft <= 0 {
		return fmt.Errorf("rule %q: action truncate requires keep > 0", r.Name)
	}
	if r.Action == "drop" && r.Pattern != "" && r.Field == "" && r.Path == "" {
		// allowed: drop whole keys whose string value matches pattern
	}
	return nil
}

func autoRuleName(i int, r Rule) string {
	base := r.Field
	if base == "" {
		base = r.Path
	}
	if base == "" {
		base = r.Pattern
	}
	if base == "" {
		base = "rule"
	}
	slug := strings.Map(func(ch rune) rune {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			return ch
		case ch >= 'A' && ch <= 'Z':
			return ch + 32
		default:
			return '-'
		}
	}, base)
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "rule"
	}
	return fmt.Sprintf("%s-%d", slug, i+1)
}

// expandEnv expands ${VAR} and $VAR placeholders, leaving them intact when
// the variable is not set.
func expandEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(m string) string {
		names := envVarPattern.FindStringSubmatch(m)
		key := names[1]
		if key == "" {
			key = names[2]
		}
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return m
	})
}

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// expandStrings recursively expands env placeholders in every string field.
func expandStrings(v any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		expandValue(rv.Elem())
	}
}

func expandValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			expandValue(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				expandValue(v.Field(i))
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			val := v.MapIndex(k)
			cp := reflect.New(val.Type()).Elem()
			cp.Set(val)
			expandValue(cp)
			v.SetMapIndex(k, cp)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			expandValue(v.Index(i))
		}
	case reflect.String:
		v.SetString(expandEnv(v.String()))
	}
}

// Getters used by the CLI.

func (c *Config) GetRule(name string) (Rule, int, bool) {
	for i, r := range c.Rules {
		if r.Name == name {
			return r, i, true
		}
	}
	return Rule{}, -1, false
}

func (c *Config) GetPolicy(name string) (Policy, bool) {
	p, ok := c.Policies[name]
	return p, ok
}

// PolicyNames returns policy names sorted for stable output.
func (c *Config) PolicyNames() []string {
	names := make([]string, 0, len(c.Policies))
	for n := range c.Policies {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}

// SourceNames returns source names sorted.
func (c *Config) SourceNames() []string {
	names := make([]string, 0, len(c.Sources))
	for n := range c.Sources {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Default returns a friendly starter configuration used by `saringan init`.
func Default() *Config {
	enabled := true
	return &Config{
		Version: CurrentVersion,
		Sources: map[string]Source{
			"example-file": {
				Type:   "file",
				Path:   "./data/customers.json",
				Format: "json",
			},
			"example-api": {
				Type:   "http",
				URL:    "https://api.example.com/v1/customers",
				Method: "GET",
				Headers: map[string]string{
					"Accept": "application/json",
				},
				Auth: &Auth{Type: "bearer", Token: "${API_TOKEN}"},
			},
		},
		Rules: []Rule{
			{
				Name:        "hide-ssn",
				Description: "Never leak social security numbers",
				Field:       "*ssn*",
				Action:      "replace",
				With:        "*hidden*",
			},
			{
				Name:        "mask-credit-cards",
				Description: "Mask credit card numbers, keep last 4 digits",
				Pattern:     `\b(?:\d[ -]?){13,19}\b`,
				Action:      "mask",
				Keep:        4,
			},
			{
				Name:        "hide-password",
				Description: "Redact password fields entirely",
				Field:       "password",
				Action:      "redact",
			},
			{
				Name:        "hash-emails",
				Description: "Replace emails with a stable hash",
				Pattern:     `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
				Action:      "hash",
				Enabled:     &enabled,
			},
		},
		Policies: map[string]Policy{
			"agent-safe": {
				Description: "Data safe to hand to an AI agent",
				Sources:     []string{"example-file", "example-api"},
				Rules:       []string{"hide-ssn", "mask-credit-cards", "hide-password", "hash-emails"},
				OnError:     "deny",
				Format:      "json",
			},
		},
	}
}

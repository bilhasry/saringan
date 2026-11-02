package config

import (
	"os"
	"strings"
	"testing"
)

const sample = `
version: 1
sources:
  customers:
    type: file
    path: ./customers.json
rules:
  - name: hide-ssn
    field: "*ssn*"
    action: replace
    with: "*hidden*"
  - name: mask-card
    pattern: '\b\d{13,19}\b'
    action: mask
    keep: 4
policies:
  safe:
    sources: [customers]
    rules: [hide-ssn, mask-card]
    on_error: deny
`

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Rules) != 2 || len(c.Sources) != 1 || len(c.Policies) != 1 {
		t.Fatalf("unexpected shape: %+v", c)
	}
	if c.Rules[0].With != "*hidden*" {
		t.Fatalf("with not parsed: %+v", c.Rules[0])
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	bad := "version: 1\nrules:\n  - name: x\n    field: a\n    action: replace\n    typo_field: 1\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestInvalidAction(t *testing.T) {
	bad := "version: 1\nrules:\n  - name: x\n    field: a\n    action: explode\n"
	_, err := Parse([]byte(bad))
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("want unknown action error, got %v", err)
	}
}

func TestMissingSelector(t *testing.T) {
	bad := "version: 1\nrules:\n  - name: x\n    action: replace\n"
	_, err := Parse([]byte(bad))
	if err == nil || !strings.Contains(err.Error(), "field, path or pattern") {
		t.Fatalf("want selector error, got %v", err)
	}
}

func TestPolicyReferencingUnknownRule(t *testing.T) {
	bad := "version: 1\nrules:\n  - name: a\n    field: x\n    action: replace\npolicies:\n  p:\n    rules: [a, ghost]\n"
	_, err := Parse([]byte(bad))
	if err == nil || !strings.Contains(err.Error(), `unknown rule "ghost"`) {
		t.Fatalf("want unknown rule error, got %v", err)
	}
}

func TestEnvExpansion(t *testing.T) {
	t.Setenv("SARINGAN_TEST_DSN", "postgres://user:pass@db/x")
	raw := "version: 1\nsources:\n  db:\n    type: postgres\n    dsn: ${SARINGAN_TEST_DSN}\n    query: select 1\n"
	c, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Sources["db"].DSN != "postgres://user:pass@db/x" {
		t.Fatalf("env not expanded: %q", c.Sources["db"].DSN)
	}
}

func TestUnsetEnvKeptAsPlaceholder(t *testing.T) {
	raw := "version: 1\nsources:\n  db:\n    type: postgres\n    dsn: ${SARINGAN_UNSET_DSN_XYZ}\n    query: select 1\n"
	c, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("config should stay valid before env is set: %v", err)
	}
	if c.Sources["db"].DSN != "${SARINGAN_UNSET_DSN_XYZ}" {
		t.Fatalf("placeholder should be preserved: %q", c.Sources["db"].DSN)
	}
}

func TestAutoRuleName(t *testing.T) {
	raw := "version: 1\nrules:\n  - field: '*ssn*'\n    action: replace\n"
	c, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Rules[0].Name == "" {
		t.Fatal("expected auto-generated rule name")
	}
}

func TestTruncateRequiresKeep(t *testing.T) {
	bad := "version: 1\nrules:\n  - name: t\n    field: a\n    action: truncate\n"
	_, err := Parse([]byte(bad))
	if err == nil || !strings.Contains(err.Error(), "requires keep") {
		t.Fatalf("want keep error, got %v", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/saringan.yaml"
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c2.Rules) != len(c.Rules) || c2.Sources["customers"].Path != "./customers.json" {
		t.Fatalf("round trip mismatch: %+v", c2)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		t.Fatalf("file not written: %v", err)
	}
}

func TestFindConfig(t *testing.T) {
	dir := t.TempDir()
	if FindConfig(dir) != "" {
		t.Fatal("expected empty when no config")
	}
	if err := os.WriteFile(dir+"/.saringan.yml", []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := FindConfig(dir); got != dir+"/.saringan.yml" {
		t.Fatalf("wrong file found: %s", got)
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatalf("Default config invalid: %v", err)
	}
}

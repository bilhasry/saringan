package censor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bilhasry/saringan/internal/config"
)

func engine(t *testing.T, rules ...config.Rule) *Engine {
	t.Helper()
	e, err := NewEngine(rules)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(b)
}

func TestReplaceFieldGlob(t *testing.T) {
	e := engine(t, config.Rule{Name: "hide-ssn", Field: "*ssn*", Action: "replace", With: "*hidden*"})
	in := decode(t, `{"ssn":"123-45-6789","user_ssn":"987-65-4321","name":"ada"}`)
	out := encode(t, e.Apply(in))
	if !strings.Contains(out, "*hidden*") {
		t.Fatalf("expected *hidden* in %s", out)
	}
	if strings.Contains(out, "123-45-6789") || strings.Contains(out, "987-65-4321") {
		t.Fatalf("ssn leaked: %s", out)
	}
	if !strings.Contains(out, "ada") {
		t.Fatalf("non-matching field changed: %s", out)
	}
}

func TestMaskKeepsLastFour(t *testing.T) {
	e := engine(t, config.Rule{Name: "mask-card", Pattern: `\b(?:\d[ -]?){13,19}\b`, Action: "mask", Keep: 4})
	in := decode(t, `{"card":"4111 1111 1111 1111","note":"pay now"}`)
	out := encode(t, e.Apply(in))
	if !strings.Contains(out, "1111") {
		t.Fatalf("last 4 should remain: %s", out)
	}
	if strings.Contains(out, "4111 1111") {
		t.Fatalf("card prefix leaked: %s", out)
	}
}

func TestRedactPassword(t *testing.T) {
	e := engine(t, config.Rule{Name: "hide-password", Field: "password", Action: "redact"})
	in := decode(t, `{"password":"hunter2","user":"bob"}`)
	out := encode(t, e.Apply(in))
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked: %s", out)
	}
	if !strings.Contains(out, `"password":null`) {
		t.Fatalf("password should be null: %s", out)
	}
}

func TestDropKey(t *testing.T) {
	e := engine(t, config.Rule{Name: "drop-secret", Path: "meta.secret", Action: "drop"})
	in := decode(t, `{"meta":{"secret":"x","visible":"y"}}`)
	out := encode(t, e.Apply(in))
	if strings.Contains(out, "secret") {
		t.Fatalf("secret key should be gone: %s", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("sibling lost: %s", out)
	}
}

func TestHashEmailPattern(t *testing.T) {
	e := engine(t, config.Rule{Name: "hash-email", Pattern: `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`, Action: "hash"})
	in := decode(t, `{"contact":"mail ada@example.com today"}`)
	out := encode(t, e.Apply(in))
	if strings.Contains(out, "ada@example.com") {
		t.Fatalf("email leaked: %s", out)
	}
	if !strings.Contains(out, "sha256:") {
		t.Fatalf("expected sha256 hash: %s", out)
	}
	if !strings.Contains(out, "mail ") || !strings.Contains(out, " today") {
		t.Fatalf("surrounding text should survive: %s", out)
	}
}

func TestPathWithWildcardOverArrays(t *testing.T) {
	e := engine(t, config.Rule{Name: "hide-tokens", Path: "users.*.credentials.token", Action: "replace", With: "*hidden*"})
	in := decode(t, `{"users":[{"credentials":{"token":"aaa"}},{"credentials":{"token":"bbb","kind":"jwt"}}]}`)
	out := encode(t, e.Apply(in))
	if strings.Contains(out, "aaa") || strings.Contains(out, "bbb") {
		t.Fatalf("token leaked: %s", out)
	}
	if !strings.Contains(out, "jwt") {
		t.Fatalf("sibling field lost: %s", out)
	}
}

func TestNestedObjectFieldMatch(t *testing.T) {
	e := engine(t, config.Rule{Name: "hide-apikey", Field: "*api_key*", Action: "replace"})
	in := decode(t, `{"config":{"api_key":"sk-123","timeout":5},"items":[{"api_key":"sk-456"}]}`)
	out := encode(t, e.Apply(in))
	if strings.Contains(out, "sk-") {
		t.Fatalf("api key leaked: %s", out)
	}
	if !strings.Contains(out, `"timeout":5`) {
		t.Fatalf("unrelated field lost: %s", out)
	}
}

func TestReplaceWithCaptureGroups(t *testing.T) {
	e := engine(t, config.Rule{Name: "keep-region", Pattern: `SSN-(\d{3})-(\d{2})-(\d{4})`, Action: "replace", With: "SSN-***-**-$3"})
	in := decode(t, `{"id":"SSN-123-45-6789"}`)
	out := encode(t, e.Apply(in))
	if !strings.Contains(out, "SSN-***-**-6789") {
		t.Fatalf("capture-group replacement failed: %s", out)
	}
	if strings.Contains(out, "123-45") {
		t.Fatalf("prefix leaked: %s", out)
	}
}

func TestTruncate(t *testing.T) {
	e := engine(t, config.Rule{Name: "short-note", Field: "note", Action: "truncate", Keep: 3})
	in := decode(t, `{"note":"very long note"}`)
	out := encode(t, e.Apply(in))
	if !strings.Contains(out, "ver…") {
		t.Fatalf("truncate failed: %s", out)
	}
}

func TestDisabledRuleSkipped(t *testing.T) {
	off := false
	e := engine(t, config.Rule{Name: "off-rule", Field: "ssn", Action: "replace", Enabled: &off})
	in := decode(t, `{"ssn":"123"}`)
	if out := encode(t, e.Apply(in)); !strings.Contains(out, "123") {
		t.Fatalf("disabled rule must not fire: %s", out)
	}
}

func TestTextMode(t *testing.T) {
	e := engine(t,
		config.Rule{Name: "mask-card", Pattern: `\b(?:\d[ -]?){13,19}\b`, Action: "mask", Keep: 4},
		config.Rule{Name: "hide-ssn", Field: "*ssn*", Action: "replace"}, // inert in text mode
	)
	out := e.ApplyText("card 4111-1111-1111-1111 ok")
	if strings.Contains(out, "4111-1111-1111") {
		t.Fatalf("card leaked: %s", out)
	}
	if !strings.Contains(out, "1111 ok") {
		t.Fatalf("surrounding text lost: %s", out)
	}
}

func TestFindText(t *testing.T) {
	e := engine(t, config.Rule{Name: "mask-card", Pattern: `\b\d{16}\b`, Action: "mask"})
	matches := e.FindText("4111111111111111 and 4000000000000002")
	if len(matches) != 2 {
		t.Fatalf("want 2 matches, got %d: %+v", len(matches), matches)
	}
	if matches[0].Rule != "mask-card" {
		t.Fatalf("wrong rule: %+v", matches[0])
	}
}

func TestStatsCountHits(t *testing.T) {
	e := engine(t, config.Rule{Name: "hide-ssn", Field: "*ssn*", Action: "replace"})
	_ = e.Apply(decode(t, `{"ssn":"a","user_ssn":"b"}`))
	if got := e.Stats()["hide-ssn"]; got != 2 {
		t.Fatalf("want 2 hits, got %d", got)
	}
}

// Regression: nodes must be censored (and counted) exactly once.
func TestStatsNoDoubleCounting(t *testing.T) {
	e := engine(t,
		config.Rule{Name: "hide-notes", Path: "*.notes", Action: "replace", With: "[hidden]"},
		config.Rule{Name: "mask-card", Pattern: `\b(?:\d[ -]?){13,19}\b`, Action: "mask", Keep: 4},
	)
	in := decode(t, `{"customers":[{"notes":"a","card":"4111 1111 1111 1111"},{"notes":"b"}]}`)
	out := encode(t, e.Apply(in))
	stats := e.Stats()
	if stats["hide-notes"] != 2 {
		t.Fatalf("hide-notes want 2 hits, got %d (out=%s)", stats["hide-notes"], out)
	}
	if stats["mask-card"] != 1 {
		t.Fatalf("mask-card want 1 hit, got %d", stats["mask-card"])
	}
}

func TestMaskWholeValueNoPattern(t *testing.T) {
	e := engine(t, config.Rule{Name: "mask-card-field", Field: "card", Action: "mask", Keep: 4})
	in := decode(t, `{"card":"4111111111111111"}`)
	out := encode(t, e.Apply(in))
	if !strings.Contains(out, "************1111") {
		t.Fatalf("expected full mask with last 4 kept: %s", out)
	}
}

func TestEngineForPolicy(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Rules: []config.Rule{
			{Name: "a", Field: "x", Action: "replace"},
			{Name: "b", Field: "y", Action: "replace"},
		},
		Policies: map[string]config.Policy{
			"only-a": {Rules: []string{"a"}},
		},
	}
	e, err := NewEngineForPolicy(c, "only-a")
	if err != nil {
		t.Fatalf("NewEngineForPolicy: %v", err)
	}
	out := encode(t, e.Apply(decode(t, `{"x":"1","y":"2"}`)))
	if !strings.Contains(out, "*hidden*") {
		t.Fatalf("rule a should fire: %s", out)
	}
	if !strings.Contains(out, `"y":"2"`) {
		t.Fatalf("rule b must not fire: %s", out)
	}
}

func TestBadPatternRejected(t *testing.T) {
	if _, err := NewEngine([]config.Rule{{Name: "bad", Pattern: "([", Action: "replace"}}); err == nil {
		t.Fatal("expected error for bad regex")
	}
}

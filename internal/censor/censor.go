// Package censor implements the filtering engine: it applies configured
// rules to arbitrary data (decoded JSON/YAML/CSV structures or raw text).
package censor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/bilhasry/saringan/internal/config"
)

// Engine applies a set of compiled rules to data.
type Engine struct {
	rules    []*compiledRule
	mu       sync.Mutex
	counts   map[string]int // rule name -> number of applications
	warnings []string
}

type compiledRule struct {
	cfg    config.Rule
	re     *regexp.Regexp
	action string
	mask   rune
	keepL  int
	keepR  int
}

// NewEngine compiles rules. Disabled rules are skipped.
func NewEngine(rules []config.Rule) (*Engine, error) {
	e := &Engine{counts: map[string]int{}}
	for _, r := range rules {
		if !r.IsEnabled() {
			continue
		}
		if err := e.add(r); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// NewEngineForPolicy builds an engine from a config and policy name.
// An empty policy name means "use every rule".
func NewEngineForPolicy(c *config.Config, policyName string) (*Engine, error) {
	var rules []config.Rule
	if policyName == "" {
		rules = c.Rules
	} else {
		p, ok := c.GetPolicy(policyName)
		if !ok {
			return nil, fmt.Errorf("unknown policy %q (have: %s)", policyName, strings.Join(c.PolicyNames(), ", "))
		}
		if len(p.Rules) == 0 {
			rules = c.Rules
		} else {
			for _, name := range p.Rules {
				r, _, found := c.GetRule(name)
				if !found {
					return nil, fmt.Errorf("policy %q references unknown rule %q", policyName, name)
				}
				rules = append(rules, r)
			}
		}
	}
	return NewEngine(rules)
}

func (e *Engine) add(r config.Rule) error {
	cr := &compiledRule{cfg: r, action: r.Action}
	if r.Pattern != "" {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return fmt.Errorf("rule %q: %w", r.Name, err)
		}
		cr.re = re
	}
	cr.mask = '*'
	if r.MaskChar != "" {
		cr.mask = []rune(r.MaskChar)[0]
	}
	cr.keepL = r.KeepLeft
	cr.keepR = r.KeepRight
	if r.Keep > 0 && r.KeepRight == 0 {
		cr.keepR = r.Keep
	}
	e.rules = append(e.rules, cr)
	return nil
}

// Warnings returns non-fatal engine notes (e.g. rules that never matched).
func (e *Engine) Warnings() []string { return e.warnings }

// Stats returns how many times each rule fired.
func (e *Engine) Stats() map[string]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]int, len(e.counts))
	for k, v := range e.counts {
		out[k] = v
	}
	return out
}

func (e *Engine) hit(name string) {
	e.mu.Lock()
	e.counts[name]++
	e.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Value mode: works on decoded structures (map/slice/scalars).
// ---------------------------------------------------------------------------

// Apply walks a decoded value and returns a censored deep copy.
func (e *Engine) Apply(v any) any {
	out, _ := e.walk(v, nil)
	return out
}

// walk applies the rules to the current node (using its key/path), then
// recurses into containers. Every node is visited exactly once.
func (e *Engine) walk(v any, path []string) (any, bool) {
	key := ""
	if len(path) > 0 {
		key = path[len(path)-1]
	}
	nv, keep := e.applyNode(key, strings.Join(path, "."), v)
	if !keep {
		return nil, false
	}
	switch t := nv.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			childPath := append(append([]string{}, path...), k)
			cnv, ckeep := e.walk(child, childPath)
			if ckeep {
				out[k] = cnv
			}
		}
		return out, true
	case []any:
		out := make([]any, 0, len(t))
		for i, child := range t {
			childPath := append(append([]string{}, path...), fmt.Sprintf("%d", i))
			cnv, ckeep := e.walk(child, childPath)
			if ckeep {
				out = append(out, cnv)
			}
		}
		return out, true
	default:
		return nv, true
	}
}

// applyNode applies every rule that selects this node (key/path) and, when the
// rule has a pattern, only censors the matching spans of string values.
// Returns the new value and whether to keep it.
func (e *Engine) applyNode(key, pathStr string, v any) (any, bool) {
	keep := true
	val := v
	for _, r := range e.rules {
		nodeSel := r.selectsNode(key, pathStr)
		s, isString := val.(string)

		if r.re != nil {
			if !isString {
				// A pattern rule can still redact/drop non-strings selected
				// by field/path (e.g. drop an object named "secret").
				if !nodeSel || (r.action != "redact" && r.action != "drop") {
					continue
				}
			} else {
				if !nodeSel && (r.cfg.Field != "" || r.cfg.Path != "") {
					continue // pattern only refines an explicit field/path scope
				}
				if !r.re.MatchString(s) {
					continue
				}
				if r.action == "redact" || r.action == "drop" {
					// whole value goes away if any span matches
				} else {
					newVal := r.censorString(s, e)
					val = newVal
					continue
				}
			}
		} else {
			if !nodeSel {
				continue
			}
		}

		switch r.action {
		case "drop":
			keep = false
			e.hit(r.cfg.Name)
			return val, keep
		case "redact":
			val = nil
			e.hit(r.cfg.Name)
		case "hash":
			if isStringOrNil(val) {
				str := toString(val)
				val = r.hashValue(str)
				e.hit(r.cfg.Name)
			}
		case "truncate":
			if s2, ok := val.(string); ok {
				val = truncate(s2, r.truncateKeep())
				e.hit(r.cfg.Name)
			}
		case "mask", "replace":
			if s2, ok := val.(string); ok {
				// no pattern: censor the whole value
				val = r.censorWhole(s2)
				e.hit(r.cfg.Name)
			}
		}
	}
	return val, keep
}

// selectsNode reports whether field/path selectors pick this node.
// A rule with no field/path selects every node (pattern decides the rest).
func (c *compiledRule) selectsNode(key, pathStr string) bool {
	hasField := c.cfg.Field != ""
	hasPath := c.cfg.Path != ""
	if !hasField && !hasPath {
		return true
	}
	if hasField && key != "" && matchGlob(c.cfg.Field, key, c.cfg.CaseSensitive) {
		return true
	}
	if hasPath && matchPath(c.cfg.Path, pathStr) {
		return true
	}
	return false
}

// censorString censors only the spans matched by the rule pattern.
func (c *compiledRule) censorString(s string, e *Engine) string {
	switch c.action {
	case "mask":
		e.hit(c.cfg.Name)
		return c.maskSpans(s)
	case "replace":
		if strings.Contains(c.cfg.With, "$") && c.re != nil {
			e.hit(c.cfg.Name)
			return c.re.ReplaceAllString(s, c.cfg.With)
		}
		e.hit(c.cfg.Name)
		return c.re.ReplaceAllString(s, c.censoredReplacement())
	case "hash":
		e.hit(c.cfg.Name)
		return c.re.ReplaceAllStringFunc(s, func(m string) string {
			return c.hashValue(m)
		})
	case "truncate":
		e.hit(c.cfg.Name)
		return c.re.ReplaceAllStringFunc(s, func(m string) string {
			return truncate(m, c.truncateKeep())
		})
	}
	return s
}

// censorWhole censors the entire string value (no pattern refinement).
func (c *compiledRule) censorWhole(s string) string {
	switch c.action {
	case "mask":
		return mask(s, c.mask, c.keepL, c.keepR)
	case "replace":
		if c.cfg.With == "" {
			return "*hidden*"
		}
		return c.cfg.With
	}
	return s
}

// maskSpans masks every pattern match inside the string. Trailing separators
// (spaces, dashes) are preserved so "4111 1111 1111 1111 " keeps its spacing.
func (c *compiledRule) maskSpans(s string) string {
	return c.re.ReplaceAllStringFunc(s, func(m string) string {
		core, tail := splitTrailingSep(m)
		if core == "" {
			return m
		}
		return mask(core, c.mask, c.keepL, c.keepR) + tail
	})
}

// splitTrailingSep separates a matched string into content and a trailing run
// of separator characters that should stay visible.
func splitTrailingSep(m string) (string, string) {
	i := len(m)
	for i > 0 {
		switch m[i-1] {
		case ' ', '-', '.', ':':
			i--
		default:
			return m[:i], m[i:]
		}
	}
	return "", m
}

// truncateKeep returns how many leading characters action=truncate keeps.
func (c *compiledRule) truncateKeep() int {
	if c.keepL > 0 {
		return c.keepL
	}
	return c.cfg.Keep
}

func (c *compiledRule) censoredReplacement() string {
	if c.cfg.Action == "replace" && c.cfg.With != "" {
		return c.cfg.With
	}
	return "*hidden*"
}

func (c *compiledRule) hashValue(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// ---------------------------------------------------------------------------
// Text mode: raw strings (files, API responses, log lines).
// ---------------------------------------------------------------------------

// ApplyText censors a raw string. Only rules with a pattern apply; rules with
// field/path selectors cannot be evaluated without structure and are skipped.
func (e *Engine) ApplyText(s string) string {
	out := s
	for _, r := range e.rules {
		if r.re == nil {
			continue
		}
		switch r.action {
		case "mask":
			e.hit(r.cfg.Name)
			out = r.maskSpans(out)
		case "replace":
			if strings.Contains(r.cfg.With, "$") {
				out = r.re.ReplaceAllString(out, r.cfg.With)
			} else {
				out = r.re.ReplaceAllString(out, r.censoredReplacement())
			}
			e.hit(r.cfg.Name)
		case "hash":
			out = r.re.ReplaceAllStringFunc(out, func(m string) string {
				return r.hashValue(m)
			})
			e.hit(r.cfg.Name)
		case "truncate":
			out = r.re.ReplaceAllStringFunc(out, func(m string) string {
				return truncate(m, r.truncateKeep())
			})
			e.hit(r.cfg.Name)
		}
	}
	return out
}

// FindText returns the matches of every pattern rule in a raw string, used by
// `saringan scan` for dry runs.
func (e *Engine) FindText(s string) []Match {
	var out []Match
	for _, r := range e.rules {
		if r.re == nil {
			continue
		}
		for _, loc := range r.re.FindAllStringIndex(s, -1) {
			out = append(out, Match{
				Rule:   r.cfg.Name,
				Action: r.action,
				Text:   s[loc[0]:loc[1]],
			})
		}
	}
	return out
}

// Match is a single pattern hit reported by scan.
type Match struct {
	Rule   string `json:"rule" yaml:"rule"`
	Action string `json:"action" yaml:"action"`
	Text   string `json:"text" yaml:"text"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pathKey(path []string, seg string) string {
	if seg == "" {
		return strings.Join(path, ".")
	}
	return strings.Join(append(path, seg), ".")
}

// matchGlob matches "name" against a glob with '*' wildcards.
func matchGlob(pattern, name string, caseSensitive bool) bool {
	if !caseSensitive {
		pattern = strings.ToLower(pattern)
		name = strings.ToLower(name)
	}
	var sb strings.Builder
	sb.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

// matchPath matches "a.b.c" against a dot-path pattern whose segments may be '*'.
func matchPath(pattern, path string) bool {
	if pattern == path {
		return true
	}
	ps := strings.Split(pattern, ".")
	as := strings.Split(path, ".")
	return matchPathSegs(ps, as)
}

func matchPathSegs(pat, act []string) bool {
	if len(pat) == 0 {
		return len(act) == 0
	}
	if pat[0] == "*" {
		if len(act) == 0 {
			return false
		}
		if matchPathSegs(pat[1:], act[1:]) {
			return true
		}
		return matchPathSegs(pat, act[1:]) // '*' also spans missing segments
	}
	if len(act) == 0 || pat[0] != act[0] {
		return false
	}
	return matchPathSegs(pat[1:], act[1:])
}

func mask(s string, ch rune, keepL, keepR int) string {
	runes := []rune(s)
	n := len(runes)
	if keepL+keepR >= n {
		// nothing meaningful to hide
		if keepL == 0 && keepR == 0 {
			return strings.Repeat(string(ch), n)
		}
		return s
	}
	var sb strings.Builder
	if keepL > 0 {
		sb.WriteString(string(runes[:keepL]))
	}
	sb.WriteString(strings.Repeat(string(ch), n-keepL-keepR))
	if keepR > 0 {
		sb.WriteString(string(runes[n-keepR:]))
	}
	return sb.String()
}

func truncate(s string, keep int) string {
	runes := []rune(s)
	if len(runes) <= keep {
		return s
	}
	return string(runes[:keep]) + "…"
}

func isStringOrNil(v any) bool {
	_, ok := v.(string)
	return ok || v == nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bilhasry/saringan/internal/config"
)

type httpSource struct {
	name string
	cfg  config.Source
}

func newHTTPSource(name string, cfg config.Source) *httpSource {
	return &httpSource{name: name, cfg: cfg}
}

func (h *httpSource) Name() string { return h.name }
func (h *httpSource) Type() string { return "http" }

func (h *httpSource) Fetch(ctx context.Context) (*Document, error) {
	method := h.cfg.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if h.cfg.Body != "" {
		body = strings.NewReader(h.cfg.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.cfg.URL, body)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", h.name, err)
	}
	for k, v := range h.cfg.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if h.cfg.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.cfg.Auth != nil {
		if err := applyAuth(req, h.cfg.Auth); err != nil {
			return nil, fmt.Errorf("source %q: %w", h.name, err)
		}
	}

	timeout := ParseTimeout(h.cfg.Timeout, 30*time.Second)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source %q: request failed: %w", h.name, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("source %q: read response: %w", h.name, err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("source %q: HTTP %s: %s", h.name, resp.Status, truncateForErr(string(data), 300))
	}

	doc := &Document{
		Source:      h.name,
		Raw:         string(data),
		ContentType: resp.Header.Get("Content-Type"),
	}
	if looksJSON(resp.Header.Get("Content-Type"), string(data)) {
		var v any
		if err := json.Unmarshal(data, &v); err == nil {
			doc.Data = v
		}
	}
	return doc, nil
}

func applyAuth(req *http.Request, a *config.Auth) error {
	switch a.Type {
	case "", "bearer":
		if a.Token == "" {
			return fmt.Errorf("auth.bearer requires a token")
		}
		req.Header.Set("Authorization", "Bearer "+a.Token)
	case "basic":
		req.SetBasicAuth(a.Username, a.Password)
	case "header":
		if a.Header == "" {
			return fmt.Errorf("auth.header requires a header name")
		}
		req.Header.Set(a.Header, a.Value)
	default:
		return fmt.Errorf("unknown auth type %q", a.Type)
	}
	return nil
}

func looksJSON(contentType, body string) bool {
	if strings.Contains(contentType, "json") {
		return true
	}
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func truncateForErr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

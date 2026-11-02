package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type stdinSource struct {
	name string
}

func newStdinSource(name string) *stdinSource {
	return &stdinSource{name: name}
}

func (s *stdinSource) Name() string { return s.name }
func (s *stdinSource) Type() string { return "stdin" }

func (s *stdinSource) Fetch(_ context.Context) (*Document, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("source %q: read stdin: %w", s.name, err)
	}
	doc := &Document{Source: s.name, Raw: string(data), ContentType: "text/plain"}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var v any
		if err := json.Unmarshal(data, &v); err == nil {
			doc.Data = v
			doc.ContentType = "application/json"
		}
	}
	return doc, nil
}

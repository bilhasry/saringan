// Package source implements the data integrations saringan can pull data
// from: files, HTTP APIs, SQL databases, environment variables and stdin.
package source

import (
	"context"
	"fmt"
	"time"

	"github.com/bilhasry/saringan/internal/config"
)

// Document is the normalized result of fetching from a source.
type Document struct {
	Data        any    // parsed value (map/slice/scalars) when the payload is structured
	Raw         string // original payload when it is plain text
	ContentType string
	Source      string
}

// IsStructured reports whether the document carries a parsed value.
func (d *Document) IsStructured() bool { return d.Data != nil }

// Source fetches a document.
type Source interface {
	Name() string
	Type() string
	Fetch(ctx context.Context) (*Document, error)
}

// Build instantiates a source by name from the configuration.
func Build(name string, cfg config.Source) (Source, error) {
	switch cfg.Type {
	case "file":
		return newFileSource(name, cfg), nil
	case "http":
		return newHTTPSource(name, cfg), nil
	case "postgres", "mysql":
		return newDBSource(name, cfg), nil
	case "env":
		return newEnvSource(name, cfg), nil
	case "stdin":
		return newStdinSource(name), nil
	default:
		return nil, fmt.Errorf("source %q: unknown type %q", name, cfg.Type)
	}
}

// ParseTimeout parses the optional timeout string.
func ParseTimeout(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

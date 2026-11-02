package source

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/bilhasry/saringan/internal/config"
)

type fileSource struct {
	name string
	cfg  config.Source
}

func newFileSource(name string, cfg config.Source) *fileSource {
	return &fileSource{name: name, cfg: cfg}
}

func (f *fileSource) Name() string { return f.name }
func (f *fileSource) Type() string { return "file" }

func (f *fileSource) Fetch(ctx context.Context) (*Document, error) {
	data, err := os.ReadFile(f.cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("source %q: read %s: %w", f.name, f.cfg.Path, err)
	}
	format := f.cfg.Format
	if format == "" {
		format = formatFromExt(f.cfg.Path)
	}
	doc := &Document{Source: f.name, Raw: string(data)}
	switch format {
	case "json":
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("source %q: parse json: %w", f.name, err)
		}
		doc.Data = v
		doc.ContentType = "application/json"
	case "yaml":
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("source %q: parse yaml: %w", f.name, err)
		}
		doc.Data = normalizeYAML(v)
		doc.ContentType = "application/yaml"
	case "csv":
		rows, err := parseCSV(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("source %q: parse csv: %w", f.name, err)
		}
		doc.Data = rows
		doc.ContentType = "text/csv"
	default:
		doc.ContentType = "text/plain"
	}
	return doc, nil
}

func formatFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".csv":
		return "csv"
	default:
		return "text"
	}
}

// parseCSV turns CSV into a slice of row maps with string values.
func parseCSV(r io.Reader) ([]map[string]any, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(header))
		for i, col := range header {
			if i < len(rec) {
				row[col] = rec[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// normalizeYAML converts yaml.MapSlice/map[any]any into JSON-friendly maps.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeYAML(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprintf("%v", k)] = normalizeYAML(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeYAML(val)
		}
		return out
	default:
		return v
	}
}

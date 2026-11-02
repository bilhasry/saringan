package source

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// SQL drivers
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/bilhasry/saringan/internal/config"
)

type dbSource struct {
	name string
	cfg  config.Source
}

func newDBSource(name string, cfg config.Source) *dbSource {
	return &dbSource{name: name, cfg: cfg}
}

func (d *dbSource) Name() string { return d.name }
func (d *dbSource) Type() string { return d.cfg.Type }

func (d *dbSource) Fetch(ctx context.Context) (*Document, error) {
	openCtx, cancel := context.WithTimeout(ctx, ParseTimeout(d.cfg.Timeout, 15*time.Second))
	defer cancel()

	db, err := sql.Open(d.cfg.Type, d.cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("source %q: open %s: %w", d.name, d.cfg.Type, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)

	rows, err := db.QueryContext(openCtx, d.cfg.Query)
	if err != nil {
		return nil, fmt.Errorf("source %q: query: %w", d.name, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("source %q: columns: %w", d.name, err)
	}

	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("source %q: scan: %w", d.name, err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = coerce(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("source %q: rows: %w", d.name, err)
	}
	return &Document{Data: out, Source: d.name, ContentType: "application/json"}, nil
}

// coerce turns driver values into JSON-friendly scalars.
func coerce(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return v
	}
}

package source

import (
	"context"
	"os"
	"sort"

	"github.com/bilhasry/saringan/internal/config"
)

type envSource struct {
	name string
	cfg  config.Source
}

func newEnvSource(name string, cfg config.Source) *envSource {
	return &envSource{name: name, cfg: cfg}
}

func (e *envSource) Name() string { return e.name }
func (e *envSource) Type() string { return "env" }

func (e *envSource) Fetch(_ context.Context) (*Document, error) {
	out := map[string]any{}
	for _, key := range e.cfg.Keys {
		if key == "*" {
			for _, kv := range os.Environ() {
				for i := 0; i < len(kv); i++ {
					if kv[i] == '=' {
						out[kv[:i]] = kv[i+1:]
						break
					}
				}
			}
			continue
		}
		val, ok := os.LookupEnv(key)
		if !ok {
			// missing env vars are reported as null so downstream sees gaps
			out[key] = nil
			continue
		}
		out[key] = val
	}
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]any, len(out))
	for _, k := range keys {
		sorted[k] = out[k]
	}
	return &Document{Data: sorted, Source: e.name, ContentType: "application/json"}, nil
}

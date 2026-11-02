package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/censor"
	"github.com/bilhasry/saringan/internal/source"
)

var (
	runPolicy  string
	runSources []string
	runFormat  string
	runOut     string
	runStats   bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Fetch data from sources and emit the censored result",
	Long: `run pulls data through the configured integrations, applies every rule
of the selected policy and prints the censored output. This is the command an
AI agent should call to obtain data safely.

On error the policy's on_error setting decides: "deny" aborts (fail-closed),
"allow" skips the failing source (fail-open).`,
	Example: `  saringan run --policy agent-safe
  saringan run --policy agent-safe --source customers --stats
  saringan run --policy agent-safe --out safe.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		policyName := runPolicy
		if policyName == "" {
			names := c.PolicyNames()
			switch {
			case len(names) == 1:
				policyName = names[0]
			case len(names) == 0:
				return fmt.Errorf("no policies defined; add one with `saringan policy add`")
			default:
				return fmt.Errorf("--policy is required (available: %s)", strings.Join(names, ", "))
			}
		}
		p, ok := c.GetPolicy(policyName)
		if !ok {
			return fmt.Errorf("unknown policy %q (available: %s)", policyName, strings.Join(c.PolicyNames(), ", "))
		}

		eng, err := censor.NewEngineForPolicy(c, policyName)
		if err != nil {
			return err
		}

		sourceNames := runSources
		if len(sourceNames) == 0 {
			sourceNames = p.Sources
			if len(sourceNames) == 0 {
				sourceNames = c.SourceNames()
			}
		}
		if len(sourceNames) == 0 {
			return fmt.Errorf("policy %q has no sources to fetch", policyName)
		}

		onError := orDefault(p.OnError, "allow")
		results := map[string]any{}
		var textOut []string
		var failures []string

		for _, name := range sourceNames {
			cfg, ok := c.Sources[name]
			if !ok {
				if onError == "deny" {
					return fmt.Errorf("policy %q references unknown source %q", policyName, name)
				}
				failures = append(failures, fmt.Sprintf("%s: unknown source", name))
				continue
			}
			src, err := source.Build(name, cfg)
			if err != nil {
				if onError == "deny" {
					return err
				}
				failures = append(failures, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			doc, err := src.Fetch(ctx)
			cancel()
			if err != nil {
				if onError == "deny" {
					return err
				}
				failures = append(failures, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			if doc.IsStructured() {
				results[name] = eng.Apply(doc.Data)
			} else {
				censored := eng.ApplyText(doc.Raw)
				if len(sourceNames) == 1 {
					results[name] = censored
				}
				textOut = append(textOut, censored)
			}
		}

		// ---- output -------------------------------------------------------
		format := orDefault(runFormat, orDefault(p.Format, "json"))
		var payload []byte
		switch format {
		case "text":
			payload = []byte(strings.Join(textOut, "\n"))
			if len(textOut) == 0 {
				// fall back to JSON of structured results
				payload, err = marshalJSON(results)
			}
		default:
			if len(results) == 1 && len(runSources) <= 1 {
				for _, v := range results {
					payload, err = marshalJSON(v)
				}
			} else {
				payload, err = marshalJSON(results)
			}
		}
		if err != nil {
			return err
		}

		if runOut != "" {
			if err := os.WriteFile(runOut, append(payload, '\n'), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote censored output to %s\n", runOut)
		} else {
			fmt.Println(string(payload))
		}

		if runStats {
			printStats(eng, policyName, failures)
		} else if len(failures) > 0 {
			for _, f := range failures {
				fmt.Fprintf(os.Stderr, "warning: skipped source %s\n", f)
			}
		}
		return nil
	},
}

func init() {
	runCmd.Flags().StringVarP(&runPolicy, "policy", "p", "", "policy to apply (default: the only one defined)")
	runCmd.Flags().StringArrayVarP(&runSources, "source", "s", nil, "override sources to fetch (repeatable)")
	runCmd.Flags().StringVarP(&runFormat, "format", "f", "", "output format: json|text (default: policy.format)")
	runCmd.Flags().StringVarP(&runOut, "out", "o", "", "write output to a file instead of stdout")
	runCmd.Flags().BoolVar(&runStats, "stats", false, "print per-rule censor counts to stderr")
}

func printStats(eng *censor.Engine, policy string, failures []string) {
	stats := eng.Stats()
	fmt.Fprintf(os.Stderr, "--- censor report (policy: %s) ---\n", policy)
	if len(stats) == 0 {
		fmt.Fprintln(os.Stderr, "no rule matched anything")
	}
	for _, name := range sortedKeys(stats) {
		fmt.Fprintf(os.Stderr, "  %-24s %d hit(s)\n", name, stats[name])
	}
	for _, w := range eng.Warnings() {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "  skipped: %s\n", f)
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode output: %w", err)
	}
	return b, nil
}

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/censor"
	"github.com/bilhasry/saringan/internal/config"
)

func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

func parseJSON(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func join(items []string) string { return strings.Join(items, ", ") }

var (
	scanPolicy string
	scanIn     string
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Dry-run: show which rules match without censoring anything",
	Long: `scan reports every match of every pattern rule in the input so you can
tune rules before enabling them. Nothing is censored; matches are printed with
the rule that would fire.`,
	Example: `  saringan scan --policy agent-safe --in customers.json
  echo "my card is 4111 1111 1111 1111" | saringan scan --policy agent-safe`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		policyName := scanPolicy
		if policyName == "" {
			names := c.PolicyNames()
			switch {
			case len(names) == 1:
				policyName = names[0]
			case len(names) == 0:
				return fmt.Errorf("no policies defined; add one with `saringan policy add`")
			default:
				return fmt.Errorf("--policy is required (available: %s)", join(names))
			}
		}
		eng, err := censor.NewEngineForPolicy(c, policyName)
		if err != nil {
			return err
		}

		in := io.Reader(os.Stdin)
		if scanIn != "" {
			f, err := os.Open(scanIn)
			if err != nil {
				return err
			}
			defer f.Close()
			in = f
		}
		data, err := io.ReadAll(in)
		if err != nil {
			return fmt.Errorf("read input: %w", err)
		}

		matches := eng.FindText(string(data))

		// Field/path rules cannot match without structure: report them so the
		// user knows they are inert in text mode.
		var structuredOnly []string
		for _, r := range c.Rules {
			if !r.IsEnabled() {
				continue
			}
			if r.Pattern == "" && (r.Field != "" || r.Path != "") {
				if policyName == "" || policyUsesRule(c, policyName, r.Name) {
					structuredOnly = append(structuredOnly, r.Name)
				}
			}
		}

		if flagJSON {
			return printJSON(map[string]any{
				"policy":          policyName,
				"matches":         matches,
				"count":           len(matches),
				"structured_only": structuredOnly,
			})
		}

		if len(matches) == 0 {
			fmt.Println("no pattern match found")
		} else {
			fmt.Printf("%s found:\n\n", plural(len(matches), "match"))
			for i, m := range matches {
				fmt.Printf("  %3d. rule=%-20s action=%-9s match=%q\n", i+1, m.Rule, m.Action, m.Text)
			}
		}
		if len(structuredOnly) > 0 {
			fmt.Printf("\nNote: these rules only apply to structured data (field/path selectors):\n")
			for _, n := range structuredOnly {
				fmt.Printf("  - %s\n", n)
			}
			fmt.Println("Use `saringan run` or `saringan filter` on JSON/YAML input to exercise them.")
		}
		return nil
	},
}

func policyUsesRule(c *config.Config, policyName, ruleName string) bool {
	p, ok := c.GetPolicy(policyName)
	if !ok {
		return true
	}
	if len(p.Rules) == 0 {
		return true
	}
	for _, n := range p.Rules {
		if n == ruleName {
			return true
		}
	}
	return false
}

func init() {
	scanCmd.Flags().StringVarP(&scanPolicy, "policy", "p", "", "policy to apply (default: the only one defined)")
	scanCmd.Flags().StringVarP(&scanIn, "in", "i", "", "scan a file instead of stdin")
}

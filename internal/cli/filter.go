package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/censor"
)

var (
	filterPolicy string
	filterIn     string
	filterFormat string
	filterStats  bool
)

var filterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Censor a stream (stdin or a file) with a policy — ideal for piping",
	Long: `filter reads raw input, applies every pattern rule of the policy and
writes the censored result to stdout. Use it as a pipe stage between a data
tool and an AI agent:

  curl -s https://api.example.com/report | saringan filter --policy agent-safe | agent`,
	Example: `  cat customers.json | saringan filter --policy agent-safe
  saringan filter --policy agent-safe --in customers.csv --format text
  echo "ssn=123-45-6789" | saringan filter --policy agent-safe`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		policyName := filterPolicy
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
		if filterIn != "" {
			f, err := os.Open(filterIn)
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

		out := censorOut(eng, string(data), filterFormat)
		if _, err := os.Stdout.Write([]byte(out)); err != nil {
			return err
		}
		if len(out) > 0 && out[len(out)-1] != '\n' {
			fmt.Println()
		}
		if filterStats {
			printStats(eng, policyName, nil)
		}
		return nil
	},
}

func censorOut(eng *censor.Engine, input, format string) string {
	// Structured input goes through value mode so field/path rules apply.
	if looksLikeJSON(input) {
		if v, err := parseJSON(input); err == nil {
			censored := eng.Apply(v)
			if format == "text" {
				return fmt.Sprintf("%v", censored)
			}
			if b, err := marshalJSON(censored); err == nil {
				return string(b)
			}
		}
	}
	return eng.ApplyText(input)
}

func init() {
	filterCmd.Flags().StringVarP(&filterPolicy, "policy", "p", "", "policy to apply (default: the only one defined)")
	filterCmd.Flags().StringVarP(&filterIn, "in", "i", "", "read from a file instead of stdin")
	filterCmd.Flags().StringVarP(&filterFormat, "format", "f", "", "json|text for structured input (default json)")
	filterCmd.Flags().BoolVar(&filterStats, "stats", false, "print per-rule censor counts to stderr")
}

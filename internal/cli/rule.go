package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/censor"
	"github.com/bilhasry/saringan/internal/config"
)

var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Add, remove, inspect and test censor rules",
}

var (
	ruleName        string
	ruleField       string
	rulePath        string
	rulePattern     string
	ruleAction      string
	ruleWith        string
	ruleMaskChar    string
	ruleKeep        int
	ruleKeepLeft    int
	ruleDescription string
	ruleDisabled    bool
	ruleCaseSens    bool
)

func init() {
	ruleCmd.PersistentFlags().StringVar(&ruleName, "name", "", "unique rule name")
	ruleCmd.PersistentFlags().StringVar(&ruleField, "field", "", `field/key glob, e.g. "*ssn*" or "password"`)
	ruleCmd.PersistentFlags().StringVar(&rulePath, "path", "", `dot path, e.g. "user.credentials.password"`)
	ruleCmd.PersistentFlags().StringVar(&rulePattern, "pattern", "", "regex applied to string values")
	ruleCmd.PersistentFlags().StringVarP(&ruleAction, "action", "a", "", "replace|mask|redact|hash|truncate|drop")
	ruleCmd.PersistentFlags().StringVarP(&ruleWith, "with", "w", "", `replacement for action=replace (default "*hidden*")`)
	ruleCmd.PersistentFlags().StringVar(&ruleMaskChar, "mask-char", "", "mask character for action=mask (default *)")
	ruleCmd.PersistentFlags().IntVar(&ruleKeep, "keep", 0, "keep last N characters when masking")
	ruleCmd.PersistentFlags().IntVar(&ruleKeepLeft, "keep-left", 0, "keep first N characters when masking")
	ruleCmd.PersistentFlags().StringVarP(&ruleDescription, "description", "d", "", "human description of the rule")
	ruleCmd.PersistentFlags().BoolVar(&ruleDisabled, "disabled", false, "add the rule in disabled state")
	ruleCmd.PersistentFlags().BoolVar(&ruleCaseSens, "case-sensitive", false, "case-sensitive field matching (default off)")

	ruleCmd.AddCommand(ruleAddCmd, ruleRemoveCmd, ruleShowCmd, ruleEnableCmd, ruleDisableCmd, ruleTestCmd)
}

var ruleAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Append a censor rule to the config",
	Example: `  saringan rule add --name hide-ssn --field "*ssn*" --action replace --with "*hidden*"
  saringan rule add --name mask-card --pattern '\b\d{13,19}\b' --action mask --keep 4
  saringan rule add --name drop-secret --path "meta.secret" --action drop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}
		if ruleName == "" {
			return fmt.Errorf("--name is required")
		}
		if _, _, exists := c.GetRule(ruleName); exists {
			return fmt.Errorf("rule %q already exists", ruleName)
		}
		if ruleAction == "" {
			return fmt.Errorf("--action is required (replace|mask|redact|hash|truncate|drop)")
		}
		if ruleField == "" && rulePath == "" && rulePattern == "" {
			return fmt.Errorf("provide at least one selector: --field, --path or --pattern")
		}

		r := config.Rule{
			Name:          ruleName,
			Description:   ruleDescription,
			Field:         ruleField,
			Path:          rulePath,
			Pattern:       rulePattern,
			Action:        ruleAction,
			With:          ruleWith,
			MaskChar:      ruleMaskChar,
			Keep:          ruleKeep,
			KeepLeft:      ruleKeepLeft,
			CaseSensitive: ruleCaseSens,
		}
		if ruleDisabled {
			off := false
			r.Enabled = &off
		}

		// Validate in isolation before touching the file.
		trial := *c
		trial.Rules = append(append([]config.Rule{}, c.Rules...), r)
		if err := trial.Validate(); err != nil {
			return err
		}
		// Engine compile check (regex sanity, truncate/keep constraints).
		if _, err := censor.NewEngine(trial.Rules); err != nil {
			return err
		}

		c.Rules = append(c.Rules, r)
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("✔ added rule %q to %s\n", r.Name, path)
		return nil
	},
}

var ruleRemoveCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "delete"},
	Short:   "Delete a rule from the config",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}
		_, idx, found := c.GetRule(args[0])
		if !found {
			return fmt.Errorf("unknown rule %q", args[0])
		}
		c.Rules = append(c.Rules[:idx], c.Rules[idx+1:]...)
		// Drop references from policies.
		for name, p := range c.Policies {
			var kept []string
			for _, rn := range p.Rules {
				if rn != args[0] {
					kept = append(kept, rn)
				}
			}
			p.Rules = kept
			c.Policies[name] = p
		}
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("✔ removed rule %q from %s\n", args[0], path)
		return nil
	},
}

var ruleShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show one rule in detail",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		r, _, found := c.GetRule(args[0])
		if !found {
			return fmt.Errorf("unknown rule %q", args[0])
		}
		if flagJSON {
			return printJSON(r)
		}
		fmt.Printf("rule:        %s\n", r.Name)
		if r.Description != "" {
			fmt.Printf("description: %s\n", r.Description)
		}
		fmt.Printf("action:      %s\n", r.Action)
		if r.Field != "" {
			fmt.Printf("field:       %s\n", r.Field)
		}
		if r.Path != "" {
			fmt.Printf("path:        %s\n", r.Path)
		}
		if r.Pattern != "" {
			fmt.Printf("pattern:     %s\n", r.Pattern)
		}
		if r.With != "" {
			fmt.Printf("with:        %s\n", r.With)
		}
		if r.Action == "mask" {
			fmt.Printf("mask:        char=%q keep_left=%d keep_right=%d\n", maskCharOrDefault(r), r.KeepLeft, keepRight(r))
		}
		fmt.Printf("enabled:     %v\n", r.IsEnabled())
		return nil
	},
}

func maskCharOrDefault(r config.Rule) string {
	if r.MaskChar != "" {
		return r.MaskChar
	}
	return "*"
}

func keepRight(r config.Rule) int {
	if r.KeepRight > 0 {
		return r.KeepRight
	}
	return r.Keep
}

var ruleEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a disabled rule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setRuleEnabled(args[0], true)
	},
}

var ruleDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a rule without deleting it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setRuleEnabled(args[0], false)
	},
}

func setRuleEnabled(name string, enabled bool) error {
	c, path, err := loadConfig()
	if err != nil {
		return err
	}
	_, idx, found := c.GetRule(name)
	if !found {
		return fmt.Errorf("unknown rule %q", name)
	}
	c.Rules[idx].Enabled = &enabled
	if err := c.Save(path); err != nil {
		return err
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("✔ %s rule %q\n", state, name)
	return nil
}

var ruleTestCmd = &cobra.Command{
	Use:   "test <name> <text...>",
	Short: "Dry-run one rule against sample text (or stdin)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		r, _, found := c.GetRule(args[0])
		if !found {
			return fmt.Errorf("unknown rule %q", args[0])
		}
		text := ""
		if len(args) == 2 {
			text = args[1]
		} else {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			text = string(data)
		}
		eng, err := censor.NewEngine([]config.Rule{r})
		if err != nil {
			return err
		}
		out := eng.ApplyText(text)
		matches := eng.FindText(text)
		if flagJSON {
			return printJSON(map[string]any{
				"rule":    r.Name,
				"input":   text,
				"output":  out,
				"matches": matches,
			})
		}
		fmt.Printf("rule:   %s (%s)\n", r.Name, r.Action)
		fmt.Printf("input:  %s\n", text)
		fmt.Printf("output: %s\n", out)
		if r.Field != "" || r.Path != "" {
			fmt.Println("note:   field/path selectors only apply to structured data; use `saringan filter` or `run` for those")
		}
		if len(matches) == 0 {
			fmt.Println("result: no pattern match")
		} else {
			fmt.Printf("result: %s matched\n", plural(len(matches), "match"))
		}
		return nil
	},
}

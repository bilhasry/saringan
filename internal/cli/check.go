package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/censor"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate the configuration and report warnings",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}

		// Compile every rule through the engine to catch engine-level issues.
		if _, err := censor.NewEngine(c.Rules); err != nil {
			return err
		}

		enabled, disabled := 0, 0
		for _, r := range c.Rules {
			if r.IsEnabled() {
				enabled++
			} else {
				disabled++
			}
		}

		var warnings []string
		for _, pName := range c.PolicyNames() {
			p := c.Policies[pName]
			if len(p.Rules) == 0 && len(c.Rules) == 0 {
				warnings = append(warnings, fmt.Sprintf("policy %q has no rules: output will be uncensored", pName))
			}
			if len(p.Sources) == 0 && len(c.Sources) == 0 {
				warnings = append(warnings, fmt.Sprintf("policy %q has no sources", pName))
			}
		}
		usedRules := map[string]bool{}
		for _, p := range c.Policies {
			if len(p.Rules) == 0 {
				for _, r := range c.Rules {
					usedRules[r.Name] = true
				}
				continue
			}
			for _, r := range p.Rules {
				usedRules[r] = true
			}
		}
		for _, r := range c.Rules {
			if !usedRules[r.Name] {
				warnings = append(warnings, fmt.Sprintf("rule %q is not referenced by any policy", r.Name))
			}
			if r.IsEnabled() && r.Pattern == "" && r.Field == "" && r.Path == "" {
				warnings = append(warnings, fmt.Sprintf("rule %q has no selectors", r.Name))
			}
		}

		if flagJSON {
			return printJSON(map[string]any{
				"config":         path,
				"valid":          true,
				"sources":        len(c.Sources),
				"rules":          len(c.Rules),
				"rules_enabled":  enabled,
				"rules_disabled": disabled,
				"policies":       len(c.Policies),
				"warnings":       warnings,
			})
		}

		fmt.Printf("✔ %s is valid\n\n", path)
		fmt.Printf("  sources:  %d (%s)\n", len(c.Sources), strings.Join(c.SourceNames(), ", "))
		fmt.Printf("  rules:    %d enabled, %d disabled\n", enabled, disabled)
		fmt.Printf("  policies: %d (%s)\n", len(c.Policies), strings.Join(c.PolicyNames(), ", "))
		if len(warnings) > 0 {
			fmt.Println("\nWarnings:")
			for _, w := range warnings {
				fmt.Printf("  ! %s\n", w)
			}
		} else {
			fmt.Println("\nNo warnings.")
		}
		return nil
	},
}

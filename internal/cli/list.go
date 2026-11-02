package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/config"
)

var listCmd = &cobra.Command{
	Use:   "list [rules|sources|policies]",
	Short: "List configured rules, sources and policies",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		what := "all"
		if len(args) == 1 {
			what = args[0]
		}
		switch what {
		case "rules", "sources", "policies", "all":
		default:
			return fmt.Errorf("unknown list target %q (want rules, sources or policies)", what)
		}

		if flagJSON {
			switch what {
			case "rules":
				return printJSON(c.Rules)
			case "sources":
				return printJSON(c.Sources)
			case "policies":
				return printJSON(c.Policies)
			default:
				return printJSON(map[string]any{
					"rules":    c.Rules,
					"sources":  c.Sources,
					"policies": c.Policies,
				})
			}
		}

		if what == "rules" || what == "all" {
			fmt.Println("RULES")
			if len(c.Rules) == 0 {
				fmt.Println("  (none)")
			}
			for _, r := range c.Rules {
				status := "on"
				if !r.IsEnabled() {
					status = "off"
				}
				fmt.Printf("  %-22s %-9s %-4s %s\n", r.Name, r.Action, status, ruleScope(r))
				if r.Description != "" {
					fmt.Printf("      %s\n", r.Description)
				}
			}
			fmt.Println()
		}
		if what == "sources" || what == "all" {
			fmt.Println("SOURCES")
			if len(c.Sources) == 0 {
				fmt.Println("  (none)")
			}
			for _, n := range c.SourceNames() {
				s := c.Sources[n]
				fmt.Printf("  %-22s %-10s %s\n", n, s.Type, sourceSummary(s))
			}
			fmt.Println()
		}
		if what == "policies" || what == "all" {
			fmt.Println("POLICIES")
			if len(c.Policies) == 0 {
				fmt.Println("  (none)")
			}
			for _, n := range c.PolicyNames() {
				p := c.Policies[n]
				fmt.Printf("  %-22s sources=%s  rules=%s\n", n, joinOrNone(p.Sources), joinOrNone(p.Rules))
				if p.Description != "" {
					fmt.Printf("      %s\n", p.Description)
				}
			}
		}
		return nil
	},
}

func ruleScope(r config.Rule) string {
	var parts []string
	if r.Field != "" {
		parts = append(parts, "field="+r.Field)
	}
	if r.Path != "" {
		parts = append(parts, "path="+r.Path)
	}
	if r.Pattern != "" {
		parts = append(parts, "pattern="+truncateMiddle(r.Pattern, 48))
	}
	return strings.Join(parts, " ")
}

func sourceSummary(s config.Source) string {
	switch s.Type {
	case "file":
		return s.Path
	case "http":
		return s.Method + " " + s.URL
	case "postgres", "mysql":
		return truncateMiddle(s.Query, 60)
	case "env":
		return strings.Join(s.Keys, ", ")
	default:
		return ""
	}
}

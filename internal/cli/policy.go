package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/config"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Inspect and manage policies (source + rule bundles)",
}

var (
	polSources []string
	polRules   []string
	polDesc    string
	polOnError string
	polFormat  string
)

func init() {
	policyCmd.PersistentFlags().StringSliceVar(&polSources, "sources", nil, "source names the policy exposes")
	policyCmd.PersistentFlags().StringSliceVar(&polRules, "rules", nil, "rule names to apply (default: all)")
	policyCmd.PersistentFlags().StringVarP(&polDesc, "description", "d", "", "policy description")
	policyCmd.PersistentFlags().StringVar(&polOnError, "on-error", "deny", "allow (fail-open) or deny (fail-closed)")
	policyCmd.PersistentFlags().StringVar(&polFormat, "format", "json", "output format: json or text")

	policyCmd.AddCommand(policyAddCmd, policyRemoveCmd, policyShowCmd)
}

var policyAddCmd = &cobra.Command{
	Use:     "add",
	Short:   "Create a policy that binds sources to censor rules",
	Example: `  saringan policy add --name agent-safe --sources file:customers,api:orders --rules hide-ssn,mask-card`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}
		if polName == "" {
			return fmt.Errorf("--name is required")
		}
		if _, ok := c.Policies[polName]; ok {
			return fmt.Errorf("policy %q already exists", polName)
		}
		p := config.Policy{
			Description: polDesc,
			Sources:     polSources,
			Rules:       polRules,
			OnError:     polOnError,
			Format:      polFormat,
		}
		trial := *c
		if trial.Policies == nil {
			trial.Policies = map[string]config.Policy{}
		}
		trial.Policies[polName] = p
		if err := trial.Validate(); err != nil {
			return err
		}
		if c.Policies == nil {
			c.Policies = map[string]config.Policy{}
		}
		c.Policies[polName] = p
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("✔ added policy %q to %s\n", polName, path)
		return nil
	},
}

var polName string

func init() {
	policyCmd.PersistentFlags().StringVar(&polName, "name", "", "unique policy name")
}

var policyRemoveCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "delete"},
	Short:   "Delete a policy",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}
		if _, ok := c.Policies[args[0]]; !ok {
			return fmt.Errorf("unknown policy %q", args[0])
		}
		delete(c.Policies, args[0])
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("✔ removed policy %q from %s\n", args[0], path)
		return nil
	},
}

var policyShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show one policy in detail",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		p, ok := c.GetPolicy(args[0])
		if !ok {
			return fmt.Errorf("unknown policy %q", args[0])
		}
		if flagJSON {
			return printJSON(map[string]any{"name": args[0], "policy": p})
		}
		fmt.Printf("name:        %s\n", args[0])
		if p.Description != "" {
			fmt.Printf("description: %s\n", p.Description)
		}
		fmt.Printf("sources:     %s\n", joinOrNone(p.Sources))
		fmt.Printf("rules:       %s\n", joinOrNone(p.Rules))
		fmt.Printf("on_error:    %s\n", orDefault(p.OnError, "allow"))
		fmt.Printf("format:      %s\n", orDefault(p.Format, "json"))
		return nil
	},
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

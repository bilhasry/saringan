// Package cli implements the saringan command line interface, designed to be
// used both by humans and by AI agents (stable flags, machine-readable output).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/config"
)

var (
	flagConfig string
	flagJSON   bool
)

// rootCmd is the saringan entry point.
var rootCmd = &cobra.Command{
	Use:   "saringan",
	Short: "Config-driven data censoring proxy for AI agents",
	Long: `saringan sits between an AI agent (or developer tooling) and your data
sources. It fetches data from integrations (files, HTTP APIs, SQL databases,
environment variables, stdin) and censors it according to declarative rules
before it ever reaches the agent.

Get started:
  saringan init                          create a starter saringan.yaml
  saringan check                         validate the configuration
  saringan rule add --name hide-ssn --field "*ssn*" --action replace --with "*hidden*"
  saringan run --policy agent-safe       fetch + censor everything for a policy
  cat data.json | saringan filter --policy agent-safe`,
	Version:       "0.1.0",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "path to saringan config (default: ./saringan.yaml)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "machine-readable JSON output where supported")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(ruleCmd)
	rootCmd.AddCommand(sourceCmd)
	rootCmd.AddCommand(policyCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(filterCmd)
	rootCmd.AddCommand(scanCmd)
}

// resolveConfigPath finds the config file to use.
func resolveConfigPath() (string, error) {
	if flagConfig != "" {
		if _, err := os.Stat(flagConfig); err != nil {
			return "", fmt.Errorf("config file not found: %s", flagConfig)
		}
		return flagConfig, nil
	}
	found := config.FindConfig(".")
	if found == "" {
		return "", fmt.Errorf("no config file found (looked for %v in the current directory); run `saringan init` or pass --config", config.DefaultFileNames)
	}
	return found, nil
}

// loadConfig resolves and loads the active configuration.
func loadConfig() (*config.Config, string, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, "", err
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, path, fmt.Errorf("%s: %w", path, err)
	}
	return c, path, nil
}

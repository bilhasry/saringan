package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/config"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter saringan.yaml configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := flagConfig
		if path == "" {
			path = "saringan.yaml"
		}
		if _, err := os.Stat(path); err == nil && !flagForce {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
		c := config.Default()
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", path)
		fmt.Println("\nNext steps:")
		fmt.Println("  1. edit sources/  so they point at your data")
		fmt.Println("  2. tweak rules/   to match your sensitive fields")
		fmt.Println("  3. saringan check to validate everything")
		return nil
	},
}

var flagForce bool

func init() {
	initCmd.Flags().BoolVarP(&flagForce, "force", "f", false, "overwrite an existing config file")
}

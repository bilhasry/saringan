package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bilhasry/saringan/internal/config"
	"github.com/bilhasry/saringan/internal/source"
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Add, remove and test data integrations",
}

var (
	srcType    string
	srcPath    string
	srcFormat  string
	srcURL     string
	srcMethod  string
	srcHeaders []string
	srcBody    string
	srcDSN     string
	srcQuery   string
	srcKeys    []string
	srcTimeout string
	srcBearer  string
)

func init() {
	sourceCmd.PersistentFlags().StringVarP(&srcType, "type", "t", "", "file|http|postgres|mysql|env|stdin")
	sourceCmd.PersistentFlags().StringVar(&srcPath, "path", "", "file: path to the file")
	sourceCmd.PersistentFlags().StringVar(&srcFormat, "format", "", "file: json|yaml|csv|text (default: by extension)")
	sourceCmd.PersistentFlags().StringVar(&srcURL, "url", "", "http: endpoint URL")
	sourceCmd.PersistentFlags().StringVar(&srcMethod, "method", "", "http: HTTP method (default GET)")
	sourceCmd.PersistentFlags().StringArrayVarP(&srcHeaders, "header", "H", nil, "http: header (key:value), repeatable")
	sourceCmd.PersistentFlags().StringVar(&srcBody, "body", "", "http: request body")
	sourceCmd.PersistentFlags().StringVar(&srcDSN, "dsn", "", "database: connection string (or ${DB_DSN})")
	sourceCmd.PersistentFlags().StringVar(&srcQuery, "query", "", "database: SQL SELECT statement")
	sourceCmd.PersistentFlags().StringArrayVar(&srcKeys, "key", nil, "env: variable name, repeatable (\"*\" for all)")
	sourceCmd.PersistentFlags().StringVar(&srcTimeout, "timeout", "", "fetch timeout, e.g. 10s")
	sourceCmd.PersistentFlags().StringVar(&srcBearer, "bearer", "", "http: bearer token value")

	sourceCmd.AddCommand(sourceAddCmd, sourceRemoveCmd, sourceShowCmd, sourceTestCmd)
}

var sourceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Register a data integration in the config",
	Example: `  saringan source add --name customers --type file --path ./data/customers.json
  saringan source add --name orders --type http --url https://api.example.com/orders --bearer ${API_TOKEN}
  saringan source add --name crm --type postgres --dsn ${DB_DSN} --query "select * from customers"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}
		if ruleName == "" && srcName == "" {
			return fmt.Errorf("--name is required")
		}
		name := srcName
		if _, ok := c.Sources[name]; ok {
			return fmt.Errorf("source %q already exists", name)
		}
		if srcType == "" {
			return fmt.Errorf("--type is required (file|http|postgres|mysql|env|stdin)")
		}

		s := config.Source{
			Type:    srcType,
			Path:    srcPath,
			Format:  srcFormat,
			URL:     srcURL,
			Method:  srcMethod,
			Body:    srcBody,
			DSN:     srcDSN,
			Query:   srcQuery,
			Keys:    srcKeys,
			Timeout: srcTimeout,
		}
		if len(srcHeaders) > 0 {
			s.Headers = map[string]string{}
			for _, h := range srcHeaders {
				k, v, ok := splitKV(h)
				if !ok {
					return fmt.Errorf("--header must be key:value, got %q", h)
				}
				s.Headers[k] = v
			}
		}
		if srcBearer != "" {
			s.Auth = &config.Auth{Type: "bearer", Token: srcBearer}
		}

		trial := *c
		trial.Sources = map[string]config.Source{}
		for k, v := range c.Sources {
			trial.Sources[k] = v
		}
		trial.Sources[name] = s
		if err := trial.Validate(); err != nil {
			return err
		}

		if c.Sources == nil {
			c.Sources = map[string]config.Source{}
		}
		c.Sources[name] = s
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("✔ added source %q (%s) to %s\n", name, s.Type, path)
		return nil
	},
}

var srcName string

func init() {
	sourceCmd.PersistentFlags().StringVar(&srcName, "name", "", "unique source name")
}

var sourceRemoveCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "delete"},
	Short:   "Delete a source from the config",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := loadConfig()
		if err != nil {
			return err
		}
		if _, ok := c.Sources[args[0]]; !ok {
			return fmt.Errorf("unknown source %q", args[0])
		}
		delete(c.Sources, args[0])
		for pname, p := range c.Policies {
			var kept []string
			for _, sn := range p.Sources {
				if sn != args[0] {
					kept = append(kept, sn)
				}
			}
			p.Sources = kept
			c.Policies[pname] = p
		}
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Printf("✔ removed source %q from %s\n", args[0], path)
		return nil
	},
}

var sourceShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show one source in detail",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		s, ok := c.Sources[args[0]]
		if !ok {
			return fmt.Errorf("unknown source %q", args[0])
		}
		if flagJSON {
			return printJSON(map[string]any{"name": args[0], "source": s})
		}
		fmt.Printf("name:    %s\n", args[0])
		fmt.Printf("type:    %s\n", s.Type)
		fmt.Printf("summary: %s\n", sourceSummary(s))
		if len(s.Headers) > 0 {
			fmt.Printf("headers: %v\n", s.Headers)
		}
		if s.Auth != nil {
			fmt.Printf("auth:    %s\n", s.Auth.Type)
		}
		if s.Timeout != "" {
			fmt.Printf("timeout: %s\n", s.Timeout)
		}
		return nil
	},
}

var sourceTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Fetch from a source and preview the result (uncensored)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := loadConfig()
		if err != nil {
			return err
		}
		s, ok := c.Sources[args[0]]
		if !ok {
			return fmt.Errorf("unknown source %q", args[0])
		}
		src, err := source.Build(args[0], s)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		start := time.Now()
		doc, err := src.Fetch(ctx)
		if err != nil {
			return err
		}
		elapsed := time.Since(start).Round(time.Millisecond)
		if flagJSON {
			return printJSON(map[string]any{
				"source":       args[0],
				"type":         src.Type(),
				"elapsed_ms":   elapsed.Milliseconds(),
				"structured":   doc.IsStructured(),
				"content_type": doc.ContentType,
				"data":         firstOrData(doc),
			})
		}
		fmt.Printf("✔ %q (%s) fetched in %s\n", args[0], src.Type(), elapsed)
		fmt.Printf("  content-type: %s\n", doc.ContentType)
		if doc.IsStructured() {
			fmt.Printf("  shape:        %s\n", shapeOf(doc.Data))
		} else {
			fmt.Printf("  shape:        text (%d bytes)\n", len(doc.Raw))
		}
		fmt.Printf("  preview:      %s\n", truncateMiddle(previewOf(doc), 160))
		return nil
	},
}

func firstOrData(doc *source.Document) any {
	if doc.IsStructured() {
		return doc.Data
	}
	return truncateMiddle(doc.Raw, 400)
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

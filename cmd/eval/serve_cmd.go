package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

var servePort string

// serveCmd runs the evaluation HTTP server.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the eval HTTP server",
	Long:  "Start an HTTP server for triggering model evaluations and retrieving results.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig(configFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		server, err := NewEvalServer(cfg)
		if err != nil {
			return fmt.Errorf("failed to create eval server: %w", err)
		}

		log.Printf("Eval server configured with %d enabled models and %d test cases", len(cfg.GetEnabledModels()), len(cfg.TestCases))
		return server.Start(servePort)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&servePort, "port", "8080", "Port for HTTP server")
}

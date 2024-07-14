// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"fmt"

	"github.com/spf13/cobra"
)

func MakeCli() {
	app, api := SetupProducer()

	rootCmd := &cobra.Command{
		Use:   "karaberus",
		Short: "Start the karaberus server",
		Run: func(cmd *cobra.Command, args []string) {
			RunProducer(app, api)
		},
	}

	// Add a command to print the OpenAPI spec.
	rootCmd.AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec",
		Run: func(cmd *cobra.Command, args []string) {
			// Use downgrade to return OpenAPI 3.0.3 YAML since oapi-codegen doesn't
			// support OpenAPI 3.1 fully yet. Use `.YAML()` instead for 3.1.
			b, _ := api.OpenAPI().DowngradeYAML()
			fmt.Println(string(b))
		},
	})

	rootCmd.PersistentFlags().IntVarP(
		&CONFIG.Listen.Port,
		"port", "p",
		CONFIG.Listen.Port,
		"Port to listen on",
	)

	// Run the CLI. When passed no commands, it starts the server.
	rootCmd.Execute()
}

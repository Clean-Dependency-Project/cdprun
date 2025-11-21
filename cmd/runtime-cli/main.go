// Package main provides the unified runtime CLI application.
// It integrates with YAML configuration and supports all runtime providers.
package main

import (
	"log"
	"os"

	"github.com/clean-dependency-project/cdprun/internal/cli"
)

func main() {
	app := cli.NewApp()

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

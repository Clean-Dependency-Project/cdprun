// Package main is the lineaje-sbom CLI bootstrap.
package main

import (
	"log"
	"os"

	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/cli"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/workflow"
)

func main() {
	runner := workflow.NewRunner(os.Stdout, os.Stderr)
	app := cli.NewApp(runner.Run)
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

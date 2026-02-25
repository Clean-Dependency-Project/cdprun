// Package main provides the lineaje-sbom CLI for sending SBOM/pom components to the Lineaje explain API.
package main

import (
	"log"
	"os"
	"time"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:     "lineaje-sbom",
		Usage:    "Parse SBOM or pom.xml, extract PURLs, and send to Lineaje explain API",
		Version:  "0.1.0",
		Compiled: time.Now(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "sbom",
				Aliases: []string{"s"},
				Usage:   "path to CycloneDX SBOM file (JSON)",
			},
			&cli.StringFlag{
				Name:    "pom",
				Aliases: []string{"p"},
				Usage:   "path to Maven pom.xml file",
			},
			&cli.StringFlag{
				Name:    "query",
				Aliases: []string{"q"},
				Value:   "recommend gos plan",
				Usage:   "query string for the explain API",
			},
			&cli.IntFlag{
				Name:  "poll-delay",
				Value: 5,
				Usage: "seconds between poll attempts",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "table",
				Usage:   "output format: table or json",
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(c *cli.Context) error {
	sbomPath := c.String("sbom")
	pomPath := c.String("pom")
	if sbomPath == "" && pomPath == "" {
		return cli.Exit("exactly one of --sbom or --pom is required", 1)
	}
	if sbomPath != "" && pomPath != "" {
		return cli.Exit("cannot use both --sbom and --pom", 1)
	}
	// Phase 1: no-op after flag validation; Phase 2+ will parse and call API
	_ = c.String("query")
	_ = c.Int("poll-delay")
	_ = c.String("output")
	return nil
}

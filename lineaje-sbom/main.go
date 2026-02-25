// Package main provides the lineaje-sbom CLI for sending SBOM/pom components to the Lineaje explain API.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/clean-dependency-project/lineaje-sbom/internal/sbom"
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

	var purls []string
	var err error
	if sbomPath != "" {
		purls, err = sbom.PURLsFromCycloneDX(sbomPath)
	} else {
		purls, err = sbom.PURLsFromPOM(pomPath)
	}
	if err != nil {
		return fmt.Errorf("parse input: %w", err)
	}

	outputFormat := c.String("output")
	if outputFormat == "json" {
		out := struct {
			PURLCount int      `json:"purl_count"`
			PURLs     []string `json:"purls"`
		}{len(purls), purls}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(out); encErr != nil {
			return fmt.Errorf("encode json: %w", encErr)
		}
		return nil
	}
	fmt.Printf("Found %d PURL(s).\n", len(purls))
	return nil
}

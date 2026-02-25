// Package main provides the lineaje-sbom CLI for sending SBOM/pom components to the Lineaje explain API.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/clean-dependency-project/lineaje-sbom/internal/auth"
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
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "enable debug logging (HTTP requests and responses) to stderr",
			},
			&cli.BoolFlag{
				Name:  "login-only",
				Usage: "only test login (uses LINEAJE_USERNAME and LINEAJE_PASSWORD); exit after success or failure",
			},
			&cli.BoolFlag{
				Name:  "print-token",
				Usage: "with --login-only: print the access token to stdout (e.g. for use with curl: TOKEN=$(lineaje-sbom --login-only --print-token))",
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(c *cli.Context) error {
	level := slog.LevelInfo
	if c.Bool("debug") {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if c.Bool("login-only") {
		cfg := auth.DefaultConfig()
		cfg.Logger = logger
		token, err := auth.GetToken(cfg)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		if c.Bool("print-token") {
			fmt.Fprintln(os.Stdout, token)
			return nil
		}
		if c.String("output") == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok", "token_preview": token[:min(20, len(token))] + "..."})
		} else {
			fmt.Fprintln(os.Stdout, "Login OK")
		}
		return nil
	}

	sbomPath := c.String("sbom")
	pomPath := c.String("pom")
	if sbomPath == "" && pomPath == "" {
		return cli.Exit("exactly one of --sbom or --pom is required", 1)
	}
	if sbomPath != "" && pomPath != "" {
		return cli.Exit("cannot use both --sbom and --pom", 1)
	}
	_ = logger

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

// Package main provides the sitegen CLI command for generating static HTML sites from release database.
package main

import (
	"context"
	"log"
	"os"

	"github.com/urfave/cli/v2"

	appcli "github.com/clean-dependency-project/cdprun/internal/cli"
	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/sitegen"
	"github.com/clean-dependency-project/cdprun/internal/storage"
)

func main() {
	app := &cli.App{
		Name:  "sitegen",
		Usage: "Generate static HTML site from release database",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "db",
				Usage:    "path to SQLite database file",
				Required: true,
				EnvVars:  []string{"SITEGEN_DB"},
			},
			&cli.StringFlag{
				Name:     "out",
				Usage:    "output directory for generated HTML files",
				Required: true,
				EnvVars:  []string{"SITEGEN_OUT"},
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "validate without writing files",
			},
			&cli.StringFlag{
				Name:    "log-level",
				Value:   "info",
				Usage:   "log level (debug, info, warn, error)",
				EnvVars: []string{"SITEGEN_LOG_LEVEL"},
			},
			&cli.StringFlag{
				Name:    "unsupported-file",
				Usage:   "path to YAML/JSON file listing unsupported runtime version prefixes (overrides registry config)",
				EnvVars: []string{"SITEGEN_UNSUPPORTED_FILE"},
			},
		},
		Action: runSitegen,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

// runSitegen executes the site generation process.
func runSitegen(c *cli.Context) error {
	// Setup logging (JSON format, stderr only)
	logLevel := appcli.ParseLogLevelOrDefault(c.String("log-level"))
	stdout, _ := appcli.NewLoggers(logLevel)

	// Open database
	dbPath := c.String("db")
	db, err := storage.InitDB(storage.Config{
		DatabasePath: dbPath,
		LogLevel:     "silent", // Database logs are verbose, suppress them
	})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			stdout.Error("failed to close database", "error", closeErr)
		}
	}()

	// Resolve unsupported-versions file: flag overrides, else empty (no filtering).
	unsupportedFile := c.String("unsupported-file")
	unsupportedCfg, err := config.LoadUnsupportedConfig(unsupportedFile)
	if err != nil {
		return err
	}

	// Create generator
	generator := sitegen.NewGenerator(db, stdout)

	// Generate site
	opts := sitegen.GenerateOptions{
		OutputDir:           c.String("out"),
		DryRun:              c.Bool("dry-run"),
		UnsupportedVersions: unsupportedCfg,
	}

	if err := generator.Generate(context.Background(), opts); err != nil {
		return err
	}

	stdout.Info("site generation completed successfully")
	return nil
}

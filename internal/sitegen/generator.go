package sitegen

import (
	"context"
	"fmt"
	"os"

	"log/slog"
)

// Generator orchestrates the HTML site generation process.
// Following Dave Cheney's principle: "Accept interfaces, return structs"
type Generator struct {
	reader ReleaseReader
	logger *slog.Logger
}

// NewGenerator creates a new Generator with the provided ReleaseReader.
func NewGenerator(reader ReleaseReader, logger *slog.Logger) *Generator {
	return &Generator{
		reader: reader,
		logger: logger,
	}
}

// GenerateOptions contains options for site generation.
type GenerateOptions struct {
	OutputDir string
	DryRun    bool
}

// Generate generates the complete static site from the database.
// This is the main entry point that orchestrates loading, building, and rendering.
func (g *Generator) Generate(ctx context.Context, opts GenerateOptions) error {
	if opts.OutputDir == "" {
		return fmt.Errorf("output directory is required")
	}

	g.logger.Info("starting site generation", "output_dir", opts.OutputDir, "dry_run", opts.DryRun)

	// Load releases from database
	releases, err := LoadReleases(g.reader)
	if err != nil {
		return fmt.Errorf("failed to load releases: %w", err)
	}

	g.logger.Info("loaded releases", "count", len(releases))

	if len(releases) == 0 {
		g.logger.Warn("no releases found in database")
		return nil
	}

	// Build site model
	model := BuildModel(releases)
	g.logger.Info("built site model",
		"runtimes", len(model.Runtimes),
	)

	// Create output directory if not in dry-run mode
	if !opts.DryRun {
		if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	} else {
		g.logger.Info("dry-run mode: skipping file writes")
	}

	// Render human-readable pages
	if !opts.DryRun {
		if err := RenderHumanPages(model, opts.OutputDir, g.logger); err != nil {
			return fmt.Errorf("failed to render human pages: %w", err)
		}
		g.logger.Info("rendered human-readable pages")
	}

	// Render PEP 503 Simple index
	if !opts.DryRun {
		if err := RenderSimpleIndex(model, opts.OutputDir, g.logger); err != nil {
			return fmt.Errorf("failed to render simple index: %w", err)
		}
		g.logger.Info("rendered PEP 503 Simple index")
	}

	g.logger.Info("site generation completed successfully")
	return nil
}


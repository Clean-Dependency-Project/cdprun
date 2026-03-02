package cli

import (
	"time"

	urcli "github.com/urfave/cli/v2"

	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/client"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/workflow"
)

// NewApp returns the CLI app wired to the provided runner.
func NewApp(run func(cfg workflow.Config) error) *urcli.App {
	return &urcli.App{
		Name:     "lineaje-sbom",
		Usage:    "Upload CycloneDX SBOMs and optionally run explain workflow",
		Version:  "0.1.0",
		Compiled: time.Now(),
		Flags: []urcli.Flag{
			&urcli.StringFlag{
				Name:    "sbom",
				Aliases: []string{"s"},
				Usage:   "path to CycloneDX SBOM file (JSON)",
			},
			&urcli.StringFlag{
				Name:    "pom",
				Aliases: []string{"p"},
				Usage:   "path to Maven pom.xml file",
			},
			&urcli.IntFlag{
				Name:  "poll-delay",
				Value: 5,
				Usage: "seconds between poll attempts",
			},
			&urcli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "json",
				Usage:   "output format: table or json",
			},
			&urcli.StringFlag{
				Name:  "sbom-upload-url",
				Value: client.DefaultUploadConfig().BaseURL,
				Usage: "SBOM upload API URL (default uses script-proven endpoint)",
			},
			&urcli.StringFlag{
				Name:  "sbom-format",
				Value: "CycloneDX",
				Usage: "SBOM format sent as sbom_format query parameter",
			},
			&urcli.StringFlag{
				Name:  "project-name",
				Value: "lineaje-sbom",
				Usage: "project name sent in sbomJob payload",
			},
			&urcli.StringFlag{
				Name:  "project-version",
				Value: "unknown",
				Usage: "project version sent in sbomJob payload",
			},
			&urcli.BoolFlag{
				Name:  "debug",
				Usage: "log HTTP request and response (method, URL, body) to stderr as JSON for auth and explain API",
			},
			&urcli.StringFlag{
				Name:  "session",
				Usage: "resume: path to a saved session file (e.g. ./sessions/<guid>.json); do not use with --sbom or --pom",
			},
			&urcli.BoolFlag{
				Name:  "login-only",
				Usage: "only test login (uses LINEAJE_USERNAME and LINEAJE_PASSWORD); exit after success or failure",
			},
			&urcli.BoolFlag{
				Name:  "print-token",
				Usage: "with --login-only: print the access token to stdout (e.g. for use with curl: TOKEN=$(lineaje-sbom --login-only --print-token))",
			},
		},
		Action: func(c *urcli.Context) error {
			return run(workflow.Config{
				SBOMPath:       c.String("sbom"),
				POMPath:        c.String("pom"),
				PollDelay:      c.Int("poll-delay"),
				Output:         c.String("output"),
				SBOMUploadURL:  c.String("sbom-upload-url"),
				SBOMFormat:     c.String("sbom-format"),
				ProjectName:    c.String("project-name"),
				ProjectVersion: c.String("project-version"),
				Debug:          c.Bool("debug"),
				SessionPath:    c.String("session"),
				LoginOnly:      c.Bool("login-only"),
				PrintToken:     c.Bool("print-token"),
			})
		},
	}
}

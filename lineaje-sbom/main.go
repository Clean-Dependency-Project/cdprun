// Package main provides the lineaje-sbom CLI to fix vulnerable dependencies using Lineaje recommendations (SBOM or pom.xml).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/auth"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/client"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/output"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/sbom"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/session"
)

const (
	defaultQuery      = "recommend gos plan"
	defaultPollDelay  = 5
	defaultSessionDir = "./sessions"
)

func main() {
	app := &cli.App{
		Name:     "lineaje-sbom",
		Usage:    "Fix vulnerable dependencies using Lineaje recommendations.",
		Version:  "0.1.0",
		Compiled: time.Now(),
		Description: `SBOM or pom.xml. Sessions auto-saved when Lineaje asks to wait. Use login and resume.`,
		UsageText: `lineaje-sbom [options]
   lineaje-sbom login [options]
   lineaje-sbom resume --session <file> [options]`,
		Flags: appFlags(),
		Commands: []*cli.Command{
			loginCommand(),
			resumeCommand(),
		},
		Action: runDefault,
	}

	// Apple-style help with EXAMPLES
	cli.AppHelpTemplate = `NAME:
   {{.Name}} - {{.Usage}}

DESCRIPTION:
   {{.Description}}

USAGE:
   {{.UsageText}}

COMMANDS:
{{range .Commands}}{{if not .HideHelp}}   {{.Name}}{{ "\t"}}{{.Usage}}{{ "\n" }}{{end}}{{end}}

OPTIONS:
{{range .VisibleFlags}}   {{.}}
{{end}}

EXAMPLES:
   lineaje-sbom --sbom sbom.json
      Get a fix plan for vulnerable dependencies from your CycloneDX SBOM.

   lineaje-sbom --pom pom.xml -o table
      Get Lineaje recommendations for your Maven dependencies; show as a table.

   lineaje-sbom login
      Sign in using LINEAJE_USERNAME and LINEAJE_PASSWORD.

   lineaje-sbom resume --session ./sessions/abc123.json
      Continue a run (session was saved automatically when Lineaje asked to wait).

{{if .Version}}
VERSION:
   {{.Version}}
{{end}}
`

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

// appFlags returns global flags for the default (explain) flow and backward compatibility.
func appFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "sbom",
			Aliases: []string{"s"},
			Usage:   "Path to a CycloneDX SBOM (JSON).",
		},
		&cli.StringFlag{
			Name:    "pom",
			Aliases: []string{"p"},
			Usage:   "Path to a Maven pom.xml.",
		},
		&cli.StringFlag{
			Name:    "query",
			Aliases: []string{"q"},
			Value:   defaultQuery,
			Usage:   "What to ask Lineaje (e.g. recommend a fix plan for your dependencies).",
		},
		&cli.IntFlag{
			Name:  "poll-delay",
			Value: defaultPollDelay,
			Usage: "Seconds between poll attempts while waiting for results.",
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Value:   "json",
			Usage:   "How to print results: table or json.",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Log HTTP requests and responses to stderr (JSON).",
		},
		&cli.StringFlag{
			Name:  "save-session",
			Value: defaultSessionDir,
			Usage: "Directory where we automatically save a session when Lineaje asks us to wait.",
		},
		&cli.BoolFlag{
			Name:  "no-save-session",
			Usage: "Do not automatically save sessions when Lineaje asks to wait.",
		},
		// Backward compatibility: same behavior as 'resume' command
		&cli.StringFlag{
			Name:   "session",
			Usage:  "Path to a saved session file to resume (same as 'resume --session').",
			Hidden: true,
		},
		// Backward compatibility: same behavior as 'login' command
		&cli.BoolFlag{
			Name:   "login-only",
			Usage:  "Sign in and exit (same as 'login' command).",
			Hidden: true,
		},
		&cli.BoolFlag{
			Name:   "print-token",
			Usage:  "With --login-only: print the access token to stdout.",
			Hidden: true,
		},
	}
}

func loginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Sign in or get an access token.",
		Description: `Uses LINEAJE_USERNAME and LINEAJE_PASSWORD. Use --print-token for the token.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "print-token",
				Usage: "Print the access token to stdout.",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "json",
				Usage:   "How to print the result: table or json.",
			},
		},
		Action: runLogin,
	}
}

func resumeCommand() *cli.Command {
	return &cli.Command{
		Name:    "resume",
		Aliases: []string{"r"},
		Usage:   "Continue a previous run from a saved session.",
		Description: `Sessions auto-saved when Lineaje asks to wait. Stored under --save-session (default: ./sessions).`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "session",
				Aliases:  []string{"s"},
				Usage:    "Path to the saved session file (e.g. ./sessions/<guid>.json).",
				Required: true,
			},
			&cli.IntFlag{
				Name:  "poll-delay",
				Value: defaultPollDelay,
				Usage: "Seconds between poll attempts.",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "json",
				Usage:   "How to print results: table or json.",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "Log HTTP requests and responses to stderr (JSON).",
			},
		},
		Action: runResume,
	}
}

func runDefault(c *cli.Context) error {
	// Backward compatibility: --login-only runs login flow
	if c.Bool("login-only") {
		return runLogin(c)
	}
	// Backward compatibility: --session runs resume flow
	if c.String("session") != "" {
		return runResume(c)
	}
	return runExplain(c)
}

func runLogin(c *cli.Context) error {
	level := slog.LevelInfo
	if c.Bool("debug") {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

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
	outputFormat := c.String("output")
	if outputFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok", "token_preview": token[:min(20, len(token))] + "..."})
	} else {
		fmt.Fprintln(os.Stdout, "Login OK")
	}
	return nil
}

func runResume(c *cli.Context) error {
	level := slog.LevelInfo
	if c.Bool("debug") {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	sessionPath := c.String("session")
	if sessionPath == "" {
		return cli.Exit("--session is required", 1)
	}

	sess, err := session.Load(sessionPath)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	authCfg := auth.DefaultConfig()
	authCfg.Logger = logger
	token, err := auth.GetToken(authCfg)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	pollSec := c.Int("poll-delay")
	if pollSec <= 0 {
		pollSec = defaultPollDelay
	}
	clientCfg := client.DefaultConfig()
	clientCfg.Token = token
	clientCfg.Query = sess.Query
	clientCfg.Components = sess.Components
	clientCfg.PollDelay = time.Duration(pollSec) * time.Second
	clientCfg.Logger = logger
	clientCfg.InitialGUID = sess.GUID
	responseBytes, err := client.Call(clientCfg)
	if err != nil {
		return fmt.Errorf("explain API: %w", err)
	}
	outputFormat := c.String("output")
	if err := output.Write(os.Stdout, responseBytes, outputFormat); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

func runExplain(c *cli.Context) error {
	level := slog.LevelInfo
	if c.Bool("debug") {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	sbomPath := c.String("sbom")
	pomPath := c.String("pom")

	if sbomPath == "" && pomPath == "" {
		return cli.Exit("provide one of --sbom or --pom to get fix recommendations for your dependencies", 1)
	}
	if sbomPath != "" && pomPath != "" {
		return cli.Exit("use either --sbom or --pom, not both", 1)
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

	authCfg := auth.DefaultConfig()
	authCfg.Logger = logger
	token, err := auth.GetToken(authCfg)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	pollSec := c.Int("poll-delay")
	if pollSec <= 0 {
		pollSec = defaultPollDelay
	}
	clientCfg := client.DefaultConfig()
	clientCfg.Token = token
	clientCfg.Query = c.String("query")
	clientCfg.Components = purls
	clientCfg.PollDelay = time.Duration(pollSec) * time.Second
	clientCfg.Logger = logger

	saveSessionDir := c.String("save-session")
	saveSessions := !c.Bool("no-save-session")
	var savedGUID string
	if saveSessions {
		clientCfg.OnStillProcessing = func(guid, query string, components []string, _ string) {
			s := session.Session{
				GUID:       guid,
				Query:      query,
				Components: components,
				CreatedAt:  time.Now().UTC().Format(time.RFC3339),
			}
			if err := session.Save(saveSessionDir, s); err != nil {
				logger.Warn("save session failed", "err", err, "guid", guid)
				return
			}
			savedGUID = guid
			logger.Info("session saved", "status", "session_saved", "guid", guid, "path", filepath.Join(saveSessionDir, guid+".json"))
		}
	}

	responseBytes, err := client.Call(clientCfg)
	if err != nil {
		return fmt.Errorf("explain API: %w", err)
	}

	if saveSessions && savedGUID != "" {
		var final struct {
			Answer interface{} `json:"answer"`
		}
		if json.Unmarshal(responseBytes, &final) == nil && final.Answer != nil {
			_ = session.RemoveFile(saveSessionDir, savedGUID)
		}
	}

	outputFormat := c.String("output")
	if err := output.Write(os.Stdout, responseBytes, outputFormat); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

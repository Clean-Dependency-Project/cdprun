// Package main provides the lineaje-sbom CLI for sending SBOM/pom components to the Lineaje explain API.
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
				Usage: "log HTTP request and response (method, URL, body) to stderr as JSON for auth and explain API",
			},
			&cli.StringFlag{
				Name:    "save-session",
				Usage:   "directory to save session files when API says still processing (default: ./sessions); one file per GUID",
				Value:   "./sessions",
			},
			&cli.BoolFlag{
				Name:  "no-save-session",
				Usage: "disable saving sessions (overrides default)",
			},
			&cli.StringFlag{
				Name:  "session",
				Usage: "resume: path to a saved session file (e.g. ./sessions/<guid>.json); do not use with --sbom or --pom",
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

	sessionPath := c.String("session")
	sbomPath := c.String("sbom")
	pomPath := c.String("pom")

	if sessionPath != "" {
		// Resume path: require no sbom/pom
		if sbomPath != "" || pomPath != "" {
			return cli.Exit("use either --sbom/--pom or --session, not both", 1)
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
			pollSec = 5
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

	// Normal path: exactly one of sbom or pom
	if sbomPath == "" && pomPath == "" {
		return cli.Exit("exactly one of --sbom, --pom or --session is required", 1)
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

	authCfg := auth.DefaultConfig()
	authCfg.Logger = logger
	token, err := auth.GetToken(authCfg)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	pollSec := c.Int("poll-delay")
	if pollSec <= 0 {
		pollSec = 5
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
				// Do not store API response content (e.g. message) in session
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

	// Only remove the session file when we got a final answer; if still "processing", keep it so user can resume
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

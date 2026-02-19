package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/hfpoc"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "hf-poc",
		Usage: "Curate an upstream HF model into an org repo (PoC)",
		Flags: []cli.Flag{
			// Optional override, but the intended usage is no flags:
			// go run ./cmd/hf-poc assess
			// go run ./cmd/hf-poc clone
			// go run ./cmd/hf-poc verify
			&cli.StringFlag{Name: "config", Usage: "path to hf_poc.yaml (optional)"},
		},
		Commands: []*cli.Command{
			cmdAssess(),
			cmdClone(),
			cmdVerify(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		// Keep stdout clean: only JSON results are written there by commands.
		// This fallback is for unexpected failures before a command can emit a result.
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func cmdAssess() *cli.Command {
	return &cli.Command{
		Name:  "assess",
		Usage: "Assess upstream model using YAML rules from hf_poc.yaml",
		Action: func(c *cli.Context) error {
			ctx := context.Background()
			cfgPath := hfpoc.ResolveConfigPath(c.String("config"))
			cfg, err := hfpoc.ReadAppConfig(cfgPath)
			if err != nil {
				_, stderrLog := newLoggers("info")
				stderrLog.Error("failed to load app config", "config", cfgPath, "error", err)
				return writeResult(hfpoc.Result{Status: "failed", Command: "assess", Message: "config_load_failed", Errors: []string{err.Error()}}, err)
			}

			stdoutLog, stderrLog := newLoggers(cfg.HF.LogLevel)

			client := hfpoc.NewClient(hfpoc.ClientOptions{
				BaseURL:   cfg.HF.BaseURL,
				Token:     tokenFromEnv(cfg.HF.TokenEnv),
				UserAgent: "cdprun-hf-poc/0.1",
				Timeout:   60 * time.Second,
			})

			rules, err := hfpoc.ReadAssessRules(cfg.Assess.RulesFile)
			if err != nil {
				stderrLog.Error("failed to load assess rules", "rules", cfg.Assess.RulesFile, "error", err)
				return writeResult(hfpoc.Result{
					Status:         "failed",
					Command:        "assess",
					Message:        "rules_load_failed",
					UpstreamRepoID: cfg.Assess.UpstreamRepoID,
					Errors:         []string{err.Error()},
				}, err)
			}

			out, err := hfpoc.Assess(ctx, client, hfpoc.AssessInput{
				RepoID:    cfg.Assess.UpstreamRepoID,
				Criteria:  rules.Criteria,
				Allowlist: rules.RequiredFiles,
			})
			if err != nil {
				stderrLog.Error("assess failed", "error", err)
				return writeResult(hfpoc.Result{
					Status:         "failed",
					Command:        "assess",
					Message:        "assess_failed",
					UpstreamRepoID: cfg.Assess.UpstreamRepoID,
					Errors:         []string{err.Error()},
				}, err)
			}

			res := hfpoc.Result{
				Status:         "ok",
				Command:        "assess",
				UpstreamRepoID: out.ModelInfo.ID,
				UpstreamSHA:    out.ModelInfo.SHA,
				Details: map[string]any{
					"passed":         out.Passed,
					"reasons":        out.Reasons,
					"license":        out.License,
					"license_source": out.LicenseSource,
					"downloads_30d":  out.ModelInfo.Downloads,
					"created_at":     out.ModelInfo.CreatedAt,
					"config_file":    cfgPath,
					"rules_file":     cfg.Assess.RulesFile,
					"criteria":       rules.Criteria,
				},
			}
			if !out.Passed {
				res.Status = "failed"
				res.Message = "intake_failed"
			} else {
				res.Message = "intake_passed"
			}
			_ = stdoutLog // keep consistent signature; logs always to stderr in our logger setup
			return writeResult(res, nil)
		},
	}
}

func cmdClone() *cli.Command {
	return &cli.Command{
		Name:  "clone",
		Usage: "Clone (curate) upstream model snapshot into org repo using hf_poc.yaml",
		Action: func(c *cli.Context) error {
			ctx := context.Background()
			cfgPath := hfpoc.ResolveConfigPath(c.String("config"))
			cfg, err := hfpoc.ReadAppConfig(cfgPath)
			if err != nil {
				_, stderrLog := newLoggers("info")
				stderrLog.Error("failed to load app config", "config", cfgPath, "error", err)
				return writeResult(hfpoc.Result{Status: "failed", Command: "clone", Message: "config_load_failed", Errors: []string{err.Error()}}, err)
			}

			_, stderrLog := newLoggers(cfg.HF.LogLevel)

			tokenEnv := cfg.HF.TokenEnv
			token := tokenFromEnv(tokenEnv)
			if token == "" {
				return writeResult(hfpoc.Result{
					Status:  "failed",
					Command: "clone",
					Message: "missing_token",
					Errors:  []string{fmt.Sprintf("env var %q is not set", tokenEnv)},
				}, errors.New("missing token"))
			}

			client := hfpoc.NewClient(hfpoc.ClientOptions{
				BaseURL:   cfg.HF.BaseURL,
				Token:     token,
				UserAgent: "cdprun-hf-poc/0.1",
				Timeout:   60 * time.Second,
			})

			upstreamRepo := cfg.Clone.UpstreamRepoID
			upstreamSHA := strings.TrimSpace(cfg.Clone.UpstreamSHA)
			if upstreamSHA == "" {
				mi, err := client.GetModelInfo(ctx, upstreamRepo)
				if err != nil {
					stderrLog.Error("failed to fetch upstream model info", "error", err)
					return writeResult(hfpoc.Result{
						Status:         "failed",
						Command:        "clone",
						Message:        "upstream_info_failed",
						UpstreamRepoID: upstreamRepo,
						Errors:         []string{err.Error()},
					}, err)
				}
				upstreamSHA = mi.SHA
			}

			orgRepoID := fmt.Sprintf("%s/%s", cfg.Clone.Org, cfg.Clone.OrgRepo)

			// Create target repo first (REST).
			_, createErr := client.CreateRepo(ctx, hfpoc.CreateRepoRequest{
				Name:         cfg.Clone.OrgRepo,
				Organization: cfg.Clone.Org,
				Private:      cfg.Clone.Private,
				Type:         "model",
			})
			if createErr != nil {
				// If repo already exists, HF typically returns 409; for PoC, treat it as non-fatal.
				stderrLog.Warn("create repo failed (might already exist)", "error", createErr, "repo", orgRepoID)
			}

			cloneOut, err := hfpoc.CloneToOrgViaGit(ctx, hfpoc.CloneGitInput{
				UpstreamRepoID: upstreamRepo,
				UpstreamSHA:    upstreamSHA,
				OrgRepoID:      orgRepoID,
				Private:        cfg.Clone.Private,
				AllowedFiles:   cfg.Clone.AllowedFiles,
				RequireGitLFS:  cfg.Clone.RequireGitLFS,
				TokenEnv:       tokenEnv,
				Logger:         stderrLog,
			})
			if err != nil {
				stderrLog.Error("clone failed", "error", err)
				return writeResult(hfpoc.Result{
					Status:         "failed",
					Command:        "clone",
					Message:        "clone_failed",
					UpstreamRepoID: upstreamRepo,
					UpstreamSHA:    upstreamSHA,
					OrgRepoID:      orgRepoID,
					Errors:         []string{err.Error()},
				}, err)
			}

			if strings.TrimSpace(cfg.State.File) != "" {
				if err := hfpoc.WriteState(cfg.State.File, hfpoc.State{
					OrgSHA:    cloneOut.OrgSHA,
					UpdatedAt: time.Now().UTC().Format(time.RFC3339),
				}); err != nil {
					stderrLog.Warn("failed to write state file", "state_file", cfg.State.File, "error", err)
				}
			}

			return writeResult(hfpoc.Result{
				Status:         "ok",
				Command:        "clone",
				Message:        "clone_ok",
				UpstreamRepoID: upstreamRepo,
				UpstreamSHA:    upstreamSHA,
				OrgRepoID:      orgRepoID,
				OrgSHA:         cloneOut.OrgSHA,
				SelectedFiles:  cfg.Clone.AllowedFiles,
				Details: map[string]any{
					"config_file": cfgPath,
					"state_file":  cfg.State.File,
				},
			}, nil)
		},
	}
}

func cmdVerify() *cli.Command {
	return &cli.Command{
		Name:  "verify",
		Usage: "Verify org repo model files using hf_poc.yaml (+ state file)",
		Action: func(c *cli.Context) error {
			ctx := context.Background()
			cfgPath := hfpoc.ResolveConfigPath(c.String("config"))
			cfg, err := hfpoc.ReadAppConfig(cfgPath)
			if err != nil {
				_, stderrLog := newLoggers("info")
				stderrLog.Error("failed to load app config", "config", cfgPath, "error", err)
				return writeResult(hfpoc.Result{Status: "failed", Command: "verify", Message: "config_load_failed", Errors: []string{err.Error()}}, err)
			}

			_, stderrLog := newLoggers(cfg.HF.LogLevel)

			client := hfpoc.NewClient(hfpoc.ClientOptions{
				BaseURL:   cfg.HF.BaseURL,
				Token:     tokenFromEnv(cfg.HF.TokenEnv), // for private repos
				UserAgent: "cdprun-hf-poc/0.1",
				Timeout:   120 * time.Second,
			})

			revision := strings.TrimSpace(cfg.Verify.Revision)
			if revision == "" && strings.TrimSpace(cfg.State.File) != "" {
				state, err := hfpoc.ReadState(cfg.State.File)
				if err == nil {
					revision = strings.TrimSpace(state.OrgSHA)
				}
			}
			if revision == "" {
				err := errors.New("verify: missing revision (set verify.revision in config or run clone to populate state file)")
				return writeResult(hfpoc.Result{Status: "failed", Command: "verify", Message: "missing_revision", Errors: []string{err.Error()}}, err)
			}

			res, err := hfpoc.Verify(ctx, client, hfpoc.VerifyInput{
				RepoID:       cfg.Verify.OrgRepoID,
				Revision:     revision,
				OrgOnlyNS:    cfg.Verify.OrgOnlyNamespace,
				AllowedFiles: cfg.Verify.AllowedFiles,
				SHA256ByFile: cfg.Verify.SHA256ByFile,
				Logger:       stderrLog,
			})
			if err != nil {
				stderrLog.Error("verify failed", "error", err)
				return writeResult(hfpoc.Result{
					Status:  "failed",
					Command: "verify",
					Message: "verify_failed",
					Errors:  []string{err.Error()},
				}, err)
			}
			if res.Details == nil {
				res.Details = map[string]any{}
			}
			res.Details["config_file"] = cfgPath
			res.Details["state_file"] = cfg.State.File
			return writeResult(res, nil)
		},
	}
}

func tokenFromEnv(envName string) string {
	name := strings.TrimSpace(envName)
	if name == "" {
		name = "HF_TOKEN"
	}
	return strings.TrimSpace(os.Getenv(name))
}

func newLoggers(level string) (*slog.Logger, *slog.Logger) {
	lvl := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	// Logs to stderr only to avoid colliding with stdout result JSON.
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	l := slog.New(handler)
	return l, l
}

func writeResult(res hfpoc.Result, retErr error) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(true)
	_ = enc.Encode(res)
	if retErr != nil {
		return cli.Exit(retErr.Error(), 1)
	}
	return nil
}

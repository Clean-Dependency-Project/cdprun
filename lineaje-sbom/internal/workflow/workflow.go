package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/auth"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/client"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/output"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/sbom"
	"github.com/clean-dependency-project/cdprun/lineaje-sbom/internal/session"
)

// Config holds normalized CLI inputs for workflow execution.
type Config struct {
	SBOMPath       string
	POMPath        string
	PollDelay      int
	Output         string
	SBOMUploadURL  string
	SBOMFormat     string
	ProjectName    string
	ProjectVersion string
	Debug          bool
	SessionPath    string
	LoginOnly      bool
	PrintToken     bool
}

// Runner executes the lineaje-sbom flow with explicit output streams.
type Runner struct {
	stdout io.Writer
	stderr io.Writer
}

// NewRunner creates a workflow runner.
func NewRunner(stdout, stderr io.Writer) *Runner {
	return &Runner{stdout: stdout, stderr: stderr}
}

// Run executes upload/explain/session workflows.
func (r *Runner) Run(cfg Config) error {
	const explainQuery = "Recommend fix plans"
	const sessionDir = "./sessions"

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(r.stderr, &slog.HandlerOptions{Level: level}))

	if cfg.LoginOnly {
		authCfg := auth.DefaultConfig()
		authCfg.Logger = logger
		token, err := auth.GetToken(authCfg)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		if cfg.PrintToken {
			fmt.Fprintln(r.stdout, token)
			return nil
		}
		if cfg.Output == "json" {
			return writeJSON(r.stdout, map[string]string{"status": "ok", "token_preview": token[:min(20, len(token))] + "..."})
		}
		fmt.Fprintln(r.stdout, "Login OK")
		return nil
	}

	if cfg.SessionPath != "" {
		if cfg.SBOMPath != "" || cfg.POMPath != "" {
			return fmt.Errorf("use either --sbom/--pom or --session, not both")
		}
		sess, err := session.Load(cfg.SessionPath)
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}
		if sess.SBOMID == "" {
			return fmt.Errorf("session missing sbom_id: %s", cfg.SessionPath)
		}

		authCfg := auth.DefaultConfig()
		authCfg.Logger = logger
		token, err := auth.GetToken(authCfg)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}

		pollSec := cfg.PollDelay
		if pollSec <= 0 {
			pollSec = 5
		}
		clientCfg := client.DefaultConfig()
		clientCfg.Token = token
		clientCfg.Query = explainQuery
		clientCfg.SBOMID = sess.SBOMID
		clientCfg.Components = sess.Components
		clientCfg.PollDelay = time.Duration(pollSec) * time.Second
		clientCfg.Logger = logger
		clientCfg.InitialGUID = normalizeExplainGUID(sess)
		clientCfg.OnStillProcessing = func(guid, _ string, _ []string, _ string) {
			sess.GUID = guid
			if err := saveSessionAtPath(cfg.SessionPath, sess); err != nil {
				logger.Warn("update session guid failed", "err", err, "session_path", cfg.SessionPath)
				return
			}
			logger.Info("session updated with guid", "status", "session_updated", "guid", guid, "session_path", cfg.SessionPath)
		}

		responseBytes, err := client.Call(clientCfg)
		if err != nil {
			return fmt.Errorf("explain API: %w", err)
		}
		if err := output.Write(r.stdout, responseBytes, cfg.Output); err != nil {
			return fmt.Errorf("output: %w", err)
		}
		return nil
	}

	if cfg.SBOMPath == "" && cfg.POMPath == "" {
		return fmt.Errorf("exactly one of --sbom, --pom or --session is required")
	}
	if cfg.SBOMPath != "" && cfg.POMPath != "" {
		return fmt.Errorf("cannot use both --sbom and --pom")
	}

	authCfg := auth.DefaultConfig()
	authCfg.Logger = logger
	token, err := auth.GetToken(authCfg)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	if cfg.SBOMPath != "" {
		uploadCfg := client.DefaultUploadConfig()
		uploadCfg.Token = token
		uploadCfg.SBOMPath = cfg.SBOMPath
		uploadCfg.SBOMFormat = cfg.SBOMFormat
		uploadCfg.ProjectName = cfg.ProjectName
		uploadCfg.ProjectVersion = cfg.ProjectVersion
		uploadCfg.BaseURL = cfg.SBOMUploadURL
		uploadCfg.Logger = logger

		uploadResp, err := client.UploadSBOM(uploadCfg)
		if err != nil {
			return fmt.Errorf("upload sbom: %w", err)
		}
		sessionID := uploadResp.SBOMID
		if sessionID == "" {
			sessionID = fmt.Sprintf("upload-%d", time.Now().UTC().UnixNano())
		}
		uploadSession := session.Session{
			SessionKey:    sessionID,
			GUID:          "",
			SBOMID:        uploadResp.SBOMID,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			UploadPayload: uploadResp.Payload,
		}
		if err := session.Save(sessionDir, uploadSession); err != nil {
			logger.Warn("save upload session failed", "err", err, "session_id", sessionID)
		} else {
			logger.Info("upload session saved", "status", "session_saved", "session_id", sessionID, "path", filepath.Join(sessionDir, sessionID+".json"))
		}
		if cfg.Output == "json" {
			return writeJSON(r.stdout, uploadResp.Raw)
		}
		fmt.Fprintf(r.stdout, "SBOM uploaded successfully. sbom_id=%s\n", uploadResp.SBOMID)
		return nil
	}

	purls, err := sbom.PURLsFromPOM(cfg.POMPath)
	if err != nil {
		return fmt.Errorf("parse input: %w", err)
	}

	pollSec := cfg.PollDelay
	if pollSec <= 0 {
		pollSec = 5
	}
	clientCfg := client.DefaultConfig()
	clientCfg.Token = token
	clientCfg.Query = explainQuery
	clientCfg.Components = purls
	clientCfg.PollDelay = time.Duration(pollSec) * time.Second
	clientCfg.Logger = logger

	var savedGUID string
	clientCfg.OnStillProcessing = func(guid, query string, components []string, _ string) {
		s := session.Session{
			GUID:       guid,
			Query:      query,
			Components: components,
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		if err := session.Save(sessionDir, s); err != nil {
			logger.Warn("save session failed", "err", err, "guid", guid)
			return
		}
		savedGUID = guid
		logger.Info("session saved", "status", "session_saved", "guid", guid, "path", filepath.Join(sessionDir, guid+".json"))
	}

	responseBytes, err := client.Call(clientCfg)
	if err != nil {
		return fmt.Errorf("explain API: %w", err)
	}

	if savedGUID != "" {
		var final struct {
			Answer interface{} `json:"answer"`
		}
		if json.Unmarshal(responseBytes, &final) == nil && final.Answer != nil {
			_ = session.RemoveFile(sessionDir, savedGUID)
		}
	}

	if err := output.Write(r.stdout, responseBytes, cfg.Output); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	return nil
}

func writeJSON(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func normalizeExplainGUID(s session.Session) string {
	if s.GUID == "" {
		return ""
	}
	if s.SBOMID != "" && s.GUID == s.SBOMID {
		return ""
	}
	return s.GUID
}

func saveSessionAtPath(path string, s session.Session) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

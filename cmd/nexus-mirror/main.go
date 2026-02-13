// Command nexus-mirror mirrors artifacts from a Nexus raw proxy repository
// into a Nexus raw hosted repository. It fetches an index.json, HEAD-checks
// the destination for each entry, and only downloads+uploads the missing ones.
//
// Logs: JSON to stderr. Output: JSON to stdout.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
)

const (
	userAgent = "cdprun-nexus-mirror/0.1"
)

// --- types ---

// mirrorConfig holds all resolved CLI configuration.
type mirrorConfig struct {
	sourceURL  string // Nexus base URL for proxy repo (download)
	sourceRepo string // proxy repo name
	sourceUser string
	sourcePass string

	destURL  string // Nexus base URL for hosted repo (upload)
	destRepo string // hosted repo name
	destUser string
	destPass string

	indexURL     string
	indexFile    string
	limit        int
	concurrency  int
	dryRun       bool
	force        bool
	includeItems bool
	timeout      time.Duration
	logLevel     slog.Level
}

type indexEntry struct {
	Platform   string `json:"platform"`
	Type       string `json:"type"`
	Binary     string `json:"binary"`
	SourcePath string `json:"source_path"`
	SHA256     string `json:"sha256"`
	Version    string `json:"version"`
}

type indexRoot struct {
	Linux   []indexEntry `json:"linux"`
	Mac     []indexEntry `json:"mac"`
	Windows []indexEntry `json:"windows"`
}

// artifact represents a single file to mirror.
type artifact struct {
	SourcePath string
	DestPath   string
	SHA256     string
	Platform   string
	FileType   string
	Version    string
}

// artifactResult captures the outcome of mirroring a single artifact.
type artifactResult struct {
	Status         string `json:"status"`
	Message        string `json:"message,omitempty"`
	Error          string `json:"error,omitempty"`
	SourcePath     string `json:"source_path"`
	DestPath       string `json:"dest_path"`
	ProxyURL       string `json:"proxy_url,omitempty"`
	DestinationURL string `json:"destination_url,omitempty"`
	Bytes          int64  `json:"bytes,omitempty"`
	SHA256Expected string `json:"sha256_expected,omitempty"`
	SHA256Actual   string `json:"sha256_actual,omitempty"`
	HeadStatus     int    `json:"head_status,omitempty"`
	Platform       string `json:"platform,omitempty"`
	FileType       string `json:"file_type,omitempty"`
	Version        string `json:"version,omitempty"`
}

// mirrorResult is the JSON output written to stdout.
type mirrorResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`

	DryRun     bool   `json:"dry_run"`
	Forced     bool   `json:"forced"`
	SourceURL  string `json:"source_url"`
	SourceRepo string `json:"source_repo"`
	DestURL    string `json:"dest_url"`
	DestRepo   string `json:"dest_repo"`
	IndexURL   string `json:"index_url"`

	IndexTotal   int `json:"index_total"`
	Checked      int `json:"checked"`
	Selected     int `json:"selected"`
	SkippedExist int `json:"skipped_exists"`
	Planned      int `json:"planned"`
	Uploaded     int `json:"uploaded"`
	Failed       int `json:"failed"`

	Items      []artifactResult `json:"items,omitempty"`
	DurationMs int64            `json:"duration_ms"`
}

type nexusRepo struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Type   string `json:"type"`
}

// --- main ---

func main() {
	app := &cli.App{
		Name:  "nexus-mirror",
		Usage: "Mirror artifacts from a Nexus proxy repo into a hosted raw repo (config file only)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "path to JSON config file (if unset, will search common paths)",
				EnvVars: []string{"NEXUS_MIRROR_CONFIG"},
			},
		},
		Action: run,
	}
	if err := app.Run(os.Args); err != nil {
		os.Exit(1)
	}
}

func run(c *cli.Context) error {
	start := time.Now()
	conf, err := parseConfig(c)
	log := newLogger(conf.logLevel)
	if err != nil {
		return finishWithError(log, start, conf, err.Error(), err)
	}

	client := &http.Client{Timeout: conf.timeout}

	if err := preflightCheck(c.Context, client, conf); err != nil {
		return finishWithError(log, start, conf, err.Error(), err)
	}

	// Index is public (e.g. GitHub Pages) -- no auth needed.
	allArtifacts, err := fetchIndex(c.Context, client, conf, log)
	if err != nil {
		return finishWithError(log, start, conf, "failed to load index.json", err)
	}

	selected, checked, skippedExist := selectMissing(c.Context, client, log, conf, allArtifacts)

	res := newMirrorResult(conf, time.Since(start))
	res.IndexTotal = len(allArtifacts)
	res.Checked = checked
	res.SkippedExist = skippedExist
	res.Selected = len(selected)

	if len(selected) == 0 {
		res.Status = "success"
		res.Message = "no missing items"
		return emitResult(res)
	}

	perItem := syncAll(c.Context, client, log, conf, selected)
	for _, r := range perItem {
		switch r.Status {
		case "planned":
			res.Planned++
		case "uploaded":
			res.Uploaded++
		case "failed":
			res.Failed++
		case "skipped_exists":
			res.SkippedExist++
		}
	}
	if conf.includeItems {
		res.Items = perItem
	}

	res.DurationMs = time.Since(start).Milliseconds()
	if res.Failed > 0 {
		res.Status = "partial_success"
		res.Message = "some items failed"
	} else {
		res.Status = "success"
		res.Message = "all items processed"
	}
	return emitResult(res)
}

// --- config ---

func parseConfig(c *cli.Context) (mirrorConfig, error) {
	configPath, err := resolveConfigPath(c.String("config"))
	if err != nil {
		return mirrorConfig{}, err
	}
	fileCfg, err := loadConfigFile(configPath)
	if err != nil {
		return mirrorConfig{}, err
	}

	// Required strings.
	sourceURL, ok := derefString(fileCfg.SourceURL)
	if !ok || strings.TrimSpace(sourceURL) == "" {
		return mirrorConfig{}, errors.New("config missing source_url")
	}
	sourceRepo, ok := derefString(fileCfg.SourceRepo)
	if !ok || strings.TrimSpace(sourceRepo) == "" {
		return mirrorConfig{}, errors.New("config missing source_repo")
	}
	destURL, ok := derefString(fileCfg.DestURL)
	if !ok || strings.TrimSpace(destURL) == "" {
		return mirrorConfig{}, errors.New("config missing dest_url")
	}
	destRepo, ok := derefString(fileCfg.DestRepo)
	if !ok || strings.TrimSpace(destRepo) == "" {
		return mirrorConfig{}, errors.New("config missing dest_repo")
	}

	indexURL, _ := derefString(fileCfg.IndexURL)
	indexFile, _ := derefString(fileCfg.IndexFile)
	if strings.TrimSpace(indexURL) == "" && strings.TrimSpace(indexFile) == "" {
		return mirrorConfig{}, errors.New("config must set either index_url or index_file")
	}

	timeout := 10 * time.Minute
	if fileCfg.Timeout != nil {
		d, err := time.ParseDuration(strings.TrimSpace(*fileCfg.Timeout))
		if err != nil {
			return mirrorConfig{}, fmt.Errorf("invalid config timeout %q: %w", *fileCfg.Timeout, err)
		}
		timeout = d
	}

	limit := 3
	if fileCfg.Limit != nil {
		limit = *fileCfg.Limit
	}
	if limit < 0 {
		limit = 0
	}

	concurrency := 5
	if fileCfg.Concurrency != nil {
		concurrency = *fileCfg.Concurrency
	}

	dryRun := false
	if fileCfg.DryRun != nil {
		dryRun = *fileCfg.DryRun
	}

	force := false
	if fileCfg.Force != nil {
		force = *fileCfg.Force
	}

	includeItems := true
	if fileCfg.IncludeItems != nil {
		includeItems = *fileCfg.IncludeItems
	}

	logLevel := slog.LevelInfo
	if fileCfg.LogLevel != nil {
		logLevel = parseLogLevel(*fileCfg.LogLevel)
	}

	conf := mirrorConfig{
		sourceURL:    strings.TrimSpace(sourceURL),
		sourceRepo:   strings.TrimSpace(sourceRepo),
		sourceUser:   strings.TrimSpace(ptrToString(fileCfg.SourceUser)),
		sourcePass:   strings.TrimSpace(ptrToString(fileCfg.SourcePassword)),
		destURL:      strings.TrimSpace(destURL),
		destRepo:     strings.TrimSpace(destRepo),
		destUser:     strings.TrimSpace(ptrToString(fileCfg.DestUser)),
		destPass:     strings.TrimSpace(ptrToString(fileCfg.DestPassword)),
		indexURL:     strings.TrimSpace(indexURL),
		indexFile:    strings.TrimSpace(indexFile),
		limit:        limit,
		concurrency:  clamp(concurrency, 1, 50),
		dryRun:       dryRun,
		force:        force,
		includeItems: includeItems,
		timeout:      timeout,
		logLevel:     logLevel,
	}

	conf.sourceURL, err = normalizeBaseURL(conf.sourceURL)
	if err != nil {
		return conf, fmt.Errorf("invalid config source_url: %w", err)
	}
	conf.destURL, err = normalizeBaseURL(conf.destURL)
	if err != nil {
		return conf, fmt.Errorf("invalid config dest_url: %w", err)
	}
	if err := validateRepoName(conf.sourceRepo); err != nil {
		return conf, fmt.Errorf("invalid config source_repo: %w", err)
	}
	if err := validateRepoName(conf.destRepo); err != nil {
		return conf, fmt.Errorf("invalid config dest_repo: %w", err)
	}
	return conf, nil
}

type configFile struct {
	SourceURL      *string        `json:"source_url"`
	SourceRepo     *string        `json:"source_repo"`
	SourceUser     *string        `json:"source_user"`
	SourcePassword *string        `json:"source_password"`
	DestURL        *string        `json:"dest_url"`
	DestRepo       *string        `json:"dest_repo"`
	DestUser       *string        `json:"dest_user"`
	DestPassword   *string        `json:"dest_password"`
	IndexURL       *string        `json:"index_url"`
	IndexFile      *string        `json:"index_file"`
	Limit          *int           `json:"limit"`
	DryRun         *bool          `json:"dry_run"`
	Force          *bool          `json:"force"`
	IncludeItems   *bool          `json:"include_items"`
	Timeout        *string        `json:"timeout"`
	LogLevel       *string        `json:"log_level"`
	Concurrency    *int           `json:"concurrency"`
}

func loadConfigFile(p string) (configFile, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return configFile{}, errors.New("empty config path")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return configFile{}, fmt.Errorf("read config file %q: %w", p, err)
	}
	var cfg configFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		return configFile{}, fmt.Errorf("decode config file %q: %w", p, err)
	}
	return cfg, nil
}

func resolveConfigPath(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return strings.TrimSpace(flag), nil
	}
	if v := strings.TrimSpace(os.Getenv("NEXUS_MIRROR_CONFIG")); v != "" {
		return v, nil
	}
	candidates := []string{
		"config/nexus_mirror.json",
		"nexus-mirror.json",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("config file not specified; set --config or NEXUS_MIRROR_CONFIG, or create config/nexus_mirror.json")
}

func ptrToString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (c configFile) getString(name string) (string, bool) {
	switch name {
	case "source-url":
		return derefString(c.SourceURL)
	case "source-repo":
		return derefString(c.SourceRepo)
	case "source-user":
		return derefString(c.SourceUser)
	case "source-password":
		return derefString(c.SourcePassword)
	case "dest-url":
		return derefString(c.DestURL)
	case "dest-repo":
		return derefString(c.DestRepo)
	case "dest-user":
		return derefString(c.DestUser)
	case "dest-password":
		return derefString(c.DestPassword)
	case "index-url":
		return derefString(c.IndexURL)
	case "index-file":
		return derefString(c.IndexFile)
	case "log-level":
		return derefString(c.LogLevel)
	default:
		return "", false
	}
}

func (c configFile) getInt(name string) (int, bool) {
	switch name {
	case "limit":
		if c.Limit == nil {
			return 0, false
		}
		return *c.Limit, true
	case "concurrency":
		if c.Concurrency == nil {
			return 0, false
		}
		return *c.Concurrency, true
	default:
		return 0, false
	}
}

func (c configFile) getBool(name string) (bool, bool) {
	switch name {
	case "dry-run":
		return derefBool(c.DryRun)
	case "force":
		return derefBool(c.Force)
	case "include-items":
		return derefBool(c.IncludeItems)
	default:
		return false, false
	}
}

func (c configFile) getDuration(name string) (time.Duration, bool) {
	if name != "timeout" || c.Timeout == nil {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSpace(*c.Timeout))
	if err != nil {
		return 0, false
	}
	return d, true
}

func derefString(p *string) (string, bool) {
	if p == nil {
		return "", false
	}
	return *p, true
}

func derefBool(p *bool) (bool, bool) {
	if p == nil {
		return false, false
	}
	return *p, true
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeBaseURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("URL must include scheme and host: %q", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func validateRepoName(name string) error {
	if strings.ContainsAny(name, "/:") {
		return fmt.Errorf("must be a Nexus repo name (no '/' or ':'): %q", name)
	}
	return nil
}

// --- preflight ---

func preflightCheck(ctx context.Context, client *http.Client, conf mirrorConfig) error {
	// Check source (proxy) repo exists and is raw.
	sourceRepos, err := listRepos(ctx, client, conf.sourceURL, conf.sourceUser, conf.sourcePass)
	if err != nil {
		return fmt.Errorf("list source repos: %w", err)
	}
	src := findRepo(sourceRepos, conf.sourceRepo)
	if src == nil {
		return fmt.Errorf("source repo %q not found in Nexus at %s", conf.sourceRepo, conf.sourceURL)
	}
	if src.Format != "raw" {
		return fmt.Errorf("source repo %q format is %q, expected raw", conf.sourceRepo, src.Format)
	}

	// Check destination (hosted) repo exists, is raw, and is hosted.
	destRepos, err := listRepos(ctx, client, conf.destURL, conf.destUser, conf.destPass)
	if err != nil {
		return fmt.Errorf("list dest repos: %w", err)
	}
	dst := findRepo(destRepos, conf.destRepo)
	if dst == nil {
		return fmt.Errorf("dest repo %q not found in Nexus at %s", conf.destRepo, conf.destURL)
	}
	if dst.Format != "raw" {
		return fmt.Errorf("dest repo %q format is %q, expected raw", conf.destRepo, dst.Format)
	}
	if dst.Type != "hosted" {
		return fmt.Errorf("dest repo %q is type %q; uploads require a hosted raw repo", conf.destRepo, dst.Type)
	}
	return nil
}

func findRepo(repos []nexusRepo, name string) *nexusRepo {
	for i := range repos {
		if repos[i].Name == name {
			return &repos[i]
		}
	}
	return nil
}

func listRepos(ctx context.Context, client *http.Client, baseURL string, user string, pass string) ([]nexusRepo, error) {
	body, err := authedGet(ctx, client, baseURL+"/service/rest/v1/repositories", user, pass)
	if err != nil {
		return nil, err
	}
	var repos []nexusRepo
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("decode repositories: %w", err)
	}
	return repos, nil
}

// --- index ---

// fetchIndex loads index.json from a local file or public URL (no auth for URL).
func fetchIndex(ctx context.Context, client *http.Client, conf mirrorConfig, log *slog.Logger) ([]artifact, error) {
	var body []byte
	if conf.indexFile != "" {
		log.Info("reading index.json", "file", conf.indexFile)
		b, err := os.ReadFile(conf.indexFile)
		if err != nil {
			return nil, fmt.Errorf("read index file: %w", err)
		}
		body = b
	} else {
		log.Info("fetching index.json", "url", conf.indexURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, conf.indexURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		// No auth: index is public.

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch index.json: %w", err)
		}
		defer resp.Body.Close()

		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch index.json: status=%d body=%q", resp.StatusCode, string(bytes.TrimSpace(b)))
		}
		body = b
	}

	var root indexRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode index.json: %w", err)
	}

	var out []artifact
	addEntry := func(entry indexEntry) {
		destPath := cleanPath(entry.Binary)
		sourcePath := cleanPath(entry.SourcePath)
		if sourcePath == "" {
			sourcePath = destPath
		}
		if sourcePath == "" || destPath == "" {
			return
		}
		out = append(out, artifact{
			SourcePath: sourcePath,
			DestPath:   destPath,
			SHA256:     strings.TrimSpace(entry.SHA256),
			Platform:   strings.TrimSpace(entry.Platform),
			FileType:   strings.TrimSpace(entry.Type),
			Version:    strings.TrimSpace(entry.Version),
		})
	}
	for _, entry := range root.Linux {
		addEntry(entry)
	}
	for _, entry := range root.Mac {
		addEntry(entry)
	}
	for _, entry := range root.Windows {
		addEntry(entry)
	}
	log.Info("index loaded", "artifacts", len(out))
	return out, nil
}

func cleanPath(p string) string {
	p = path.Clean(strings.TrimSpace(strings.TrimPrefix(p, "/")))
	if p == "." {
		return ""
	}
	return p
}

// --- select missing ---

// selectMissing HEAD-checks the destination for each artifact and returns
// only those that are missing. limit controls how many missing items to collect
// (0 means all).
func selectMissing(ctx context.Context, client *http.Client, log *slog.Logger, conf mirrorConfig, all []artifact) (selected []artifact, checked int, skippedExist int) {
	if conf.force {
		if conf.limit > 0 && len(all) > conf.limit {
			return all[:conf.limit], conf.limit, 0
		}
		return all, len(all), 0
	}

	for _, a := range all {
		if ctx.Err() != nil {
			break
		}
		destURL := artifactDestURL(conf, a.DestPath)
		code, err := headStatus(ctx, client, destURL, conf.destUser, conf.destPass)
		checked++

		switch {
		case err != nil:
			log.Warn("HEAD failed; selecting anyway", "url", destURL, "error", err)
			selected = append(selected, a)
		case code == http.StatusOK:
			skippedExist++
			continue
		case code >= 500:
			log.Warn("HEAD 5xx; selecting anyway", "url", destURL, "status", code)
			selected = append(selected, a)
		default:
			// 404 or other non-200: treat as missing.
			selected = append(selected, a)
		}

		if conf.limit > 0 && len(selected) >= conf.limit {
			break
		}
	}
	return
}

// --- sync ---

func syncAll(ctx context.Context, client *http.Client, log *slog.Logger, conf mirrorConfig, items []artifact) []artifactResult {
	workers := conf.concurrency
	if workers > len(items) {
		workers = len(items)
	}

	jobs := make(chan artifact)
	results := make(chan artifactResult)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for a := range jobs {
				results <- syncOne(ctx, client, log, conf, a)
			}
		}()
	}

	go func() {
		for _, a := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- a:
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var out []artifactResult
	for r := range results {
		out = append(out, r)
	}
	return out
}

func syncOne(ctx context.Context, client *http.Client, log *slog.Logger, conf mirrorConfig, a artifact) artifactResult {
	ar := artifactResult{
		SourcePath:     a.SourcePath,
		DestPath:       a.DestPath,
		SHA256Expected: a.SHA256,
		Platform:       a.Platform,
		FileType:       a.FileType,
		Version:        a.Version,
		ProxyURL:       artifactSourceURL(conf, a.SourcePath),
		DestinationURL: artifactDestURL(conf, a.DestPath),
	}

	// Re-check in case the asset appeared between selection and processing.
	if !conf.force {
		if code, err := headStatus(ctx, client, ar.DestinationURL, conf.destUser, conf.destPass); err == nil {
			ar.HeadStatus = code
			if code == http.StatusOK {
				ar.Status = "skipped_exists"
				ar.Message = "already exists"
				return ar
			}
		}
	}

	if conf.dryRun {
		ar.Status = "planned"
		ar.Message = "would download and upload"
		return ar
	}

	tmpFile, bytesWritten, actualSHA, err := downloadArtifact(ctx, client, ar.ProxyURL, conf.sourceUser, conf.sourcePass, log)
	if err != nil {
		ar.Status = "failed"
		ar.Message = "download failed"
		ar.Error = err.Error()
		return ar
	}
	defer os.Remove(tmpFile)
	ar.Bytes = bytesWritten
	ar.SHA256Actual = actualSHA

	if ar.SHA256Expected != "" && !strings.EqualFold(ar.SHA256Expected, actualSHA) {
		ar.Status = "failed"
		ar.Message = "sha256 mismatch"
		ar.Error = fmt.Sprintf("expected %s got %s", ar.SHA256Expected, actualSHA)
		return ar
	}

	if err := uploadArtifact(ctx, client, conf, tmpFile, a.DestPath, log); err != nil {
		ar.Status = "failed"
		ar.Message = "upload failed"
		ar.Error = err.Error()
		return ar
	}

	ar.Status = "uploaded"
	ar.Message = "uploaded successfully"
	return ar
}

// --- HTTP helpers ---

func artifactSourceURL(conf mirrorConfig, sourcePath string) string {
	return conf.sourceURL + "/repository/" + url.PathEscape(conf.sourceRepo) + "/" + sourcePath
}

func artifactDestURL(conf mirrorConfig, destPath string) string {
	return conf.destURL + "/repository/" + url.PathEscape(conf.destRepo) + "/" + destPath
}

func headStatus(ctx context.Context, client *http.Client, targetURL string, user string, pass string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// authedGet performs an authenticated GET and returns the response body.
func authedGet(ctx context.Context, client *http.Client, targetURL string, user string, pass string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status=%d body=%q", targetURL, resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	return body, nil
}

// downloadArtifact streams an artifact to a temp file and computes its SHA256.
// The caller is responsible for removing the returned temp file path.
func downloadArtifact(ctx context.Context, client *http.Client, artifactURL string, user string, pass string, log *slog.Logger) (tmpPath string, bytesWritten int64, sha256hex string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}

	log.Info("downloading via proxy", "url", artifactURL)
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return "", 0, "", fmt.Errorf("download: status=%d body=%q", resp.StatusCode, string(bytes.TrimSpace(body)))
	}

	tmpFile, err := os.CreateTemp("", "nexus-mirror-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temp file: %w", err)
	}

	hasher := sha256.New()
	bytesWritten, copyErr := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body)

	// Always close before returning so the file is flushable.
	closeErr := tmpFile.Close()

	if copyErr != nil {
		os.Remove(tmpFile.Name()) // clean up on error
		return "", 0, "", fmt.Errorf("write temp file: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpFile.Name())
		return "", 0, "", fmt.Errorf("close temp file: %w", closeErr)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	log.Info("downloaded", "bytes", bytesWritten, "sha256", checksum)
	return tmpFile.Name(), bytesWritten, checksum, nil
}

// uploadArtifact uploads a local file to the destination Nexus hosted repo
// using the raw components REST API.
func uploadArtifact(ctx context.Context, client *http.Client, conf mirrorConfig, filePath string, destPath string, log *slog.Logger) error {
	dir := path.Dir(destPath)
	if dir == "." {
		dir = ""
	}
	filename := path.Base(destPath)

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	apiURL := conf.destURL + "/service/rest/v1/components?repository=" + url.QueryEscape(conf.destRepo)

	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)

	// Stream the multipart body so we don't buffer large files in memory.
	go func() {
		defer func() {
			_ = multipartWriter.Close()
			_ = pipeWriter.Close()
		}()

		// WriteField errors are propagated via the pipe: if the reader
		// side is closed (e.g. server rejected early), writes will fail,
		// and CreateFormFile below will surface the error.
		_ = multipartWriter.WriteField("raw.directory", dir)
		_ = multipartWriter.WriteField("raw.asset1.filename", filename)

		part, err := multipartWriter.CreateFormFile("raw.asset1", filename)
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			_ = pipeWriter.CloseWithError(err)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, pipeReader)
	if err != nil {
		return fmt.Errorf("build upload request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	if conf.destUser != "" && conf.destPass != "" {
		req.SetBasicAuth(conf.destUser, conf.destPass)
	}

	log.Info("uploading", "url", apiURL, "dir", dir, "file", filename)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return fmt.Errorf("upload: status=%d body=%q", resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	return nil
}

// --- output helpers ---

func newMirrorResult(conf mirrorConfig, elapsed time.Duration) mirrorResult {
	idx := conf.indexURL
	if conf.indexFile != "" {
		idx = conf.indexFile
	}
	return mirrorResult{
		DryRun:     conf.dryRun,
		Forced:     conf.force,
		SourceURL:  conf.sourceURL,
		SourceRepo: conf.sourceRepo,
		DestURL:    conf.destURL,
		DestRepo:   conf.destRepo,
		IndexURL:   idx,
		DurationMs: elapsed.Milliseconds(),
	}
}

func finishWithError(log *slog.Logger, start time.Time, conf mirrorConfig, msg string, err error) error {
	log.Error("failed", "error", err)
	res := newMirrorResult(conf, time.Since(start))
	res.Status = "failed"
	res.Message = msg
	_ = emitResult(res)
	return err
}

func emitResult(res mirrorResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// --- logging ---

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	}))
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

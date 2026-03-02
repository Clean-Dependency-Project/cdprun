// Package client provides the Lineaje explain API client with polling.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

const (
	defaultExplainURL = "https://lineaje-gpt-service.v2.prod.veedna.com/api/v1/explain"
	defaultMaxPolls   = 3
)

// stillProcessingRE matches API messages indicating the request is still processing (case-insensitive).
// Matches both "still processing" and "being processed".
var stillProcessingRE = regexp.MustCompile(`(?i)(still\s+processing|being\s+processed)`)

// OnStillProcessing is called when the first response has a guid and message indicates
// the request is still processing. Optional; used by main to save session.
type OnStillProcessingFunc func(guid, query string, components []string, message string)

// Config holds explain API client configuration.
type Config struct {
	BaseURL            string
	Token              string
	Query              string
	SBOMID             string
	Components         []string
	PollDelay          time.Duration
	MaxPolls           int
	Timeout            time.Duration
	Logger             *slog.Logger
	InitialGUID        string             // when set, skip first request and only poll with this guid
	OnStillProcessing  OnStillProcessingFunc
}

// DefaultConfig returns config with default URL and poll settings.
func DefaultConfig() Config {
	return Config{
		BaseURL:   defaultExplainURL,
		PollDelay: 5 * time.Second,
		MaxPolls:  defaultMaxPolls,
		Timeout:   60 * time.Second,
	}
}

// explainRequest is the JSON body sent to the API.
type explainRequest struct {
	Query    string            `json:"query"`
	SBOMID   string            `json:"sbom_id,omitempty"`
	GUID     string            `json:"guid,omitempty"`
	Metadata *explainMetadata  `json:"metadata,omitempty"`
}

type explainMetadata struct {
	Components []string `json:"components"`
}

// explainResponse is used to parse guid, message, and answer from the API JSON.
type explainResponse struct {
	GUID    string      `json:"guid,omitempty"`
	Message string      `json:"message,omitempty"`
	Answer  interface{} `json:"answer,omitempty"`
}

// Call sends the initial request (no guid), then polls up to MaxPolls times with the returned guid.
// It returns the full response body from the last successful request (for table/JSON output in CLI).
func Call(cfg Config) ([]byte, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("token is required")
	}
	if cfg.SBOMID == "" && len(cfg.Components) == 0 {
		return nil, fmt.Errorf("sbom_id or at least one component PURL is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultExplainURL
	}
	if cfg.MaxPolls <= 0 {
		cfg.MaxPolls = defaultMaxPolls
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	reqBody := explainRequest{Query: cfg.Query, SBOMID: cfg.SBOMID}
	if len(cfg.Components) > 0 {
		reqBody.Metadata = &explainMetadata{Components: cfg.Components}
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}

	var guid string
	var respBytes []byte

	if cfg.InitialGUID != "" {
		// Resume path: skip first request, poll only with saved guid
		guid = cfg.InitialGUID
		if cfg.Logger != nil {
			cfg.Logger.Info("resuming with saved session, will poll",
				"status", "resuming",
				"guid", guid,
				"max_polls", cfg.MaxPolls,
				"poll_interval_seconds", int(cfg.PollDelay.Seconds()))
		}
	} else {
		// First request: no guid
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		respBytes, err = doRequest(httpClient, cfg.BaseURL, cfg.Token, bodyBytes, cfg.Logger)
		if err != nil {
			return nil, fmt.Errorf("initial request: %w", err)
		}

		var first explainResponse
		if err := json.Unmarshal(respBytes, &first); err != nil {
			return nil, fmt.Errorf("parse initial response: %w", err)
		}
		if first.Answer != nil {
			return respBytes, nil
		}
		guid = first.GUID
		if guid == "" {
			return respBytes, nil
		}
		// Notify caller when still processing so they can save session
		msg := first.Message
		if guid != "" && cfg.OnStillProcessing != nil {
			cfg.OnStillProcessing(guid, cfg.Query, cfg.Components, msg)
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("request still processing, will poll",
				"status", "still_processing",
				"max_polls", cfg.MaxPolls,
				"poll_interval_seconds", int(cfg.PollDelay.Seconds()))
		}
	}

	// Poll with guid
	for i := 0; i < cfg.MaxPolls; i++ {
		time.Sleep(cfg.PollDelay)
		reqBody.GUID = guid
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal poll request: %w", err)
		}
		respBytes, err = doRequest(httpClient, cfg.BaseURL, cfg.Token, bodyBytes, cfg.Logger)
		if err != nil {
			return nil, fmt.Errorf("poll request: %w", err)
		}
		var polled explainResponse
		if err := json.Unmarshal(respBytes, &polled); err != nil {
			return nil, fmt.Errorf("parse poll response: %w", err)
		}
		if polled.Answer != nil {
			return respBytes, nil
		}
		if i < cfg.MaxPolls-1 && cfg.Logger != nil {
			cfg.Logger.Info("request still processing, polling again",
				"status", "still_processing",
				"attempt", i+2,
				"max_polls", cfg.MaxPolls,
				"next_poll_seconds", int(cfg.PollDelay.Seconds()))
		}
	}
	return respBytes, nil
}

func doRequest(c *http.Client, baseURL, token string, body []byte, logger *slog.Logger) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	if logger != nil && logger.Enabled(context.Background(), slog.LevelDebug) {
		logger.Debug("http_request", "method", req.Method, "url", req.URL.String(), "body", string(body))
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	respBytes := buf.Bytes()

	if logger != nil && logger.Enabled(context.Background(), slog.LevelDebug) {
		logger.Debug("http_response", "status", resp.StatusCode, "body", string(respBytes))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return respBytes, nil
}

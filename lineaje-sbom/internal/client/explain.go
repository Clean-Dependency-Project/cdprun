// Package client provides the Lineaje explain API client with polling.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	defaultExplainURL = "https://lineaje-gpt-service.v2.prod.veedna.com/api/v1/explain"
	defaultMaxPolls   = 3
)

// Config holds explain API client configuration.
type Config struct {
	BaseURL    string
	Token      string
	Query      string
	Components []string
	PollDelay  time.Duration
	MaxPolls   int
	Timeout    time.Duration
	Logger     *slog.Logger
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
	GUID     string            `json:"guid,omitempty"`
	Metadata explainMetadata   `json:"metadata"`
}

type explainMetadata struct {
	Components []string `json:"components"`
}

// explainResponse is used to parse guid and answer from the API JSON.
type explainResponse struct {
	GUID   string      `json:"guid,omitempty"`
	Answer interface{} `json:"answer,omitempty"`
}

// Call sends the initial request (no guid), then polls up to MaxPolls times with the returned guid.
// It returns the full response body from the last successful request (for table/JSON output in CLI).
func Call(cfg Config) ([]byte, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("token is required")
	}
	if len(cfg.Components) == 0 {
		return nil, fmt.Errorf("at least one component PURL is required")
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

	reqBody := explainRequest{
		Query:    cfg.Query,
		Metadata: explainMetadata{Components: cfg.Components},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}

	// First request: no guid
	respBytes, err := doRequest(httpClient, cfg.BaseURL, cfg.Token, bodyBytes, cfg.Logger)
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
	guid := first.GUID
	if guid == "" {
		return respBytes, nil
	}

	// Poll with guid
	for i := 0; i < cfg.MaxPolls; i++ {
		time.Sleep(cfg.PollDelay)
		reqBody.GUID = guid
		bodyBytes, err = json.Marshal(reqBody)
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

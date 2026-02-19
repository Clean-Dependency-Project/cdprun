package hfpoc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	userAgent  string
}

type ClientOptions struct {
	BaseURL   string
	Token     string
	UserAgent string
	Timeout   time.Duration
}

func NewClient(opts ClientOptions) *Client {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "https://huggingface.co"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = "cdprun-hf-poc/0.1"
	}
	return &Client{
		baseURL: base,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		token:     strings.TrimSpace(opts.Token),
		userAgent: ua,
	}
}

func (c *Client) GetModelInfo(ctx context.Context, repoID string) (ModelInfo, error) {
	var out ModelInfo
	url := fmt.Sprintf("%s/api/models/%s", c.baseURL, repoID)
	if err := c.getJSON(ctx, url, &out); err != nil {
		return ModelInfo{}, err
	}
	return out, nil
}

func (c *Client) CreateRepo(ctx context.Context, req CreateRepoRequest) (CreateRepoResponse, error) {
	var out CreateRepoResponse
	url := fmt.Sprintf("%s/api/repos/create", c.baseURL)
	if err := c.postJSON(ctx, url, req, &out); err != nil {
		return CreateRepoResponse{}, err
	}
	return out, nil
}

// DownloadResolve downloads a repo file using the public resolve endpoint.
// This works for models/datasets/spaces, but for PoC we only use models.
func (c *Client) DownloadResolve(ctx context.Context, repoID, revision, pathInRepo string) ([]byte, error) {
	repoID = strings.TrimSpace(repoID)
	revision = strings.TrimSpace(revision)
	pathInRepo = strings.TrimLeft(strings.TrimSpace(pathInRepo), "/")
	if repoID == "" || revision == "" || pathInRepo == "" {
		return nil, fmt.Errorf("download resolve: repoID, revision, and pathInRepo are required")
	}
	url := fmt.Sprintf("%s/%s/resolve/%s/%s", c.baseURL, repoID, revision, pathInRepo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download resolve: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download resolve: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download resolve: status=%d body=%q", resp.StatusCode, string(b))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download resolve: read body: %w", err)
	}
	return b, nil
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("GET %s: build request: %w", url, err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: request failed: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s: status=%d body=%q", url, resp.StatusCode, string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: decode JSON: %w", url, err)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, url string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("POST %s: marshal JSON: %w", url, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		return fmt.Errorf("POST %s: build request: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: request failed: %w", url, err)
	}
	defer resp.Body.Close()

	// HF sometimes returns 200/201 on create; treat any 2xx as success.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("POST %s: status=%d body=%q", url, resp.StatusCode, string(rb))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("POST %s: decode JSON: %w", url, err)
	}
	return nil
}

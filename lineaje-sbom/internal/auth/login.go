// Package auth provides Lineaje Identity Service login and token retrieval.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	defaultLoginURL         = "https://lineaje-identity-service.v2.prod.veedna.com/lineajeidentity/api/v1/login"
	maxRetries              = 3
	waitBetweenRetries      = 5 * time.Second
	loginRequestTimeout     = 30 * time.Second
	envUsername             = "LINEAJE_USERNAME"
	envPassword             = "LINEAJE_PASSWORD"
)

// LoginResponse represents the JSON response from the login API.
type LoginResponse struct {
	AccessToken string `json:"accessToken"`
}

// Config holds login configuration (URL and credentials source).
type Config struct {
	LoginURL  string
	Username  string
	Password  string
	MaxRetries int
	WaitBetweenRetries time.Duration
	Timeout   time.Duration
}

// DefaultConfig returns config with defaults and env vars for username/password.
func DefaultConfig() Config {
	return Config{
		LoginURL:           defaultLoginURL,
		Username:           os.Getenv(envUsername),
		Password:           os.Getenv(envPassword),
		MaxRetries:         maxRetries,
		WaitBetweenRetries: waitBetweenRetries,
		Timeout:             loginRequestTimeout,
	}
}

// GetToken fetches a Lineaje access token using the configured credentials.
// It retries up to cfg.MaxRetries with cfg.WaitBetweenRetries between attempts.
// Returns the access token or an error.
func GetToken(cfg Config) (string, error) {
	if cfg.Username == "" || cfg.Password == "" {
		return "", fmt.Errorf("credentials required: set %s and %s", envUsername, envPassword)
	}
	if cfg.LoginURL == "" {
		cfg.LoginURL = defaultLoginURL
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = maxRetries
	}
	if cfg.WaitBetweenRetries <= 0 {
		cfg.WaitBetweenRetries = waitBetweenRetries
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = loginRequestTimeout
	}

	payload := map[string]string{
		"username": cfg.Username,
		"password": cfg.Password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal login payload: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		token, err := doLogin(cfg.LoginURL, body, cfg.Timeout)
		if err == nil && token != "" {
			return token, nil
		}
		lastErr = err
		if attempt < cfg.MaxRetries {
			time.Sleep(cfg.WaitBetweenRetries)
		}
	}
	return "", fmt.Errorf("failed after %d attempts: %w", cfg.MaxRetries, lastErr)
}

func doLogin(loginURL string, body []byte, timeout time.Duration) (string, error) {
	req, err := http.NewRequest(http.MethodPost, loginURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login returned status %d", resp.StatusCode)
	}

	var out LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("login succeeded but accessToken missing in response")
	}
	return out.AccessToken, nil
}

package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetToken_MissingCredentials(t *testing.T) {
	cfg := Config{
		LoginURL:   "http://localhost:9999/login",
		Username:   "",
		Password:   "",
		MaxRetries: 1,
	}
	_, err := GetToken(cfg)
	if err == nil {
		t.Fatal("expected error when credentials missing")
	}
	if err.Error() == "" {
		t.Error("error message should mention LINEAJE_USERNAME and LINEAJE_PASSWORD")
	}
}

func TestGetToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(LoginResponse{AccessToken: "test-token-123"})
	}))
	defer server.Close()

	cfg := Config{
		LoginURL:           server.URL,
		Username:           "user",
		Password:           "pass",
		MaxRetries:         1,
		WaitBetweenRetries: 0,
	}
	token, err := GetToken(cfg)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if token != "test-token-123" {
		t.Errorf("token = %q, want test-token-123", token)
	}
}

func TestGetToken_MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(LoginResponse{AccessToken: ""})
	}))
	defer server.Close()

	cfg := Config{
		LoginURL:   server.URL,
		Username:   "user",
		Password:   "pass",
		MaxRetries: 1,
	}
	_, err := GetToken(cfg)
	if err == nil {
		t.Fatal("expected error when accessToken is missing")
	}
}

func TestGetToken_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := Config{
		LoginURL:   server.URL,
		Username:   "user",
		Password:   "pass",
		MaxRetries: 1,
	}
	_, err := GetToken(cfg)
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestDefaultConfig_EnvVars(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LoginURL != defaultLoginURL {
		t.Errorf("LoginURL = %q, want %q", cfg.LoginURL, defaultLoginURL)
	}
	if cfg.MaxRetries != maxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, maxRetries)
	}
	// Username/Password come from env; we don't set them in test to avoid side effects
}

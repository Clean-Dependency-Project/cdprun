package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCall_MissingToken(t *testing.T) {
	cfg := Config{
		BaseURL:    "http://localhost:9999",
		Components: []string{"pkg:maven/org/artifact@1.0"},
		PollDelay:  time.Millisecond,
		MaxPolls:   1,
	}
	_, err := Call(cfg)
	if err == nil {
		t.Fatal("expected error when token is missing")
	}
}

func TestCall_EmptyComponents(t *testing.T) {
	cfg := Config{
		BaseURL:    "http://localhost:9999",
		Token:      "test-token",
		Components: nil,
	}
	_, err := Call(cfg)
	if err == nil {
		t.Fatal("expected error when both sbom_id and components are empty")
	}
}

func TestCall_SuccessWithSBOMIDOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in map[string]interface{}
		_ = json.Unmarshal(body, &in)
		if in["sbom_id"] != "sbom-123" {
			t.Fatalf("sbom_id = %v", in["sbom_id"])
		}
		if _, ok := in["metadata"]; ok {
			t.Fatalf("did not expect metadata in request body: %v", in)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"answer": "ok"})
	}))
	defer server.Close()

	cfg := Config{
		BaseURL:   server.URL,
		Token:     "test-token",
		Query:     "recommend",
		SBOMID:    "sbom-123",
		PollDelay: time.Millisecond,
		MaxPolls:  1,
		Timeout:   5 * time.Second,
	}
	if _, err := Call(cfg); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestCall_InitialGUIDIncludesGuidAndSBOMID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in map[string]interface{}
		_ = json.Unmarshal(body, &in)
		if in["guid"] != "guid-123" {
			t.Fatalf("guid = %v", in["guid"])
		}
		if in["sbom_id"] != "sbom-123" {
			t.Fatalf("sbom_id = %v", in["sbom_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"answer": "ok"})
	}))
	defer server.Close()

	cfg := Config{
		BaseURL:    server.URL,
		Token:      "test-token",
		Query:      "recommend",
		SBOMID:     "sbom-123",
		InitialGUID:"guid-123",
		PollDelay:  time.Millisecond,
		MaxPolls:   1,
		Timeout:    5 * time.Second,
	}
	if _, err := Call(cfg); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestCall_SuccessFirstResponseHasAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or wrong Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"answer": "Recommendation: use version 2.0",
			"guid":   "",
		})
	}))
	defer server.Close()

	cfg := Config{
		BaseURL:    server.URL,
		Token:      "test-token",
		Query:      "recommend",
		Components: []string{"pkg:maven/org/a@1.0"},
		PollDelay:  time.Millisecond,
		MaxPolls:   1,
		Timeout:    5 * time.Second,
	}
	body, err := Call(cfg)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["answer"] == nil {
		t.Error("expected answer in response")
	}
}

func TestCall_SuccessAfterPoll(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"guid":   "test-guid-123",
				"answer": nil,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"guid":   "test-guid-123",
			"answer": "Final recommendation",
		})
	}))
	defer server.Close()

	cfg := Config{
		BaseURL:    server.URL,
		Token:      "test-token",
		Query:      "recommend",
		Components: []string{"pkg:maven/org/a@1.0"},
		PollDelay:  10 * time.Millisecond,
		MaxPolls:   3,
		Timeout:    5 * time.Second,
	}
	body, err := Call(cfg)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 requests, got %d", callCount)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["answer"] != "Final recommendation" {
		t.Errorf("answer = %v", out["answer"])
	}
}

func TestCall_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := Config{
		BaseURL:    server.URL,
		Token:      "test-token",
		Components: []string{"pkg:maven/org/a@1.0"},
		PollDelay:  time.Millisecond,
		MaxPolls:   1,
		Timeout:    5 * time.Second,
	}
	_, err := Call(cfg)
	if err == nil {
		t.Fatal("expected error on 400")
	}
}

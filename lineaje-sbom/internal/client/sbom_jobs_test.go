package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadSBOM_MissingToken(t *testing.T) {
	cfg := DefaultUploadConfig()
	cfg.Token = ""
	cfg.SBOMPath = "some.json"
	if _, err := UploadSBOM(cfg); err == nil {
		t.Fatal("expected error when token is missing")
	}
}

func TestUploadSBOM_SuccessTopLevelSBOMID(t *testing.T) {
	temp := writeTempSBOMFile(t)
	var gotAuth string
	var gotQuery string
	var gotSBOMJob string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		gotSBOMJob = r.FormValue("sbomJob")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"sbom_id": "abc-123"})
	}))
	defer server.Close()

	cfg := DefaultUploadConfig()
	cfg.BaseURL = server.URL
	cfg.Token = "token-123"
	cfg.SBOMPath = temp
	cfg.Timeout = 2 * time.Second
	resp, err := UploadSBOM(cfg)
	if err != nil {
		t.Fatalf("UploadSBOM: %v", err)
	}
	if resp.SBOMID != "abc-123" {
		t.Fatalf("SBOMID = %q", resp.SBOMID)
	}
	if gotAuth != "Bearer token-123" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !strings.Contains(gotQuery, "sbom_format=CycloneDX") {
		t.Fatalf("query missing sbom_format: %q", gotQuery)
	}
	if !strings.Contains(gotSBOMJob, `"project_version":"unknown"`) {
		t.Fatalf("sbomJob missing project_version: %q", gotSBOMJob)
	}
}

func TestUploadSBOM_SuccessNestedSBOMID(t *testing.T) {
	temp := writeTempSBOMFile(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"sbom_id": "nested-456",
			},
		})
	}))
	defer server.Close()

	cfg := DefaultUploadConfig()
	cfg.BaseURL = server.URL
	cfg.Token = "token-123"
	cfg.SBOMPath = temp
	resp, err := UploadSBOM(cfg)
	if err != nil {
		t.Fatalf("UploadSBOM: %v", err)
	}
	if resp.SBOMID != "nested-456" {
		t.Fatalf("SBOMID = %q", resp.SBOMID)
	}
}

func TestUploadSBOM_SuccessResultSBOMID(t *testing.T) {
	temp := writeTempSBOMFile(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"sbom_id": "result-789",
			},
		})
	}))
	defer server.Close()

	cfg := DefaultUploadConfig()
	cfg.BaseURL = server.URL
	cfg.Token = "token-123"
	cfg.SBOMPath = temp
	resp, err := UploadSBOM(cfg)
	if err != nil {
		t.Fatalf("UploadSBOM: %v", err)
	}
	if resp.SBOMID != "result-789" {
		t.Fatalf("SBOMID = %q", resp.SBOMID)
	}
}

func TestUploadSBOM_NonOKStatus(t *testing.T) {
	temp := writeTempSBOMFile(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer server.Close()

	cfg := DefaultUploadConfig()
	cfg.BaseURL = server.URL
	cfg.Token = "token-123"
	cfg.SBOMPath = temp
	if _, err := UploadSBOM(cfg); err == nil {
		t.Fatal("expected error on non-200 response")
	}
}

func TestUploadSBOM_EmptyBodyObjectStillReturnsRaw(t *testing.T) {
	temp := writeTempSBOMFile(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := DefaultUploadConfig()
	cfg.BaseURL = server.URL
	cfg.Token = "token-123"
	cfg.SBOMPath = temp
	resp, err := UploadSBOM(cfg)
	if err != nil {
		t.Fatalf("UploadSBOM: %v", err)
	}
	if resp.SBOMID != "" {
		t.Fatalf("SBOMID = %q, want empty", resp.SBOMID)
	}
	if resp.Raw == nil {
		t.Fatal("expected raw response map")
	}
}

func writeTempSBOMFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.cdx.json")
	if err := os.WriteFile(path, []byte(`{"bomFormat":"CycloneDX","components":[]}`), 0o644); err != nil {
		t.Fatalf("write temp sbom: %v", err)
	}
	return path
}

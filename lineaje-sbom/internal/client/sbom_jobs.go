// Package client provides a Lineaje SBOM upload client.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const defaultSBOMUploadURL = "https://scim-service.v2.prod.veedna.com/scim/api/v1/sbom_jobs/upload"

// UploadConfig contains SBOM upload configuration.
type UploadConfig struct {
	BaseURL        string
	Token          string
	SBOMPath       string
	SBOMFormat     string
	ProjectName    string
	ProjectVersion string
	CrawlerType    string
	SBOMType       string
	Timeout        time.Duration
	Logger         *slog.Logger
}

// UploadResponse is the normalized upload result.
type UploadResponse struct {
	SBOMID  string
	Raw     map[string]interface{}
	Payload json.RawMessage
}

// DefaultUploadConfig returns a production-safe upload config.
func DefaultUploadConfig() UploadConfig {
	return UploadConfig{
		BaseURL:        defaultSBOMUploadURL,
		SBOMFormat:     "CycloneDX",
		ProjectName:    "lineaje-sbom",
		ProjectVersion: "unknown",
		CrawlerType:    "veeUI",
		SBOMType:       "pkg",
		Timeout:        60 * time.Second,
	}
}

// UploadSBOM uploads a CycloneDX SBOM and returns parsed upload metadata.
func UploadSBOM(cfg UploadConfig) (UploadResponse, error) {
	if cfg.Token == "" {
		return UploadResponse{}, fmt.Errorf("token is required")
	}
	if cfg.SBOMPath == "" {
		return UploadResponse{}, fmt.Errorf("sbom path is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultSBOMUploadURL
	}
	if cfg.SBOMFormat == "" {
		cfg.SBOMFormat = "CYCLONEDX"
	}
	if cfg.ProjectName == "" {
		cfg.ProjectName = "lineaje-sbom"
	}
	if cfg.ProjectVersion == "" {
		cfg.ProjectVersion = "unknown"
	}
	if cfg.CrawlerType == "" {
		cfg.CrawlerType = "veeUI"
	}
	if cfg.SBOMType == "" {
		cfg.SBOMType = "pkg"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	endpoint, err := uploadURL(cfg.BaseURL, cfg.SBOMFormat)
	if err != nil {
		return UploadResponse{}, fmt.Errorf("build upload url: %w", err)
	}
	body, contentType, err := uploadBody(cfg)
	if err != nil {
		return UploadResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return UploadResponse{}, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "*/*")

	if cfg.Logger != nil && cfg.Logger.Enabled(context.Background(), slog.LevelDebug) {
		cfg.Logger.Debug("sbom_upload_request", "url", endpoint, "sbom_path", cfg.SBOMPath, "sbom_format", cfg.SBOMFormat, "project_name", cfg.ProjectName, "project_version", cfg.ProjectVersion)
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return UploadResponse{}, fmt.Errorf("upload sbom request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return UploadResponse{}, fmt.Errorf("read upload response: %w", err)
	}
	if cfg.Logger != nil && cfg.Logger.Enabled(context.Background(), slog.LevelDebug) {
		cfg.Logger.Debug("sbom_upload_response", "status", resp.StatusCode, "body", string(respBytes))
	}
	if resp.StatusCode != http.StatusOK {
		return UploadResponse{}, fmt.Errorf("upload returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return UploadResponse{}, fmt.Errorf("parse upload response: %w", err)
	}
	sbomID := extractSBOMID(raw)
	return UploadResponse{
		SBOMID:  sbomID,
		Raw:     raw,
		Payload: json.RawMessage(append([]byte(nil), respBytes...)),
	}, nil
}

func uploadURL(baseURL, format string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("sbom_format", format)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func uploadBody(cfg UploadConfig) (*bytes.Buffer, string, error) {
	payload := map[string]string{
		"crawler_type":    cfg.CrawlerType,
		"sbom_type":       cfg.SBOMType,
		"name":            cfg.ProjectName,
		"project_version": cfg.ProjectVersion,
	}
	jobJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal sbomJob payload: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	jobHeader := make(textproto.MIMEHeader)
	jobHeader.Set("Content-Disposition", `form-data; name="sbomJob"`)
	jobHeader.Set("Content-Type", "application/json")
	jobPart, err := writer.CreatePart(jobHeader)
	if err != nil {
		return nil, "", fmt.Errorf("create sbomJob part: %w", err)
	}
	if _, err := jobPart.Write(jobJSON); err != nil {
		return nil, "", fmt.Errorf("write sbomJob part: %w", err)
	}

	filePart, err := writer.CreateFormFile("file", filepath.Base(cfg.SBOMPath))
	if err != nil {
		return nil, "", fmt.Errorf("create file part: %w", err)
	}
	f, err := os.Open(cfg.SBOMPath)
	if err != nil {
		return nil, "", fmt.Errorf("open sbom file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(filePart, f); err != nil {
		return nil, "", fmt.Errorf("write sbom file part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}

func extractSBOMID(raw map[string]interface{}) string {
	if v, ok := raw["sbom_id"].(string); ok && v != "" {
		return v
	}
	data, ok := raw["data"].(map[string]interface{})
	if ok {
		if v, ok := data["sbom_id"].(string); ok && v != "" {
			return v
		}
	}
	result, ok := raw["result"].(map[string]interface{})
	if !ok {
		return ""
	}
	if v, ok := result["sbom_id"].(string); ok && v != "" {
		return v
	}
	return ""
}

// Package session provides persistence for explain-request sessions (GUID, query,
// components) so they can be resumed later. No token is ever stored.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Session holds the data needed to resume an explain request.
// It is persisted as JSON; no token or credentials are stored.
// API response content (e.g. message) is not stored.
type Session struct {
	SessionKey    string          `json:"session_key,omitempty"`
	GUID          string          `json:"guid"`
	SBOMID        string          `json:"sbom_id"`
	Query         string          `json:"query"`
	Components    []string        `json:"components"`
	CreatedAt     string          `json:"created_at,omitempty"`
	Message       string          `json:"message,omitempty"` // not set on save; only for backward compatibility when loading
	UploadPayload json.RawMessage `json:"upload_payload"`
}

// Load reads a session from the file at path (e.g. ./sessions/<guid>.json).
func Load(path string) (Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, err
	}
	return s, nil
}

// Save writes the session to dir/<guid>.json, creating dir if needed.
// Multiple sessions can coexist in the same directory.
func Save(dir string, s Session) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	fileKey := s.SessionKey
	if fileKey == "" {
		fileKey = s.GUID
	}
	if fileKey == "" {
		fileKey = s.SBOMID
	}
	if fileKey == "" {
		return os.ErrInvalid
	}
	path := filepath.Join(dir, fileKey+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// RemoveFile deletes only the session file for the given guid in dir
// (i.e. dir/<guid>.json). The directory is not removed.
func RemoveFile(dir, guid string) error {
	path := filepath.Join(dir, guid+".json")
	return os.Remove(path)
}

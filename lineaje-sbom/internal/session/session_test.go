package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	s := Session{
		GUID:       "50eb3fa8-b3f7-41cf-8427-c3cb4018d38a",
		Query:      "recommend gos plan",
		Components: []string{"pkg:maven/org.springframework/spring-core@6.2.3"},
		CreatedAt:  "2025-02-25T20:00:00Z",
		Message:    "Request is still processing.",
	}

	if err := Save(dir, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(dir, s.GUID+".json")
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.GUID != s.GUID || loaded.Query != s.Query || loaded.Message != s.Message {
		t.Errorf("loaded session mismatch: got %+v", loaded)
	}
	if len(loaded.Components) != len(s.Components) || (len(s.Components) > 0 && loaded.Components[0] != s.Components[0]) {
		t.Errorf("loaded.Components = %v, want %v", loaded.Components, s.Components)
	}
}

func TestRemoveFile(t *testing.T) {
	dir := t.TempDir()
	guid := "test-guid-123"

	if err := Save(dir, Session{GUID: guid, Query: "q", Components: nil}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(dir, guid+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should exist: %v", err)
	}

	if err := RemoveFile(dir, guid); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("session file should be removed: %v", err)
	}

	// Dir should still exist
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory should remain: %v", err)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	s := Session{GUID: "create-dir-guid", Query: "q", Components: nil}

	if err := Save(dir, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(dir, s.GUID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should exist after MkdirAll: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Error("Load missing file should return error")
	}
}

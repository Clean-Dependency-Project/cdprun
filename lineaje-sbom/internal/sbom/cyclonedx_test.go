package sbom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPURLsFromCycloneDX_EmptyComponents(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bom.json")
	if err := os.WriteFile(f, []byte(`{"components":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	purls, err := PURLsFromCycloneDX(f)
	if err != nil {
		t.Fatalf("PURLsFromCycloneDX: %v", err)
	}
	if len(purls) != 0 {
		t.Errorf("len(purls) = %d, want 0", len(purls))
	}
}

func TestPURLsFromCycloneDX_WithPURLs(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bom.json")
	json := `{"components":[
		{"purl":"pkg:maven/org.springframework/spring-core@6.2.3"},
		{"purl":"pkg:maven/org.springframework/spring-web@6.2.3"},
		{"purl":""},
		{"purl":"pkg:maven/org.springframework/spring-core@6.2.3"}
	]}`
	if err := os.WriteFile(f, []byte(json), 0644); err != nil {
		t.Fatal(err)
	}
	purls, err := PURLsFromCycloneDX(f)
	if err != nil {
		t.Fatalf("PURLsFromCycloneDX: %v", err)
	}
	if len(purls) != 2 {
		t.Errorf("len(purls) = %d, want 2 (deduplicated)", len(purls))
	}
	want := map[string]bool{
		"pkg:maven/org.springframework/spring-core@6.2.3": true,
		"pkg:maven/org.springframework/spring-web@6.2.3":  true,
	}
	for _, p := range purls {
		if !want[p] {
			t.Errorf("unexpected purl %q", p)
		}
	}
}

func TestPURLsFromCycloneDX_NoFile(t *testing.T) {
	_, err := PURLsFromCycloneDX(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPURLsFromCycloneDX_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(f, []byte(`{components`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := PURLsFromCycloneDX(f)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

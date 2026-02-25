package packaging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequireSHA256Match(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sha, err := ComputeFileSHA256(p)
	if err != nil {
		t.Fatalf("ComputeFileSHA256: %v", err)
	}

	if err := RequireSHA256Match(p, sha); err != nil {
		t.Fatalf("RequireSHA256Match expected success, got: %v", err)
	}

	if err := RequireSHA256Match(p, "deadbeef"); err == nil {
		t.Fatalf("RequireSHA256Match expected mismatch error, got nil")
	}
}

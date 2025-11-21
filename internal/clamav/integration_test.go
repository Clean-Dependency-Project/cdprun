package clamav

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegration_DockerScanner_RealDocker(t *testing.T) {
	runner := NewRealCommandRunner()

	// Skip if Docker not available
	if !isDockerAvailable(runner) {
		t.Skip("Docker not available, skipping integration test")
	}

	scanner := NewDockerScanner(runner, "clamav/clamav-debian:latest", nil)

	t.Run("clean file", func(t *testing.T) {
		// Create a clean test file
		tmpDir := t.TempDir()
		cleanFile := filepath.Join(tmpDir, "clean.txt")
		if err := os.WriteFile(cleanFile, []byte("This is a clean file"), 0600); err != nil {
			t.Fatalf("failed to create clean file: %v", err)
		}

		result, err := scanner.Scan(context.Background(), cleanFile)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		if !result.Clean {
			t.Error("expected Clean=true for clean file")
		}

		if len(result.Threats) != 0 {
			t.Errorf("expected no threats, got %v", result.Threats)
		}

		if result.Metadata.EngineVersion == "" || result.Metadata.EngineVersion == "unknown" {
			t.Errorf("expected version info, got %s", result.Metadata.EngineVersion)
		}

		t.Logf("Scan successful: engine=%s, date=%s, duration=%v",
			result.Metadata.EngineVersion,
			result.Metadata.DatabaseDate,
			result.Metadata.ScanDuration)
	})

	t.Run("EICAR test file", func(t *testing.T) {
		// Create EICAR test file
		tmpDir := t.TempDir()
		eicarFile := filepath.Join(tmpDir, "eicar.txt")
		// EICAR standard antivirus test string
		eicarContent := "X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
		if err := os.WriteFile(eicarFile, []byte(eicarContent), 0600); err != nil {
			t.Fatalf("failed to create EICAR file: %v", err)
		}

		result, err := scanner.Scan(context.Background(), eicarFile)
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		if result.Clean {
			t.Error("expected Clean=false for EICAR file")
		}

		if len(result.Threats) == 0 {
			t.Error("expected at least one threat for EICAR file")
		}

		if len(result.Threats) > 0 {
			t.Logf("Detected threat: %s", result.Threats[0])
			// EICAR is typically detected as "Eicar-Signature" or similar
			if result.Threats[0] != "Eicar-Signature" && result.Threats[0] != "Eicar-Test-Signature" {
				t.Logf("Warning: unexpected threat name %s (expected Eicar-Signature)", result.Threats[0])
			}
		}

		t.Logf("EICAR detection successful: threats=%v, engine=%s",
			result.Threats,
			result.Metadata.EngineVersion)
	})
}


package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProofArtifactPath returns a sibling path next to localPath using stem + "." + suffix
// (e.g. /dir/foo.zip + audit.json -> /dir/foo.audit.json).
func ProofArtifactPath(localPath, suffix string) string {
	base := filepath.Base(localPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(filepath.Dir(localPath), stem+"."+suffix)
}

// WriteChecksumAuditRecord writes the standard checksum verification audit JSON next to the artifact.
func WriteChecksumAuditRecord(
	result DownloadResult,
	expectedChecksum, actualChecksum string,
	checksumVerified bool,
	verificationStatus, errorMessage string,
) error {
	fileName := filepath.Base(result.LocalPath)
	fileSize := result.FileSize
	if stat, err := os.Stat(result.LocalPath); err == nil {
		fileSize = stat.Size()
	}

	record := map[string]interface{}{
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"runtime":             result.Runtime,
		"version":             result.Version,
		"platform":            result.Platform.Classifier,
		"file_name":           fileName,
		"file_path":           result.LocalPath,
		"source_url":          result.URL,
		"file_size":           fileSize,
		"checksum_algorithm":  "sha256",
		"checksum_expected":   expectedChecksum,
		"checksum_actual":     actualChecksum,
		"checksum_verified":   checksumVerified,
		"gpg_verified":        false,
		"verification_status": verificationStatus,
		"verification_type":   "checksum-sha256",
	}
	if errorMessage != "" {
		record["error"] = errorMessage
	}

	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal proof record: %w", err)
	}

	auditPath := ProofArtifactPath(result.LocalPath, "audit.json")
	if err := os.WriteFile(auditPath, encoded, 0644); err != nil {
		return fmt.Errorf("write audit file: %w", err)
	}

	return nil
}

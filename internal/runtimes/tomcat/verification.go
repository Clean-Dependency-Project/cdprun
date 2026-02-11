package tomcat

import (
	"context"
	"crypto/sha512"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/gpg"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
)

//go:embed keys/*.asc
var embeddedTomcatKeys embed.FS

// TomcatVerificationStrategy implements verification for Tomcat downloads
type TomcatVerificationStrategy struct {
	config  *config.Runtime
	stdout  *slog.Logger
	stderr  *slog.Logger
	keyRing gpg.KeyRing
}

// NewTomcatVerificationStrategy creates a new verification strategy for Tomcat
func NewTomcatVerificationStrategy(config *config.Runtime, stdout, stderr *slog.Logger) *TomcatVerificationStrategy {
	strategy := &TomcatVerificationStrategy{
		config: config,
		stdout: stdout,
		stderr: stderr,
	}

	// Initialize GPG keyring
	if err := strategy.initializeKeyRing(); err != nil {
		stderr.Error("failed to initialize GPG keyring", "error", err)
	}

	return strategy
}

// initializeKeyRing loads all embedded GPG keys using the standard pattern
func (v *TomcatVerificationStrategy) initializeKeyRing() error {
	v.stdout.Debug("initializing Tomcat GPG keyring")

	// Use standard GPG key loading pattern
	keyRing, err := gpg.LoadKeyRingFromEmbedFS(embeddedTomcatKeys, "keys")
	if err != nil {
		return fmt.Errorf("failed to load embedded Tomcat GPG keys: %w", err)
	}

	v.keyRing = keyRing
	v.stdout.Debug("initialized Tomcat GPG keyring using standard pattern")
	return nil
}

// Verify implements the VerificationStrategy interface
func (v *TomcatVerificationStrategy) Verify(ctx context.Context, result runtime.DownloadResult) error {
	if result.Task == nil || result.Task.FileType != "main" {
		return fmt.Errorf("can only verify main files")
	}

	v.stdout.Debug("verifying Tomcat download",
		"file", result.LocalPath,
		"runtime", result.Runtime,
		"version", result.Version)

	var checksumVerified, gpgVerified bool
	var verificationStatus = "success"
	var errorMsg string

	// Verify SHA512 checksum
	checksumFile := result.LocalPath + ".sha512"
	if err := v.verifySHA512(result.LocalPath, checksumFile); err != nil {
		errorMsg = fmt.Sprintf("SHA512 verification failed: %v", err)
		verificationStatus = "failed"
		checksumVerified = false
		// Create audit file for failed verification
		v.createIndividualAuditFile(result, checksumVerified, gpgVerified, verificationStatus, errorMsg)
		return fmt.Errorf("SHA512 verification failed: %w", err)
	}
	checksumVerified = true

	// Verify GPG signature if available
	signatureFile := result.LocalPath + ".asc"
	if _, err := os.Stat(signatureFile); err == nil {
		if err := v.verifyGPGSignature(result.LocalPath, signatureFile); err != nil {
			// GPG verification failure is not fatal but should be logged
			v.stderr.Warn("GPG verification failed", "file", result.LocalPath, "error", err)
			gpgVerified = false
		} else {
			v.stdout.Debug("GPG verification successful", "file", result.LocalPath)
			gpgVerified = true
		}
	} else {
		v.stdout.Debug("no GPG signature file found", "expected", signatureFile)
		gpgVerified = false
	}

	// Create audit file for successful verification
	if err := v.createIndividualAuditFile(result, checksumVerified, gpgVerified, verificationStatus, ""); err != nil {
		v.stderr.Error("failed to create audit file", "file", result.LocalPath, "error", err)
		// Don't fail verification if audit file creation fails
	}

	return nil
}

// GetType returns the verification type
func (v *TomcatVerificationStrategy) GetType() string {
	return "tomcat-sha512-gpg"
}

// RequiresAdditionalFiles indicates that this strategy needs checksum and signature files
func (v *TomcatVerificationStrategy) RequiresAdditionalFiles() bool {
	return true
}

// verifySHA512 verifies the SHA512 checksum of a file
func (v *TomcatVerificationStrategy) verifySHA512(filePath, checksumPath string) error {
	// Read the checksum file
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}

	// Parse the checksum (format: "<hash> *<filename>" or "<hash>  <filename>")
	checksumStr := strings.TrimSpace(string(checksumData))
	parts := strings.Fields(checksumStr)
	if len(parts) < 2 {
		return fmt.Errorf("invalid checksum file format")
	}
	expectedHash := strings.ToLower(parts[0])

	// Calculate actual hash
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha512.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))

	// Compare hashes
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	v.stdout.Debug("SHA512 checksum verified",
		"file", filepath.Base(filePath),
		"hash", actualHash[:16]+"...")

	return nil
}

// verifyGPGSignature verifies the GPG signature of a file using the standard GPG utilities
func (v *TomcatVerificationStrategy) verifyGPGSignature(filePath, signaturePath string) error {
	if v.keyRing == nil {
		return fmt.Errorf("GPG keyring not initialized")
	}

	// Use standard GPG verification
	err := gpg.VerifyDetachedSignature(v.keyRing, filePath, signaturePath)
	if err != nil {
		return fmt.Errorf("GPG signature verification failed: %w", err)
	}

	return nil
}

// createIndividualAuditFile creates an individual audit file for a downloaded file
func (v *TomcatVerificationStrategy) createIndividualAuditFile(result runtime.DownloadResult, checksumVerified, gpgVerified bool, verificationStatus, errorMsg string) error {
	// Create audit file path (filename.ext.audit.json)
	auditFilePath := result.LocalPath + ".audit.json"

	// Extract filename for cleaner data
	filename := filepath.Base(result.LocalPath)

	// Get checksum file paths
	checksumFile := result.LocalPath + ".sha512"
	signatureFile := result.LocalPath + ".asc"

	// Try to read the actual checksum value
	var expectedChecksum, calculatedChecksum string
	if checksumData, err := os.ReadFile(checksumFile); err == nil {
		parts := strings.Fields(string(checksumData))
		if len(parts) > 0 {
			expectedChecksum = parts[0]
		}
	}

	// Build audit record
	auditRecord := map[string]interface{}{
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
		"file_name":            filename,
		"file_path":            result.LocalPath,
		"file_size":            result.FileSize,
		"runtime":              result.Runtime,
		"version":              result.Version,
		"platform":             fmt.Sprintf("%s-%s", result.Platform.OS, result.Platform.Arch),
		"download_url":         result.URL,
		"download_duration_ms": result.Duration.Milliseconds(),

		// Verification details
		"checksum_verified":   checksumVerified,
		"checksum_algorithm":  "sha512",
		"checksum_file":       filepath.Base(checksumFile),
		"gpg_verified":        gpgVerified,
		"gpg_signature_file":  filepath.Base(signatureFile),
		"verification_status": verificationStatus,

		// Checksums
		"expected_checksum":   expectedChecksum,
		"calculated_checksum": calculatedChecksum,

		// Validation methods
		"checksum_validation_method": "individual_sha512_file",
		"gpg_validation_method":      "detached_signature_verification",
		"gpg_keyring_source":         "embedded_tomcat_keys",

		// Audit metadata
		"audit_version": "1.0",
		"audit_tool":    "tomcat-verification-strategy",
	}

	// Add error message if verification failed
	if errorMsg != "" {
		auditRecord["error"] = errorMsg
		auditRecord["verification_failed_reason"] = errorMsg
	}

	// Add successful verification details
	if checksumVerified {
		auditRecord["checksum_match"] = true
		// If we have the checksum, calculate it from file
		if expectedChecksum != "" {
			if file, err := os.Open(result.LocalPath); err == nil {
				defer file.Close()
				hasher := sha512.New()
				if _, err := io.Copy(hasher, file); err == nil {
					calculatedChecksum = hex.EncodeToString(hasher.Sum(nil))
					auditRecord["calculated_checksum"] = calculatedChecksum
					auditRecord["checksum_match"] = (calculatedChecksum == expectedChecksum)
				}
			}
		}
	}

	// Marshal to JSON with pretty printing
	auditJSON, err := json.MarshalIndent(auditRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal audit record: %w", err)
	}

	// Write audit file
	if err := os.WriteFile(auditFilePath, auditJSON, 0644); err != nil {
		return fmt.Errorf("failed to write audit file: %w", err)
	}

	v.stdout.Debug("created individual audit file",
		"file", result.LocalPath,
		"audit_file", auditFilePath,
		"checksum_verified", checksumVerified,
		"gpg_verified", gpgVerified,
		"status", verificationStatus)

	return nil
}

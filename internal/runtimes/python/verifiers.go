package python

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/config"
	"github.com/clean-dependency-project/cdprun/internal/gpg"
	"github.com/clean-dependency-project/cdprun/internal/runtime"
	"github.com/clean-dependency-project/cdprun/internal/sigstore"
)

//go:embed keys
var embeddedKeysFS embed.FS

// NoOpVerifier is a no-operation verifier that always succeeds.
// Used when GPG key loading fails as a fallback.
type NoOpVerifier struct {
	runtimeName string
	logger      *slog.Logger
}

// Verify always returns nil (success) for no-op verification.
func (v *NoOpVerifier) Verify(ctx context.Context, result runtime.DownloadResult) error {
	v.logger.Warn("no-op verification - GPG keys not available",
		"runtime", v.runtimeName,
		"file", result.LocalPath)
	return nil
}

// GetType returns the verification strategy type.
func (v *NoOpVerifier) GetType() string {
	return "noop"
}

// RequiresAdditionalFiles returns false since no-op verification doesn't need extra files.
func (v *NoOpVerifier) RequiresAdditionalFiles() bool {
	return false
}

// PythonGPGVerifier implements GPG signature verification for Python downloads
type PythonGPGVerifier struct {
	keyRing gpg.KeyRing
	logger  *slog.Logger
}

// NewPythonGPGVerifier creates a new GPG verifier for Python downloads using embedded keys
func NewPythonGPGVerifier(logger *slog.Logger) runtime.VerificationStrategy {
	if logger == nil {
		logger = slog.Default()
	}

	verifier := &PythonGPGVerifier{
		logger: logger,
	}

	// Load embedded keys using custom logic for numbered files
	keyRing, err := loadPythonEmbeddedKeys(embeddedKeysFS, "keys", logger)
	if err != nil {
		logger.Error("Failed to load Python GPG keys from embedded filesystem", "error", err)
		// Return a no-op verifier if key loading fails
		return &NoOpVerifier{runtimeName: "python", logger: logger}
	}

	verifier.keyRing = keyRing
	logger.Info("Loaded Python GPG verification keys", "source", "embedded")
	return verifier
}

// loadPythonEmbeddedKeys loads GPG keys from embedded filesystem, handling numbered files
func loadPythonEmbeddedKeys(fs embed.FS, keysPath string, logger *slog.Logger) (gpg.KeyRing, error) {
	files, err := fs.ReadDir(keysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded keys directory: %w", err)
	}

	keyRing := gpg.NewRealKeyRing()
	keyCount := 0

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		// Skip .gitkeep files
		if fileName == ".gitkeep" {
			continue
		}

		filePath := filepath.Join(keysPath, fileName)
		keyData, err := fs.ReadFile(filePath)
		if err != nil {
			logger.Warn("Failed to read embedded key file", "file", fileName, "error", err)
			continue
		}

		// Check if this looks like a GPG key file
		keyContent := string(keyData)
		if !strings.Contains(keyContent, "BEGIN PGP PUBLIC KEY BLOCK") {
			logger.Debug("Skipping non-GPG file", "file", fileName)
			continue
		}

		if len(keyData) > 100*1024*1024 { // 100MB max
			logger.Warn("Embedded key file exceeds maximum allowed size", "file", fileName)
			continue
		}

		key, err := gpg.NewRealKey(keyContent)
		if err != nil {
			logger.Warn("Failed to parse embedded key", "file", fileName, "error", err)
			continue
		}

		// Validate key (check if revoked, etc.)
		if key.IsRevoked() {
			logger.Warn("Skipping revoked key", "file", fileName, "fingerprint", key.GetFingerprint())
			continue
		}

		if err := keyRing.AddKey(key); err != nil {
			logger.Warn("Failed to add key to keyring", "file", fileName, "error", err)
			continue
		}

		keyCount++
		logger.Debug("Loaded Python GPG key", "file", fileName, "fingerprint", key.GetFingerprint())
	}

	if keyCount == 0 {
		return nil, fmt.Errorf("no valid GPG keys found in embedded directory")
	}

	logger.Info("Successfully loaded Python GPG keys", "count", keyCount)
	return keyRing, nil
}

// Verify implements the VerificationStrategy interface
func (v *PythonGPGVerifier) Verify(ctx context.Context, result runtime.DownloadResult) error {
	if v.keyRing == nil {
		v.logger.Error("No GPG keyring available for verification",
			"file", result.LocalPath,
			"runtime", result.Runtime)
		return fmt.Errorf("no GPG keyring available for verification")
	}

	if result.Error != nil {
		v.logger.Error("Cannot verify failed download",
			"file", result.LocalPath,
			"download_error", result.Error,
			"runtime", result.Runtime)
		return fmt.Errorf("cannot verify failed download: %w", result.Error)
	}

	v.logger.Info("Starting Python GPG verification",
		"file", result.LocalPath,
		"url", result.URL,
		"runtime", result.Runtime,
		"version", result.Version,
		"file_size", result.FileSize)

	// Determine file type based on filename
	fileName := filepath.Base(result.LocalPath)

	// For signature files (.asc), these are detached signatures in Python's case
	// We should skip verification of signature files themselves
	if strings.HasSuffix(fileName, ".asc") {
		v.logger.Debug("Detected signature file, skipping direct verification",
			"file", result.LocalPath,
			"runtime", result.Runtime,
			"note", "Signature files are used to verify main files")
		return nil // Don't verify signature files directly
	}

	// For main files, look for corresponding signature files
	v.logger.Debug("Detected main file, looking for signature file",
		"file", result.LocalPath,
		"runtime", result.Runtime)
	return v.verifyMainFile(result)
}

// GetType returns the verification strategy type
func (v *PythonGPGVerifier) GetType() string {
	return "python-gpg"
}

// RequiresAdditionalFiles returns true since we need signature files
func (v *PythonGPGVerifier) RequiresAdditionalFiles() bool {
	return true
}

// verifyMainFile verifies a main download file by looking for its signature
func (v *PythonGPGVerifier) verifyMainFile(result runtime.DownloadResult) error {
	// Look for corresponding .asc signature file
	ascFilePath := result.LocalPath + ".asc"

	v.logger.Debug("Looking for signature file",
		"main_file", result.LocalPath,
		"signature_file", ascFilePath)

	// Check if signature file exists
	var gpgVerified bool
	var verificationStatus string

	if _, err := os.Stat(ascFilePath); os.IsNotExist(err) {
		v.logger.Warn("Signature file not found for main file, skipping GPG verification",
			"main_file", result.LocalPath,
			"signature_file", ascFilePath)

		// No signature file, so no GPG verification attempted
		gpgVerified = false
		verificationStatus = "signature_file_missing"

		// Create individual audit file for this download
		if auditErr := v.createIndividualAuditFile(result, gpgVerified, verificationStatus); auditErr != nil {
			v.logger.Error("Failed to create individual audit file",
				"main_file", result.LocalPath,
				"error", auditErr)
		}

		return nil // Optional verification - don't fail if signature file is missing
	}

	v.logger.Debug("Found signature file, performing detached signature verification",
		"main_file", result.LocalPath,
		"signature_file", ascFilePath)

	// Verify detached signature
	err := gpg.VerifyDetachedSignature(v.keyRing, result.LocalPath, ascFilePath)
	if err != nil {
		v.logger.Error("GPG verification failed for main file",
			"main_file", result.LocalPath,
			"signature_file", ascFilePath,
			"error", err)

		// GPG verification failed
		gpgVerified = false
		verificationStatus = "gpg_verification_failed"

		// Create individual audit file for this download
		if auditErr := v.createIndividualAuditFile(result, gpgVerified, verificationStatus); auditErr != nil {
			v.logger.Error("Failed to create individual audit file",
				"main_file", result.LocalPath,
				"error", auditErr)
		}

		return fmt.Errorf("GPG verification failed for %s: %w", result.LocalPath, err)
	}

	v.logger.Info("GPG verification successful for main file",
		"main_file", result.LocalPath,
		"signature_file", ascFilePath)

	// GPG verification succeeded
	gpgVerified = true
	verificationStatus = "success"

	// Create individual audit file for this download
	if auditErr := v.createIndividualAuditFile(result, gpgVerified, verificationStatus); auditErr != nil {
		v.logger.Error("Failed to create individual audit file",
			"main_file", result.LocalPath,
			"error", auditErr)
	}

	return nil
}

// createIndividualAuditFile creates an individual audit file for a downloaded Python file
func (v *PythonGPGVerifier) createIndividualAuditFile(result runtime.DownloadResult, gpgVerified bool, verificationStatus string) error {
	// Reconstruct signature URL (educated guess based on main file URL)
	// NOTE: Actual URL comes from Python download sources but we only have main file URL
	var signatureFileURL string
	urlIsReconstructed := true

	if result.URL != "" {
		signatureFileURL = result.URL + ".asc"
	}

	// Create audit record
	auditRecord := map[string]interface{}{
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"file_path":           result.LocalPath,
		"file_url":            result.URL,
		"runtime":             "python",
		"checksum_verified":   false, // Python doesn't use checksum verification
		"gpg_verified":        gpgVerified,
		"verification_status": verificationStatus,
	}

	// Add GPG-specific details if signature file exists
	signatureFile := result.LocalPath + ".asc"
	if _, err := os.Stat(signatureFile); err == nil {
		auditRecord["gpg_validation_method"] = "detached_signature_verification"
		auditRecord["gpg_keyring_source"] = "embedded_python_keys"
		auditRecord["signature_file"] = signatureFile
		auditRecord["signature_file_url"] = signatureFileURL
		auditRecord["signature_url_reconstructed"] = urlIsReconstructed
	}

	// Get file info for additional metadata
	if fileInfo, err := os.Stat(result.LocalPath); err == nil {
		auditRecord["file_size"] = fileInfo.Size()
		auditRecord["file_modified"] = fileInfo.ModTime().UTC().Format(time.RFC3339)
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(auditRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal audit record to JSON: %w", err)
	}

	// Create audit file path (same as main file but with .audit.json extension)
	auditFilePath := result.LocalPath + ".audit.json"

	// Write audit file
	if err := os.WriteFile(auditFilePath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write audit file %s: %w", auditFilePath, err)
	}

	v.logger.Info("Created individual audit file for Python download",
		"main_file", result.LocalPath,
		"audit_file", auditFilePath,
		"gpg_verified", gpgVerified,
		"verification_status", verificationStatus,
		"file_url", result.URL)

	return nil
}

// PythonSigstoreVerifier verifies upstream Sigstore bundles published by
// python.org against a pinned identity resolved from the runtime config. It is
// verify-only: cdprun never signs artifacts. Verification is skipped (ClamAV
// still runs) when the bundle sidecar is absent or when no identity row matches
// the version (an old release Sigstore intentionally ignores). Verification
// fails only when a configured version's bundle is present but does not match.
type PythonSigstoreVerifier struct {
	config *config.Runtime
	logger *slog.Logger
}

// NewPythonSigstoreVerifier creates a Sigstore verifier bound to the runtime
// configuration, which supplies the bundle suffix and version-keyed identities.
func NewPythonSigstoreVerifier(cfg *config.Runtime, logger *slog.Logger) runtime.VerificationStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &PythonSigstoreVerifier{config: cfg, logger: logger}
}

// GetType returns the verification strategy type.
func (v *PythonSigstoreVerifier) GetType() string {
	return "python-sigstore"
}

// RequiresAdditionalFiles returns true since the .sigstore bundle sidecar must
// be downloaded alongside the main artifact.
func (v *PythonSigstoreVerifier) RequiresAdditionalFiles() bool {
	return true
}

// Verify implements the VerificationStrategy interface.
func (v *PythonSigstoreVerifier) Verify(ctx context.Context, result runtime.DownloadResult) error {
	if result.Error != nil {
		v.logger.Error("Cannot verify failed download",
			"file", result.LocalPath,
			"download_error", result.Error,
			"runtime", result.Runtime)
		return fmt.Errorf("cannot verify failed download: %w", result.Error)
	}

	fileName := filepath.Base(result.LocalPath)
	bundleSuffix := v.config.Verification.Methods.Sigstore.BundleSuffixOrDefault()

	// Skip verification of the bundle sidecar itself.
	if strings.HasSuffix(fileName, bundleSuffix) {
		v.logger.Debug("Detected sigstore bundle file, skipping direct verification",
			"file", result.LocalPath)
		return nil
	}

	return v.verifyMainFile(result, bundleSuffix)
}

// verifyMainFile verifies a main download file against its .sigstore bundle.
// Old releases (no matching identity row in config) are skipped rather than
// failed, so Sigstore only enforces the latest configured release lines.
func (v *PythonSigstoreVerifier) verifyMainFile(result runtime.DownloadResult, bundleSuffix string) error {
	bundlePath := result.LocalPath + bundleSuffix

	entry, err := v.config.ResolveSigstoreIdentityEntry(result.Version)
	if err != nil {
		// Unparseable version is a real error, not an old-release skip.
		v.logger.Error("Cannot resolve sigstore identity for version",
			"main_file", result.LocalPath,
			"version", result.Version,
			"error", err)
		if auditErr := v.writeAudit(result, false, "version_unparseable", "", "", bundlePath); auditErr != nil {
			v.logger.Error("Failed to create audit file",
				"main_file", result.LocalPath, "error", auditErr)
		}
		return fmt.Errorf("sigstore identity not resolvable for python %s: %w", result.Version, err)
	}
	if entry == nil {
		// No identity row for this version: an old release Sigstore ignores.
		v.logger.Info("No sigstore identity configured for version, skipping verification",
			"main_file", result.LocalPath,
			"version", result.Version)
		if auditErr := v.writeAudit(result, false, "identity_not_configured", "", "", bundlePath); auditErr != nil {
			v.logger.Error("Failed to create audit file",
				"main_file", result.LocalPath, "error", auditErr)
		}
		return nil
	}

	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		v.logger.Warn("Sigstore bundle not found for main file, skipping verification",
			"main_file", result.LocalPath,
			"bundle_file", bundlePath)
		// Bundle is optional; skip crypto verification (ClamAV still runs).
		if auditErr := v.writeAudit(result, false, "bundle_missing", entry.CertIdentity, entry.OIDCIssuer, bundlePath); auditErr != nil {
			v.logger.Error("Failed to create audit file",
				"main_file", result.LocalPath, "error", auditErr)
		}
		return nil
	}

	certIdentity, oidcIssuer := entry.CertIdentity, entry.OIDCIssuer
	v.logger.Info("Starting Python Sigstore verification",
		"file", result.LocalPath,
		"bundle_file", bundlePath,
		"version", result.Version,
		"cert_identity", certIdentity,
		"oidc_issuer", oidcIssuer)

	if err := sigstore.VerifyBundle(result.LocalPath, bundlePath, oidcIssuer, certIdentity); err != nil {
		v.logger.Error("Sigstore verification failed for main file",
			"main_file", result.LocalPath,
			"bundle_file", bundlePath,
			"error", err)
		if auditErr := v.writeAudit(result, false, "sigstore_verification_failed", certIdentity, oidcIssuer, bundlePath); auditErr != nil {
			v.logger.Error("Failed to create audit file",
				"main_file", result.LocalPath, "error", auditErr)
		}
		return fmt.Errorf("sigstore verification failed for %s: %w", result.LocalPath, err)
	}

	v.logger.Info("Sigstore verification successful for main file",
		"main_file", result.LocalPath,
		"bundle_file", bundlePath)

	if auditErr := v.writeAudit(result, true, "success", certIdentity, oidcIssuer, bundlePath); auditErr != nil {
		v.logger.Error("Failed to create audit file",
			"main_file", result.LocalPath, "error", auditErr)
	}
	return nil
}

// writeAudit writes a per-artifact .audit.json recording the Sigstore
// verification outcome.
func (v *PythonSigstoreVerifier) writeAudit(result runtime.DownloadResult, verified bool, status, certIdentity, oidcIssuer, bundlePath string) error {
	auditRecord := map[string]interface{}{
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"file_path":           result.LocalPath,
		"file_url":            result.URL,
		"runtime":             "python",
		"checksum_verified":   false,
		"sigstore_verified":   verified,
		"verification_status": status,
		"verification_type":   "sigstore",
		"bundle_file":         bundlePath,
	}
	if certIdentity != "" {
		auditRecord["cert_identity"] = certIdentity
	}
	if oidcIssuer != "" {
		auditRecord["oidc_issuer"] = oidcIssuer
	}

	if fileInfo, err := os.Stat(result.LocalPath); err == nil {
		auditRecord["file_size"] = fileInfo.Size()
		auditRecord["file_modified"] = fileInfo.ModTime().UTC().Format(time.RFC3339)
	}

	jsonData, err := json.MarshalIndent(auditRecord, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal audit record to JSON: %w", err)
	}

	auditFilePath := result.LocalPath + ".audit.json"
	if err := os.WriteFile(auditFilePath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write audit file %s: %w", auditFilePath, err)
	}

	v.logger.Info("Created individual audit file for Python download",
		"main_file", result.LocalPath,
		"audit_file", auditFilePath,
		"sigstore_verified", verified,
		"verification_status", status,
		"file_url", result.URL)
	return nil
}

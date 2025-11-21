package gpg

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
)

//go:embed nodejs-keys/*.asc
var EmbeddedKeysFS embed.FS

const (
	maxKeyFileSize = 1024 * 1024 * 1024 // 1GB max file size for downloads (increased from 100MB to support large JDK files)
	keyFileMode    = 0600               // Required file permissions for key files on Unix systems
)

// EmbeddedFS interface allows for testing with mock filesystem
type EmbeddedFS interface {
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
}

// KeyRing represents a collection of PGP keys for signature verification
type KeyRing interface {
	VerifyDetached(message []byte, signature []byte) error
	AddKey(key Key) error
}

// Key represents a PGP public key
type Key interface {
	IsRevoked() bool
	GetFingerprint() string
}

// ClearTextMessage represents a clear-signed PGP message
type ClearTextMessage struct {
	Data      []byte
	Signature []byte
}

// RealKeyRing implements KeyRing interface using gopenpgp v2 for actual cryptographic verification
type RealKeyRing struct {
	keyRing *crypto.KeyRing
}

// RealKey implements Key interface with actual PGP key data
type RealKey struct {
	pgpKey      *crypto.Key
	fingerprint string
	revoked     bool
}

// NewRealKeyRing creates a new RealKeyRing using gopenpgp v2
func NewRealKeyRing() *RealKeyRing {
	return &RealKeyRing{
		keyRing: nil, // Will be initialized when first key is added
	}
}

// VerifyDetached implements KeyRing interface with real GPG verification
func (rk *RealKeyRing) VerifyDetached(message []byte, signature []byte) error {
	if rk.keyRing == nil {
		return fmt.Errorf("no keys in keyring")
	}

	// Create PlainMessage from the data
	plainMessage := crypto.NewPlainMessage(message)

	// Create PGPSignature from the signature
	pgpSignature, err := crypto.NewPGPSignatureFromArmored(string(signature))
	if err != nil {
		// Try binary format if armored fails
		pgpSignature = crypto.NewPGPSignature(signature)
	}

	// Verify the detached signature
	err = rk.keyRing.VerifyDetached(plainMessage, pgpSignature, crypto.GetUnixTime())
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

// AddKey implements KeyRing interface
func (rk *RealKeyRing) AddKey(key Key) error {
	if key == nil {
		return fmt.Errorf("key cannot be nil")
	}

	realKey, ok := key.(*RealKey)
	if !ok {
		return fmt.Errorf("unsupported key type")
	}

	// Initialize keyring if needed
	if rk.keyRing == nil {
		var err error
		rk.keyRing, err = crypto.NewKeyRing(realKey.pgpKey)
		if err != nil {
			return fmt.Errorf("failed to create keyring: %w", err)
		}
	} else {
		// Add the key to existing keyring
		if err := rk.keyRing.AddKey(realKey.pgpKey); err != nil {
			return fmt.Errorf("failed to add key to keyring: %w", err)
		}
	}

	return nil
}

// NewRealKey creates a new RealKey from armored data using gopenpgp v2
func NewRealKey(armoredData string) (*RealKey, error) {
	if armoredData == "" {
		return nil, fmt.Errorf("armored data cannot be empty")
	}

	pgpKey, err := crypto.NewKeyFromArmored(armoredData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PGP key: %w", err)
	}

	fingerprint := pgpKey.GetFingerprint()

	return &RealKey{
		pgpKey:      pgpKey,
		fingerprint: fingerprint,
		revoked:     false, // TODO: Check if key is revoked
	}, nil
}

// IsRevoked implements Key interface
func (rk *RealKey) IsRevoked() bool {
	return rk.revoked
}

// GetFingerprint implements Key interface
func (rk *RealKey) GetFingerprint() string {
	return rk.fingerprint
}

// SimpleKeyRing is a basic implementation of KeyRing for demonstration
type SimpleKeyRing struct {
	keys []Key
}

// SimpleKey is a basic implementation of Key for demonstration
type SimpleKey struct {
	fingerprint string
	revoked     bool
	data        []byte
}

// NewSimpleKeyRing creates a new SimpleKeyRing
func NewSimpleKeyRing() *SimpleKeyRing {
	return &SimpleKeyRing{
		keys: make([]Key, 0),
	}
}

// NewSimpleKey creates a new SimpleKey from armored data
func NewSimpleKey(armoredData string) (*SimpleKey, error) {
	if armoredData == "" {
		return nil, fmt.Errorf("armored data cannot be empty")
	}

	// For demonstration, we'll just store the data and generate a simple fingerprint
	// In a real implementation, this would parse the PGP key
	hash := sha256.Sum256([]byte(armoredData))
	return &SimpleKey{
		fingerprint: fmt.Sprintf("fp_%x", hash[:8]),
		revoked:     false,
		data:        []byte(armoredData),
	}, nil
}

// VerifyDetached implements KeyRing interface
func (kr *SimpleKeyRing) VerifyDetached(message []byte, signature []byte) error {
	if len(kr.keys) == 0 {
		return fmt.Errorf("no keys available for verification")
	}

	// In a real implementation, this would perform actual signature verification
	// For demonstration, we'll just check that message and signature are not empty
	if len(message) == 0 {
		return fmt.Errorf("message cannot be empty")
	}
	if len(signature) == 0 {
		return fmt.Errorf("signature cannot be empty")
	}

	return nil
}

// AddKey implements KeyRing interface
func (kr *SimpleKeyRing) AddKey(key Key) error {
	if key == nil {
		return fmt.Errorf("key cannot be nil")
	}

	kr.keys = append(kr.keys, key)
	return nil
}

// IsRevoked implements Key interface
func (k *SimpleKey) IsRevoked() bool {
	return k.revoked
}

// GetFingerprint implements Key interface
func (k *SimpleKey) GetFingerprint() string {
	return k.fingerprint
}

// VerifyDetachedSignature verifies a detached signature (.sig file) against the given data file
// using the provided KeyRing.
func VerifyDetachedSignature(keyRing KeyRing, dataFilePath string, sigFilePath string) error {
	if keyRing == nil {
		return fmt.Errorf("keyring cannot be nil")
	}

	// Get file info before reading to check size
	dataFileInfo, err := os.Stat(dataFilePath)
	if err != nil {
		return fmt.Errorf("failed to read data file: %w", err)
	}
	if dataFileInfo.Size() > maxKeyFileSize {
		return fmt.Errorf("data file exceeds maximum allowed size of %d bytes", maxKeyFileSize)
	}

	sigFileInfo, err := os.Stat(sigFilePath)
	if err != nil {
		return fmt.Errorf("failed to read signature file: %w", err)
	}
	if sigFileInfo.Size() > maxKeyFileSize {
		return fmt.Errorf("signature file exceeds maximum allowed size of %d bytes", maxKeyFileSize)
	}

	dataFileContent, err := os.ReadFile(dataFilePath)
	if err != nil {
		return fmt.Errorf("failed to read data file: %w", err)
	}

	sigFileContent, err := os.ReadFile(sigFilePath)
	if err != nil {
		return fmt.Errorf("failed to read signature file: %w", err)
	}

	err = keyRing.VerifyDetached(dataFileContent, sigFileContent)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}

// VerifyClearSignedFile verifies an ASCII-armored clear-signed file (.asc file)
// using the provided KeyRing. It returns the plaintext content of the message if verification is successful.
func VerifyClearSignedFile(keyRing KeyRing, ascFilePath string) (string, error) {
	if keyRing == nil {
		return "", fmt.Errorf("keyring cannot be nil")
	}

	// Get file info before reading to check size
	ascFileInfo, err := os.Stat(ascFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read clear-signed file: %w", err)
	}
	if ascFileInfo.Size() > maxKeyFileSize {
		return "", fmt.Errorf("clear-signed file exceeds maximum allowed size of %d bytes", maxKeyFileSize)
	}

	ascFileContent, err := os.ReadFile(ascFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read clear-signed file: %w", err)
	}

	clearTextMessage, err := ParseClearTextMessage(string(ascFileContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse clear-signed message: %w", err)
	}

	err = keyRing.VerifyDetached(clearTextMessage.Data, clearTextMessage.Signature)
	if err != nil {
		return "", fmt.Errorf("signature verification failed: %w", err)
	}

	return string(clearTextMessage.Data), nil
}

// ParseClearTextMessage parses a clear-signed message from armored text
func ParseClearTextMessage(armoredText string) (*ClearTextMessage, error) {
	if armoredText == "" {
		return nil, fmt.Errorf("armored text cannot be empty")
	}

	// Basic parsing for demonstration - in real implementation would parse PGP clear-signed message
	lines := strings.Split(armoredText, "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("invalid clear-signed message format")
	}

	// Find the message and signature parts
	messageStart := -1
	signatureStart := -1

	for i, line := range lines {
		if strings.Contains(line, "BEGIN PGP SIGNED MESSAGE") {
			messageStart = i + 1
		}
		if strings.Contains(line, "BEGIN PGP SIGNATURE") {
			signatureStart = i
			break
		}
	}

	if messageStart == -1 || signatureStart == -1 {
		return nil, fmt.Errorf("invalid clear-signed message structure")
	}

	// Extract message (skip hash line if present)
	messageLines := lines[messageStart:signatureStart]
	if len(messageLines) > 0 && strings.HasPrefix(messageLines[0], "Hash:") {
		messageLines = messageLines[2:] // Skip hash line and empty line
	}

	message := strings.Join(messageLines, "\n")
	signature := strings.Join(lines[signatureStart:], "\n")

	return &ClearTextMessage{
		Data:      []byte(strings.TrimSpace(message)),
		Signature: []byte(signature),
	}, nil
}

// LoadKeyRingFromPath loads all ASCII-armored PGP public keys from the given directory path
// and returns a KeyRing containing these keys using real GPG verification.
func LoadKeyRingFromPath(keysPath string) (KeyRing, error) {
	files, err := os.ReadDir(keysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read keys directory: %w", err)
	}

	keyRing := NewRealKeyRing()
	keyCount := 0

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".asc" {
			continue
		}

		filePath := filepath.Join(keysPath, file.Name())
		if err := validateKeyFile(filePath); err != nil {
			return nil, fmt.Errorf("invalid key file '%s': %w", file.Name(), err)
		}

		keyData, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}

		key, err := NewRealKey(string(keyData))
		if err != nil {
			return nil, fmt.Errorf("failed to parse armored key: %w", err)
		}

		if err := validateKey(key); err != nil {
			return nil, fmt.Errorf("invalid key in file '%s': %w", file.Name(), err)
		}

		if err := keyRing.AddKey(key); err != nil {
			return nil, fmt.Errorf("failed to add key to keyring: %w", err)
		}
		keyCount++
	}

	if keyCount == 0 {
		return nil, fmt.Errorf("no .asc keys found in directory")
	}
	return keyRing, nil
}

// LoadKeyRingFromStrings loads PGP public keys from a slice of ASCII-armored key strings
// and returns a KeyRing containing these keys using real GPG verification.
func LoadKeyRingFromStrings(armoredKeys []string) (KeyRing, error) {
	if len(armoredKeys) == 0 {
		return nil, fmt.Errorf("no armored keys provided")
	}

	keyRing := NewRealKeyRing()
	for i, armoredKey := range armoredKeys {
		key, err := NewRealKey(armoredKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse armored key string at index %d: %w", i, err)
		}

		if err := validateKey(key); err != nil {
			return nil, fmt.Errorf("invalid key at index %d: %w", i, err)
		}

		if err := keyRing.AddKey(key); err != nil {
			return nil, fmt.Errorf("failed to add key to keyring: %w", err)
		}
	}
	return keyRing, nil
}

// LoadKeyRingFromEmbeddedFS loads PGP public keys from an embedded filesystem using real GPG verification.
func LoadKeyRingFromEmbeddedFS(fs EmbeddedFS, keysPath string) (KeyRing, error) {
	files, err := fs.ReadDir(keysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded keys directory: %w", err)
	}

	keyRing := NewRealKeyRing()
	keyCount := 0

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".asc" {
			continue
		}

		filePath := filepath.Join(keysPath, file.Name())
		keyData, err := fs.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded key file: %w", err)
		}

		if len(keyData) > maxKeyFileSize {
			return nil, fmt.Errorf("embedded key file '%s' exceeds maximum allowed size", file.Name())
		}

		key, err := NewRealKey(string(keyData))
		if err != nil {
			return nil, fmt.Errorf("failed to parse armored key: %w", err)
		}

		if err := validateKey(key); err != nil {
			return nil, fmt.Errorf("invalid key in embedded file '%s': %w", file.Name(), err)
		}

		if err := keyRing.AddKey(key); err != nil {
			return nil, fmt.Errorf("failed to add key to keyring: %w", err)
		}
		keyCount++
	}

	if keyCount == 0 {
		return nil, fmt.Errorf("no .asc keys found in embedded directory")
	}
	return keyRing, nil
}

// LoadKeyRingFromEmbedFS is a convenience wrapper for LoadKeyRingFromEmbeddedFS that accepts embed.FS
func LoadKeyRingFromEmbedFS(fs embed.FS, keysPath string) (KeyRing, error) {
	return LoadKeyRingFromEmbeddedFS(fs, keysPath)
}

// LoadSimpleKeyRingFromPath loads keys using the simple (mock) implementation for testing
func LoadSimpleKeyRingFromPath(keysPath string) (KeyRing, error) {
	files, err := os.ReadDir(keysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read keys directory: %w", err)
	}

	keyRing := NewSimpleKeyRing()
	keyCount := 0

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".asc" {
			continue
		}

		filePath := filepath.Join(keysPath, file.Name())
		keyData, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file: %w", err)
		}

		key, err := NewSimpleKey(string(keyData))
		if err != nil {
			return nil, fmt.Errorf("failed to parse armored key: %w", err)
		}

		if err := keyRing.AddKey(key); err != nil {
			return nil, fmt.Errorf("failed to add key to keyring: %w", err)
		}
		keyCount++
	}

	if keyCount == 0 {
		return nil, fmt.Errorf("no .asc keys found in directory")
	}
	return keyRing, nil
}

// validateKeyFile checks if a key file has appropriate permissions and size
func validateKeyFile(filePath string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to access key file: %w", err)
	}

	if fileInfo.Size() > maxKeyFileSize {
		return fmt.Errorf("key file exceeds maximum allowed size of %d bytes", maxKeyFileSize)
	}

	// Check file permissions (allow both 0600 and 0644 for compatibility)
	perm := fileInfo.Mode().Perm()
	if perm != keyFileMode && perm != 0644 {
		return fmt.Errorf("key file has incorrect permissions. Expected %o or 0644, got %o", keyFileMode, perm)
		}

	return nil
}

// validateKey performs basic validation of a PGP key
func validateKey(key Key) error {
	if key == nil {
		return fmt.Errorf("key is nil")
	}

	// Check if key is revoked
	if key.IsRevoked() {
		return fmt.Errorf("key is revoked")
	}

	return nil
}

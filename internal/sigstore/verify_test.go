package sigstore

import (
	"testing"

	"github.com/sigstore/sigstore-go/pkg/bundle"
)

const (
	fixtureBundlePath  = "testdata/Python-3.15.0b3.tar.xz.sigstore"
	fixtureCertID      = "hugo@python.org"
	fixtureOIDCIssuer  = "https://github.com/login/oauth"
)

// loadFixtureDigest extracts the message digest + algorithm embedded in the
// committed python.org bundle so the test can verify offline via digest without
// checking in the ~35MB artifact.
func loadFixtureDigest(t *testing.T) ([]byte, string) {
	t.Helper()
	b, err := bundle.LoadJSONFromPath(fixtureBundlePath)
	if err != nil {
		t.Fatalf("loading fixture bundle: %v", err)
	}
	content, err := b.SignatureContent()
	if err != nil {
		t.Fatalf("signature content: %v", err)
	}
	msg, ok := content.(interface {
		Digest() []byte
		DigestAlgorithm() string
	})
	if !ok {
		t.Fatalf("bundle SignatureContent %T does not expose a message digest", content)
	}
	return msg.Digest(), msg.DigestAlgorithm()
}

func TestVerifyBundleDigest_Pass(t *testing.T) {
	digest, alg := loadFixtureDigest(t)
	if err := VerifyBundleDigest(digest, alg, fixtureBundlePath, fixtureOIDCIssuer, fixtureCertID); err != nil {
		t.Fatalf("expected verification to pass, got: %v", err)
	}
}

func TestVerifyBundleDigest_TamperedDigestFails(t *testing.T) {
	digest, alg := loadFixtureDigest(t)
	if len(digest) == 0 {
		t.Fatal("fixture digest is empty")
	}
	tampered := append([]byte(nil), digest...)
	tampered[0] ^= 0xff
	err := VerifyBundleDigest(tampered, alg, fixtureBundlePath, fixtureOIDCIssuer, fixtureCertID)
	if err == nil {
		t.Fatal("expected verification to fail for tampered digest, got nil")
	}
}

func TestVerifyBundleDigest_WrongIdentityFails(t *testing.T) {
	digest, alg := loadFixtureDigest(t)
	err := VerifyBundleDigest(digest, alg, fixtureBundlePath, fixtureOIDCIssuer, "impostor@python.org")
	if err == nil {
		t.Fatal("expected verification to fail for wrong identity, got nil")
	}
}

func TestVerifyBundleDigest_WrongIssuerFails(t *testing.T) {
	digest, alg := loadFixtureDigest(t)
	err := VerifyBundleDigest(digest, alg, fixtureBundlePath, "https://accounts.google.com", fixtureCertID)
	if err == nil {
		t.Fatal("expected verification to fail for wrong issuer, got nil")
	}
}

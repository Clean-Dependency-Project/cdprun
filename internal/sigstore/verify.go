// Package sigstore provides upstream Sigstore bundle verification for downloaded
// runtime artifacts. It is verify-only: cdprun never signs or attests.
package sigstore

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

//go:embed trusted-root-public-good.json
var trustedRootJSON []byte

// VerifyBundle verifies that the artifact at artifactPath matches the Sigstore
// bundle at bundlePath, signed by the expected OIDC issuer and certificate
// identity (SAN). It uses the embedded public-good trusted root and the
// verification material embedded in the bundle (cert, transparency log entry,
// timestamps), so no network access to GitHub or TUF is required.
func VerifyBundle(artifactPath, bundlePath, oidcIssuer, certIdentity string) error {
	artifactFile, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("opening artifact %s: %w", artifactPath, err)
	}
	defer func() { _ = artifactFile.Close() }()
	return verifyBundle(verify.WithArtifact(artifactFile), bundlePath, oidcIssuer, certIdentity)
}

// VerifyBundleDigest verifies a Sigstore bundle against a precomputed artifact
// digest (hex-decoded bytes) and digest algorithm (e.g. "sha256"), pinned to the
// expected OIDC issuer and certificate identity. It is the offline-friendly
// counterpart to VerifyBundle: no artifact file is read, only the committed
// bundle and embedded trusted root are used.
func VerifyBundleDigest(artifactDigest []byte, algorithm, bundlePath, oidcIssuer, certIdentity string) error {
	return verifyBundle(verify.WithArtifactDigest(algorithm, artifactDigest), bundlePath, oidcIssuer, certIdentity)
}

// newVerifier builds a verifier over the embedded public-good trusted root with
// the same thresholds used by sigstore-go's verification example.
func newVerifier() (*verify.Verifier, error) {
	trustedRoot, err := root.NewTrustedRootFromJSON(trustedRootJSON)
	if err != nil {
		return nil, fmt.Errorf("parsing trusted root: %w", err)
	}
	trustedMaterial := root.TrustedMaterialCollection{trustedRoot}
	return verify.NewVerifier(trustedMaterial,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
}

func verifyBundle(artifactPolicy verify.ArtifactPolicyOption, bundlePath, oidcIssuer, certIdentity string) error {
	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return fmt.Errorf("loading sigstore bundle %s: %w", bundlePath, err)
	}
	verifier, err := newVerifier()
	if err != nil {
		return err
	}
	certID, err := verify.NewShortCertificateIdentity(oidcIssuer, "", certIdentity, "")
	if err != nil {
		return fmt.Errorf("building certificate identity: %w", err)
	}
	if _, err := verifier.Verify(b, verify.NewPolicy(
		artifactPolicy,
		verify.WithCertificateIdentity(certID),
	)); err != nil {
		return fmt.Errorf("sigstore verification failed: %w", err)
	}
	return nil
}

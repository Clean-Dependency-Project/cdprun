package config

import "testing"

func TestRegistry_PythonSigstore(t *testing.T) {
	cfg, err := LoadConfig("../../runtime-registry.yaml")
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	p, ok := cfg.Runtimes["python"]
	if !ok {
		t.Fatal("python runtime not found")
	}
	if !p.SigstoreEnabled() {
		t.Error("expected python sigstore enabled")
	}
	if p.GPGEnabled() {
		t.Error("expected python GPG disabled when sigstore enabled")
	}
	// 3.15 is covered by the identity table.
	entry, err := p.ResolveSigstoreIdentityEntry("3.15.0b3")
	if err != nil {
		t.Fatalf("resolve 3.15.0b3: %v", err)
	}
	if entry == nil || entry.CertIdentity != "hugo@python.org" || entry.OIDCIssuer != "https://github.com/login/oauth" {
		t.Errorf("3.15.0b3 -> entry=%+v", entry)
	}
	// 3.14 is also covered.
	entry, err = p.ResolveSigstoreIdentityEntry("3.14.0")
	if err != nil {
		t.Fatalf("resolve 3.14.0: %v", err)
	}
	if entry == nil || entry.CertIdentity != "hugo@python.org" || entry.OIDCIssuer != "https://github.com/login/oauth" {
		t.Errorf("3.14.0 -> entry=%+v", entry)
	}
	// 3.13 is intentionally NOT covered (old release) -> entry resolver skips.
	entry, err = p.ResolveSigstoreIdentityEntry("3.13.0")
	if err != nil {
		t.Fatalf("resolve entry 3.13.0: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry (skip) for old release 3.13.0, got %+v", entry)
	}
	// Only the 3.14–3.17 lines are configured.
	wantLines := map[string]bool{"3.14": false, "3.15": false, "3.16": false, "3.17": false}
	for _, id := range p.Verification.Methods.Sigstore.Identities {
		if _, ok := wantLines[id.VersionPrefix]; ok {
			wantLines[id.VersionPrefix] = true
		} else {
			t.Errorf("unexpected identity row for version_prefix %q", id.VersionPrefix)
		}
	}
	for line, found := range wantLines {
		if !found {
			t.Errorf("expected identity row for %q not found", line)
		}
	}
}

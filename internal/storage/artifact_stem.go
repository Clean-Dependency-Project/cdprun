package storage

import "strings"

// sidecarSuffixes are proof files published next to a binary.
// Longer suffixes come first so ".audit.json" is not left with a trailing ".json".
var sidecarSuffixes = []string{
	".audit.json",
	".metadata.json",
	".sha512",
	".asc",
	".sig",
	".cert",
}

// binaryExtensions are archive and installer suffixes.
// Compound suffixes come first so ".tar.xz" is removed in one step.
var binaryExtensions = []string{
	".tar.xz",
	".tar.gz",
	".tar.bz2",
	".tgz",
	".zip",
	".exe",
	".pkg",
	".msi",
	".dmg",
	".apk",
	".rpm",
	".deb",
}

// ArtifactStem returns the name that groups a binary with its proof files.
//
// Node and Tomcat publish "<binary>.audit.json" (the extension stays).
// VS Code and JetBrains publish "<stem>.audit.json" (the extension is dropped).
// Both forms, and the binary itself, reduce to the same stem. Two binaries that
// differ by more than that extension, such as a Tomcat zip and its .exe, stay distinct.
func ArtifactStem(filename string) string {
	name := stripSuffix(filename, sidecarSuffixes)
	return stripSuffix(name, binaryExtensions)
}

// AttachSidecars copies audit, signature, certificate, and metadata from
// platform rows that have no binary onto the one binary with the same stem.
// Rows that were only a sidecar are removed. A row is left alone when more
// than one binary shares its stem.
func AttachSidecars(artifacts ReleaseArtifacts) ReleaseArtifacts {
	out := artifacts
	out.Platforms = append([]PlatformArtifact(nil), artifacts.Platforms...)

	bins := map[stemKey][]int{}
	for i, platform := range out.Platforms {
		if platform.Binary == nil || strings.TrimSpace(platform.Binary.Filename) == "" {
			continue
		}
		key := stemKeyFor(platform.PlatformOS, platform.PlatformArch, ArtifactStem(platform.Binary.Filename))
		bins[key] = append(bins[key], i)
	}

	drop := make([]bool, len(out.Platforms))
	for i, platform := range out.Platforms {
		if platform.Binary != nil {
			continue
		}
		stem := sidecarStem(platform)
		if stem == "" {
			continue
		}
		targets := bins[stemKeyFor(platform.PlatformOS, platform.PlatformArch, stem)]
		if len(targets) != 1 && platform.PlatformOS == "" && platform.PlatformArch == "" {
			targets = binariesWithStem(bins, stem)
		}
		if len(targets) != 1 {
			continue
		}
		fillSidecars(&out.Platforms[targets[0]], platform)
		drop[i] = true
	}

	kept := make([]PlatformArtifact, 0, len(out.Platforms))
	for i, platform := range out.Platforms {
		if drop[i] {
			continue
		}
		kept = append(kept, platform)
	}
	out.Platforms = kept
	out.Metadata.PlatformCount = len(kept)
	return out
}

type stemKey struct {
	os   string
	arch string
	stem string
}

func stemKeyFor(osName, arch, stem string) stemKey {
	return stemKey{
		os:   strings.ToLower(strings.TrimSpace(osName)),
		arch: strings.ToLower(strings.TrimSpace(arch)),
		stem: stem,
	}
}

func binariesWithStem(bins map[stemKey][]int, stem string) []int {
	var hits []int
	for key, idxs := range bins {
		if key.stem != stem {
			continue
		}
		hits = append(hits, idxs...)
	}
	return hits
}

func sidecarStem(platform PlatformArtifact) string {
	names := []*ArtifactFile{platform.Signature, platform.Certificate, platform.MetadataFile}
	for _, file := range names {
		if file != nil && strings.TrimSpace(file.Filename) != "" {
			return ArtifactStem(file.Filename)
		}
	}
	if platform.Audit != nil && strings.TrimSpace(platform.Audit.Filename) != "" {
		return ArtifactStem(platform.Audit.Filename)
	}
	return ""
}

func fillSidecars(dst *PlatformArtifact, src PlatformArtifact) {
	if dst.Audit == nil && src.Audit != nil {
		audit := *src.Audit
		dst.Audit = &audit
	}
	if dst.Signature == nil && src.Signature != nil {
		sig := *src.Signature
		dst.Signature = &sig
	}
	if dst.Certificate == nil && src.Certificate != nil {
		cert := *src.Certificate
		dst.Certificate = &cert
	}
	if dst.MetadataFile == nil && src.MetadataFile != nil {
		meta := *src.MetadataFile
		dst.MetadataFile = &meta
	}
}

func stripSuffix(name string, suffixes []string) string {
	lower := strings.ToLower(name)
	for _, suffix := range suffixes {
		if strings.HasSuffix(lower, suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}

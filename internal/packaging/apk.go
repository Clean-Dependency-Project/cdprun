package packaging

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

type APKBuildOptions struct {
	Runtime       string
	Name          string
	Version       string
	Release       string
	Arch          string
	Summary       string
	License       string
	URL           string
	InstallPrefix string

	PayloadDir string
	OutDir     string
}

// BuildAPKFromPayload builds an APK by packaging a staged payload directory representing filesystem root.
//
// This is intentionally minimal for phase 3. It assumes `abuild` is available and configured
// in the environment it runs in (typically an Alpine container as a non-root user with a key).
func BuildAPKFromPayload(ctx context.Context, runner CommandRunner, opts APKBuildOptions) (BuildResult, error) {
	start := time.Now()

	if runner == nil {
		return BuildResult{}, fmt.Errorf("runner is required")
	}
	if opts.Runtime == "" {
		return BuildResult{}, fmt.Errorf("runtime is required")
	}
	if opts.Name == "" || opts.Version == "" || opts.Release == "" {
		return BuildResult{}, fmt.Errorf("name, version, and release are required")
	}
	if opts.PayloadDir == "" {
		return BuildResult{}, fmt.Errorf("payload dir is required")
	}
	if opts.OutDir == "" {
		return BuildResult{}, fmt.Errorf("out dir is required")
	}
	if opts.InstallPrefix == "" {
		return BuildResult{}, fmt.Errorf("install prefix is required")
	}

	workDir, err := os.MkdirTemp("", "cdprun-apkbuild-*")
	if err != nil {
		return BuildResult{}, fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	srcDir := filepath.Join(workDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return BuildResult{}, fmt.Errorf("create src dir: %w", err)
	}

	// Create a tarball source that extracts to $srcdir/payload so APKBUILD can copy it into $pkgdir.
	payloadCopy := filepath.Join(srcDir, "payload")
	if err := os.MkdirAll(payloadCopy, 0755); err != nil {
		return BuildResult{}, fmt.Errorf("create payload copy dir: %w", err)
	}
	if _, _, err := runner.Run(ctx, "", "cp", []string{"-a", opts.PayloadDir + "/.", payloadCopy + "/"}, nil); err != nil {
		return BuildResult{}, err
	}

	sourceTar := fmt.Sprintf("%s-%s.tar.gz", opts.Name, opts.Version)
	sourceTarPath := filepath.Join(workDir, sourceTar)
	if _, _, err := runner.Run(ctx, "", "tar", []string{"-czf", sourceTarPath, "-C", srcDir, "payload"}, nil); err != nil {
		return BuildResult{}, err
	}
	sourceSHA512, err := computeFileSHA512(sourceTarPath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("compute source sha512: %w", err)
	}

	apkbuildPath := filepath.Join(workDir, "APKBUILD")
	apkbuild, err := renderAPKBUILD(apkbuildParams{
		Name:          opts.Name,
		Version:       opts.Version,
		Release:       opts.Release,
		Arch:          opts.Arch,
		Summary:       nonEmpty(opts.Summary, fmt.Sprintf("%s runtime", opts.Name)),
		License:       nonEmpty(opts.License, "Proprietary"),
		URL:           nonEmpty(opts.URL, "https://example.invalid"),
		SourceTarball: sourceTar,
		SourceSHA512:  sourceSHA512,
	})
	if err != nil {
		return BuildResult{}, fmt.Errorf("render APKBUILD: %w", err)
	}
	if err := os.WriteFile(apkbuildPath, []byte(apkbuild), 0644); err != nil {
		return BuildResult{}, fmt.Errorf("write APKBUILD: %w", err)
	}

	// Build the APK. Output goes under ~/packages by default.
	if out, errOut, err := runner.Run(ctx, workDir, "abuild", []string{"-r"}, nil); err != nil {
		return BuildResult{}, fmt.Errorf("%w (abuild stdout=%q, stderr=%q)", err, truncateForError(out, 4096), truncateForError(errOut, 4096))
	}

	// Find produced .apk under $HOME/packages (best effort). We copy it into opts.OutDir.
	home := os.Getenv("HOME")
	if home == "" {
		return BuildResult{}, fmt.Errorf("HOME is required to locate abuild output")
	}

	built, err := filepath.Glob(filepath.Join(home, "packages", "*", "*.apk"))
	if err != nil {
		return BuildResult{}, fmt.Errorf("glob built apks: %w", err)
	}
	if len(built) == 0 {
		return BuildResult{}, fmt.Errorf("no apk produced under %s", filepath.Join(home, "packages"))
	}

	if err := os.MkdirAll(opts.OutDir, 0755); err != nil {
		return BuildResult{}, fmt.Errorf("create out dir: %w", err)
	}

	outPath := filepath.Join(opts.OutDir, filepath.Base(built[0]))
	if _, _, err := runner.Run(ctx, "", "cp", []string{"-f", built[0], outPath}, nil); err != nil {
		return BuildResult{}, err
	}

	outSHA, err := ComputeFileSHA256(outPath)
	if err != nil {
		return BuildResult{}, err
	}

	return BuildResult{
		Runtime:         opts.Runtime,
		PackageType:     PackageTypeAPK,
		PackageName:     opts.Name,
		Version:         opts.Version,
		Release:         opts.Release,
		Arch:            opts.Arch,
		InstallPrefix:   opts.InstallPrefix,
		PackageFilename: filepath.Base(outPath),
		PackagePath:     outPath,
		PackageSHA256:   outSHA,
		Duration:        time.Since(start),
	}, nil
}

func truncateForError(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

type apkbuildParams struct {
	Name          string
	Version       string
	Release       string
	Arch          string
	Summary       string
	License       string
	URL           string
	SourceTarball string
	SourceSHA512  string
}

func renderAPKBUILD(p apkbuildParams) (string, error) {
	tpl, err := template.New("APKBUILD").Parse(apkbuildTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, p); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const apkbuildTemplate = `# Generated by cdprun (phase 3)

pkgname={{.Name}}
pkgver={{.Version}}
pkgrel={{.Release}}
pkgdesc="{{.Summary}}"
url="{{.URL}}"
license="{{.License}}"
arch="{{.Arch}}"
options="!strip !check"
source="{{.SourceTarball}}"
sha512sums="{{.SourceSHA512}}  {{.SourceTarball}}"
builddir="$srcdir/payload"

package() {
	mkdir -p "$pkgdir"
	cp -a "$builddir"/. "$pkgdir"/
}
`

func computeFileSHA512(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

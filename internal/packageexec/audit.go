package packageexec

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/clean-dependency-project/cdprun/internal/packaging"
)

// Phase 1 — load download audit (read-only).
// Phase 2 — assemble build + test metadata after container runs.
// Phase 3 — write sibling {rpm}.audit.json next to the .rpm.

// WriteRPMAuditRecord writes a single JSON file next to the built RPM containing
// inlined download verification (from the upstream tarball's .audit.json),
// build container metadata, and test container output.
func WriteRPMAuditRecord(
	workspaceDir string,
	runID string,
	target Target,
	build packaging.BuildResult,
	execSpec TargetExecutorSpec,
	testStdout string,
	testStderr string,
	testErr error,
) error {
	if strings.ToLower(strings.TrimSpace(target.Target)) != "rpm" {
		return fmt.Errorf("WriteRPMAuditRecord: target is not rpm")
	}
	rpmAbs, err := resolvePath(workspaceDir, build.PackagePath)
	if err != nil {
		return fmt.Errorf("resolve package path: %w", err)
	}
	auditPath := rpmAbs + ".audit.json"

	downloadObj, err := loadDownloadAuditObject(workspaceDir, target.InputPath)
	if err != nil {
		downloadObj = fallbackDownloadObject(target)
	}

	testStatus := "success"
	overallVer := "success"
	if testErr != nil {
		testStatus = "failed"
		overallVer = "failed"
	}

	testOutput, errParse := parseTestOutputJSON(testStdout)
	testSection := map[string]interface{}{
		"image":  execSpec.Test.Image,
		"script": execSpec.Test.Script,
		"status": testStatus,
	}
	if strings.TrimSpace(testStderr) != "" {
		testSection["stderr"] = strings.TrimSpace(testStderr)
	}
	if testErr != nil {
		testSection["error"] = testErr.Error()
	}
	if errParse == nil && testOutput != nil {
		testSection["output"] = testOutput
	} else if strings.TrimSpace(testStdout) != "" {
		testSection["output_raw"] = strings.TrimSpace(testStdout)
	}

	record := map[string]interface{}{
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"run_id":              runID,
		"runtime":             target.Runtime,
		"version":             target.Version,
		"package_type":        "rpm",
		"package_name":        build.PackageName,
		"package_filename":    build.PackageFilename,
		"package_path":        build.PackagePath,
		"package_sha256":      build.PackageSHA256,
		"install_prefix":      target.InstallPrefix,
		"arch":                build.Arch,
		"release":             build.Release,
		"download":            downloadObj,
		"build":               buildSection(execSpec.Build, build),
		"test":                testSection,
		"verification_type":   "package-build-test-rpm",
		"verification_status": overallVer,
	}

	enc, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal rpm audit: %w", err)
	}
	if err := os.WriteFile(auditPath, enc, 0644); err != nil {
		return fmt.Errorf("write rpm audit file: %w", err)
	}
	return nil
}

func buildSection(spec ContainerSpec, build packaging.BuildResult) map[string]interface{} {
	out := map[string]interface{}{
		"image":  spec.Image,
		"script": spec.Script,
		"status": "success",
	}
	if build.Duration > 0 {
		out["duration_ms"] = build.Duration.Milliseconds()
	}
	return out
}

func loadDownloadAuditObject(workspaceDir, inputPath string) (map[string]interface{}, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("empty input path")
	}
	full, err := resolvePath(workspaceDir, inputPath)
	if err != nil {
		return nil, err
	}
	auditPath := full + ".audit.json"
	data, err := os.ReadFile(auditPath)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func fallbackDownloadObject(target Target) map[string]interface{} {
	return map[string]interface{}{
		"input_path":         target.InputPath,
		"input_sha256":       target.InputSHA256,
		"audit_file_missing": true,
	}
}

func parseTestOutputJSON(stdout string) (map[string]interface{}, error) {
	s := strings.TrimSpace(stdout)
	if s == "" {
		return nil, fmt.Errorf("empty test stdout")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// DownloadAuditPathForInput returns the path to the sibling audit file for an
// input tarball (workspace-relative or absolute), for tests and tooling.
func DownloadAuditPathForInput(workspaceDir, inputPath string) (string, error) {
	full, err := resolvePath(workspaceDir, inputPath)
	if err != nil {
		return "", err
	}
	return full + ".audit.json", nil
}

// RPMAuditPath returns the RPM audit file path next to the given RPM path.
func RPMAuditPath(rpmPath string) string {
	return rpmPath + ".audit.json"
}

// IsRPMAuditFilename reports whether name is a sibling audit for an RPM
// (e.g. OSPO-nodejs-22.22.2-1.amzn2023.x86_64.rpm.audit.json).
func IsRPMAuditFilename(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(lower, ".rpm.audit.json")
}

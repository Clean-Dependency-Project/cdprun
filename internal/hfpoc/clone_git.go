package hfpoc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CloneGitInput struct {
	UpstreamRepoID string
	UpstreamSHA    string // pinned upstream revision

	OrgRepoID string // org/name
	Private   bool

	AllowedFiles  []string
	RequireGitLFS bool

	TokenEnv string // e.g. HF_TOKEN

	Logger *slog.Logger
}

type CloneGitOutput struct {
	OrgSHA string
}

// CloneToOrgViaGit implements a "manual-assisted" clone:
// - create org repo via REST (done outside this function)
// - clone upstream via git
// - keep only allowlisted files
// - re-init as a fresh repo and push to org using an auth header (no token in URL)
//
// This keeps the Go PoC small while still demonstrating the key value: we can
// curate upstream artifacts into an org-owned source of truth.
func CloneToOrgViaGit(ctx context.Context, in CloneGitInput) (CloneGitOutput, error) {
	log := in.Logger
	if log == nil {
		log = slog.Default()
	}

	upstreamRepo := strings.TrimSpace(in.UpstreamRepoID)
	upstreamSHA := strings.TrimSpace(in.UpstreamSHA)
	orgRepo := strings.TrimSpace(in.OrgRepoID)
	if upstreamRepo == "" || upstreamSHA == "" || orgRepo == "" {
		return CloneGitOutput{}, fmt.Errorf("clone: upstream_repo_id, upstream_sha, and org_repo_id are required")
	}
	tokenEnv := strings.TrimSpace(in.TokenEnv)
	if tokenEnv == "" {
		tokenEnv = "HF_TOKEN"
	}
	token := strings.TrimSpace(os.Getenv(tokenEnv))
	if token == "" {
		return CloneGitOutput{}, fmt.Errorf("clone: env var %q is not set", tokenEnv)
	}

	tmpDir, err := os.MkdirTemp("", "cdprun-hf-poc-*")
	if err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: create temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	repoDir := filepath.Join(tmpDir, "repo")

	cloneURL := fmt.Sprintf("https://huggingface.co/%s", upstreamRepo)
	log.Info("cloning upstream repo", "repo", upstreamRepo)
	if err := runGit(ctx, token, tmpDir, "clone", "--depth", "1", cloneURL, repoDir); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git clone: %w", err)
	}

	// Checkout pinned SHA (even if shallow clone doesn't include it, this will fail
	// and force the user to adjust; good for deterministic behavior).
	log.Info("checking out pinned revision", "sha", upstreamSHA)
	if err := runGit(ctx, token, repoDir, "fetch", "--depth", "1", "origin", upstreamSHA); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git fetch pinned sha: %w", err)
	}
	if err := runGit(ctx, token, repoDir, "checkout", upstreamSHA); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git checkout pinned sha: %w", err)
	}

	// Ensure LFS objects are present for allowlisted files (best effort).
	include := strings.Join(in.AllowedFiles, ",")
	if include != "" {
		if err := ensureGitLFSAvailable(ctx, token, repoDir, in.RequireGitLFS); err != nil {
			return CloneGitOutput{}, err
		}
		log.Info("fetching LFS files (if any)", "include", include)
		if err := runGit(ctx, token, repoDir, "lfs", "fetch", "--include", include); err != nil {
			return CloneGitOutput{}, fmt.Errorf("clone: git lfs fetch: %w", err)
		}
		if err := runGit(ctx, token, repoDir, "lfs", "checkout"); err != nil {
			return CloneGitOutput{}, fmt.Errorf("clone: git lfs checkout: %w", err)
		}
	}

	// Remove non-allowlisted files (except .gitattributes, which often defines LFS patterns).
	keep := map[string]bool{
		".gitattributes": true,
	}
	for _, f := range in.AllowedFiles {
		ff := strings.TrimSpace(f)
		if ff != "" {
			keep[ff] = true
		}
	}

	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: read repo dir: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		if keep[name] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(repoDir, name)); err != nil {
			return CloneGitOutput{}, fmt.Errorf("clone: remove %q: %w", name, err)
		}
	}

	// Re-init as a fresh curated repo.
	if err := os.RemoveAll(filepath.Join(repoDir, ".git")); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: remove .git: %w", err)
	}
	if err := runGit(ctx, token, repoDir, "init"); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git init: %w", err)
	}
	if err := runGit(ctx, token, repoDir, "add", "."); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git add: %w", err)
	}
	if err := runGit(ctx, token, repoDir, "-c", "user.name=cdprun-hf-poc", "-c", "user.email=cdprun-hf-poc@local", "commit", "-m", "Curate model snapshot"); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git commit: %w", err)
	}

	cp := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cp.Dir = repoDir
	out, err := cp.Output()
	if err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git rev-parse: %w", err)
	}
	orgSHA := strings.TrimSpace(string(out))

	pushURL := fmt.Sprintf("https://huggingface.co/%s", orgRepo)
	log.Info("pushing curated snapshot to org repo", "repo", orgRepo)
	if err := runGit(ctx, token, repoDir, "branch", "-M", "main"); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git branch -M main: %w", err)
	}
	if err := runGit(ctx, token, repoDir, "remote", "add", "origin", pushURL); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git remote add: %w", err)
	}
	if err := runGit(ctx, token, repoDir, "push", "-u", "origin", "main"); err != nil {
		return CloneGitOutput{}, fmt.Errorf("clone: git push: %w", err)
	}

	return CloneGitOutput{OrgSHA: orgSHA}, nil
}

func ensureGitLFSAvailable(ctx context.Context, token, repoDir string, required bool) error {
	// `git lfs` is an extension; `git` itself may be present while `git-lfs` is not.
	// We probe via `git lfs version`.
	if err := runGit(ctx, token, repoDir, "lfs", "version"); err == nil {
		return nil
	}
	if !required {
		// Proceed without LFS. This is only useful for metadata-only clones.
		return nil
	}
	return fmt.Errorf(
		"clone: git-lfs is required but not installed; install it (e.g. `brew install git-lfs && git lfs install`) or set clone.require_git_lfs=false and remove weight files from clone.allowed_files",
	)
}

func runGit(ctx context.Context, token, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Avoid embedding token in URLs; pass it as an auth header.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		fmt.Sprintf("GIT_HTTP_EXTRAHEADER=Authorization: Bearer %s", token),
	)
	b, err := cmd.CombinedOutput()
	if err != nil {
		// Ensure we don't accidentally print tokens: output shouldn't contain it, but keep message generic.
		return fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(b)))
	}
	return nil
}

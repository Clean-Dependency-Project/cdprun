package hfpoc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type VerifyInput struct {
	RepoID    string
	Revision  string
	OrgOnlyNS string // if set, require repo to be under this org namespace

	AllowedFiles []string
	SHA256ByFile map[string]string

	Logger *slog.Logger
}

func Verify(ctx context.Context, c *Client, in VerifyInput) (Result, error) {
	log := in.Logger
	if log == nil {
		log = slog.Default()
	}

	repo := strings.TrimSpace(in.RepoID)
	rev := strings.TrimSpace(in.Revision)
	if repo == "" || rev == "" {
		return Result{}, fmt.Errorf("verify: repo_id and revision are required")
	}
	if in.OrgOnlyNS != "" {
		wantPrefix := strings.TrimSpace(in.OrgOnlyNS) + "/"
		if !strings.HasPrefix(repo, wantPrefix) {
			return Result{
				Status:    "failed",
				Message:   "org_only_consumption_required",
				OrgRepoID: repo,
				Details: map[string]any{
					"required_org": strings.TrimSpace(in.OrgOnlyNS),
				},
			}, nil
		}
	}

	res := Result{
		Status:        "ok",
		Command:       "verify",
		OrgRepoID:     repo,
		OrgSHA:        rev,
		SelectedFiles: append([]string{}, in.AllowedFiles...),
		Details:       map[string]any{},
	}

	for _, f := range in.AllowedFiles {
		ff := strings.TrimSpace(f)
		if ff == "" {
			continue
		}
		log.Info("downloading file for verification", "repo", repo, "revision", rev, "file", ff)
		b, err := c.DownloadResolve(ctx, repo, rev, ff)
		if err != nil {
			res.Status = "failed"
			res.Errors = append(res.Errors, fmt.Sprintf("download_failed:%s:%v", ff, err))
			continue
		}
		got := sha256Hex(b)
		want := strings.TrimSpace(in.SHA256ByFile[ff])
		if want != "" && !strings.EqualFold(got, want) {
			res.Status = "failed"
			res.Errors = append(res.Errors, fmt.Sprintf("sha256_mismatch:%s:want=%s got=%s", ff, want, got))
			continue
		}
		// Always report got digest for evidence, even if want digest was empty.
		if res.Details["sha256_by_file"] == nil {
			res.Details["sha256_by_file"] = map[string]string{}
		}
		res.Details["sha256_by_file"].(map[string]string)[ff] = got
		res.Verified++
	}

	if res.Status != "ok" {
		res.Message = "verification_failed"
	} else {
		res.Message = "verification_ok"
	}
	return res, nil
}

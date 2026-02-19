package hfpoc

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type AssessInput struct {
	RepoID    string
	Criteria  IntakeCriteria
	Allowlist []string // allowed files (optional sanity check against siblings)
}

type AssessOutput struct {
	ModelInfo ModelInfo

	License       string
	LicenseSource string

	Passed  bool
	Reasons []string
}

func Assess(ctx context.Context, c *Client, in AssessInput) (AssessOutput, error) {
	repo := strings.TrimSpace(in.RepoID)
	if repo == "" {
		return AssessOutput{}, fmt.Errorf("assess: repo_id is required")
	}

	mi, err := c.GetModelInfo(ctx, repo)
	if err != nil {
		return AssessOutput{}, fmt.Errorf("assess: get model info: %w", err)
	}

	now := time.Now().UTC()
	license, licenseSource, reasons := evaluateModel(mi, in.Criteria, in.Allowlist, now)

	return AssessOutput{
		ModelInfo:     mi,
		License:       license,
		LicenseSource: licenseSource,
		Passed:        len(reasons) == 0,
		Reasons:       reasons,
	}, nil
}

func evaluateModel(mi ModelInfo, criteria IntakeCriteria, requiredFiles []string, now time.Time) (license, licenseSource string, reasons []string) {
	// License check: model cards are not guaranteed to include license metadata, so
	// keep this configurable. For PoC value scenarios, we want a clear pass/fail gate.
	license, licenseSource = extractLicense(mi)

	if criteria.RequireModelCard && len(mi.CardData) == 0 {
		reasons = append(reasons, "missing_model_card_metadata")
	}

	if license == "" && !criteria.AllowMissingLicense {
		reasons = append(reasons, "missing_license")
	}
	if license != "" && len(criteria.AllowedLicenses) > 0 {
		allowed := false
		for _, al := range criteria.AllowedLicenses {
			if strings.EqualFold(strings.TrimSpace(al), strings.TrimSpace(license)) {
				allowed = true
				break
			}
		}
		if !allowed {
			reasons = append(reasons, "license_not_allowed:"+license)
			reasons = append(reasons, "license_source:"+licenseSource)
		}
	}

	// Required card fields: only checks top-level keys in cardData.
	for _, field := range criteria.RequiredCardFields {
		key := strings.TrimSpace(field)
		if key == "" {
			continue
		}
		if !cardFieldPresent(mi.CardData, key) {
			reasons = append(reasons, "missing_card_field:"+key)
		}
	}

	// Deny tags: exact match or prefix match when pattern ends with '*'.
	for _, pattern := range criteria.DenyTags {
		p := strings.TrimSpace(pattern)
		if p == "" {
			continue
		}
		for _, tag := range mi.Tags {
			t := strings.TrimSpace(tag)
			if tagMatchesPattern(t, p) {
				reasons = append(reasons, "denied_tag:"+p+":matched="+t)
				break
			}
		}
	}

	if criteria.MinDownloads30d > 0 && mi.Downloads < criteria.MinDownloads30d {
		reasons = append(reasons, fmt.Sprintf("min_downloads_not_met:have=%d need=%d", mi.Downloads, criteria.MinDownloads30d))
	}

	if criteria.MinAgeDays > 0 {
		createdAt := strings.TrimSpace(mi.CreatedAt)
		if createdAt == "" {
			reasons = append(reasons, "missing_created_at")
		} else {
			ts, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				reasons = append(reasons, "invalid_created_at:"+createdAt)
			} else {
				ageDays := int(now.Sub(ts).Hours() / 24)
				if ageDays < criteria.MinAgeDays {
					reasons = append(reasons, fmt.Sprintf("min_age_not_met:have_days=%d need_days=%d", ageDays, criteria.MinAgeDays))
				}
			}
		}
	}

	// Optional: ensure required files exist upstream (quick sanity).
	if len(requiredFiles) > 0 && len(mi.Siblings) > 0 {
		exists := make(map[string]bool, len(mi.Siblings))
		for _, s := range mi.Siblings {
			exists[s.RFilename] = true
		}
		for _, f := range requiredFiles {
			ff := strings.TrimSpace(f)
			if ff == "" {
				continue
			}
			if !exists[ff] {
				reasons = append(reasons, "missing_required_file:"+ff)
			}
		}
	}

	return license, licenseSource, reasons
}

func cardFieldPresent(card map[string]any, key string) bool {
	if len(card) == 0 {
		return false
	}
	v, ok := card[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) != ""
	case []any:
		return len(t) > 0
	case []string:
		return len(t) > 0
	default:
		return true
	}
}

func tagMatchesPattern(tag, pattern string) bool {
	t := strings.ToLower(strings.TrimSpace(tag))
	p := strings.ToLower(strings.TrimSpace(pattern))
	if strings.HasSuffix(p, "*") {
		prefix := strings.TrimSuffix(p, "*")
		return strings.HasPrefix(t, prefix)
	}
	return t == p
}

func extractLicense(mi ModelInfo) (license string, source string) {
	// 1) Prefer card metadata license when present.
	if mi.CardData != nil {
		if raw, ok := mi.CardData["license"]; ok {
			switch v := raw.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v), "cardData.license"
				}
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s), "cardData.license[]"
					}
				}
			}
		}
		// Some cards store license in other forms; keep small and robust.
		if raw, ok := mi.CardData["licenses"]; ok {
			switch v := raw.(type) {
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s), "cardData.licenses[]"
					}
				}
			}
		}
	}

	// 2) Fall back to tag-based license, common pattern: "license:mit".
	for _, tag := range mi.Tags {
		t := strings.TrimSpace(tag)
		if strings.HasPrefix(strings.ToLower(t), "license:") {
			v := strings.TrimSpace(t[len("license:"):])
			if v != "" {
				return v, "tags.license:*"
			}
		}
	}
	return "", ""
}

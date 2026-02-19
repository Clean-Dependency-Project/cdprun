# HF PoC: Curate Upstream Model Into Org Repo

This is a quick prototype that demonstrates value (control, stability, security) by:

1. assessing an upstream HF model (intake criteria),
2. cloning a curated snapshot into an org-owned repo,
3. verifying the org repo by pinned commit SHA and SHA256.

Output contract:

- `stdout`: final JSON result
- `stderr`: JSON logs

This keeps logs and machine output from colliding.

## Prereqs

- `go` available
- `git` installed
- `git-lfs` installed (required if cloning weights like `pytorch_model.bin`)
- HF access token exported (do not commit tokens into files)

```bash
export HF_TOKEN="YOUR_TOKEN"
```

Install git-lfs on macOS (example):

```bash
brew install git-lfs
git lfs install
```

## Scenario A (pass): intake -> clone -> verify

Assess upstream:

```bash
go run ./cmd/hf-poc assess
```

Clone into org (creates repo if needed, then pushes curated snapshot):

```bash
go run ./cmd/hf-poc clone
```

`clone` writes the resulting `org_sha` into the configured state file (default: `config/hf_poc_state.yaml`).

Verify against the pinned org commit SHA:

```bash
go run ./cmd/hf-poc verify
```

## Scenario B (fail-intake)

Use intentionally strict rules to show failures (license, model card fields, tags, downloads, age):

```bash
cp config/hf_assess_rules_fail.yaml config/hf_assess_rules.yaml
go run ./cmd/hf-poc assess
```

## Scenario C (fail-integrity)

Provide a wrong SHA256 mapping and verify fails closed:

Edit `config/hf_poc.yaml` and set `verify.sha256_by_file.config.json` to an incorrect value, then run `verify` again.

## Scenario D (policy enforcement)

Try verifying upstream while `--org-only` is set; it should refuse:

Set `verify.org_repo_id` to an upstream repo (e.g. `sshleifer/tiny-gpt2`) while leaving `verify.org_only_namespace` set, then run `verify` and it will refuse.

## Notes

- This PoC intentionally defers Sigstore verification to a follow-up phase.\n+

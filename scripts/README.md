# Nexus Mirror Script Manual

This document explains how to use `nexus_mirror.py` in this folder.

For the full enterprise runtime supply-chain view (Go validation pipeline, `index.json` publishing, and Nexus distribution), see `docs/architecture.md`.

## FAQ

### How does it download?

The script runs this flow:

1. load config
2. preflight-check source and destination repos
3. for each `indexes` entry in order:
   - fetch `index_url`
   - apply policy filter from `policy_file` (`supported == true`)
   - `HEAD` check destination for each approved artifact
   - if missing: download from source and upload to destination

All HTTP calls are made through external `curl` commands.

### How does it use policy?

Each index is paired with a policy file:

```json
{ "key": "python", "index_url": "...", "policy_file": "policies/python-policy.json" }
```

For that pair, only policy entries with `supported: true` are approved.  
Version matching is prefix-based:

- policy `3.12` matches artifact `3.12.10`
- policy `22` matches artifact `22.18.0`

### Where do I check why something did not download?

Check both outputs:

- stdout JSON result:
  - top-level: `status`, `message`, `selected`, `uploaded`, `failed`
  - per-index: `indexes` and `indexes_by_key`
  - per-item detail: `items` (includes `status`, `message`, `error`, `head_status`, paths)
- stderr JSON logs:
  - set `log_level` to `debug` for detailed decision logs
  - includes reasons like `skipped existing artifact`, policy filter counts, and limit-stop messages

### Why was an artifact skipped?

Common reasons:

- destination already has it (`HEAD 200`)
- version is not approved by policy (`supported` is false or unmatched)
- global `limit` was reached before later artifacts/indexes

### How do I run dry-run vs real upload?

Set in config:

- `"dry_run": true` -> plan only, no upload
- `"dry_run": false` -> real upload

Run command is the same:

```bash
python3 scripts/nexus_mirror.py
```

### How do credentials work?

Use `*_env` keys in config (recommended), for example:

- `source_user_env`, `source_password_env`
- `dest_user_env`, `dest_password_env`

Then export those variables before running.

## What this script does

`nexus_mirror.py` mirrors approved artifacts from a Nexus proxy repository to a Nexus hosted repository.

It works in this order:

1. reads config
2. validates source and destination repositories
3. reads each index URL in sequence
4. filters artifacts using the paired policy file (`supported: true`)
5. checks destination with `HEAD`
6. downloads missing artifacts from source
7. uploads them to destination


## Requirements

- Python 3
- `curl` in PATH
- Nexus access to source and destination repositories

## Config file discovery

The script looks for config in this order:

1. `--config <path>`
2. `NEXUS_MIRROR_CONFIG` env var
3. `config/nexus_mirror.json`
4. `nexus-mirror.json`

## Config format

Use one config file with `indexes` entries, each pairing an index URL with a policy file.

Example:

```json
{
  "indexes": [
    {
      "key": "nodejs",
      "index_url": "https://clean-dependency-project.github.io/cdprun/simple/nodejs/index.json",
      "policy_file": "policies/nodejs-policy.json"
    },
    {
      "key": "tomcat",
      "index_url": "https://clean-dependency-project.github.io/cdprun/simple/tomcat/index.json",
      "policy_file": "policies/tomcat-policy.json"
    },
    {
      "key": "python",
      "index_url": "https://clean-dependency-project.github.io/cdprun/simple/python/index.json",
      "policy_file": "policies/python-policy.json"
    }
  ],
  "source_url": "http://localhost:8081",
  "source_repo": "cdprun",
  "source_user_env": "NEXUS_SOURCE_USER",
  "source_password_env": "NEXUS_SOURCE_PASSWORD",
  "dest_url": "http://localhost:8081",
  "dest_repo": "ospo",
  "dest_user_env": "NEXUS_DEST_USER",
  "dest_password_env": "NEXUS_DEST_PASSWORD",
  "limit": 25,
  "dry_run": false,
  "force": false,
  "platform_case_map": {
    "linux": "Linux",
    "windows": "Window",
    "mac": "Mac"
  },
  "timeout": "10m",
  "log_level": "info"
}
```

## Credential handling

You can provide credentials in two ways:

- direct values (`source_user`, `source_password`, `dest_user`, `dest_password`)
- env-var references (`source_user_env`, `source_password_env`, `dest_user_env`, `dest_password_env`)

If an `*_env` field is set, that environment variable is required.

Example:

```bash
export NEXUS_SOURCE_USER=admin
export NEXUS_SOURCE_PASSWORD=password
export NEXUS_DEST_USER=admin
export NEXUS_DEST_PASSWORD=password
```

## Run examples

Default config path:

```bash
python3 scripts/nexus_mirror.py
```

Explicit config path:

```bash
python3 scripts/nexus_mirror.py --config config/nexus_mirror.json
```

Dry run (from config):

- set `"dry_run": true` in config
- run the same command

## Output model

The script writes:

- JSON logs to stderr
- final JSON result to stdout

Top-level result fields include:

- `status`, `message`
- `dry_run`, `forced`
- `selected`, `planned`, `uploaded`, `failed`, `skipped_exists`
- `indexes` (per-index counters)
- `indexes_by_key` (same counters keyed by `key`)
- `items` (detailed per-artifact results)

## How policy filtering works

For each index:

- script loads the paired policy JSON file
- collects versions where `supported == true`
- artifact is approved when index `version` matches a supported policy version prefix

Examples:

- policy version `3.12` approves artifact version `3.12.10`
- policy version `22` approves artifact version `22.18.0`

## Limit behavior

- `limit = 0` means no limit
- `limit > 0` is a global cap across all index entries in sequence

## Case mapping for destination paths

If destination uses different top-level folder casing (`Linux`, `Window`, `Mac`), use `platform_case_map`.

Without mapping, lowercase index paths and mixed-case destination folders can create duplicate paths.

## Troubleshooting

`status=failed` with `401`:

- credentials are wrong or missing
- verify env vars referenced by `*_env`

`selected=0` but expected downloads:

- artifacts may already exist in destination
- policy may be filtering versions out
- set `log_level` to `debug` for decision logs

`source delete` returns `405`:

- expected for Nexus proxy repositories; delete is not allowed there


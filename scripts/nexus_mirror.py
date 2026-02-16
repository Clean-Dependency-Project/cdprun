#!/usr/bin/env python3
"""
Mirror artifacts from Nexus proxy to hosted repo.

Config-file driven. Logs JSON to stderr and emits JSON result to stdout.
HTTP is invoked through external curl processes.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import posixpath
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Set, Tuple

USER_AGENT = "cdprun-nexus-mirror/0.1"
LOG_LEVEL_ORDER = {"debug": 10, "info": 20, "warning": 30, "error": 40}
LOG_MIN_LEVEL = LOG_LEVEL_ORDER["info"]


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def set_log_level(level: str) -> None:
    global LOG_MIN_LEVEL
    LOG_MIN_LEVEL = LOG_LEVEL_ORDER.get((level or "").strip().lower(), LOG_LEVEL_ORDER["info"])


def log(level: str, msg: str, **fields: Any) -> None:
    level_norm = (level or "info").strip().lower()
    if LOG_LEVEL_ORDER.get(level_norm, 100) < LOG_MIN_LEVEL:
        return
    payload: Dict[str, Any] = {"timestamp": now_iso(), "level": level, "msg": msg}
    payload.update(fields)
    sys.stderr.write(json.dumps(payload, ensure_ascii=True) + "\n")
    sys.stderr.flush()


def parse_duration_to_seconds(raw: str) -> int:
    s = (raw or "").strip().lower()
    if not s:
        raise ValueError("empty duration")
    if s.endswith("ms"):
        return max(1, int(float(s[:-2]) / 1000.0))
    if s.endswith("s"):
        return max(1, int(float(s[:-1])))
    if s.endswith("m"):
        return max(1, int(float(s[:-1]) * 60))
    if s.endswith("h"):
        return max(1, int(float(s[:-1]) * 3600))
    return max(1, int(float(s)))


def resolve_config_path(flag_value: Optional[str]) -> str:
    if flag_value and flag_value.strip():
        return flag_value.strip()
    env_path = (os.getenv("NEXUS_MIRROR_CONFIG") or "").strip()
    if env_path:
        return env_path
    for candidate in ("config/nexus_mirror.json", "nexus-mirror.json"):
        if os.path.exists(candidate):
            return candidate
    raise SystemExit(
        "config file not specified; set NEXUS_MIRROR_CONFIG, pass --config, "
        "or create config/nexus_mirror.json"
    )


def read_config(path: str) -> Dict[str, Any]:
    with open(path, "rb") as f:
        return json.loads(f.read().decode("utf-8"))


def req_string(cfg: Dict[str, Any], key: str) -> str:
    val = cfg.get(key)
    if isinstance(val, str) and val.strip():
        return val.strip()
    raise SystemExit(f"config missing {key}")


def opt_string(cfg: Dict[str, Any], key: str, default: str = "") -> str:
    val = cfg.get(key)
    if isinstance(val, str):
        return val.strip()
    return default


def opt_bool(cfg: Dict[str, Any], key: str, default: bool) -> bool:
    val = cfg.get(key)
    return val if isinstance(val, bool) else default


def opt_int(cfg: Dict[str, Any], key: str, default: int) -> int:
    val = cfg.get(key)
    return val if isinstance(val, int) else default


def opt_platform_case_map(cfg: Dict[str, Any], key: str) -> Dict[str, str]:
    raw = cfg.get(key)
    if raw is None:
        return {}
    if not isinstance(raw, dict):
        raise SystemExit(f"{key} must be an object")
    out: Dict[str, str] = {}
    for k, v in raw.items():
        if not isinstance(k, str) or not isinstance(v, str):
            raise SystemExit(f"{key} keys and values must be strings")
        key_norm = k.strip().lower()
        val_norm = v.strip()
        if key_norm and val_norm:
            out[key_norm] = val_norm
    return out


def resolve_credential(cfg: Dict[str, Any], value_key: str, env_key_field: str) -> str:
    env_name = opt_string(cfg, env_key_field, "")
    if env_name:
        val = os.getenv(env_name, "")
        if val == "":
            raise SystemExit(f"env var {env_name!r} (from {env_key_field}) is not set")
        return val
    return opt_string(cfg, value_key, "")


def normalize_base_url(raw: str, key: str) -> str:
    v = raw.strip()
    if "://" not in v:
        raise SystemExit(f"{key} must include scheme and host: {v!r}")
    scheme, rest = v.split("://", 1)
    if not scheme or not rest:
        raise SystemExit(f"{key} must include scheme and host: {v!r}")
    return v.rstrip("/")


def validate_repo(name: str, key: str) -> str:
    if "/" in name or ":" in name:
        raise SystemExit(f"{key} must be a Nexus repo name (no '/' or ':'): {name!r}")
    return name


def parse_index_pairs(cfg: Dict[str, Any]) -> List[Dict[str, str]]:
    indexes = cfg.get("indexes")
    if not isinstance(indexes, list) or not indexes:
        raise SystemExit("config missing indexes (array of {index_url, policy_file})")
    out: List[Dict[str, str]] = []
    for idx, item in enumerate(indexes):
        if not isinstance(item, dict):
            raise SystemExit(f"indexes[{idx}] must be an object")
        url = item.get("index_url")
        policy = item.get("policy_file")
        key = item.get("key")
        if not isinstance(url, str) or not url.strip():
            raise SystemExit(f"indexes[{idx}].index_url is required")
        if not isinstance(policy, str) or not policy.strip():
            raise SystemExit(f"indexes[{idx}].policy_file is required")
        policy_path = policy.strip()
        if isinstance(key, str) and key.strip():
            index_key = key.strip()
        else:
            base = os.path.basename(policy_path)
            index_key = base.removesuffix(".json").removesuffix("-policy")
        out.append({"key": index_key, "index_url": url.strip(), "policy_file": policy_path})
    return out


def make_conf(cfg: Dict[str, Any]) -> Dict[str, Any]:
    timeout_s = parse_duration_to_seconds(opt_string(cfg, "timeout", "10m"))
    limit = opt_int(cfg, "limit", 3)
    if limit < 0:
        limit = 0
    return {
        "source_url": normalize_base_url(req_string(cfg, "source_url"), "source_url"),
        "source_repo": validate_repo(req_string(cfg, "source_repo"), "source_repo"),
        "source_user": resolve_credential(cfg, "source_user", "source_user_env"),
        "source_pass": resolve_credential(cfg, "source_password", "source_password_env"),
        "dest_url": normalize_base_url(req_string(cfg, "dest_url"), "dest_url"),
        "dest_repo": validate_repo(req_string(cfg, "dest_repo"), "dest_repo"),
        "dest_user": resolve_credential(cfg, "dest_user", "dest_user_env"),
        "dest_pass": resolve_credential(cfg, "dest_password", "dest_password_env"),
        "indexes": parse_index_pairs(cfg),
        "limit": limit,
        "dry_run": opt_bool(cfg, "dry_run", False),
        "force": opt_bool(cfg, "force", False),
        "include_items": True,
        "timeout_s": timeout_s,
        "log_level": opt_string(cfg, "log_level", "info"),
        "platform_case_map": opt_platform_case_map(cfg, "platform_case_map"),
    }


def clean_path(p: str) -> str:
    raw = (p or "").strip().lstrip("/")
    norm = posixpath.normpath(raw)
    return "" if norm == "." else norm


def rewrite_dest_case(dest_path: str, case_map: Dict[str, str]) -> str:
    if not case_map:
        return dest_path
    parts = dest_path.split("/", 1)
    head = parts[0].strip()
    tail = parts[1] if len(parts) > 1 else ""
    mapped = case_map.get(head.lower())
    if not mapped:
        return dest_path
    if tail:
        return f"{mapped}/{tail}"
    return mapped


def repo_asset_url(base_url: str, repo: str, p: str) -> str:
    return f"{base_url}/repository/{repo}/{p}"


def curl_common(timeout_s: int) -> List[str]:
    return ["curl", "-sS", "--connect-timeout", str(timeout_s), "--max-time", str(timeout_s), "-A", USER_AGENT]


def maybe_auth(args: List[str], user: str, password: str) -> List[str]:
    if user and password:
        return args + ["-u", f"{user}:{password}"]
    return args


def run_curl(args: List[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True)


def get_repos(base_url: str, timeout_s: int, user: str, password: str) -> List[Dict[str, Any]]:
    url = f"{base_url}/service/rest/v1/repositories"
    cp = run_curl(maybe_auth(curl_common(timeout_s), user, password) + [url])
    if cp.returncode != 0:
        raise RuntimeError(f"GET {url}: curl failed: {cp.stderr.strip()}")
    try:
        return json.loads(cp.stdout)
    except json.JSONDecodeError as e:
        raise RuntimeError(f"GET {url}: invalid JSON response") from e


def preflight(conf: Dict[str, Any]) -> None:
    src_url = f"{conf['source_url']}/service/rest/v1/repositories"
    dst_url = f"{conf['dest_url']}/service/rest/v1/repositories"
    log("info", "listing repositories", which="source", url=src_url)
    src_repos = get_repos(conf["source_url"], conf["timeout_s"], conf["source_user"], conf["source_pass"])
    log("info", "listing repositories", which="dest", url=dst_url)
    dst_repos = get_repos(conf["dest_url"], conf["timeout_s"], conf["dest_user"], conf["dest_pass"])

    src = next((r for r in src_repos if r.get("name") == conf["source_repo"]), None)
    if not src:
        raise RuntimeError(f"source repo {conf['source_repo']!r} not found in Nexus at {conf['source_url']}")
    if src.get("format") != "raw":
        raise RuntimeError(f"source repo {conf['source_repo']!r} format is {src.get('format')!r}, expected raw")

    dst = next((r for r in dst_repos if r.get("name") == conf["dest_repo"]), None)
    if not dst:
        raise RuntimeError(f"dest repo {conf['dest_repo']!r} not found in Nexus at {conf['dest_url']}")
    if dst.get("format") != "raw":
        raise RuntimeError(f"dest repo {conf['dest_repo']!r} format is {dst.get('format')!r}, expected raw")
    if dst.get("type") != "hosted":
        raise RuntimeError(f"dest repo {conf['dest_repo']!r} is type {dst.get('type')!r}; uploads require a hosted raw repo")


def load_policy_supported_versions(policy_file: str) -> Set[str]:
    with open(policy_file, "rb") as f:
        raw = json.loads(f.read().decode("utf-8"))
    if not isinstance(raw, list):
        raise RuntimeError(f"policy_file must be a JSON array: {policy_file}")
    approved: Set[str] = set()
    for e in raw:
        if not isinstance(e, dict):
            continue
        if e.get("supported") is True:
            v = str(e.get("version", "")).strip()
            if v:
                approved.add(v)
    return approved


def version_approved(version: str, approved: Set[str]) -> bool:
    v = (version or "").strip()
    if not v:
        return False
    for p in approved:
        if v == p or v.startswith(p + "."):
            return True
    return False


def fetch_index(index_url: str, timeout_s: int) -> List[Dict[str, Any]]:
    log("info", "fetching index.json", url=index_url)
    cp = run_curl(curl_common(timeout_s) + [index_url])
    if cp.returncode != 0:
        raise RuntimeError(f"fetch index.json: curl failed: {cp.stderr.strip()}")
    root = json.loads(cp.stdout)
    out: List[Dict[str, Any]] = []
    for key in ("linux", "mac", "windows"):
        for e in root.get(key, []) or []:
            dest_path = clean_path(str(e.get("binary", "")))
            source_path = clean_path(str(e.get("source_path", ""))) or dest_path
            if not source_path or not dest_path:
                continue
            out.append(
                {
                    "source_path": source_path,
                    "dest_path": dest_path,
                    "sha256": str(e.get("sha256", "")).strip(),
                    "platform": str(e.get("platform", "")).strip(),
                    "file_type": str(e.get("type", "")).strip(),
                    "version": str(e.get("version", "")).strip(),
                }
            )
    log("info", "index loaded", url=index_url, artifacts=len(out))
    return out


def head_status(url: str, timeout_s: int, user: str, password: str) -> Tuple[int, Optional[str]]:
    cmd = maybe_auth(curl_common(timeout_s), user, password) + ["-I", "-o", "/dev/null", "-w", "%{http_code}", url]
    cp = run_curl(cmd)
    if cp.returncode != 0:
        return 0, cp.stderr.strip() or "curl failed"
    try:
        return int(cp.stdout.strip()), None
    except ValueError:
        return 0, f"invalid status from curl: {cp.stdout!r}"


def select_missing(
    conf: Dict[str, Any], artifacts: List[Dict[str, Any]], limit_budget: int
) -> Tuple[List[Dict[str, Any]], int, int]:
    if conf["force"]:
        selected = artifacts if limit_budget == 0 else artifacts[:limit_budget]
        checked = len(artifacts) if limit_budget == 0 else len(selected)
        return selected, checked, 0

    selected: List[Dict[str, Any]] = []
    checked = 0
    skipped = 0
    for a in artifacts:
        durl = repo_asset_url(conf["dest_url"], conf["dest_repo"], a["dest_path"])
        code, err = head_status(durl, conf["timeout_s"], conf["dest_user"], conf["dest_pass"])
        checked += 1
        if err:
            log("warning", "HEAD failed; selecting anyway", url=durl, error=err)
            log("debug", "selected for download due to head error", dest_path=a["dest_path"], source_path=a["source_path"])
            selected.append(a)
        elif code == 200:
            skipped += 1
            log("debug", "skipped existing artifact", dest_path=a["dest_path"], head_status=code)
        elif code >= 500:
            log("warning", "HEAD 5xx; selecting anyway", url=durl, status=code)
            log("debug", "selected for download due to head 5xx", dest_path=a["dest_path"], head_status=code)
            selected.append(a)
        else:
            log("debug", "selected missing artifact", dest_path=a["dest_path"], head_status=code)
            selected.append(a)
        if limit_budget > 0 and len(selected) >= limit_budget:
            log("debug", "stopping selection due to limit budget", selected=len(selected), limit_budget=limit_budget)
            break
    return selected, checked, skipped


def download_to_temp(url: str, timeout_s: int, user: str, password: str) -> Tuple[str, int, str]:
    fd, tmp_path = tempfile.mkstemp(prefix="nexus-mirror-")
    os.close(fd)
    cmd = maybe_auth(curl_common(timeout_s), user, password) + ["-L", "-o", tmp_path, "-w", "%{http_code}", url]
    cp = run_curl(cmd)
    if cp.returncode != 0:
        try:
            os.remove(tmp_path)
        except OSError:
            pass
        raise RuntimeError(f"download: curl failed: {cp.stderr.strip()}")
    code = int((cp.stdout or "0").strip() or "0")
    if code != 200:
        try:
            os.remove(tmp_path)
        except OSError:
            pass
        raise RuntimeError(f"download: status={code}")

    h = hashlib.sha256()
    size = 0
    with open(tmp_path, "rb") as f:
        while True:
            b = f.read(64 * 1024)
            if not b:
                break
            h.update(b)
            size += len(b)
    return tmp_path, size, h.hexdigest()


def upload_file(conf: Dict[str, Any], local_path: str, dest_path: str) -> None:
    directory = posixpath.dirname(dest_path)
    if directory == ".":
        directory = ""
    filename = posixpath.basename(dest_path)
    api = f"{conf['dest_url']}/service/rest/v1/components?repository={conf['dest_repo']}"
    cmd = maybe_auth(curl_common(conf["timeout_s"]), conf["dest_user"], conf["dest_pass"]) + [
        "-X",
        "POST",
        "-F",
        f"raw.directory={directory}",
        "-F",
        f"raw.asset1.filename={filename}",
        "-F",
        f"raw.asset1=@{local_path};filename={filename}",
        "-o",
        "/dev/null",
        "-w",
        "%{http_code}",
        api,
    ]
    cp = run_curl(cmd)
    if cp.returncode != 0:
        raise RuntimeError(f"upload: curl failed: {cp.stderr.strip()}")
    code = int((cp.stdout or "0").strip() or "0")
    if code < 200 or code >= 300:
        raise RuntimeError(f"upload: status={code}")


def compact_item(d: Dict[str, Any]) -> Dict[str, Any]:
    out = dict(d)
    for k in list(out.keys()):
        v = out[k]
        if k in ("source_path", "dest_path"):
            continue
        if v in ("", None):
            out.pop(k, None)
        if k in ("bytes", "head_status") and v == 0:
            out.pop(k, None)
    return out


def run_one(conf: Dict[str, Any], a: Dict[str, Any]) -> Dict[str, Any]:
    purl = repo_asset_url(conf["source_url"], conf["source_repo"], a["source_path"])
    durl = repo_asset_url(conf["dest_url"], conf["dest_repo"], a["dest_path"])
    item: Dict[str, Any] = {
        "status": "",
        "message": "",
        "error": "",
        "source_path": a["source_path"],
        "dest_path": a["dest_path"],
        "proxy_url": purl,
        "destination_url": durl,
        "bytes": 0,
        "sha256_expected": a.get("sha256", ""),
        "sha256_actual": "",
        "head_status": 0,
        "platform": a.get("platform", ""),
        "file_type": a.get("file_type", ""),
        "version": a.get("version", ""),
    }

    if not conf["force"]:
        code, err = head_status(durl, conf["timeout_s"], conf["dest_user"], conf["dest_pass"])
        if not err:
            item["head_status"] = code
            if code == 200:
                item["status"] = "skipped_exists"
                item["message"] = "already exists"
                item.pop("error", None)
                log("debug", "skipped between selection and processing", dest_path=a["dest_path"], head_status=code)
                return compact_item(item)

    if conf["dry_run"]:
        item["status"] = "planned"
        item["message"] = "would download and upload"
        item.pop("error", None)
        log("debug", "dry-run planned item", dest_path=a["dest_path"], source_path=a["source_path"])
        return compact_item(item)

    tmp_path = ""
    try:
        log("info", "downloading via proxy", url=purl)
        tmp_path, size, sha = download_to_temp(purl, conf["timeout_s"], conf["source_user"], conf["source_pass"])
        item["bytes"] = size
        item["sha256_actual"] = sha
        expected = item["sha256_expected"]
        if expected and expected.lower() != sha.lower():
            item["status"] = "failed"
            item["message"] = "sha256 mismatch"
            item["error"] = f"expected {expected} got {sha}"
            log("debug", "sha256 mismatch", dest_path=a["dest_path"], expected=expected, actual=sha)
            return compact_item(item)
        log("info", "uploading", url=durl, file=posixpath.basename(a["dest_path"]))
        upload_file(conf, tmp_path, a["dest_path"])
        item["status"] = "uploaded"
        item["message"] = "uploaded successfully"
        item.pop("error", None)
        return compact_item(item)
    except Exception as e:  # noqa: BLE001
        item["status"] = "failed"
        msg = str(e)
        item["message"] = "upload failed" if msg.startswith("upload:") else "download failed"
        item["error"] = msg
        return compact_item(item)
    finally:
        if tmp_path:
            try:
                os.remove(tmp_path)
            except OSError:
                pass


def emit(res: Dict[str, Any], started: float, exit_code: int) -> int:
    res["duration_ms"] = int((time.time() - started) * 1000)
    json.dump(res, sys.stdout, indent=2, ensure_ascii=True)
    sys.stdout.write("\n")
    return exit_code


def main(argv: List[str]) -> int:
    started = time.time()
    parser = argparse.ArgumentParser(prog="nexus-mirror", add_help=True)
    parser.add_argument("--config", default=None, help="optional config path")
    args = parser.parse_args(argv)

    conf = make_conf(read_config(resolve_config_path(args.config)))
    set_log_level(conf.get("log_level", "info"))
    index_urls = [x["index_url"] for x in conf["indexes"]]
    remaining = conf["limit"]

    res: Dict[str, Any] = {
        "status": "",
        "message": "",
        "dry_run": conf["dry_run"],
        "forced": conf["force"],
        "source_url": conf["source_url"],
        "source_repo": conf["source_repo"],
        "dest_url": conf["dest_url"],
        "dest_repo": conf["dest_repo"],
        "index_url": index_urls[0] if index_urls else "",
        "index_urls": index_urls,
        "index_total": 0,
        "checked": 0,
        "selected": 0,
        "skipped_exists": 0,
        "planned": 0,
        "uploaded": 0,
        "failed": 0,
        "indexes": [],
        "indexes_by_key": {},
        "duration_ms": 0,
    }

    try:
        preflight(conf)
        all_item_results: List[Dict[str, Any]] = []

        for pair in conf["indexes"]:
            index_key = pair["key"]
            index_url = pair["index_url"]
            policy_file = pair["policy_file"]
            approved = load_policy_supported_versions(policy_file)
            log("debug", "loaded policy approvals", key=index_key, policy_file=policy_file, approved_versions=len(approved))

            artifacts = fetch_index(index_url, conf["timeout_s"])
            approved_artifacts = []
            for a in artifacts:
                if not version_approved(a.get("version", ""), approved):
                    continue
                b = dict(a)
                original_dest = b["dest_path"]
                b["dest_path"] = rewrite_dest_case(original_dest, conf["platform_case_map"])
                if b["dest_path"] != original_dest:
                    log(
                        "debug",
                        "rewrote destination path case",
                        key=index_key,
                        original_dest_path=original_dest,
                        mapped_dest_path=b["dest_path"],
                    )
                approved_artifacts.append(b)
            rejected = len(artifacts) - len(approved_artifacts)
            log(
                "debug",
                "applied policy filter",
                key=index_key,
                index_url=index_url,
                total=len(artifacts),
                approved=len(approved_artifacts),
                rejected=rejected,
            )

            budget = 0 if conf["limit"] == 0 else remaining
            selected, checked, skipped = select_missing(conf, approved_artifacts, budget)

            per_index = {
                "key": index_key,
                "index_url": index_url,
                "policy_file": policy_file,
                "index_total": len(artifacts),
                "approved_total": len(approved_artifacts),
                "rejected_by_policy": rejected,
                "checked": checked,
                "selected": len(selected),
                "skipped_exists": skipped,
                "planned": 0,
                "uploaded": 0,
                "failed": 0,
            }

            for a in selected:
                item = run_one(conf, a)
                all_item_results.append(item)
                st = item.get("status")
                if st == "planned":
                    per_index["planned"] += 1
                    res["planned"] += 1
                elif st == "uploaded":
                    per_index["uploaded"] += 1
                    res["uploaded"] += 1
                elif st == "failed":
                    per_index["failed"] += 1
                    res["failed"] += 1
                elif st == "skipped_exists":
                    per_index["skipped_exists"] += 1
                    res["skipped_exists"] += 1

            res["indexes"].append(per_index)
            res["indexes_by_key"][index_key] = per_index
            res["index_total"] += len(artifacts)
            res["checked"] += checked
            res["selected"] += len(selected)
            res["skipped_exists"] += skipped

            if conf["limit"] > 0:
                remaining -= len(selected)
                if remaining <= 0:
                    break

        res["items"] = all_item_results

        if res["selected"] == 0:
            res["status"] = "success"
            res["message"] = "no missing items"
        elif res["failed"] > 0:
            res["status"] = "partial_success"
            res["message"] = "some items failed"
        else:
            res["status"] = "success"
            res["message"] = "all items processed"

        return emit(res, started, 0)
    except Exception as e:  # noqa: BLE001
        log("error", "failed", error=str(e))
        res["status"] = "failed"
        res["message"] = str(e)
        return emit(res, started, 1)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))


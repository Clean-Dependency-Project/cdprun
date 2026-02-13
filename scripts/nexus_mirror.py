#!/usr/bin/env python3
"""
nexus_mirror.py mirrors artifacts from a Nexus raw proxy repo into a Nexus raw hosted repo.

Logs: JSON to stderr. Output: JSON to stdout.
No external dependencies.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import logging
import os
import posixpath
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from http.client import HTTPConnection, HTTPSConnection
from typing import Dict, Iterable, List, Optional, Tuple


USER_AGENT = "cdprun-nexus-mirror/0.1"


def _parse_duration(raw: str) -> float:
    s = (raw or "").strip().lower()
    if not s:
        raise ValueError("empty duration")
    mul = 1.0
    if s.endswith("ms"):
        mul = 0.001
        s = s[:-2]
    elif s.endswith("s"):
        mul = 1.0
        s = s[:-1]
    elif s.endswith("m"):
        mul = 60.0
        s = s[:-1]
    elif s.endswith("h"):
        mul = 3600.0
        s = s[:-1]
    return float(s) * mul


def _basic_auth_header(user: str, password: str) -> str:
    token = base64.b64encode(f"{user}:{password}".encode("utf-8")).decode("ascii")
    return f"Basic {token}"


class JsonLogFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: Dict[str, object] = {
            "timestamp": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(),
            "level": record.levelname.lower(),
            "msg": record.getMessage(),
        }
        fields = getattr(record, "fields", None)
        if isinstance(fields, dict):
            payload.update(fields)
        return json.dumps(payload, separators=(",", ":"), ensure_ascii=True)


def new_logger(level: str) -> logging.Logger:
    logger = logging.getLogger("nexus-mirror")
    logger.setLevel(logging.DEBUG)
    logger.handlers[:] = []
    handler = logging.StreamHandler(sys.stderr)
    handler.setLevel(_log_level(level))
    handler.setFormatter(JsonLogFormatter())
    logger.addHandler(handler)
    logger.propagate = False
    return logger


def _log_level(s: str) -> int:
    v = (s or "").strip().lower()
    if v == "debug":
        return logging.DEBUG
    if v in ("warn", "warning"):
        return logging.WARNING
    if v == "error":
        return logging.ERROR
    return logging.INFO


def log(logger: logging.Logger, level: int, msg: str, **fields: object) -> None:
    logger.log(level, msg, extra={"fields": fields})


def clean_path(p: str) -> str:
    p = (p or "").strip()
    if p.startswith("/"):
        p = p[1:]
    p = posixpath.normpath(p)
    return "" if p == "." else p


@dataclass(frozen=True)
class Config:
    source_url: str
    source_repo: str
    source_user: str
    source_password: str
    dest_url: str
    dest_repo: str
    dest_user: str
    dest_password: str
    index_url: str
    index_file: str
    limit: int
    dry_run: bool
    force: bool
    include_items: bool
    timeout_s: float
    log_level: str


def _load_config_file(path: str) -> Dict[str, object]:
    p = (path or "").strip()
    if not p:
        return {}
    with open(p, "rb") as f:
        return json.loads(f.read().decode("utf-8"))

def parse_args(argv: List[str]) -> Config:
    p = argparse.ArgumentParser(prog="nexus-mirror", add_help=True)
    p.add_argument("--config", default=None, help="path to JSON config file (optional)")

    a = p.parse_args(argv)
    config_path = resolve_config_path(a.config)
    raw = _load_config_file(config_path)
    return config_from_file(raw)


def resolve_config_path(flag_value: Optional[str]) -> str:
    if flag_value and flag_value.strip():
        return flag_value.strip()
    env = (os.getenv("NEXUS_MIRROR_CONFIG") or "").strip()
    if env:
        return env
    for candidate in ("config/nexus_mirror.json", "nexus-mirror.json"):
        if os.path.exists(candidate):
            return candidate
    raise SystemExit(
        "config file not specified; set NEXUS_MIRROR_CONFIG, pass --config, "
        "or create config/nexus_mirror.json"
    )


def _get_str(cfg: Dict[str, object], key: str, required: bool = False, default: str = "") -> str:
    v = cfg.get(key)
    if isinstance(v, str):
        s = v.strip()
        if s:
            return s
    if required:
        raise SystemExit(f"config missing {key}")
    return default


def _get_bool(cfg: Dict[str, object], key: str, default: bool) -> bool:
    v = cfg.get(key)
    return v if isinstance(v, bool) else default


def _get_int(cfg: Dict[str, object], key: str, default: int) -> int:
    v = cfg.get(key)
    return v if isinstance(v, int) else default


def config_from_file(cfg: Dict[str, object]) -> Config:
    index_url = _get_str(cfg, "index_url")
    index_file = _get_str(cfg, "index_file")
    if not index_url and not index_file:
        raise SystemExit("config must set either index_url or index_file")

    timeout_raw = _get_str(cfg, "timeout", default="10m")
    try:
        timeout_s = _parse_duration(timeout_raw)
    except ValueError as e:
        raise SystemExit(f"invalid config timeout: {e}") from e

    limit_val = _get_int(cfg, "limit", 3)
    limit = limit_val if limit_val > 0 else 0

    return Config(
        source_url=_normalize_base_url(_get_str(cfg, "source_url", required=True), "source_url"),
        source_repo=_require_repo(_get_str(cfg, "source_repo", required=True), "source_repo"),
        source_user=_get_str(cfg, "source_user", default=""),
        source_password=_get_str(cfg, "source_password", default=""),
        dest_url=_normalize_base_url(_get_str(cfg, "dest_url", required=True), "dest_url"),
        dest_repo=_require_repo(_get_str(cfg, "dest_repo", required=True), "dest_repo"),
        dest_user=_get_str(cfg, "dest_user", default=""),
        dest_password=_get_str(cfg, "dest_password", default=""),
        index_url=index_url,
        index_file=index_file,
        limit=limit,
        dry_run=_get_bool(cfg, "dry_run", False),
        force=_get_bool(cfg, "force", False),
        include_items=_get_bool(cfg, "include_items", True),
        timeout_s=timeout_s,
        log_level=_get_str(cfg, "log_level", default="info"),
    )


def _normalize_base_url(raw: str, name: str) -> str:
    s = (raw or "").strip()
    if not s:
        raise SystemExit(f"config missing {name}")
    u = urllib.parse.urlparse(s)
    if not u.scheme or not u.netloc:
        raise SystemExit(f"{name} must include scheme and host: {s!r}")
    u = u._replace(path=u.path.rstrip("/"), query="", fragment="")
    return urllib.parse.urlunparse(u)


def _require_repo(name: str, flag: str) -> str:
    s = (name or "").strip()
    if not s:
        raise SystemExit(f"config missing {flag}")
    if "/" in s or ":" in s:
        raise SystemExit(f"{flag} must be a Nexus repo name (no '/' or ':'): {s!r}")
    return s


def build_repo_asset_url(base: str, repo: str, p: str) -> str:
    return f"{base}/repository/{urllib.parse.quote(repo)}/{p}"


def request(method: str, url: str, timeout_s: float, auth: Optional[Tuple[str, str]] = None) -> urllib.request.Request:
    req = urllib.request.Request(url=url, method=method)
    req.add_header("User-Agent", USER_AGENT)
    if auth and auth[0] and auth[1]:
        req.add_header("Authorization", _basic_auth_header(auth[0], auth[1]))
    return req


def head_status(url: str, timeout_s: float, auth: Optional[Tuple[str, str]]) -> Tuple[int, Optional[str]]:
    try:
        with urllib.request.urlopen(request("HEAD", url, timeout_s, auth), timeout=timeout_s) as resp:
            return int(resp.status), None
    except urllib.error.HTTPError as e:
        return int(e.code), None
    except Exception as e:  # noqa: BLE001 - show in logs/output
        return 0, str(e)


def get_bytes(url: str, timeout_s: float, auth: Optional[Tuple[str, str]]) -> bytes:
    try:
        with urllib.request.urlopen(request("GET", url, timeout_s, auth), timeout=timeout_s) as resp:
            body = resp.read(4 * 1024 * 1024)
            if int(resp.status) != 200:
                raise RuntimeError(f"GET {url}: status={resp.status} body={body.strip()!r}")
            return body
    except urllib.error.HTTPError as e:
        body = e.read(4 * 1024 * 1024)
        raise RuntimeError(f"GET {url}: status={e.code} body={body.strip()!r}") from e


def list_repos(conf: Config, logger: logging.Logger, which: str) -> List[dict]:
    if which == "source":
        base, auth = conf.source_url, (conf.source_user, conf.source_password)
    else:
        base, auth = conf.dest_url, (conf.dest_user, conf.dest_password)

    url = f"{base}/service/rest/v1/repositories"
    log(logger, logging.INFO, "listing repositories", url=url, which=which)
    body = get_bytes(url, conf.timeout_s, auth if auth[0] and auth[1] else None)
    return json.loads(body.decode("utf-8"))


def preflight(conf: Config, logger: logging.Logger) -> None:
    src_repos = list_repos(conf, logger, "source")
    dst_repos = list_repos(conf, logger, "dest")

    src = next((r for r in src_repos if r.get("name") == conf.source_repo), None)
    if not src:
        raise RuntimeError(f"source repo {conf.source_repo!r} not found in Nexus at {conf.source_url}")
    if src.get("format") != "raw":
        raise RuntimeError(f"source repo {conf.source_repo!r} format is {src.get('format')!r}, expected raw")

    dst = next((r for r in dst_repos if r.get("name") == conf.dest_repo), None)
    if not dst:
        raise RuntimeError(f"dest repo {conf.dest_repo!r} not found in Nexus at {conf.dest_url}")
    if dst.get("format") != "raw":
        raise RuntimeError(f"dest repo {conf.dest_repo!r} format is {dst.get('format')!r}, expected raw")
    if dst.get("type") != "hosted":
        raise RuntimeError(f"dest repo {conf.dest_repo!r} is type {dst.get('type')!r}; uploads require a hosted raw repo")


def fetch_index(conf: Config, logger: logging.Logger) -> List[dict]:
    if conf.index_file:
        log(logger, logging.INFO, "reading index.json", file=conf.index_file)
        with open(conf.index_file, "rb") as f:
            body = f.read()
    else:
        log(logger, logging.INFO, "fetching index.json", url=conf.index_url)
        body = get_bytes(conf.index_url, conf.timeout_s, auth=None)
    root = json.loads(body.decode("utf-8"))

    out: List[dict] = []
    for key in ("linux", "mac", "windows"):
        for entry in root.get(key, []) or []:
            dest_path = clean_path(entry.get("binary", ""))
            source_path = clean_path(entry.get("source_path", "")) or dest_path
            if not dest_path or not source_path:
                continue
            out.append(
                {
                    "source_path": source_path,
                    "dest_path": dest_path,
                    "sha256": (entry.get("sha256") or "").strip(),
                    "platform": (entry.get("platform") or "").strip(),
                    "file_type": (entry.get("type") or "").strip(),
                    "version": (entry.get("version") or "").strip(),
                }
            )
    log(logger, logging.INFO, "index loaded", artifacts=len(out))
    return out


def select_missing(conf: Config, logger: logging.Logger, artifacts: List[dict]) -> Tuple[List[dict], int, int]:
    if conf.force:
        selected = artifacts if conf.limit == 0 else artifacts[: conf.limit]
        checked = len(selected) if conf.limit else len(artifacts)
        return selected, checked, 0

    selected: List[dict] = []
    checked = 0
    skipped = 0
    for a in artifacts:
        dest_url = build_repo_asset_url(conf.dest_url, conf.dest_repo, a["dest_path"])
        auth = (conf.dest_user, conf.dest_password) if conf.dest_user and conf.dest_password else None
        code, err = head_status(dest_url, conf.timeout_s, auth)
        checked += 1

        if err:
            log(logger, logging.WARNING, "HEAD failed; selecting anyway", url=dest_url, error=err)
            selected.append(a)
        elif code == 200:
            skipped += 1
        elif code >= 500:
            log(logger, logging.WARNING, "HEAD 5xx; selecting anyway", url=dest_url, status=code)
            selected.append(a)
        else:
            selected.append(a)

        if conf.limit and len(selected) >= conf.limit:
            break
    return selected, checked, skipped


def download_to_temp(url: str, timeout_s: float, auth: Tuple[str, str], logger: logging.Logger) -> Tuple[str, int, str]:
    log(logger, logging.INFO, "downloading via proxy", url=url)
    h = hashlib.sha256()
    fd, tmp_path = tempfile.mkstemp(prefix="nexus-mirror-")
    size = 0
    try:
        with os.fdopen(fd, "wb") as f:
            with urllib.request.urlopen(request("GET", url, timeout_s, auth), timeout=timeout_s) as resp:
                if int(resp.status) != 200:
                    body = resp.read(8 * 1024)
                    raise RuntimeError(f"download: status={resp.status} body={body.strip()!r}")
                while True:
                    chunk = resp.read(64 * 1024)
                    if not chunk:
                        break
                    f.write(chunk)
                    h.update(chunk)
                    size += len(chunk)
        sha = h.hexdigest()
        log(logger, logging.INFO, "downloaded", bytes=size, sha256=sha)
        return tmp_path, size, sha
    except urllib.error.HTTPError as e:
        body = e.read(8 * 1024)
        raise RuntimeError(f"download: status={e.code} body={body.strip()!r}") from e
    except Exception:
        try:
            os.remove(tmp_path)
        except OSError:
            pass
        raise


def _multipart_iter(boundary: str, directory: str, filename: str, fileobj) -> Iterable[bytes]:
    b = boundary
    crlf = b"\r\n"

    def field(name: str, value: str) -> Iterable[bytes]:
        yield f"--{b}\r\n".encode("ascii")
        yield f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode("ascii")
        yield value.encode("utf-8")
        yield crlf

    yield from field("raw.directory", directory)
    yield from field("raw.asset1.filename", filename)

    yield f"--{b}\r\n".encode("ascii")
    yield f'Content-Disposition: form-data; name="raw.asset1"; filename="{filename}"\r\n'.encode("ascii")
    yield b"Content-Type: application/octet-stream\r\n\r\n"
    while True:
        chunk = fileobj.read(64 * 1024)
        if not chunk:
            break
        yield chunk
    yield crlf
    yield f"--{b}--\r\n".encode("ascii")


def upload_artifact(conf: Config, file_path: str, dest_path: str, logger: logging.Logger) -> None:
    directory = posixpath.dirname(dest_path)
    if directory == ".":
        directory = ""
    filename = posixpath.basename(dest_path)

    u = urllib.parse.urlparse(conf.dest_url)
    conn_cls = HTTPSConnection if u.scheme == "https" else HTTPConnection
    conn = conn_cls(u.netloc, timeout=conf.timeout_s)

    api_path = f"/service/rest/v1/components?repository={urllib.parse.quote(conf.dest_repo)}"
    boundary = "nexus_mirror_" + os.urandom(8).hex()

    headers = {
        "User-Agent": USER_AGENT,
        "Content-Type": f"multipart/form-data; boundary={boundary}",
    }
    if conf.dest_user and conf.dest_password:
        headers["Authorization"] = _basic_auth_header(conf.dest_user, conf.dest_password)

    log(logger, logging.INFO, "uploading", url=f"{conf.dest_url}{api_path}", dir=directory, file=filename)
    with open(file_path, "rb") as f:
        body_iter = _multipart_iter(boundary, directory, filename, f)
        conn.request("POST", api_path, body=body_iter, headers=headers, encode_chunked=True)
        resp = conn.getresponse()
        try:
            if resp.status < 200 or resp.status >= 300:
                body = resp.read(16 * 1024)
                raise RuntimeError(f"upload: status={resp.status} body={body.strip()!r}")
        finally:
            resp.read()  # drain
            conn.close()


def run_one(conf: Config, logger: logging.Logger, a: dict) -> dict:
    proxy_url = build_repo_asset_url(conf.source_url, conf.source_repo, a["source_path"])
    dest_url = build_repo_asset_url(conf.dest_url, conf.dest_repo, a["dest_path"])

    res = {
        "status": "",
        "message": "",
        "error": "",
        "source_path": a["source_path"],
        "dest_path": a["dest_path"],
        "proxy_url": proxy_url,
        "destination_url": dest_url,
        "bytes": 0,
        "sha256_expected": a.get("sha256", ""),
        "sha256_actual": "",
        "head_status": 0,
        "platform": a.get("platform", ""),
        "file_type": a.get("file_type", ""),
        "version": a.get("version", ""),
    }

    if not conf.force:
        auth = (conf.dest_user, conf.dest_password) if conf.dest_user and conf.dest_password else None
        code, err = head_status(dest_url, conf.timeout_s, auth)
        if not err:
            res["head_status"] = code
            if code == 200:
                res["status"] = "skipped_exists"
                res["message"] = "already exists"
                res.pop("error", None)
                return _strip_empty(res)

    if conf.dry_run:
        res["status"] = "planned"
        res["message"] = "would download and upload"
        res.pop("error", None)
        return _strip_empty(res)

    tmp_path = ""
    try:
        src_auth = (conf.source_user, conf.source_password) if conf.source_user and conf.source_password else ("", "")
        tmp_path, size, sha = download_to_temp(
            proxy_url, conf.timeout_s, src_auth, logger
        )
        res["bytes"] = size
        res["sha256_actual"] = sha
        expected = (a.get("sha256") or "").strip()
        if expected and expected.lower() != sha.lower():
            res["status"] = "failed"
            res["message"] = "sha256 mismatch"
            res["error"] = f"expected {expected} got {sha}"
            return _strip_empty(res)

        upload_artifact(conf, tmp_path, a["dest_path"], logger)
        res["status"] = "uploaded"
        res["message"] = "uploaded successfully"
        res.pop("error", None)
        return _strip_empty(res)
    except Exception as e:  # noqa: BLE001 - surface in JSON output
        res["status"] = "failed"
        res["message"] = "download failed" if "download" in str(e).lower() else "upload failed"
        res["error"] = str(e)
        return _strip_empty(res)
    finally:
        if tmp_path:
            try:
                os.remove(tmp_path)
            except OSError:
                pass


def _strip_empty(d: dict) -> dict:
    out = dict(d)
    for k in list(out.keys()):
        v = out[k]
        if v == "" or v == 0 or v is None:
            if k in ("source_path", "dest_path"):
                continue
            if k in ("bytes", "head_status") and v == 0:
                out.pop(k, None)
            elif v == "":
                out.pop(k, None)
    return out


def main(argv: List[str]) -> int:
    start = time.time()
    conf = parse_args(argv)
    logger = new_logger(conf.log_level)

    idx = conf.index_file or conf.index_url
    result = {
        "status": "",
        "message": "",
        "dry_run": conf.dry_run,
        "forced": conf.force,
        "source_url": conf.source_url,
        "source_repo": conf.source_repo,
        "dest_url": conf.dest_url,
        "dest_repo": conf.dest_repo,
        "index_url": idx,
        "index_total": 0,
        "checked": 0,
        "selected": 0,
        "skipped_exists": 0,
        "planned": 0,
        "uploaded": 0,
        "failed": 0,
        "duration_ms": 0,
    }

    try:
        preflight(conf, logger)
        artifacts = fetch_index(conf, logger)
        selected, checked, skipped = select_missing(conf, logger, artifacts)

        result["index_total"] = len(artifacts)
        result["checked"] = checked
        result["skipped_exists"] = skipped
        result["selected"] = len(selected)

        if not selected:
            result["status"] = "success"
            result["message"] = "no missing items"
            return _emit(result, start)

        items: List[dict] = []
        for a in selected:
            r = run_one(conf, logger, a)
            items.append(r)
            st = r.get("status")
            if st == "planned":
                result["planned"] += 1
            elif st == "uploaded":
                result["uploaded"] += 1
            elif st == "failed":
                result["failed"] += 1
            elif st == "skipped_exists":
                result["skipped_exists"] += 1

        if conf.include_items:
            result["items"] = items

        if result["failed"] > 0:
            result["status"] = "partial_success"
            result["message"] = "some items failed"
        else:
            result["status"] = "success"
            result["message"] = "all items processed"
        return _emit(result, start)
    except Exception as e:  # noqa: BLE001
        log(logger, logging.ERROR, "failed", error=str(e))
        result["status"] = "failed"
        result["message"] = str(e)
        return _emit(result, start, exit_code=1)


def _emit(result: dict, start: float, exit_code: int = 0) -> int:
    result["duration_ms"] = int((time.time() - start) * 1000)
    json.dump(result, sys.stdout, indent=2, ensure_ascii=True)
    sys.stdout.write("\n")
    sys.stdout.flush()
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))


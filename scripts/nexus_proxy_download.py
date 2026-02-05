#!/usr/bin/env python3
"""
Nexus Proxy Download Script

Downloads runtime binaries (Python, Node.js) through Nexus proxy repositories.
Uses HEAD requests to check if files are already cached, downloads through
the proxy to populate the cache if needed, and verifies SHA256 checksums.

Uses only Python standard library (no external dependencies).
"""

import argparse
import base64
import hashlib
import json
import logging
import os
import sys
import tempfile
import urllib.request
import urllib.error
from datetime import datetime, timezone
from typing import Any


def build_auth_header(username: str, password: str) -> str | None:
    """Build Basic Auth header value if credentials are provided."""
    if username and password:
        credentials = f"{username}:{password}"
        encoded = base64.b64encode(credentials.encode("utf-8")).decode("utf-8")
        return f"Basic {encoded}"
    return None


class JsonFormatter(logging.Formatter):
    """Format log records as JSON for structured logging."""

    def format(self, record: logging.LogRecord) -> str:
        log_entry = {
            "timestamp": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "level": record.levelname,
            "message": record.getMessage(),
            "module": record.module,
        }
        if record.exc_info:
            log_entry["exception"] = self.formatException(record.exc_info)
        return json.dumps(log_entry)


def setup_logging() -> logging.Logger:
    """Configure logging to output JSON to stderr."""
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(JsonFormatter())
    logger = logging.getLogger(__name__)
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    return logger


logger = setup_logging()


def load_config(config_path: str) -> dict[str, Any]:
    """Load configuration from JSON file."""
    try:
        with open(config_path, "r") as f:
            return json.load(f)
    except FileNotFoundError:
        logger.error(f"Config file not found: {config_path}")
        sys.exit(1)
    except json.JSONDecodeError as e:
        logger.error(f"Invalid JSON in config file {config_path}: {e}")
        sys.exit(1)


def load_policy(policy_file: str) -> list[dict[str, Any]]:
    """Load policy file."""
    try:
        with open(policy_file, "r") as f:
            return json.load(f)
    except FileNotFoundError:
        logger.error(f"Policy file not found: {policy_file}")
        sys.exit(1)
    except json.JSONDecodeError as e:
        logger.error(f"Invalid JSON in policy file {policy_file}: {e}")
        sys.exit(1)


def is_approved(version: str, policy_data: list[dict[str, Any]]) -> bool:
    """Check if a version is approved by policy."""
    # Extract major.minor version for Python (e.g., "3.12" from "3.12.12")
    # or major version for Node.js (e.g., "20" from "20.20.0")
    parts = version.split(".")
    major_version = parts[0]
    major_minor_version = f"{parts[0]}.{parts[1]}" if len(parts) > 1 else major_version

    for entry in policy_data:
        policy_version = entry.get("version", "")
        # Match either major or major.minor
        if policy_version == major_version or policy_version == major_minor_version:
            return (
                entry.get("recommended", False)
                or entry.get("supported", False)
                or entry.get("under_review", False)
            )
    return False


def fetch_index(index_url: str) -> dict[str, Any]:
    """Fetch index.json from upstream."""
    try:
        logger.info(f"Fetching index from {index_url}")
        req = urllib.request.Request(index_url)
        req.add_header("User-Agent", "nexus-proxy-download/1.0")
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.URLError as e:
        logger.error(f"Failed to fetch index from {index_url}: {e}")
        sys.exit(1)
    except json.JSONDecodeError as e:
        logger.error(f"Invalid JSON in index from {index_url}: {e}")
        sys.exit(1)


def check_exists_in_nexus(nexus_url: str, auth_header: str | None = None) -> bool:
    """Check if a file exists in Nexus using HEAD request."""
    try:
        req = urllib.request.Request(nexus_url, method="HEAD")
        req.add_header("User-Agent", "nexus-proxy-download/1.0")
        if auth_header:
            req.add_header("Authorization", auth_header)
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.status == 200
    except urllib.error.HTTPError as e:
        if e.code == 404:
            return False
        logger.warning(f"HEAD request failed for {nexus_url}: HTTP {e.code}")
        return False
    except urllib.error.URLError as e:
        logger.warning(f"HEAD request failed for {nexus_url}: {e}")
        return False


def download_through_proxy(nexus_url: str, target_path: str, auth_header: str | None = None) -> bool:
    """Download a file through the Nexus proxy."""
    try:
        os.makedirs(os.path.dirname(target_path), exist_ok=True)
        req = urllib.request.Request(nexus_url)
        req.add_header("User-Agent", "nexus-proxy-download/1.0")
        if auth_header:
            req.add_header("Authorization", auth_header)
        with urllib.request.urlopen(req, timeout=300) as response:
            with open(target_path, "wb") as f:
                while True:
                    chunk = response.read(8192)
                    if not chunk:
                        break
                    f.write(chunk)
        return True
    except urllib.error.URLError as e:
        logger.error(f"Failed to download {nexus_url}: {e}")
        return False
    except IOError as e:
        logger.error(f"Failed to write file {target_path}: {e}")
        return False


def verify_sha256(file_path: str, expected_sha256: str) -> bool:
    """Verify the SHA256 checksum of a file."""
    sha256_hash = hashlib.sha256()
    try:
        with open(file_path, "rb") as f:
            for byte_block in iter(lambda: f.read(8192), b""):
                sha256_hash.update(byte_block)
        actual_sha256 = sha256_hash.hexdigest()
        return actual_sha256.lower() == expected_sha256.lower()
    except IOError as e:
        logger.error(f"Failed to read file for SHA256 verification: {e}")
        return False


def build_nexus_url(nexus_base: str, repository: str, binary_path: str) -> str:
    """Build the full Nexus proxy URL for a binary."""
    return f"{nexus_base.rstrip('/')}/repository/{repository}/{binary_path}"


def get_nexus_auth(config: dict[str, Any]) -> str | None:
    """Get Nexus authentication header from config and environment."""
    username = config.get("nexus_username", "")
    password_env = config.get("nexus_password_env", "")
    
    if not username or not password_env:
        return None
    
    password = os.environ.get(password_env, "")
    if not password:
        logger.warning(f"Environment variable {password_env} not set, proceeding without authentication")
        return None
    
    return build_auth_header(username, password)


def process_runtime(
    runtime_name: str,
    runtime_config: dict[str, Any],
    config: dict[str, Any],
    temp_dir: str,
    dry_run: bool,
    auth_header: str | None = None,
) -> dict[str, list]:
    """Process a single runtime, downloading all approved binaries."""
    result = {"downloaded": [], "skipped": [], "cached": [], "failed": []}

    # Load policy
    policy_file = runtime_config["policy_file"]
    policy_data = load_policy(policy_file)

    # Fetch index
    index_url = f"{config['index_base_url'].rstrip('/')}/{runtime_config['index_path']}"
    index_data = fetch_index(index_url)

    nexus_base = config["nexus_url"]
    repository = runtime_config["repository"]

    platforms = ["linux", "mac", "windows"]

    for platform in platforms:
        if platform not in index_data:
            continue

        for item in index_data[platform]:
            version = item.get("version", "")
            binary_path = item.get("binary", "")
            expected_sha256 = item.get("sha256", "")
            item_platform = item.get("platform", platform)

            if not binary_path:
                continue

            if not is_approved(version, policy_data):
                logger.info(f"Skipping {runtime_name} {version} - not approved by policy")
                result["skipped"].append({
                    "runtime": runtime_name,
                    "platform": item_platform,
                    "version": version,
                    "reason": "not_approved",
                })
                continue

            nexus_url = build_nexus_url(nexus_base, repository, binary_path)
            filename = os.path.basename(binary_path)

            # Check if already cached in Nexus
            if check_exists_in_nexus(nexus_url, auth_header):
                logger.info(f"Already cached in Nexus: {filename}")
                result["cached"].append({
                    "runtime": runtime_name,
                    "platform": item_platform,
                    "version": version,
                    "file": filename,
                })
                continue

            if dry_run:
                logger.info(f"[DRY-RUN] Would download: {filename}")
                result["downloaded"].append({
                    "runtime": runtime_name,
                    "platform": item_platform,
                    "version": version,
                    "file": filename,
                    "dry_run": True,
                })
                continue

            # Download through proxy
            target_path = os.path.join(temp_dir, runtime_name, platform, filename)
            logger.info(f"Downloading {filename} for {platform}...")

            if download_through_proxy(nexus_url, target_path, auth_header):
                # Verify SHA256 if provided
                if expected_sha256:
                    if verify_sha256(target_path, expected_sha256):
                        logger.info(f"Successfully verified {filename}")
                        result["downloaded"].append({
                            "runtime": runtime_name,
                            "platform": item_platform,
                            "version": version,
                            "file": filename,
                            "sha256_verified": True,
                        })
                    else:
                        logger.error(f"SHA256 mismatch for {filename}")
                        result["failed"].append({
                            "runtime": runtime_name,
                            "platform": item_platform,
                            "version": version,
                            "file": filename,
                            "error": "SHA256 mismatch",
                        })
                else:
                    logger.info(f"Downloaded {filename} (no SHA256 provided)")
                    result["downloaded"].append({
                        "runtime": runtime_name,
                        "platform": item_platform,
                        "version": version,
                        "file": filename,
                        "sha256_verified": False,
                    })
            else:
                result["failed"].append({
                    "runtime": runtime_name,
                    "platform": item_platform,
                    "version": version,
                    "file": filename,
                    "error": "Download failed",
                })

    return result


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Download runtime binaries through Nexus proxy repositories"
    )
    parser.add_argument(
        "--config",
        default="config/nexus_download.json",
        help="Path to configuration file (default: config/nexus_download.json)",
    )
    parser.add_argument(
        "--runtime",
        choices=["python", "nodejs"],
        help="Download only specific runtime (default: all)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Check what would be downloaded without actually downloading",
    )
    parser.add_argument(
        "--output-format",
        choices=["json", "text"],
        default="json",
        help="Output format for summary (default: json)",
    )

    args = parser.parse_args()

    # Load configuration
    config = load_config(args.config)

    # Get authentication header if configured
    auth_header = get_nexus_auth(config)

    # Create temporary directory for downloads
    temp_dir = tempfile.mkdtemp(prefix="nexus-proxy-download-")
    logger.info(f"Using temporary directory: {temp_dir}")

    # Determine which runtimes to process
    runtimes_to_process = config.get("runtimes", {})
    if args.runtime:
        if args.runtime in runtimes_to_process:
            runtimes_to_process = {args.runtime: runtimes_to_process[args.runtime]}
        else:
            logger.error(f"Runtime '{args.runtime}' not found in configuration")
            sys.exit(1)

    # Process each runtime
    summary = {
        "temp_dir": temp_dir,
        "dry_run": args.dry_run,
        "downloaded": [],
        "cached": [],
        "skipped": [],
        "failed": [],
        "status": "success",
    }

    for runtime_name, runtime_config in runtimes_to_process.items():
        logger.info(f"Processing runtime: {runtime_name}")
        result = process_runtime(
            runtime_name, runtime_config, config, temp_dir, args.dry_run, auth_header
        )

        summary["downloaded"].extend(result["downloaded"])
        summary["cached"].extend(result["cached"])
        summary["skipped"].extend(result["skipped"])
        summary["failed"].extend(result["failed"])

    # Determine overall status
    if summary["failed"]:
        summary["status"] = "partial_success" if summary["downloaded"] else "failed"

    # Output summary
    if args.output_format == "json":
        print(json.dumps(summary, indent=2))
    else:
        print(f"\nDownload Summary:")
        print(f"  Temp directory: {temp_dir}")
        print(f"  Downloaded: {len(summary['downloaded'])}")
        print(f"  Cached: {len(summary['cached'])}")
        print(f"  Skipped: {len(summary['skipped'])}")
        print(f"  Failed: {len(summary['failed'])}")
        print(f"  Status: {summary['status']}")

    # Exit with error code if there were failures
    if summary["status"] == "failed":
        sys.exit(1)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3

import json
import logging
import os
import subprocess
import sys
import tempfile
import hashlib
from datetime import datetime, timezone

class JsonFormatter(logging.Formatter):
    def format(self, record):
        log_entry = {
            "timestamp": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "level": record.levelname,
            "message": record.getMessage(),
            "module": record.module,
        }
        if record.exc_info:
            log_entry["exception"] = self.formatException(record.exc_info)
        return json.dumps(log_entry)

def setup_logging():
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(JsonFormatter())
    logger = logging.getLogger()
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    return logger

logger = setup_logging()

def run_curl(args):
    """Run curl with the given arguments and return stdout, stderr, and return code."""
    cmd = ["curl", "-s"] + args
    try:
        process = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return process.stdout, process.stderr, process.returncode
    except subprocess.CalledProcessError as e:
        logger.error(f"curl failed: {e.stderr}")
        raise

def get_content_length(url):
    """Get the Content-Length of a URL using curl -I."""
    try:
        # -L to follow redirects, -I for HEAD
        cmd = ["curl", "-s", "-L", "-I", url]
        process = subprocess.run(cmd, capture_output=True, text=True, check=True)
        for line in process.stdout.splitlines():
            if line.lower().startswith("content-length:"):
                return int(line.split(":")[1].strip())
    except Exception as e:
        logger.error(f"Failed to get Content-Length for {url}: {e}")
    return None

def download_file(url, target_path):
    """Download a file using curl."""
    os.makedirs(os.path.dirname(target_path), exist_ok=True)
    try:
        cmd = ["curl", "-s", "-L", "-o", target_path, url]
        subprocess.run(cmd, check=True)
        return True
    except subprocess.CalledProcessError as e:
        logger.error(f"Failed to download {url}: {e}")
        return False

def verify_sha256(file_path, expected_sha256):
    """Verify the SHA256 of a file."""
    sha256_hash = hashlib.sha256()
    with open(file_path, "rb") as f:
        for byte_block in iter(lambda: f.read(4096), b""):
            sha256_hash.update(byte_block)
    actual_sha256 = sha256_hash.hexdigest()
    return actual_sha256 == expected_sha256

def load_settings():
    try:
        with open("settings.json", "r") as f:
            return json.load(f)
    except Exception as e:
        logger.error(f"Failed to load settings.json: {e}")
        sys.exit(1)

def load_policy(policy_file):
    try:
        with open(policy_file, "r") as f:
            return json.load(f)
    except Exception as e:
        logger.error(f"Failed to load policy file {policy_file}: {e}")
        sys.exit(1)

def is_approved(version, policy_data):
    # Extract major version
    major_version = version.split(".")[0]
    for entry in policy_data:
        if entry.get("version") == major_version:
            return (entry.get("recommended", False) or 
                    entry.get("supported", False) or 
                    entry.get("under_review", False))
    return False

def main():
    settings = load_settings()
    index_url = settings.get("index_url")
    policy_file = settings.get("policy_file")

    if not index_url or not policy_file:
        logger.error("index_url or policy_file missing in settings.json")
        sys.exit(1)

    policy_data = load_policy(policy_file)
    
    logger.info(f"Fetching index from {index_url}")
    index_data_str, _, _ = run_curl([index_url])
    index_data = json.loads(index_data_str)

    temp_dir = tempfile.mkdtemp(prefix="nexus-sync-")
    logger.info(f"Created temporary directory: {temp_dir}")

    summary = {
        "temp_dir": temp_dir,
        "downloaded": [],
        "skipped": [],
        "failed": [],
        "status": "success"
    }

    platforms = ["linux", "mac", "windows"]
    
    for platform in platforms:
        if platform not in index_data:
            continue
        
        for item in index_data[platform]:
            version = item.get("version")
            if not is_approved(version, policy_data):
                logger.info(f"Skipping version {version} as it is not approved by policy")
                continue

            binary_rel_path = item.get("binary")
            expected_sha256 = item.get("sha256")
            
            # The binary path in index.json is relative to the base_url or index_url.
            # We prefer base_url from settings if available.
            base_url = settings.get("base_url")
            if binary_rel_path.startswith("http"):
                binary_url = binary_rel_path
            elif base_url:
                binary_url = f"{base_url.rstrip('/')}/{binary_rel_path}"
            else:
                base_url_from_index = index_url.rsplit("/", 1)[0]
                binary_url = f"{base_url_from_index}/{binary_rel_path}"

            filename = os.path.basename(binary_rel_path)
            # Organise into temp_dir/platform/filename
            target_path = os.path.join(temp_dir, platform, filename)

            # HEAD optimization
            remote_size = get_content_length(binary_url)
            if os.path.exists(target_path) and remote_size is not None and os.path.getsize(target_path) == remote_size:
                logger.info(f"Skipping download of {filename}, already exists and size matches")
                summary["skipped"].append({"platform": platform, "version": version, "file": filename})
                continue

            logger.info(f"Downloading {filename} for {platform}...")
            if download_file(binary_url, target_path):
                if expected_sha256:
                    if verify_sha256(target_path, expected_sha256):
                        logger.info(f"Successfully verified {filename}")
                        summary["downloaded"].append({"platform": platform, "version": version, "file": filename})
                    else:
                        logger.error(f"SHA256 mismatch for {filename}")
                        summary["failed"].append({"platform": platform, "version": version, "file": filename, "error": "SHA256 mismatch"})
                        summary["status"] = "partial_success"
                else:
                    logger.info(f"Downloaded {filename} (no SHA256 provided)")
                    summary["downloaded"].append({"platform": platform, "version": version, "file": filename})
            else:
                summary["failed"].append({"platform": platform, "version": version, "file": filename, "error": "Download failed"})
                summary["status"] = "partial_success"

    # Final summary to stdout
    print(json.dumps(summary, indent=2))

if __name__ == "__main__":
    main()


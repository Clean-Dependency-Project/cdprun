#!/usr/bin/env python3
"""
Functional test script for Python RPM installation.

Validates that the installed Python works correctly by testing:
- Version matches expected
- Standard library imports
- SSL/TLS functionality
- SQLite3 database operations
- File I/O operations
- Subprocess execution

Outputs JSON results for CI integration.
Exit code: 0 if all tests pass, 1 if any fail.

Usage: /path/to/python3 test-python.py 3.13.11
"""

import json
import os
import sys
import tempfile
import traceback
from datetime import datetime


def run_tests(expected_version: str) -> dict:
    """Run all tests and return results as a dictionary."""
    results = {
        "timestamp": datetime.utcnow().isoformat() + "Z",
        "expected_version": expected_version,
        "python_executable": sys.executable,
        "tests": [],
        "passed": 0,
        "failed": 0,
        "success": False,
    }

    def add_result(name: str, passed: bool, details: str = "", error: str = ""):
        result = {"name": name, "passed": passed, "details": details}
        if error:
            result["error"] = error
        results["tests"].append(result)
        if passed:
            results["passed"] += 1
        else:
            results["failed"] += 1

    # Test 1: Version check
    try:
        actual_version = f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}"
        passed = actual_version == expected_version
        add_result(
            "version_check",
            passed,
            f"actual={actual_version}, expected={expected_version}",
        )
    except Exception as e:
        add_result("version_check", False, error=str(e))

    # Test 2: Standard library imports
    stdlib_modules = [
        "json",
        "hashlib",
        "sqlite3",
        "ssl",
        "http.server",
        "asyncio",
        "venv",
        "pathlib",
        "urllib.request",
        "email",
        "xml.etree.ElementTree",
        "ctypes",
        "multiprocessing",
        "concurrent.futures",
        "typing",
        "dataclasses",
    ]
    for module in stdlib_modules:
        try:
            __import__(module)
            add_result(f"import_{module.replace('.', '_')}", True, f"imported {module}")
        except ImportError as e:
            add_result(f"import_{module.replace('.', '_')}", False, error=str(e))

    # Test 3: SSL/TLS functionality
    try:
        import ssl

        openssl_version = ssl.OPENSSL_VERSION
        # Check that we can create an SSL context
        ctx = ssl.create_default_context()
        add_result("ssl_functionality", True, f"OpenSSL: {openssl_version}")
    except Exception as e:
        add_result("ssl_functionality", False, error=str(e))

    # Test 4: SQLite3 operations
    try:
        import sqlite3

        conn = sqlite3.connect(":memory:")
        cursor = conn.cursor()
        cursor.execute("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
        cursor.execute("INSERT INTO test (value) VALUES (?)", ("hello",))
        cursor.execute("SELECT value FROM test WHERE id = 1")
        row = cursor.fetchone()
        conn.close()
        passed = row is not None and row[0] == "hello"
        add_result("sqlite3_operations", passed, "in-memory DB create/insert/select")
    except Exception as e:
        add_result("sqlite3_operations", False, error=str(e))

    # Test 5: File I/O
    try:
        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".txt") as f:
            test_data = "Python RPM test data: 日本語 emoji 🐍"
            f.write(test_data)
            temp_path = f.name

        with open(temp_path, "r") as f:
            read_data = f.read()

        os.unlink(temp_path)
        passed = read_data == test_data
        add_result("file_io", passed, "write/read temp file with unicode")
    except Exception as e:
        add_result("file_io", False, error=str(e))

    # Test 6: Subprocess execution
    try:
        import subprocess

        result = subprocess.run(
            [sys.executable, "--version"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        passed = result.returncode == 0 and "Python" in result.stdout
        add_result("subprocess", passed, f"output: {result.stdout.strip()}")
    except Exception as e:
        add_result("subprocess", False, error=str(e))

    # Test 7: Hashlib (cryptographic functions)
    try:
        import hashlib

        data = b"test data for hashing"
        sha256 = hashlib.sha256(data).hexdigest()
        sha512 = hashlib.sha512(data).hexdigest()
        passed = len(sha256) == 64 and len(sha512) == 128
        add_result("hashlib", passed, f"sha256={sha256[:16]}..., sha512={sha512[:16]}...")
    except Exception as e:
        add_result("hashlib", False, error=str(e))

    # Test 8: Asyncio basic functionality
    try:
        import asyncio

        async def async_test():
            await asyncio.sleep(0.001)
            return "async works"

        loop_result = asyncio.run(async_test())
        passed = loop_result == "async works"
        add_result("asyncio", passed, "async/await execution")
    except Exception as e:
        add_result("asyncio", False, error=str(e))

    # Test 9: JSON encoding/decoding
    try:
        test_obj = {"key": "value", "number": 42, "list": [1, 2, 3], "unicode": "日本語"}
        encoded = json.dumps(test_obj, ensure_ascii=False)
        decoded = json.loads(encoded)
        passed = decoded == test_obj
        add_result("json_codec", passed, "encode/decode with unicode")
    except Exception as e:
        add_result("json_codec", False, error=str(e))

    # Test 10: pip availability
    try:
        import pip

        pip_version = pip.__version__
        add_result("pip_available", True, f"pip version: {pip_version}")
    except ImportError as e:
        add_result("pip_available", False, error=str(e))

    # Calculate final status
    results["success"] = results["failed"] == 0

    return results


def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "Usage: test-python.py <expected_version>"}))
        sys.exit(1)

    expected_version = sys.argv[1]

    try:
        results = run_tests(expected_version)
        print(json.dumps(results, indent=2))
        sys.exit(0 if results["success"] else 1)
    except Exception as e:
        error_result = {
            "error": str(e),
            "traceback": traceback.format_exc(),
            "success": False,
        }
        print(json.dumps(error_result, indent=2))
        sys.exit(1)


if __name__ == "__main__":
    main()

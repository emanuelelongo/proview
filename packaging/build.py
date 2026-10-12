"""Build Proview with metadata and optional Google OAuth configuration.

Use an argument vector rather than interpolating credentials into shell code.
The configuration identifies the application; user tokens are never build input.
"""

import argparse
from datetime import datetime, timezone
import os
import re
import subprocess


def git_metadata(commit="HEAD"):
    try:
        result = subprocess.run(
            ["git", "show", "-s", "--format=%H%n%cI", commit],
            check=True, capture_output=True, text=True,
        )
        sha, date = result.stdout.strip().splitlines()
        return sha, utc_date(date)
    except (OSError, subprocess.CalledProcessError, ValueError):
        return "Unknown", "Unknown"


def utc_date(value):
    date = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if date.tzinfo is None:
        raise ValueError("Commit date requires a timezone")
    return date.astimezone(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def metadata_flags(parser):
    sha = os.environ.get("PROVIEW_COMMIT_SHA", "")
    date = os.environ.get("PROVIEW_COMMIT_DATE", "")
    if not sha or not date:
        git_sha, git_date = git_metadata()
        sha, date = sha or git_sha, date or git_date
    if date != "Unknown":
        try:
            date = utc_date(date)
        except ValueError:
            parser.error("PROVIEW_COMMIT_DATE must be an ISO 8601 timestamp")
    flags = []
    for name, value in (
        ("Name", os.environ.get("PROVIEW_APP_NAME") or "Proview"),
        ("Version", os.environ.get("PROVIEW_BUILD_VERSION") or "DEV"),
        ("Repository", os.environ.get("PROVIEW_REPOSITORY_URL") or "https://github.com/emanuelelongo/proview"),
        ("Commit", sha), ("Date", date),
    ):
        # Quote each entire assignment for Go's linker argument parser, which
        # supports spaces in quoted arguments but does not process shell escapes.
        if "'" in value or any(ord(char) < 32 for char in value):
            parser.error(f"Invalid build metadata: {name}")
        flags.extend(["-X", f"'emanuelelongo/proview/internal/buildinfo.{name}={value}'"])
    return flags


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("-o", required=True)
    parser.add_argument("--windows-gui", action="store_true")
    parser.add_argument("--bundle-ocr", action="store_true", help="Build and embed the self-contained OCR runtime")
    options = parser.parse_args()
    flags = ["-H", "windowsgui"] if options.windows_gui else []
    for name, variable in (
        ("GoogleClientID", "PROVIEW_GOOGLE_CLIENT_ID"),
        ("GoogleClientSecret", "PROVIEW_GOOGLE_CLIENT_SECRET"),
    ):
        value = os.environ.get(variable, "")
        # Google desktop IDs/secrets use these characters. Reject malformed
        # configuration without printing its value or allowing linker options.
        if value and not re.fullmatch(r"[A-Za-z0-9._-]+", value):
            parser.error(f"{variable} contains invalid characters")
        if value:
            flags.extend(["-X", f"emanuelelongo/proview/pkg/signatures/cloud.{name}={value}"])
    flags.extend(metadata_flags(parser))
    command = ["go", "build", "-trimpath"]
    if options.bundle_ocr:
        from ocr_runtime import build_runtime, target_os
        import platform
        arch = os.environ.get("GOARCH") or ("arm64" if platform.machine().lower() in ("arm64", "aarch64") else "amd64")
        build_runtime(os.environ.get("GOOS") or target_os(), arch)
        command.extend(["-tags", "proview_ocr_bundle"])
    if flags:
        command.extend(["-ldflags", " ".join(flags)])
    command.extend(["-o", options.o, "./cmd/gui"])
    result = subprocess.call(command)
    if result == 0 and options.bundle_ocr and (os.environ.get("GOOS") or target_os()) == target_os():
        host_arch = "arm64" if platform.machine().lower() in ("arm64", "aarch64") else "amd64"
        if arch == host_arch:
            # Test the produced executable, with external OCR configuration hidden.
            environment = os.environ.copy()
            environment.pop("PROVIEW_TESSERACT_PATH", None)
            environment.pop("TESSDATA_PREFIX", None)
            environment.pop("PROVIEW_OCR", None)
            result = subprocess.call([os.path.abspath(options.o), "--check-ocr"], env=environment)
    return result


if __name__ == "__main__":
    raise SystemExit(main())

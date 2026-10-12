"""Validate native release packages and stage checksums before publishing anything."""

import hashlib
import os
from pathlib import Path
import re
import shutil


TARGETS = {("macos", "arm64"), ("macos", "amd64"), ("windows", "amd64"),
           ("linux", "amd64"), ("linux", "arm64")}
PACKAGE_NAME = re.compile(
    r"Proview-(v1(?:-dev\.|\.)(?:0|[1-9][0-9]*))-"
    r"(macos|windows|linux)-(amd64|arm64)\.(zip|AppImage)"
)


def prepare(source, output, release_tag, prerelease, metadata):
    tag_pattern = r"v1(?:-dev\.|\.)(?:0|[1-9][0-9]*)" if prerelease else r"v1\.(?:0|[1-9][0-9]*)"
    if not re.fullmatch(tag_pattern, release_tag):
        raise ValueError("Invalid release tag")
    packages = []
    seen = set()
    for path in sorted(source.iterdir()):
        match = PACKAGE_NAME.fullmatch(path.name)
        if not path.is_file() or not match:
            raise ValueError("Unexpected release asset: " + path.name)
        tag, platform, arch, extension = match.groups()
        target = (platform, arch)
        expected_extension = "AppImage" if platform == "linux" else "zip"
        if target not in TARGETS or tag != release_tag or extension != expected_extension:
            raise ValueError("Invalid package target or version: " + path.name)
        if target in seen:
            raise ValueError("Duplicate package target: " + path.name)
        if path.stat().st_size == 0:
            raise ValueError("Empty package: " + path.name)
        seen.add(target)
        packages.append(path)
    if seen != TARGETS:
        raise ValueError(f"A complete release requires all {len(TARGETS)} native packages")

    # Only stage a release after validating the entire build matrix.
    output.mkdir(parents=True, exist_ok=True)
    checksums = []
    for path in packages:
        shutil.copy2(path, output / path.name)
        with path.open("rb") as package:
            checksum = hashlib.sha256()
            for chunk in iter(lambda: package.read(1024 * 1024), b""):
                checksum.update(chunk)
            digest = checksum.hexdigest()
        checksums.append(f"{digest}  {path.name}\n")
    (output / "SHA256SUMS.txt").write_text("".join(checksums))
    notes = (
        f"Build from branch {metadata['branch']}.\n\n"
        f"Commit: {metadata['commit']}\nBuild: {metadata['run_url']}\n\n"
        "Google Drive and S3 signature synchronization are available in all packages.\n\n"
        "Packages: macOS Apple Silicon and Intel, Windows x64, Linux x64 and ARM64.\n"
        "macOS applications are signed ad hoc and are not notarized.\n"
        "Linux AppImages require executable permission before launching.\n"
        f"SHA256SUMS.txt contains the checksums of all {len(packages)} packages.\n"
    )
    (output / "release-notes.md").write_text(notes)


def main():
    prerelease = os.environ["PRERELEASE"]
    if prerelease not in ("true", "false"):
        raise ValueError("Invalid prerelease setting")
    metadata = {
        "branch": os.environ["RELEASE_BRANCH"], "commit": os.environ["RELEASE_COMMIT"],
        "run_url": (f"{os.environ['GITHUB_SERVER_URL']}/{os.environ['GITHUB_REPOSITORY']}"
                    f"/actions/runs/{os.environ['GITHUB_RUN_ID']}"),
    }
    prepare(Path("dist"), Path("release-assets"), os.environ["RELEASE_TAG"],
            prerelease == "true", metadata)


if __name__ == "__main__":
    main()

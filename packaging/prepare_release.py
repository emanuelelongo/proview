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


def release_notes(release_tag, metadata):
    repo = metadata.get("repo_url") or metadata["run_url"].split("/actions/")[0]
    changes = metadata.get("changes", "Release highlights will be added here.")
    bullets = "\n".join("- " + line.strip().removeprefix("- ")
                        for line in changes.splitlines() if line.strip())
    download = f"{repo}/releases/download/{release_tag}"
    previous = metadata.get("previous_tag", "")
    changelog = (f"{repo}/compare/{previous}...{release_tag}" if previous
                 else f"{repo}/tree/{release_tag}")
    source_label = "Full changelog" if previous else "Source code"
    return (
        "## What's new\n\n" + bullets + "\n\n"
        f"[{source_label}]({changelog})\n\n"
        "## Downloads\n\n"
        "Choose the package for your computer:\n\n"
        "| Platform | Download |\n| --- | --- |\n"
        f"| macOS · Apple Silicon | [ZIP]({download}/Proview-{release_tag}-macos-arm64.zip) |\n"
        f"| macOS · Intel | [ZIP]({download}/Proview-{release_tag}-macos-amd64.zip) |\n"
        f"| Windows · x64 | [ZIP]({download}/Proview-{release_tag}-windows-amd64.zip) |\n"
        f"| Linux · x64 | [AppImage]({download}/Proview-{release_tag}-linux-amd64.AppImage) |\n"
        f"| Linux · ARM64 | [AppImage]({download}/Proview-{release_tag}-linux-arm64.AppImage) |\n\n"
        "## Getting started\n\n"
        "- **macOS:** unzip and open Proview.app. The app is signed ad hoc and is not notarized; macOS may ask you to allow it in Privacy & Security.\n"
        "- **Windows:** unzip the package and launch proview.exe.\n"
        "- **Linux:** make the AppImage executable, then launch it.\n"
        "- Google Drive and S3 signature synchronization are optional and available in all packages.\n\n"
        f"[SHA-256 checksums]({download}/SHA256SUMS.txt) · "
        f"[Report an issue]({repo}/issues)\n\n"
        "<details>\n<summary>Build details</summary>\n\n"
        f"- Branch: `{metadata['branch']}`\n"
        f"- Commit: [{metadata['commit'][:7]}]({repo}/commit/{metadata['commit']})\n"
        f"- [Build workflow]({metadata['run_url']})\n\n</details>\n"
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
    (output / "release-notes.md").write_text(release_notes(release_tag, metadata))


def main():
    prerelease = os.environ["PRERELEASE"]
    if prerelease not in ("true", "false"):
        raise ValueError("Invalid prerelease setting")
    metadata = {
        "branch": os.environ["RELEASE_BRANCH"], "commit": os.environ["RELEASE_COMMIT"],
        "run_url": (f"{os.environ['GITHUB_SERVER_URL']}/{os.environ['GITHUB_REPOSITORY']}"
                    f"/actions/runs/{os.environ['GITHUB_RUN_ID']}"),
        "repo_url": f"{os.environ['GITHUB_SERVER_URL']}/{os.environ['GITHUB_REPOSITORY']}",
    }
    prepare(Path("dist"), Path("release-assets"), os.environ["RELEASE_TAG"],
            prerelease == "true", metadata)


if __name__ == "__main__":
    main()

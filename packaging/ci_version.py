"""Use the Release workflow's run number minus an offset for release versions."""

import os
import subprocess
from pathlib import Path

from build import git_metadata


def versions(event, run_number, branch, run_offset="0"):
    number = int(run_number)
    if number < 1:
        raise ValueError("The workflow run number must be positive")
    offset = int(run_offset)
    if offset < 0:
        raise ValueError("The release run offset must be nonnegative")
    number -= offset
    if number < 0:
        raise ValueError("The workflow run number must be at least the release run offset")
    if event not in ("pull_request", "workflow_dispatch"):
        raise ValueError("Unsupported release event")
    develop = branch == "develop"
    tag = f"v1.{number}" if develop else f"v1-dev.{number}"
    return {
        "version": f"1.{number}.0-dev",
        "tag": tag,
        "prerelease": "true",
    }


def main():
    event = os.environ["GITHUB_EVENT_NAME"]
    if event == "workflow_dispatch":
        branch = os.environ["TARGET_BRANCH"]
        # Resolve only branch refs, then pin all jobs and the release to this SHA.
        subprocess.run(["git", "check-ref-format", f"refs/heads/{branch}"], check=True)
        subprocess.run(["git", "fetch", "--no-tags", "--depth=1", "origin",
                        f"refs/heads/{branch}"], check=True)
        commit = "FETCH_HEAD"
    else:
        branch = os.environ.get("GITHUB_HEAD_REF") or os.environ["GITHUB_REF_NAME"]
        commit = os.environ["GITHUB_SHA"]
    values = versions(event, os.environ["GITHUB_RUN_NUMBER"], branch,
                      os.environ.get("RELEASE_RUN_OFFSET") or "0")
    sha, date = git_metadata(commit)
    if "Unknown" in (sha, date):
        raise ValueError("Cannot determine CI commit metadata")
    values.update(commit_sha=sha, commit_date=date, branch=branch)
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
        for name, value in values.items():
            output.write(f"{name}={value}\n")


if __name__ == "__main__":
    main()

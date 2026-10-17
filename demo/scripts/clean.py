#!/usr/bin/env python3
"""Clear the disposable demo workspace; final media lives in docs/screenshots."""
import argparse
from contextlib import contextmanager
import os
from pathlib import Path
import shutil

ROOT = Path(__file__).resolve().parents[2]


def check_workspace(folder):
    if folder.is_symlink():
        raise ValueError("Refusing to clean a symlinked demo directory")
    # Also recognize markers from the old timestamped layout during migration.
    if folder.exists() and any(folder.rglob(".recording")):
        raise ValueError("A recording is active; wait for it to finish")


@contextmanager
def workspace_lock(folder):
    folder.parent.mkdir(parents=True, exist_ok=True)
    lock = folder.parent / ".demo-recording"
    try:
        descriptor = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError as error:
        raise ValueError("Demo workspace is busy (" + str(lock) + "). Wait for the current command; remove a stale lock only after its runner has exited.") from error
    try:
        with os.fdopen(descriptor, "w") as stream:
            stream.write(str(os.getpid()) + "\n")
        yield
    finally:
        lock.unlink(missing_ok=True)


def reset_workspace(folder, dry_run=False):
    """Caller holds workspace_lock for mutations; dry runs are read-only."""
    check_workspace(folder)
    paths = sorted(folder.iterdir()) if folder.exists() else []
    for path in paths:
        print(("Would remove " if dry_run else "Removing ") + str(path))
        if not dry_run:
            if path.is_symlink() or path.is_file():
                path.unlink()
            else:
                shutil.rmtree(path)
    return paths


def clean(folder, dry_run=False):
    if dry_run:
        return reset_workspace(folder, dry_run=True)
    with workspace_lock(folder):
        paths = reset_workspace(folder)
    print("Demo workspace cleared. Final media in docs/screenshots is unchanged.")
    return paths


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="list files without deleting them")
    args = parser.parse_args()
    try:
        clean(ROOT / "build/demo", args.dry_run)
    except (ValueError, OSError) as error:
        parser.exit(1, "ERROR: " + str(error) + "\n")


if __name__ == "__main__":
    main()

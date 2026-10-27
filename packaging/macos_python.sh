#!/usr/bin/env bash
# Keep local macOS builds off Apple's older system Python.
set -euo pipefail

supports_build() {
    "$1" -c 'import sys, tarfile; sys.exit(not (sys.version_info >= (3, 10) and hasattr(tarfile, "data_filter")))' >/dev/null 2>&1
}

if supports_build python3; then
    exec python3 "$@"
fi

if command -v brew >/dev/null 2>&1; then
    python_bin="$(brew --prefix)/opt/python@3/libexec/bin/python3"
    if supports_build "$python_bin"; then
        exec "$python_bin" "$@"
    fi
fi

echo 'macOS builds require Python 3.10+ with tarfile.data_filter support. Install it with: brew install python' >&2
exit 1

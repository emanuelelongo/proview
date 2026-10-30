#!/usr/bin/env bash
# Keep local macOS builds off Apple's older system Python.
set -euo pipefail

supports_build() {
    "$1" -c 'import sys, tarfile; sys.exit(not (sys.version_info >= (3, 10) and hasattr(tarfile, "data_filter")))' >/dev/null 2>&1
}

if supports_build python3; then
    exec python3 "$@"
fi

for candidate in python3.14 python3.13 python3.12 python3.11 python3.10; do
    if command -v "$candidate" >/dev/null 2>&1 && supports_build "$candidate"; then
        exec "$candidate" "$@"
    fi
done

if command -v brew >/dev/null 2>&1; then
    prefix="$(brew --prefix)"
    if supports_build "$prefix/opt/python@3/libexec/bin/python3"; then
        exec "$prefix/opt/python@3/libexec/bin/python3" "$@"
    fi
    for candidate in "$prefix"/opt/python@*/libexec/bin/python3 "$prefix"/bin/python3*; do
        if [[ -x "$candidate" ]] && supports_build "$candidate"; then
            exec "$candidate" "$@"
        fi
    done
fi

echo 'macOS builds require Python 3.10+ with tarfile.data_filter support. Install it with: brew install python' >&2
exit 1

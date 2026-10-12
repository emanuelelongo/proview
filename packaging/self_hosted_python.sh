#!/usr/bin/env bash
# Select the Homebrew Python installed on the Apple Silicon self-hosted runner.
set -euo pipefail

python_bin="$(brew --prefix)/opt/python@3/libexec/bin"
if [[ ! -x "$python_bin/python3" ]]; then
    echo 'Missing Homebrew Python. Install it with: brew install python' >&2
    exit 1
fi
export PATH="$python_bin:$PATH"
python3 - <<'PYTHON'
import sys
import tarfile

if not hasattr(tarfile, "data_filter"):
    raise SystemExit("The self-hosted runner requires Python with tarfile.data_filter support.")
print("Self-hosted Python:", sys.executable, sys.version.split()[0])
PYTHON
# Subsequent workflow steps, Task and its child processes use the same Python.
printf '%s\n' "$python_bin" >> "$GITHUB_PATH"

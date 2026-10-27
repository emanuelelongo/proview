import os
from pathlib import Path
import subprocess
import tempfile
from unittest import TestCase


LAUNCHER = Path(__file__).with_name("macos_python.sh").resolve()


class MacOSPythonTests(TestCase):
    def run_launcher(self, compatible_path, compatible_brew=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / "bin"
            tools.mkdir()

            def interpreter(path, compatible, label):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(
                    '#!/bin/bash\nif [[ "$1" == -c ]]; then\n'
                    f'  exit {0 if compatible else 1}\nfi\n'
                    f'printf "%s\\n" "{label}" "$@"\n'
                )
                path.chmod(0o755)

            interpreter(tools / "python3", compatible_path, "path")
            if compatible_brew is not None:
                # Include spaces to check interpreter and argument quoting.
                prefix = root / "brew prefix"
                interpreter(prefix / "opt/python@3/libexec/bin/python3", compatible_brew, "brew")
                brew = tools / "brew"
                brew.write_text(f'#!/bin/bash\nprintf "%s\\n" "{prefix}"\n')
                brew.chmod(0o755)
            return subprocess.run(
                ["/bin/bash", str(LAUNCHER), "script with spaces.py", "--bundle-ocr"],
                env=dict(os.environ, PATH=str(tools)), capture_output=True, text=True,
            )

    def test_compatible_path_python_is_preferred(self):
        result = self.run_launcher(True, True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["path", "script with spaces.py", "--bundle-ocr"])

    def test_old_system_python_falls_back_to_homebrew(self):
        result = self.run_launcher(False, True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["brew", "script with spaces.py", "--bundle-ocr"])

    def test_missing_or_incompatible_homebrew_explains_installation(self):
        for compatible in (None, False):
            with self.subTest(compatible=compatible):
                result = self.run_launcher(False, compatible)
                self.assertEqual(result.returncode, 1)
                self.assertIn("brew install python", result.stderr)
                self.assertEqual(result.stdout, "")

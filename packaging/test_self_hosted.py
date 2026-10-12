import os
from pathlib import Path
import subprocess
import tempfile
from unittest import TestCase, mock
import zipfile

import self_hosted


class SelfHostedTests(TestCase):
    def test_docker_forwards_build_metadata_and_credentials_by_name(self):
        with mock.patch.dict(os.environ, {"PROVIEW_GOOGLE_CLIENT_SECRET": "private-value"}):
            command = self_hosted.docker_command("proview/linux:arm64", "linux/arm64", ["task"], Path("/repo"))
        self.assertNotIn("private-value", " ".join(command))
        for variable in self_hosted.BUILD_ENV:
            self.assertIn(variable, command)
        self.assertNotIn("GH_TOKEN", command)
        self.assertIn("linux/arm64", command)
        self.assertIn("type=bind,source=/repo,target=/workspace", command)
        self.assertIn("GIT_CONFIG_VALUE_0=/workspace", command)

    def test_linux_targets_keep_baselines_and_stage_appimages(self):
        for arch, baseline, appimage_arch in (("amd64", "ubuntu:22.04", "x86_64"),
                                               ("arm64", "ubuntu:24.04", "aarch64")):
            with self.subTest(arch=arch), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                package = root / f"build/linux-{arch}/Proview-1.42.0-dev-{appimage_arch}.AppImage"
                package.parent.mkdir(parents=True)
                package.write_bytes(b"AppImage")
                with mock.patch("self_hosted.Path.cwd", return_value=root), mock.patch.dict(
                    os.environ, {"PROVIEW_VERSION": "1.42.0-dev"},
                ), mock.patch("self_hosted.subprocess.run") as run:
                    self_hosted.package("linux", arch, "v1.123456789", Path(".ci-tooling"))
                self.assertIn("BASE_IMAGE=" + baseline, run.call_args_list[0].args[0])
                container = run.call_args_list[1].args[0]
                self.assertIn("linux/" + arch, container)
                script = container[-4]
                self.assertIn('go test ./internal/pdfengine;', script)
                self.assertIn('task --taskfile "$1" package:linux ARCH="$2"', script)
                self.assertEqual((root / f"dist/Proview-v1.123456789-linux-{arch}.AppImage").read_bytes(), b"AppImage")

    def test_windows_package_contains_gui_executable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            previous = Path.cwd()
            os.chdir(root)
            self.addCleanup(os.chdir, previous)
            executable = root / "build/windows-amd64/proview.exe"
            executable.parent.mkdir(parents=True)
            executable.write_bytes(b"Windows executable")
            with mock.patch("self_hosted.subprocess.run") as run:
                self_hosted.package("windows", "amd64", "v1-dev.123456789", Path(".ci-tooling"))
            command = run.call_args_list[1].args[0]
            self.assertIn(".ci-tooling/packaging/build.py", command)
            self.assertIn("--windows-gui", " ".join(command))
            self.assertIn("GOOS=windows", " ".join(command))
            with zipfile.ZipFile(root / "dist/Proview-v1-dev.123456789-windows-amd64.zip") as archive:
                self.assertEqual(archive.namelist(), ["proview.exe"])
                self.assertEqual(archive.read("proview.exe"), b"Windows executable")

    def test_linux_staging_is_temporary_and_removed_on_success_or_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            package = root / "build/linux-arm64/Proview-1.0.0-dev-aarch64.AppImage"
            package.parent.mkdir(parents=True)
            package.write_bytes(b"AppImage")
            with mock.patch("self_hosted.Path.cwd", return_value=root), mock.patch.dict(
                os.environ, {"PROVIEW_VERSION": "1.0.0-dev"},
            ), mock.patch("self_hosted.subprocess.run") as run:
                self_hosted.package("linux", "arm64", "v1.0", Path(".ci-tooling"))
            script = run.call_args_list[1].args[0][-4]
            tools = root / "tools"
            tools.mkdir()
            (tools / "go").write_text("#!/bin/sh\nexit 0\n")
            (tools / "task").write_text(
                '#!/bin/sh\nprintf "%s\\n" "$@" > "$PROVIEW_TEST_TASK_ARGS"\n'
                'exit "$PROVIEW_TEST_TASK_EXIT"\n'
            )
            for executable in tools.iterdir():
                executable.chmod(0o755)
            arguments = root / "arguments"
            for status in (0, 1):
                with self.subTest(status=status):
                    result = subprocess.run(
                        ["bash", "-ceu", script, "package", "Taskfile.yaml", "arm64"],
                        env=dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
                                 PROVIEW_TEST_TASK_ARGS=str(arguments), PROVIEW_TEST_TASK_EXIT=str(status)),
                        capture_output=True, text=True,
                    )
                    self.assertEqual(result.returncode, status, result.stderr)
                    appdir = next(line.removeprefix("APPDIR=") for line in
                                  arguments.read_text().splitlines() if line.startswith("APPDIR="))
                    self.assertTrue(appdir.startswith("/tmp/proview-appimage."))
                    self.assertFalse(Path(appdir).parent.exists())

    def test_failed_build_does_not_stage_a_release_asset(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with mock.patch("self_hosted.Path.cwd", return_value=root), mock.patch(
                "self_hosted.subprocess.run", side_effect=subprocess.CalledProcessError(1, "docker"),
            ), self.assertRaises(subprocess.CalledProcessError):
                self_hosted.package("linux", "amd64", "v1-dev.123456789", Path(".ci-tooling"))
            self.assertEqual(list((root / "dist").iterdir()), [])

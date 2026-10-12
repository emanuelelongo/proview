import contextlib
import io
import os
from pathlib import Path
import struct
import subprocess
import tempfile
from unittest import TestCase, mock

import build


class BuildConfigurationTests(TestCase):
    def setUp(self):
        git = mock.patch("build.git_metadata", return_value=("a" * 40, "2026-10-07T10:20:30Z"))
        self.git_metadata = git.start()
        self.addCleanup(git.stop)
        resources = mock.patch("build.windows_icon_resources")
        self.resources = resources.start()
        self.addCleanup(resources.stop)

    def command(self, environment, extra=()):
        with mock.patch.dict(os.environ, environment, clear=True), mock.patch(
            "sys.argv", ["build.py", "-o", "output", *extra]
        ), mock.patch("subprocess.call", return_value=0) as execute:
            self.assertEqual(build.main(), 0)
            return execute.call_args.args[0]

    def test_oauth_configuration_and_windows_flags(self):
        command = self.command({
            "PROVIEW_GOOGLE_CLIENT_ID": "fake-google-id",
            "PROVIEW_GOOGLE_CLIENT_SECRET": "fake-google-secret",
        }, ["--windows-gui"])
        self.assertEqual(command[0:3], ["go", "build", "-trimpath"])
        self.assertIn("-H windowsgui", command[4])
        self.assertIn("GoogleClientID=fake-google-id", command[4])
        self.assertIn("GoogleClientSecret=fake-google-secret", command[4])
        self.assertEqual(command[-3:], ["-o", "output", "./cmd/gui"])
        self.resources.assert_called_once_with()

    def test_windows_target_embeds_icon_without_gui_flag(self):
        self.command({"GOOS": "windows"})
        self.resources.assert_called_once_with()

    def test_build_without_oauth_configuration(self):
        command = self.command({})
        self.assertNotIn("GoogleClientID", " ".join(command))
        self.assertNotIn("GoogleClientSecret", " ".join(command))
        if os.name != "nt":
            self.resources.assert_not_called()

    def test_invalid_credentials_do_not_leak(self):
        error = io.StringIO()
        with contextlib.redirect_stderr(error), self.assertRaises(SystemExit):
            self.command({"PROVIEW_GOOGLE_CLIENT_SECRET": "fake secret"})
        self.assertNotIn("fake secret", error.getvalue())

    def test_local_build_embeds_dev_and_git_metadata(self):
        command = self.command({})
        flags = command[4]
        for expected in ("Name=Proview", "Version=DEV", "Commit=" + "a" * 40,
                         "Date=2026-10-07T10:20:30Z",
                         "Repository=https://github.com/emanuelelongo/proview"):
            self.assertIn(expected, flags)
        self.git_metadata.assert_called_once_with()

    def test_ci_metadata_overrides_local_git_and_normalizes_date(self):
        command = self.command({
            "PROVIEW_APP_NAME": "Proview Test",
            "PROVIEW_BUILD_VERSION": "v1-dev.42",
            "PROVIEW_REPOSITORY_URL": "https://github.com/example/proview",
            "PROVIEW_COMMIT_SHA": "b" * 40,
            "PROVIEW_COMMIT_DATE": "2026-10-07T12:20:30+02:00",
        })
        for expected in ("Name=Proview Test'", "Version=v1-dev.42", "Commit=" + "b" * 40,
                         "Date=2026-10-07T10:20:30Z", "Repository=https://github.com/example/proview"):
            self.assertIn(expected, command[4])
        self.git_metadata.assert_not_called()

    def test_missing_git_embeds_unknown_metadata(self):
        self.git_metadata.return_value = ("Unknown", "Unknown")
        flags = self.command({})[4]
        self.assertIn("Commit=Unknown", flags)
        self.assertIn("Date=Unknown", flags)

    def test_invalid_metadata_is_rejected_before_building(self):
        for variable, value in (("PROVIEW_COMMIT_DATE", "2026-10-07"),
                                ("PROVIEW_COMMIT_DATE", "invalid"),
                                ("PROVIEW_APP_NAME", "app' -X injected=value"),
                                ("PROVIEW_BUILD_VERSION", "v1\ninvalid")):
            with self.subTest(variable=variable, value=value), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                self.command({variable: value})


class GitMetadataTests(TestCase):
    def test_reads_commit_sha_and_committer_date(self):
        with mock.patch("subprocess.run", return_value=subprocess.CompletedProcess(
            [], 0, "a" * 40 + "\n2026-10-07T12:20:30+02:00\n",
        )) as run:
            self.assertEqual(build.git_metadata("abc123"), ("a" * 40, "2026-10-07T10:20:30Z"))
            self.assertEqual(run.call_args.args[0], ["git", "show", "-s", "--format=%H%n%cI", "abc123"])

    def test_unavailable_git_or_commit_is_unknown(self):
        for error in (FileNotFoundError(), subprocess.CalledProcessError(128, "git")):
            with self.subTest(error=error), mock.patch("subprocess.run", side_effect=error):
                self.assertEqual(build.git_metadata(), ("Unknown", "Unknown"))


class WindowsIconTests(TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        previous = Path.cwd()
        os.chdir(temporary.name)
        self.addCleanup(os.chdir, previous)
        Path("cmd/gui").mkdir(parents=True)
        environment = mock.patch.dict(os.environ, {"GOARCH": "amd64"}, clear=True)
        environment.start()
        self.addCleanup(environment.stop)
        compiler = mock.patch("build.shutil.which", return_value="/tools/windres")
        compiler.start()
        self.addCleanup(compiler.stop)

    def compile(self, command, *, cwd, check):
        self.assertTrue(check)
        self.assertEqual(command[:5], ["/tools/windres", "--input", "icon.rc", "--output", "icon.syso"])
        self.assertEqual(command[5:7], ["--output-format", "coff"])
        self.assertEqual((cwd / "icon.rc").read_text(), '1 ICON "proview.ico"\n')
        self.assertTrue((cwd / "proview.ico").is_file())
        (cwd / "icon.syso").write_bytes(b"compiled icon")

    def test_native_resource_exists_during_build_and_is_removed_afterwards(self):
        for arch, target in (("amd64", "pe-x86-64"), ("386", "pe-i386")):
            with self.subTest(arch=arch), mock.patch.dict(os.environ, {"GOARCH": arch}), mock.patch(
                "build.subprocess.run", side_effect=self.compile
            ) as compiler:
                resource = Path(f"cmd/gui/proview_icon_windows_{arch}.syso")
                with build.windows_icon_resources():
                    self.assertEqual(resource.read_bytes(), b"compiled icon")
                    self.assertEqual(compiler.call_args.args[0][-2:], ["--target", target])
                self.assertFalse(resource.exists())

    def test_failed_build_removes_resource(self):
        with mock.patch("build.subprocess.run", side_effect=self.compile):
            with self.assertRaisesRegex(RuntimeError, "build failed"):
                with build.windows_icon_resources():
                    raise RuntimeError("build failed")
        self.assertFalse(Path("cmd/gui/proview_icon_windows_amd64.syso").exists())

    def test_windows_build_links_resource_and_returns_compiler_failure(self):
        resource = Path("cmd/gui/proview_icon_windows_amd64.syso")

        def compile_go(command):
            self.assertEqual(command[:2], ["go", "build"])
            self.assertEqual(resource.read_bytes(), b"compiled icon")
            return 1

        with mock.patch.dict(os.environ, {"GOOS": "windows"}), mock.patch(
            "sys.argv", ["build.py", "--windows-gui", "-o", "proview.exe"]
        ), mock.patch("build.git_metadata", return_value=("Unknown", "Unknown")), mock.patch(
            "build.subprocess.run", side_effect=self.compile
        ), mock.patch("build.subprocess.call", side_effect=compile_go):
            self.assertEqual(build.main(), 1)
        self.assertFalse(resource.exists())

    def test_existing_resource_is_never_overwritten_or_deleted(self):
        resource = Path("cmd/gui/proview_icon_windows_amd64.syso")
        resource.write_bytes(b"existing resource")
        with mock.patch("build.subprocess.run", side_effect=self.compile), self.assertRaises(FileExistsError):
            with build.windows_icon_resources():
                self.fail("existing resource was overwritten")
        self.assertEqual(resource.read_bytes(), b"existing resource")

    def test_missing_or_failing_compiler_stops_before_build(self):
        with mock.patch("build.shutil.which", return_value=None), self.assertRaisesRegex(RuntimeError, "windres"):
            with build.windows_icon_resources():
                self.fail("missing windres was ignored")
        with mock.patch("build.subprocess.run", side_effect=subprocess.CalledProcessError(1, "windres")):
            with self.assertRaises(subprocess.CalledProcessError):
                with build.windows_icon_resources():
                    self.fail("resource compiler failure was ignored")
        self.assertFalse(Path("cmd/gui/proview_icon_windows_amd64.syso").exists())

    def test_ico_contains_valid_png_frames_at_standard_sizes(self):
        data = (Path(build.__file__).resolve().parent / "windows/proview.ico").read_bytes()
        self.assertEqual(struct.unpack_from("<HHH", data), (0, 1, 7))
        offset = 6 + 16 * 7
        for index, size in enumerate((16, 24, 32, 48, 64, 128, 256)):
            width, height, colors, reserved, planes, bits, length, location = struct.unpack_from("<BBBBHHII", data, 6 + index * 16)
            self.assertEqual((width, height, colors, reserved, planes, bits, location),
                             (size % 256, size % 256, 0, 0, 1, 32, offset))
            frame = data[location:location + length]
            self.assertEqual(len(frame), length)
            self.assertEqual(frame[:8], b"\x89PNG\r\n\x1a\n")
            self.assertEqual(frame[12:16], b"IHDR")
            self.assertEqual(struct.unpack_from(">II", frame, 16), (size, size))
            offset += length
        self.assertEqual(offset, len(data))

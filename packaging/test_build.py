import contextlib
import io
import os
import subprocess
from unittest import TestCase, mock

import build


class BuildConfigurationTests(TestCase):
    def setUp(self):
        git = mock.patch("build.git_metadata", return_value=("a" * 40, "2026-10-07T10:20:30Z"))
        self.git_metadata = git.start()
        self.addCleanup(git.stop)

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

    def test_build_without_oauth_configuration(self):
        command = self.command({})
        self.assertNotIn("GoogleClientID", " ".join(command))
        self.assertNotIn("GoogleClientSecret", " ".join(command))

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

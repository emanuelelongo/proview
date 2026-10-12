import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from unittest import TestCase, mock

import ci_version
import prepare_release
from ci_version import versions
from prepare_release import prepare, TARGETS


class ReleaseTests(TestCase):
    def test_release_template_includes_highlights_and_download_links(self):
        changes = "New feature\n- Bug fix"
        notes = prepare_release.release_notes("v1.4", {
            "branch": "develop", "commit": "target-sha", "run_url": "https://github.com/example/proview/actions/runs/123",
            "previous_tag": "v1.3", "changes": changes,
        })
        self.assertIn("- New feature\n- Bug fix", notes)
        self.assertNotIn("- - Bug fix", notes)
        self.assertIn("https://github.com/example/proview/compare/v1.3...v1.4", notes)
        for platform, arch in TARGETS:
            extension = "AppImage" if platform == "linux" else "zip"
            self.assertIn(f"/releases/download/v1.4/Proview-v1.4-{platform}-{arch}.{extension}", notes)

    def test_ci_outputs_include_metadata_for_the_built_commit(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "outputs"
            environment = {
                "GITHUB_EVENT_NAME": "pull_request", "GITHUB_RUN_NUMBER": "42",
                "GITHUB_REF_NAME": "feature/test",
                "GITHUB_SHA": "a" * 40, "GITHUB_OUTPUT": str(output),
                "GITHUB_RUN_ID": "37671593535", "RELEASE_NUMBER": "37671593535",
            }
            with mock.patch.dict(os.environ, environment, clear=True), mock.patch(
                "ci_version.git_metadata", return_value=("a" * 40, "2026-10-07T10:20:30Z"),
            ) as metadata:
                ci_version.main()
            metadata.assert_called_once_with("a" * 40)
            values = dict(line.split("=", 1) for line in output.read_text().splitlines())
            self.assertEqual(values["commit_sha"], "a" * 40)
            self.assertEqual(values["commit_date"], "2026-10-07T10:20:30Z")
            self.assertEqual(values["tag"], "v1-dev.42")

    def test_tags_and_native_versions(self):
        for event in ("workflow_dispatch", "pull_request"):
            for branch in ("feature/test", "develop", "develop-feature", "main"):
                with self.subTest(event=event, branch=branch):
                    version = versions(event, "42", branch)
                    develop = branch == "develop"
                    self.assertEqual(version["tag"], "v1.42" if develop else "v1-dev.42")
                    self.assertEqual(version["version"], "1.42.0-dev")
                    self.assertEqual(version["prerelease"], "true")
        with self.assertRaises(ValueError):
            versions("workflow_dispatch", "0", "develop")
        with self.assertRaises(ValueError):
            versions("schedule", "42", "develop")
        with self.assertRaises(ValueError):
            versions("push", "42", "develop")

    def test_release_numbering_can_restart_without_resetting_workflow_runs(self):
        for branch, tag in (("develop", "v1.0"), ("feature/test", "v1-dev.0")):
            with self.subTest(branch=branch):
                version = versions("workflow_dispatch", "31", branch, "31")
                self.assertEqual(version["tag"], tag)
                self.assertEqual(version["version"], "1.0.0-dev")
        self.assertEqual(versions("workflow_dispatch", "32", "develop", "31")["tag"], "v1.1")
        for offset in ("-1", "32", "invalid"):
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                versions("workflow_dispatch", "31", "develop", offset)

    def test_ci_applies_repository_release_offset(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "outputs"
            environment = {
                "GITHUB_EVENT_NAME": "pull_request", "GITHUB_RUN_NUMBER": "31",
                "RELEASE_RUN_OFFSET": "31", "GITHUB_REF_NAME": "develop",
                "GITHUB_SHA": "a" * 40, "GITHUB_OUTPUT": str(output),
            }
            with mock.patch.dict(os.environ, environment, clear=True), mock.patch(
                "ci_version.git_metadata", return_value=("a" * 40, "2026-10-07T10:20:30Z"),
            ):
                ci_version.main()
            values = dict(line.split("=", 1) for line in output.read_text().splitlines())
            self.assertEqual(values["tag"], "v1.0")
            self.assertEqual(values["version"], "1.0.0-dev")

    def test_manual_run_resolves_input_branch_instead_of_workflow_branch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            remote, checkout = root / "remote", root / "checkout"
            remote.mkdir()

            def git(*arguments, cwd=remote, env=None):
                return subprocess.run(["git", *arguments], cwd=cwd, env=env,
                                      check=True, capture_output=True, text=True).stdout.strip()

            git("init", "-b", "workflow")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.test")
            git("commit", "--allow-empty", "-m", "Workflow commit")
            workflow_sha = git("rev-parse", "HEAD")
            git("checkout", "-b", "develop")
            commit_env = dict(os.environ, GIT_AUTHOR_DATE="2026-10-07T12:20:30+02:00",
                              GIT_COMMITTER_DATE="2026-10-07T12:20:30+02:00")
            git("commit", "--allow-empty", "-m", "Develop commit", env=commit_env)
            develop_sha = git("rev-parse", "HEAD")
            git("checkout", "-b", "feature/test")
            git("commit", "--allow-empty", "-m", "Feature commit", env=commit_env)
            feature_sha = git("rev-parse", "HEAD")
            git("checkout", "workflow")
            git("clone", "--depth=1", remote.as_uri(), str(checkout))

            for branch, sha, tag in (("develop", develop_sha, "v1.42"),
                                     ("feature/test", feature_sha, "v1-dev.42")):
                with self.subTest(branch=branch):
                    output = root / ("outputs-" + branch.replace("/", "-"))
                    env = dict(os.environ, GITHUB_EVENT_NAME="workflow_dispatch",
                               GITHUB_RUN_NUMBER="42", GITHUB_SHA=workflow_sha,
                               GITHUB_REF_NAME="workflow", TARGET_BRANCH=branch,
                               GITHUB_OUTPUT=str(output))
                    subprocess.run([sys.executable, "-B", str(Path(ci_version.__file__).resolve())],
                                   cwd=checkout, env=env, check=True, capture_output=True, text=True)
                    values = dict(line.split("=", 1) for line in output.read_text().splitlines())
                    self.assertEqual(values["commit_sha"], sha)
                    self.assertEqual(values["commit_date"], "2026-10-07T10:20:30Z")
                    self.assertEqual(values["branch"], branch)
                    self.assertEqual(values["tag"], tag)
                    self.assertEqual(values["prerelease"], "true")

            for branch in ("missing-branch", "../invalid", ""):
                with self.subTest(branch=branch):
                    output = root / "failed-output"
                    env.update(TARGET_BRANCH=branch, GITHUB_OUTPUT=str(output))
                    result = subprocess.run([sys.executable, "-B", str(Path(ci_version.__file__).resolve())],
                                            cwd=checkout, env=env, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse(output.exists())

    def test_release_notes_use_target_commit_and_branch(self):
        env = {
            "PRERELEASE": "true", "RELEASE_BRANCH": "feature/test",
            "RELEASE_COMMIT": "target-sha", "RELEASE_TAG": "v1-dev.42",
            "GITHUB_REF_NAME": "workflow",
            "GITHUB_SHA": "workflow-sha", "GITHUB_SERVER_URL": "https://github.com",
            "GITHUB_REPOSITORY": "example/proview", "GITHUB_RUN_ID": "123",
        }
        with mock.patch.dict(os.environ, env, clear=True), mock.patch("prepare_release.prepare") as prepare:
            prepare_release.main()
        metadata = prepare.call_args.args[-1]
        self.assertEqual(metadata["branch"], "feature/test")
        self.assertEqual(metadata["commit"], "target-sha")

    def stage(self, root, branch, event="workflow_dispatch", run_number="42", run_offset="0"):
        version = versions(event, run_number, branch, run_offset)
        source, output = root / "dist", root / "release-assets"
        source.mkdir()
        for platform, arch in TARGETS:
            extension = "AppImage" if platform == "linux" else "zip"
            path = source / f"Proview-{version['tag']}-{platform}-{arch}.{extension}"
            path.write_bytes(path.name.encode())
        return source, output, version

    def publish_assets(self, source, output, version):
        prepare(source, output, version["tag"],
                version["prerelease"] == "true", {
                    "branch": "feature/test", "commit": "abc123", "run_url": "https://example.test/run",
                })

    def verify_checksums(self, destination, count):
        lines = (destination / "SHA256SUMS.txt").read_text().splitlines()
        self.assertEqual(len(lines), count)
        for line in lines:
            digest, name = line.split("  ")
            self.assertEqual(digest, hashlib.sha256((destination / name).read_bytes()).hexdigest())
        return lines

    def test_branch_prerelease_has_five_packages(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output, version = self.stage(Path(directory), "feature/test")
            self.publish_assets(source, output, version)
            lines = self.verify_checksums(output, 5)
            self.assertTrue(all("Proview-v1-dev.42-" in line and "gsync" not in line for line in lines))
            self.assertIn("Google Drive and S3", (output / "release-notes.md").read_text())

    def test_develop_prerelease_has_five_packages(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output, version = self.stage(Path(directory), "develop")
            self.publish_assets(source, output, version)
            lines = self.verify_checksums(output, 5)
            self.assertTrue(all("Proview-v1.42-" in line for line in lines))
            self.assertIn("Google Drive and S3", (output / "release-notes.md").read_text())

    def test_zero_release_number_stages_all_packages(self):
        for branch in ("develop", "feature/test"):
            with self.subTest(branch=branch), tempfile.TemporaryDirectory() as directory:
                source, output, version = self.stage(Path(directory), branch,
                                                     run_number="31", run_offset="31")
                self.publish_assets(source, output, version)
                lines = self.verify_checksums(output, 5)
                self.assertTrue(all(f"Proview-{version['tag']}-" in line for line in lines))

    def test_missing_or_wrong_package_prevents_staging_any_release(self):
        for branch, event in (("feature/test", "workflow_dispatch"), ("develop", "workflow_dispatch")):
            for failure in ("missing", "empty", "wrong-tag", "extra", "wrong-name", "wrong-extension"):
                with self.subTest(branch=branch, event=event, failure=failure), tempfile.TemporaryDirectory() as directory:
                    source, output, version = self.stage(Path(directory), branch, event)
                    package = next(source.iterdir())
                    if failure == "missing":
                        package.unlink()
                    elif failure == "empty":
                        package.write_bytes(b"")
                    elif failure == "wrong-tag":
                        package.rename(package.with_name(package.name.replace("42", "43")))
                    elif failure == "wrong-name":
                        package.rename(package.with_name("Extra-" + package.name))
                    elif failure == "wrong-extension":
                        package.rename(package.with_suffix(".zip" if package.suffix == ".AppImage" else ".AppImage"))
                    else:
                        (source / "unexpected.txt").write_text("extra")
                    with self.assertRaises(ValueError):
                        self.publish_assets(source, output, version)
                    self.assertFalse(output.exists())

    def test_invalid_release_tags_prevent_staging(self):
        for tag, prerelease in (("v1-dev.-1", True), ("v1.00", True),
                                ("v1-dev.42", False), ("v1.42-extra", True)):
            with self.subTest(tag=tag, prerelease=prerelease), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                with self.assertRaises(ValueError):
                    prepare(root, root / "output", tag, prerelease, {})
                self.assertFalse((root / "output").exists())

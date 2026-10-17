"""Check release routing and PR/manual verification workflows."""

from itertools import product
from pathlib import Path
import re
import os
import subprocess
import sys
import tempfile
import textwrap
from unittest import TestCase


WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/ci.yml"
PR_WORKFLOW = WORKFLOW.with_name("checks.yml")


def job_field(job, field, workflow_path=WORKFLOW):
    workflow = workflow_path.read_text()
    start = workflow.index(f"  {job}:\n")
    end = re.search(r"\n  [\w-]+:\n", workflow[start + 1:])
    body = workflow[start:start + 1 + end.start()] if end else workflow[start:]
    lines = body.splitlines()
    for index, line in enumerate(lines):
        prefix = f"    {field}: "
        if line.startswith(prefix):
            value = line[len(prefix):]
            if value == ">-":
                continuation = []
                for following in lines[index + 1:]:
                    if not following.startswith("      "):
                        break
                    continuation.append(following.strip())
                value = " ".join(continuation)
            return value
    raise AssertionError(f"Missing {field} in {job}")


def evaluate(expression, values):
    # These routing expressions use only comparisons, boolean operators and
    # status checks. Substitute their contexts, then evaluate without builtins.
    expression = expression.removeprefix("${{").removesuffix("}}").strip()
    expression = re.sub(
        r"inputs\.(?:runner|branch)|github\.event_name|github\.event\.pull_request\.number|github\.(?:ref|sha)|needs\.[\w-]+\.result|always\(\)|cancelled\(\)",
        lambda match: repr(values[match.group()]), expression,
    )
    expression = expression.replace("&&", " and ").replace("||", " or ")
    expression = re.sub(r"!(?!=)", " not ", expression).strip()
    return eval(expression, {"__builtins__": {}}, {})


class WorkflowTests(TestCase):
    def test_only_manual_dispatch_selects_a_build_path(self):
        for event, runner, native, cross in (
            ("workflow_dispatch", "github-hosted", True, False),
            ("workflow_dispatch", "self-hosted", False, True),
            ("pull_request", "", False, False),
            ("pull_request", "github-hosted", False, False),
            ("pull_request", "self-hosted", False, False),
        ):
            with self.subTest(event=event, runner=runner):
                values = {"github.event_name": event, "inputs.runner": runner}
                self.assertEqual(evaluate(job_field("native-build", "if"), values), native)
                self.assertEqual(evaluate(job_field("cross-build", "if"), values), cross)
                for job, hosted in (("version", "ubuntu-22.04"), ("quality", "ubuntu-latest"),
                                    ("release", "ubuntu-22.04")):
                    expected = "macos-self-hosted" if runner == "self-hosted" else hosted
                    self.assertEqual(evaluate(job_field(job, "runs-on"), values), expected)

    def test_publication_requires_success_from_selected_path_and_shared_checks(self):
        expression = job_field("release", "if")
        statuses = ("success", "failure", "cancelled", "skipped")
        for runner, results in product(("github-hosted", "self-hosted"), product(statuses, repeat=4)):
            version, quality, native, cross = results
            selected, unused = (cross, native) if runner == "self-hosted" else (native, cross)
            expected = version == quality == selected == "success" and unused == "skipped"
            values = {
                "inputs.runner": runner, "github.event_name": "workflow_dispatch",
                "always()": True, "cancelled()": False,
                **{f"needs.{job}.result": result for job, result in
                   zip(("version", "quality", "native-build", "cross-build"), results)},
            }
            with self.subTest(runner=runner, results=results):
                self.assertEqual(evaluate(expression, values), expected)
                self.assertFalse(evaluate(expression, dict(values, **{"cancelled()": True})))
                self.assertFalse(evaluate(expression, dict(values, **{"github.event_name": "pull_request"})))


    def test_release_and_pull_request_triggers_are_separate(self):
        release_triggers = WORKFLOW.read_text().split("\npermissions:", 1)[0]
        pr_triggers = PR_WORKFLOW.read_text().split("\npermissions:", 1)[0]
        self.assertIn("  workflow_dispatch:", release_triggers)
        self.assertNotIn("  pull_request:", release_triggers)
        self.assertNotIn("  push:", release_triggers)
        self.assertIn("  pull_request:", pr_triggers)
        self.assertIn("  workflow_dispatch:", pr_triggers)
        self.assertNotIn("  push:", pr_triggers)

    def test_manual_target_is_independent_of_workflow_source(self):
        workflow = PR_WORKFLOW.read_text()
        expression = re.search(r"(?m)^          ref: (.+)$", workflow).group(1)
        for source in ("refs/heads/develop", "refs/heads/other-workflow"):
            values = {"github.event_name": "workflow_dispatch", "inputs.branch": "feature/test",
                      "github.ref": source, "github.sha": "workflow-source-sha"}
            self.assertEqual(evaluate(expression, values), "feature/test")
            values["github.event_name"] = "pull_request"
            values["github.sha"] = "proposed-merge-sha"
            self.assertEqual(evaluate(expression, values), "proposed-merge-sha")
        self.assertEqual(job_field("tests", "needs", PR_WORKFLOW), "target")
        self.assertIn("ref: ${{ needs.target.outputs.commit }}", workflow)
        self.assertIn("commit: ${{ steps.resolve.outputs.commit }}", workflow)

    def test_manual_runs_and_prs_do_not_cancel_unrelated_checks(self):
        workflow = PR_WORKFLOW.read_text()
        group = re.search(r"(?m)^  group: (.+)$", workflow).group(1)
        def render(event, number, ref, branch=""):
            values = {"github.event_name": event, "github.event.pull_request.number": number,
                      "github.ref": ref, "inputs.branch": branch}
            return re.sub(r"\$\{\{(.*?)\}\}",
                          lambda match: str(evaluate(match.group(), values)), group)
        manual = render("workflow_dispatch", "", "refs/heads/develop", "feature/test")
        self.assertEqual(manual, render("workflow_dispatch", "", "refs/heads/other-workflow", "feature/test"))
        self.assertNotEqual(manual, render("workflow_dispatch", "", "refs/heads/develop", "another-target"))
        self.assertNotEqual(manual, render("pull_request", 2, "refs/pull/2/merge"))
        self.assertNotEqual(render("pull_request", 2, "refs/pull/2/merge"),
                            render("pull_request", 3, "refs/pull/3/merge"))

    def test_pull_requests_test_every_native_target_without_packaging(self):
        workflow = PR_WORKFLOW.read_text()
        self.assertEqual(set(re.findall(r"            os: (.+)", workflow)), {
            "macos-15", "macos-15-intel", "windows-2022", "ubuntu-22.04", "ubuntu-24.04-arm",
        })
        self.assertIn("go test -timeout 10m ./...", workflow)
        self.assertIn("go vet ./...", workflow)
        self.assertIn("go test -race -timeout 5m ./internal/applog", workflow)
        self.assertIn("xargs -0 gofmt -l", workflow)
        self.assertIn("python3 -m unittest discover -s packaging", workflow)
        for forbidden in ("go build", "packaging/build.py", "task package:",
                          "upload-artifact", "download-artifact", "gh release", "secrets.",
                          "self-hosted", "continue-on-error", "contents: write"):
            self.assertNotIn(forbidden, workflow)
        self.assertIn("cancel-in-progress: true", workflow)
        self.assertEqual(job_field("pr-checks", "name", PR_WORKFLOW), "PR checks")
        self.assertEqual(job_field("pr-checks", "needs", PR_WORKFLOW), "tests")
        self.assertEqual(job_field("pr-checks", "if", PR_WORKFLOW), "always()")

    def test_pr_summary_gate_fails_unless_the_whole_matrix_succeeds(self):
        blocks = re.findall(r"(?m)^( +)python3 - <<'PYTHON'\n(.*?)^\1PYTHON$",
                            PR_WORKFLOW.read_text(), re.DOTALL)
        self.assertEqual(len(blocks), 2)
        aggregate = textwrap.dedent(blocks[-1][1])
        for result in ("success", "failure", "cancelled", "skipped"):
            with self.subTest(result=result), tempfile.TemporaryDirectory() as directory:
                summary = Path(directory) / "summary.md"
                completed = subprocess.run(
                    [sys.executable, "-c", aggregate], capture_output=True, text=True,
                    env=dict(os.environ, TESTS_RESULT=result, GITHUB_STEP_SUMMARY=str(summary)),
                )
                self.assertEqual(completed.returncode == 0, result == "success")
                self.assertIn(f"**{result}**", summary.read_text())
        target_summary = textwrap.dedent(blocks[0][1])
        with tempfile.TemporaryDirectory() as directory:
            summary = Path(directory) / "summary.md"
            subprocess.run([sys.executable, "-c", target_summary], check=True,
                           env=dict(os.environ, GITHUB_STEP_SUMMARY=str(summary), TARGET_NAME="Linux x64", CHECKED_COMMIT="checked-target-sha",
                                    TESTS_RESULT="failure", VET_RESULT="success", RACE_RESULT="success",
                                    FORMATTING_RESULT="success", TOOLING_RESULT="success"))
            self.assertIn("Go tests (including native PDF security) | failure", summary.read_text())
            self.assertIn("Go vet | success", summary.read_text())
            self.assertIn("Checked commit: `checked-target-sha`", summary.read_text())

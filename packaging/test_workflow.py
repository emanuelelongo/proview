"""Check runner routing and publication gates in the actual Build workflow."""

from itertools import product
from pathlib import Path
import re
from unittest import TestCase


WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/ci.yml"


def job_field(job, field):
    workflow = WORKFLOW.read_text()
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
        r"inputs\.runner|github\.event_name|needs\.[\w-]+\.result|always\(\)|cancelled\(\)",
        lambda match: repr(values[match.group()]), expression,
    )
    expression = expression.replace("&&", " and ").replace("||", " or ")
    expression = re.sub(r"!(?!=)", " not ", expression).strip()
    return eval(expression, {"__builtins__": {}}, {})


class WorkflowTests(TestCase):
    def test_manual_runner_choice_and_pull_requests_select_one_build_path(self):
        for event, runner, native, cross in (
            ("workflow_dispatch", "github-hosted", True, False),
            ("workflow_dispatch", "self-hosted", False, True),
            ("pull_request", "", True, False),
        ):
            with self.subTest(event=event, runner=runner):
                values = {"github.event_name": event, "inputs.runner": runner}
                self.assertEqual(evaluate(job_field("native-build", "if"), values), native)
                self.assertEqual(evaluate(job_field("cross-build", "if"), values), cross)
                for job, hosted in (("version", "ubuntu-22.04"), ("quality", "ubuntu-latest"),
                                    ("release", "ubuntu-22.04")):
                    expected = "macos-self-hosted" if cross else hosted
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

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("demo_record", Path(__file__).with_name("record.py"))
record = importlib.util.module_from_spec(spec)
spec.loader.exec_module(record)


class RecorderTests(unittest.TestCase):
    def test_window_permission_errors_keep_the_macos_reason(self):
        session = record.Session("owned-window", Path("unused"))
        for code in ("-25211", "-1743"):
            error = record.subprocess.CalledProcessError(1, ["osascript"], stderr=("System Events: access denied (" + code + ")").encode())
            with self.subTest(code=code), patch.object(record, "run", side_effect=error):
                with self.assertRaisesRegex(RuntimeError, "macOS denied.*Accessibility") as raised:
                    session.window("place")
                self.assertIn(code, str(raised.exception))

    def test_menu_action_requires_labels_and_uses_the_owned_window(self):
        for path in (None, [], ["File"], ["File", ""], "File", ["View", "Markdown Theme", ""], ["View", "A", "B", "C"]):
            with self.subTest(path=path), self.assertRaises(ValueError):
                record.validate_action({"op": "menu", "path": path})
        action = {"op": "menu", "path": ["File", "Create PDF from Selection"]}
        record.validate_action(action)
        session = record.Session("owned-window", Path("unused"))
        with patch.object(session, "window") as window:
            session.action(action)
        self.assertEqual(window.call_args_list[-1].args, ("menu", "File", "Create PDF from Selection"))

        action = {"op": "menu", "path": ["View", "Markdown Theme", "Light"]}
        record.validate_action(action)
        with patch.object(session, "window") as window:
            session.action(action)
        self.assertEqual(window.call_args_list[-1].args, ("menu", "View", "Markdown Theme", "Light"))

    def test_modified_click_releases_keys_after_input_failure(self):
        session = record.Session("owned-window", Path("unused"))
        session.bounds = [80, 60, 1000, 740]
        action = {"op": "click", "at": [.1, .2], "modifiers": ["shift"]}
        record.validate_action(action)
        with patch.object(session, "window"), patch.object(session, "input", side_effect=RuntimeError("input failed")), patch.object(record, "run") as release:
            with self.assertRaisesRegex(RuntimeError, "input failed"):
                session.action(action)
        release.assert_called_once_with(["cliclick", "ku:shift"], timeout=5)

    def test_navigation_keys_extend_the_recorded_clip(self):
        session = record.Session("owned-window", Path("unused"))
        session.input_times = [101, 101.2]
        with patch.object(session, "window"), patch.object(record.time, "monotonic", side_effect=[103, 104]):
            session.action({"op": "key", "key": "pagedown"})
        clip = record.clip_interval(session.input_times, 100, 10, {"before": .25, "after": 1, "max_seconds": 6})
        self.assertEqual(clip, {"start": .75, "duration": 4.25})

    def test_subprocess_diagnostics_reach_the_report_and_console(self):
        error = record.subprocess.CalledProcessError(1, ["osascript", "window.applescript"], stderr=b"System Events: keystroke failed (-1700)\n")
        self.assertIn("System Events: keystroke failed (-1700)", record.error_details(error))
        self.assertIn("osascript", record.error_details(error))
        self.assertEqual(record.error_details(ValueError("invalid case")), "invalid case")

    def test_missing_screen_permission_explains_the_next_step_and_keeps_the_error(self):
        session = record.Session("owned-window", Path("unused"))
        session.bounds = [80, 60, 1000, 740]
        failure = record.subprocess.CalledProcessError(1, ["screencapture"], stderr=b"could not create image from rect")
        with patch.object(session, "window"), patch.object(record, "run", side_effect=failure), patch.object(record, "screen_capture_access", return_value=False):
            with self.assertRaises(RuntimeError) as caught:
                session.screenshot(Path("ready.png"))
        self.assertIn("Screen Recording permission is missing", str(caught.exception))
        self.assertIn("could not create image from rect", str(caught.exception))

    def test_cached_pid_keeps_window_checks_scoped_to_the_original_process(self):
        session = record.Session("owned-window", Path("unused"))
        result = record.subprocess.CompletedProcess([], 0, stdout=b"123,80,60,1000,740\n")
        with patch.object(record, "run", return_value=result) as command:
            session.window("place")
            session.window("key", "0", "command")
        self.assertEqual(command.call_args_list[0].args[0][4], "0")
        self.assertIn("runningApplicationWithProcessIdentifier(123)", command.call_args_list[1].args[0][-1])
        self.assertEqual(command.call_args_list[2].args[0][4:], ["123", "0", "command"])
        changed = record.subprocess.CompletedProcess([], 0, stdout=b"456,80,60,1000,740\n")
        with patch.object(record, "run", return_value=changed):
            with self.assertRaisesRegex(RuntimeError, "identity changed"):
                session.window("assert")

    def test_clip_removes_setup_waits_but_preserves_gesture_and_final_hold(self):
        clip = record.clip_interval([102, 102.7], 100, 6, {"before": .25, "after": .65, "max_seconds": 3})
        self.assertEqual(clip, {"start": 1.75, "duration": 1.6})
        args = record.encode_args(Path("raw.mov"), Path("result.gif"), "gif", clip)
        self.assertEqual(args[args.index("-ss") + 1], "1.75")
        self.assertEqual(args[args.index("-t") + 1], "1.6")
        with self.assertRaisesRegex(RuntimeError, "ended before"):
            record.clip_interval([105, 106], 100, 6, {})
        with self.assertRaisesRegex(RuntimeError, "too slow"):
            record.clip_interval([101, 105], 100, 10, {"max_seconds": 3})

    def test_all_scenes_use_the_normal_binary_with_a_disposable_profile(self):
        work = Path("/repo/build/demo/sign-pdf/work")
        source = work / "sample.pdf"
        command, env = record.launch_command(Path("/repo/build/proview"), {}, work, source)
        self.assertEqual(command, ["/repo/build/proview", "--foreground", "--new-window", "--work-dir", str(work), "--data-dir", str(work.parent / "profile" / str(record.os.getpid())), str(source)])
        self.assertIsNone(env)

    def test_picker_refuses_paths_outside_fixture_copies(self):
        for filename in ["../private.png", "/private.png", "", ".."]:
            with self.assertRaises(ValueError):
                record.validate_action({"op": "choose-file", "file": filename})
        record.validate_action({"op": "choose-file", "file": "johndoe.png"})

    def test_picker_does_not_control_another_applications_dialog(self):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary) / "johndoe.png"
            fixture.write_bytes(b"fixture")
            session = record.Session("owned", Path(temporary))
            session.pid = 123
            processes = record.subprocess.CompletedProcess([], 0, stdout=b"55 999 /usr/bin/osascript\n56 123 /usr/bin/other\n")
            with patch.object(record, "run", return_value=processes) as command, patch.object(record.time, "monotonic", side_effect=[0, 0, 11]), patch.object(record.time, "sleep"):
                with self.assertRaisesRegex(RuntimeError, "owned by the demo"):
                    session.choose_file(fixture)
            command.assert_called_once_with(["ps", "-axo", "pid=,ppid=,comm="], timeout=5)

    def test_doctor_cli_checks_the_standard_binary_without_capturing(self):
        with patch.object(record, "doctor", return_value=[]) as doctor, patch.object(record, "generate") as generate:
            self.assertEqual(record.main(["--doctor", "import-signature"]), 0)
        doctor.assert_called_once_with((record.ROOT / "build/proview").resolve())
        generate.assert_not_called()

    def test_palette_target_follows_layout_and_rejects_missing_color(self):
        for top in (315, 340):
            rgb = bytearray(1000 * 740 * 3)
            green = bytes.fromhex("8bc34a")
            # Include a distractor in the document, outside the modal.
            for left, y0 in ((510, top), (200, 200)):
                for y in range(y0, y0 + 32):
                    start = (y * 1000 + left) * 3
                    rgb[start:start + 32 * 3] = green * 32
            point = record.color_point(rgb, "#8bc34a")
            self.assertTrue(510 <= point[0] * 1000 < 542)
            self.assertTrue(top <= point[1] * 740 < top + 32)
            with self.assertRaisesRegex(RuntimeError, "not visible"):
                record.color_point(rgb, "#f44336")
        with self.assertRaises(ValueError):
            record.validate_action({"op": "choose-color", "value": "green"})

    def test_catalog_and_original_fixture_checksums(self):
        catalog = record.load_catalog()
        record.verify_fixtures()
        self.assertEqual({case["format"] for case in catalog["cases"]}, {"png", "gif"})
        self.assertIn("pdf-page-drag", {case["id"] for case in catalog["cases"]})

    def test_catalog_matches_every_readme_image_and_description(self):
        catalog = record.load_catalog()
        images = record.readme_media(record.ROOT / "README.md")
        self.assertTrue(images)
        self.assertEqual({case["target"] for case in catalog["cases"]}, set(images))
        for case in catalog["cases"]:
            self.assertEqual(case["readme_alt"], images[case["target"]])

    def test_readme_drift_is_reported(self):
        catalog = record.load_catalog()
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "cases.json"
            catalog["cases"][0]["target"] = "unlinked.png"
            path.write_text(json.dumps(catalog))
            with self.assertRaisesRegex(ValueError, "README media and demo targets differ"):
                record.load_catalog(path)

    def test_readme_clickable_link_must_match_the_embedded_image(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "README.md"
            content = (record.ROOT / "README.md").read_text()
            path.write_text(content.replace('href="docs/screenshots/pdf.gif"', 'href="docs/screenshots/old.gif"'))
            with self.assertRaisesRegex(ValueError, "image link and source differ"):
                record.load_catalog(readme=path)

    def test_generations_reuse_one_workspace_and_replace_only_the_requested_asset(self):
        import contextlib
        import io
        with tempfile.TemporaryDirectory() as temporary, contextlib.redirect_stdout(io.StringIO()):
            root = Path(temporary)
            folder = root / "build/demo"
            folder.mkdir(parents=True)
            (folder / "previous-capture.mov").write_bytes(b"old raw")
            binary = root / "build/proview"
            binary.write_bytes(b"executable")
            final = root / "docs/screenshots/pdf.gif"
            final.parent.mkdir(parents=True)
            final.write_bytes(b"previous final")
            unrelated = final.with_name("overview.png")
            unrelated.write_bytes(b"unchanged")
            def capture(binary, case, workspace):
                self.assertEqual(workspace, folder)
                self.assertFalse((folder / "previous-capture.mov").exists())
                candidate = folder / "result.gif"
                candidate.write_bytes(b"new final")
                record.publish_output(candidate, final)
                return {"id": case["id"], "output": "docs/screenshots/pdf.gif"}
            with patch.object(record, "ROOT", root), patch.object(record, "record_case", side_effect=capture):
                record.generate(binary, [{"id": "pdf-page-drag", "automation": "scripted"}])
            self.assertEqual(final.read_bytes(), b"new final")
            self.assertEqual(unrelated.read_bytes(), b"unchanged")
            self.assertTrue((folder / "report.json").is_file())
            self.assertFalse(any(path.is_dir() for path in folder.iterdir()))
            with patch.object(record, "ROOT", root), patch.object(record, "record_case", side_effect=RuntimeError("capture failed")):
                with self.assertRaisesRegex(RuntimeError, "capture failed"):
                    record.generate(binary, [{"id": "pdf-page-drag", "automation": "scripted"}])
            self.assertEqual(final.read_bytes(), b"new final")
            self.assertFalse((root / "build/.demo-recording").exists())

    def test_invalid_recipes_fail_before_desktop_access(self):
        original = record.load_catalog()
        mutations = [
            lambda c: c["cases"][0].update(fixture="../../README.md"),
            lambda c: c["cases"][0].update(target="../../../README.md"),
            lambda c: c["cases"].append(c["cases"][0]),
            lambda c: c["cases"][1]["actions"][0].update(op="shell"),
            lambda c: c["cases"][1]["actions"][0].update(**{"from": [2, .5]}),
            lambda c: c["cases"][0].update(format="jpeg"),
        ]
        for mutate in mutations:
            with self.subTest(mutation=mutate), tempfile.TemporaryDirectory() as folder:
                catalog = copy.deepcopy(original)
                mutate(catalog)
                path = Path(folder) / "cases.json"
                path.write_text(json.dumps(catalog))
                with self.assertRaises(ValueError):
                    record.load_catalog(path)

    def test_dry_run_cannot_launch_apps_or_read_the_desktop(self):
        with patch.object(record, "doctor", side_effect=AssertionError("desktop check")), patch.object(record.subprocess, "run", side_effect=AssertionError("process started")), patch.object(record.subprocess, "Popen", side_effect=AssertionError("app launched")), patch("builtins.print"):
            self.assertEqual(record.main(["--dry-run", "image-crop", "pdf-page-drag"]), 0)

    def test_coordinates_follow_the_detected_page_not_a_fixed_zoom(self):
        rgb = bytearray([30] * (1000 * 740 * 3))
        for y in range(85, 695):
            for x in range(350, 810):
                offset = (y * 1000 + x) * 3
                rgb[offset:offset + 3] = b"\xff\xff\xff"
        rect = record.page_rect(rgb)
        self.assertEqual(rect, (350, 85, 460, 610))
        session = record.Session("owned-window", Path("unused"))
        session.bounds = [80, 60, 1000, 740]
        self.assertEqual(session.point([.5, .5], "page", rect), (660, 450))
        with self.assertRaises(RuntimeError):
            record.page_rect(bytes([30] * len(rgb)))

    def test_failed_drag_always_releases_the_mouse_button(self):
        session = record.Session("owned-window", Path("unused"))
        session.bounds = [80, 60, 1000, 740]
        with patch.object(session, "window"), patch.object(record, "run", side_effect=[RuntimeError("drag failed"), None]) as command:
            with self.assertRaisesRegex(RuntimeError, "drag failed"):
                session.action({"op": "drag", "from": [.2, .2], "to": [.8, .8]})
        self.assertEqual(command.call_args_list[-1].args[0][0], "cliclick")
        self.assertTrue(command.call_args_list[-1].args[0][1].startswith("du:"))

    def test_unknown_cases_and_encoding(self):
        with self.assertRaises(ValueError):
            record.select_cases(record.load_catalog(), ["nonexistent"])
        args = record.encode_args(Path("a path/raw.mov"), Path("a path/result.gif"), "gif")
        self.assertIn("a path/raw.mov", args)
        self.assertIn("palettegen", " ".join(args))
        self.assertIn("paletteuse", " ".join(args))
        self.assertEqual(args[-1], "a path/result.gif")


if __name__ == "__main__":
    unittest.main()

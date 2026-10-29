#!/usr/bin/env python3
"""Capture real Proview windows on macOS using declarative demo cases.

Standard library only. --dry-run never starts an application or captures a screen.
"""
import argparse
import ctypes
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import time
from html.parser import HTMLParser

from clean import reset_workspace, workspace_lock

ROOT = Path(__file__).resolve().parents[2]
DEMO = ROOT / "demo"
TOOLS = ("osascript", "screencapture", "cliclick", "ffmpeg", "ffprobe")
ACTIONS = {"key", "click", "drag", "wait", "text", "choose-file", "choose-color", "menu"}
FORMATS = {"png", "gif", "mp4"}


def run(args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, capture_output=True, **kwargs)


def error_details(error):
    message = str(error)
    if isinstance(error, subprocess.CalledProcessError):
        details = error.stderr
        if isinstance(details, bytes):
            details = details.decode(errors="replace")
        if details and details.strip():
            message += "\n" + details.strip()
    return message


def screen_capture_access():
    """Read the permission state without opening a prompt or capturing pixels."""
    if platform.system() != "Darwin":
        return None
    try:
        graphics = ctypes.CDLL("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
        graphics.CGPreflightScreenCaptureAccess.restype = ctypes.c_bool
        return graphics.CGPreflightScreenCaptureAccess()
    except (OSError, AttributeError):
        return None


def confined(base, relative):
    path = (base / relative).resolve()
    if path == base.resolve() or base.resolve() not in path.parents:
        raise ValueError("Path must stay inside " + str(base) + ": " + relative)
    return path


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_action(action):
    op = action.get("op")
    if op not in ACTIONS:
        raise ValueError("Unknown action: " + str(op))
    if op in {"key", "click"}:
        if set(action.get("modifiers", [])) - {"command", "shift", "option", "control"}:
            raise ValueError("Unsupported key modifier")
    if op == "key":
        key = action.get("key", "")
        if key not in {"escape", "return", "tab", "pageup", "pagedown"} and len(key) != 1:
            raise ValueError("Unsupported key: " + key)
    if op in {"click", "drag"}:
        for name in (["at"] if op == "click" else ["from", "to"]):
            point = action[name]
            if len(point) != 2 or not all(isinstance(n, (int, float)) and math.isfinite(n) and 0 <= n <= 1 for n in point):
                raise ValueError("Points must be normalized coordinates in [0, 1]")
        if action.get("space", "window") not in {"window", "page"}:
            raise ValueError("Unknown coordinate space")
    if op == "choose-file":
        filename = action.get("file", "")
        if not filename or Path(filename).name != filename or filename in {".", ".."}:
            raise ValueError("Picker input must name a fixture file")
    if op == "choose-color" and not re.fullmatch(r"#[0-9a-fA-F]{6}", action.get("value", "")):
        raise ValueError("Color action needs a six-digit hex value")
    if op == "text" and not isinstance(action.get("value"), str):
        raise ValueError("Text action needs a value")
    if op == "menu":
        path = action.get("path")
        if not isinstance(path, list) or not 2 <= len(path) <= 3 or not all(isinstance(label, str) and label for label in path):
            raise ValueError("Menu action needs a menu and item labels")
    for key in ("seconds", "duration"):
        if key in action and not 0 < action[key] <= 60:
            raise ValueError("Action time must be between 0 and 60 seconds")


def readme_media(path):
    class Images(HTMLParser):
        def __init__(self):
            super().__init__()
            self.images = {}
            self.link = None

        def handle_endtag(self, tag):
            if tag == "a":
                self.link = None

        def handle_starttag(self, tag, attrs):
            attrs = dict(attrs)
            if tag == "a":
                self.link = attrs.get("href")
            source = attrs.get("src", "")
            if tag == "img" and source.startswith("docs/screenshots/"):
                if self.link and self.link != source:
                    raise ValueError("README image link and source differ: " + source)
                self.images[source.removeprefix("docs/screenshots/")] = attrs.get("alt", "")

    parser = Images()
    content = path.read_text()
    parser.feed(content)
    for alt, target in re.findall(r"!\[([^\]]*)\]\((docs/screenshots/[^)\s]+)\)", content):
        parser.images[target.removeprefix("docs/screenshots/")] = alt
    return parser.images


def load_catalog(path=DEMO / "cases.json", readme=ROOT / "README.md"):
    catalog = json.loads(path.read_text())
    if catalog.get("version") != 1 or catalog.get("window") != [1000, 740]:
        raise ValueError("Unsupported catalog version or window size; recalibrate coordinates first")
    cases = catalog["cases"]
    ids = set()
    outputs = set()
    for case in cases:
        case_id = case["id"]
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", case_id) or case_id in ids:
            raise ValueError("Invalid or duplicate case id: " + case_id)
        ids.add(case_id)
        if case["format"] not in FORMATS:
            raise ValueError("Unsupported media format")
        source = confined(ROOT, case["repository_file"]) if case.get("repository_file") else confined(DEMO / "fixtures", case["fixture"])
        if not source.is_file():
            raise ValueError("Missing demo input: " + str(source))
        target = confined(ROOT / "docs/screenshots", case["target"])
        if target.suffix != "." + case["format"] or case["target"] in outputs:
            raise ValueError("Duplicate target or mismatched media extension")
        outputs.add(case["target"])
        if case.get("automation") not in {"scripted", "pending"}:
            raise ValueError("Each case needs an explicit automation status")
        if case["automation"] == "pending" and not case.get("steps"):
            raise ValueError("Pending cases need agent-readable steps")
        if not case.get("expected"):
            raise ValueError("Each case needs visual acceptance criteria")
        if not 0 <= case.get("cleanup_undo", 0) <= 30:
            raise ValueError("Invalid cleanup undo count")
        if not 2 <= case.get("seconds", 6) <= 60:
            raise ValueError("Capture duration must be 2..60 seconds")
        if case.get("clip"):
            options = case["clip"]
            if case["format"] == "png" or set(options) - {"before", "after", "max_seconds"}:
                raise ValueError("Clip settings require an animation and supported timing fields")
            for name, value in options.items():
                if not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0 or value > 10:
                    raise ValueError("Invalid clip timing: " + name)
        for action in case.get("setup", []) + case.get("actions", []) + case.get("cleanup", []):
            validate_action(action)
    images = readme_media(readme)
    if outputs != set(images):
        raise ValueError("README media and demo targets differ; update README.md and demo/cases.json together. "
                         "Missing cases: " + ", ".join(sorted(set(images) - outputs)) +
                         "; missing README images: " + ", ".join(sorted(outputs - set(images))))
    for case in cases:
        if not case.get("readme_section") or case.get("readme_alt") != images[case["target"]]:
            raise ValueError("Update the README section/alt text with case " + case["id"])
    return catalog


def verify_fixtures():
    manifest = json.loads((DEMO / "fixtures/manifest.json").read_text())
    for name, digest in manifest["sha256"].items():
        if sha256(confined(DEMO / "fixtures", name)) != digest:
            raise ValueError("Fixture changed: " + name + "; review the change and update its SHA-256 in demo/fixtures/manifest.json")


def doctor(binary):
    problems = []
    if platform.system() != "Darwin":
        problems.append("Desktop capture requires macOS")
    for tool in TOOLS:
        resolved = shutil.which(tool)
        print(tool + ": " + (resolved or "MISSING"))
        if resolved is None:
            problems.append("Missing " + tool)
    if not binary.is_file() or not os.access(binary, os.X_OK):
        problems.append("Build an executable with task build, or pass --binary PATH")
    print("binary: " + str(binary))
    print("One-time setup: brew install cliclick ffmpeg")
    print("Allow the invoking terminal/agent Accessibility, Automation (System Events), and Screen Recording in macOS System Settings.")
    print("Permissions are checked by the first real capture; doctor does not request them or control the desktop.")
    return problems


def select_cases(catalog, requested):
    by_id = {case["id"]: case for case in catalog["cases"]}
    if not requested or requested == ["all"]:
        return catalog["cases"]
    unknown = set(requested) - by_id.keys()
    if unknown:
        raise ValueError("Unknown cases: " + ", ".join(sorted(unknown)))
    return [by_id[key] for key in dict.fromkeys(requested)]


def encode_args(source, output, media_format, clip=None):
    args = ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y"]
    if clip:
        args += ["-ss", str(clip["start"])]
    args += ["-i", str(source)]
    if clip:
        args += ["-t", str(clip["duration"])]
    if media_format == "gif":
        args += ["-filter_complex", "fps=12,scale=800:-1:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=sierra2_4a", "-loop", "0"]
    elif media_format == "mp4":
        args += ["-vf", "scale=1000:-2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-an"]
    else:
        args += ["-vf", "scale=1000:-1", "-frames:v", "1"]
    return args + [str(output)]


def clip_interval(input_times, began, raw_seconds, options):
    start = max(0, input_times[0] - began - options.get("before", .25))
    end = input_times[-1] - began + options.get("after", .65)
    if end > raw_seconds:
        raise RuntimeError("Recording ended before the gesture and final hold completed; increase the raw seconds budget")
    if end - start > options.get("max_seconds", 3):
        raise RuntimeError("The gesture is too slow for this clip; shorten its actions before recording again")
    return {"start": round(start, 3), "duration": round(end - start, 3)}


def largest_run(values):
    runs = []
    start = None
    for index, value in enumerate(list(values) + [False]):
        if value and start is None:
            start = index
        elif not value and start is not None:
            runs.append((start, index))
            start = None
    if not runs:
        raise RuntimeError("Cannot locate the white PDF page; inspect the diagnostic capture")
    return max(runs, key=lambda pair: pair[1] - pair[0])


def page_rect(rgb, width=1000, height=740):
    """Find the fixture's white paper edge after fit/toolbar changes (no guessed zoom)."""
    def white(x, y):
        offset = (y * width + x) * 3
        return min(rgb[offset:offset + 3]) > 240
    if len(rgb) != width * height * 3:
        raise RuntimeError("Unexpected capture dimensions")
    left, right = largest_run([x > 160 and sum(white(x, y) for y in range(155, height - 60, 6)) > 55 for x in range(width)])
    top, bottom = largest_run([y > 75 and y < height - 30 and white(left + 5, y) for y in range(height)])
    if right - left < 200 or bottom - top < 250:
        raise RuntimeError("Page detection failed; inspect the diagnostic capture")
    return left, top, right - left, bottom - top


def color_point(rgb, value, width=1000, height=740):
    """Locate a visible basic palette swatch, independent of recent-color rows."""
    if len(rgb) != width * height * 3:
        raise RuntimeError("Unexpected capture dimensions")
    wanted_row = bytes.fromhex(value.lstrip("#")) * 16
    # Confine targeting to the centered modal, away from document highlights.
    for y in range(260, 450):
        for x in range(320, 680):
            offset = (y * width + x) * 3
            if rgb[offset:offset + 48] != wanted_row:
                continue
            # A solid 16-point square distinguishes a swatch from text/edges.
            if all(rgb[((y + dy) * width + x) * 3:((y + dy) * width + x) * 3 + 48] == wanted_row
                   for dy in range(1, 16)):
                return [(x + 8) / width, (y + 8) / height]
    raise RuntimeError("Requested palette color is not visible; inspect color-picker.png")


class Session:
    def __init__(self, marker, folder):
        self.marker = marker
        self.folder = folder
        self.bounds = None
        self.pid = None
        self.input_times = []

    def window(self, operation, *args):
        try:
            result = run(["osascript", DEMO / "scripts/window.applescript", operation, self.marker, str(self.pid or 0), *args], timeout=15)
        except subprocess.CalledProcessError as error:
            details = error_details(error)
            if "(-25211)" in details or "(-1743)" in details:
                raise RuntimeError(
                    "macOS denied Accessibility or System Events Automation access for the invoking terminal/agent. "
                    "Enable it in System Settings > Privacy & Security, then retry.\n" + details
                ) from error
            raise
        if operation == "close":
            return None
        values = [int(value) for value in result.stdout.decode().strip().split(",")]
        if len(values) != 5 or values[3:] != [1000, 740]:
            raise RuntimeError("Window dimensions differ from the calibrated 1000 x 740 profile")
        if self.pid is not None and values[0] != self.pid:
            raise RuntimeError("Demo process identity changed")
        self.pid = values[0]
        self.bounds = values[1:]
        if operation == "place":
            run(["osascript", "-l", "JavaScript", "-e",
                 'ObjC.import("AppKit"); $.NSRunningApplication.runningApplicationWithProcessIdentifier(' + str(self.pid) + ').activateWithOptions(3);'], timeout=10)
            time.sleep(.5)
        return self.bounds

    def input(self, args, timeout):
        self.input_times.append(time.monotonic())
        try:
            return run(args, timeout=timeout)
        finally:
            self.input_times.append(time.monotonic())

    def region(self):
        return ",".join(str(value) for value in self.bounds)

    def screenshot(self, path):
        self.window("assert")
        try:
            run(["screencapture", "-x", "-R" + self.region(), path], timeout=15)
        except subprocess.CalledProcessError as error:
            if screen_capture_access() is False:
                raise RuntimeError(
                    "Screen Recording permission is missing for the invoking terminal/agent. "
                    "Enable it in System Settings > Privacy & Security > Screen & System Audio Recording, "
                    "then restart that terminal/agent if macOS requests it.\n" + error_details(error)
                ) from error
            raise

    def settle(self):
        # Wait for render publication, then require repeated equal window captures.
        time.sleep(.5)
        previous = None
        repeats = 0
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            path = self.folder / "ready.png"
            self.screenshot(path)
            raw = run(["ffmpeg", "-v", "error", "-i", path, "-vf", "scale=1000:740", "-f", "rawvideo", "-pix_fmt", "rgb24", "-"], timeout=20).stdout
            digest = hashlib.sha256(raw).hexdigest()
            repeats = repeats + 1 if digest == previous else 0
            previous = digest
            if repeats >= 2:
                return
            time.sleep(.4)
        raise RuntimeError("Window did not settle; inspect ready.png (loading, dialog or animation)")

    def point(self, point, space, rect=None):
        if space == "page":
            x, y, w, h = rect
        else:
            x, y, w, h = 0, 0, 1000, 740
        return (round(self.bounds[0] + x + point[0] * w), round(self.bounds[1] + y + point[1] * h))

    def action(self, action):
        op = action["op"]
        # Key commands already assert ownership in the same AppleScript call.
        # Waits send no input; the next actual input checks focus again.
        if op not in {"key", "wait", "choose-file"}:
            self.window("assert")
        if op == "key":
            self.input_times.append(time.monotonic())
            try:
                self.window("key", action["key"], ",".join(action.get("modifiers", [])))
            finally:
                self.input_times.append(time.monotonic())
        elif op == "menu":
            self.window("menu", *action["path"])
        elif op == "choose-file":
            self.choose_file(confined(self.folder / "work", action["file"]))
        elif op == "choose-color":
            path = self.folder / "color-picker.png"
            self.screenshot(path)
            raw = run(["ffmpeg", "-v", "error", "-i", path, "-vf", "scale=1000:740", "-f", "rawvideo", "-pix_fmt", "rgb24", "-"], timeout=20).stdout
            x, y = self.point(color_point(raw, action["value"]), "window")
            self.input(["cliclick", "c:%d,%d" % (x, y)], timeout=10)
        elif op == "text":
            self.input(["cliclick", "t:" + action["value"]], timeout=10)
        elif op == "wait":
            time.sleep(action.get("seconds", .5))
        else:
            space = action.get("space", "window")
            rect = None
            if space == "page":
                path = self.folder / "geometry.png"
                self.screenshot(path)
                raw = run(["ffmpeg", "-v", "error", "-i", path, "-vf", "scale=1000:740", "-f", "rawvideo", "-pix_fmt", "rgb24", "-"], timeout=20).stdout
                rect = page_rect(raw)
            if op == "click":
                x, y = self.point(action["at"], space, rect)
                names = {"command": "cmd", "shift": "shift", "option": "alt", "control": "ctrl"}
                modifiers = [names[name] for name in action.get("modifiers", [])]
                try:
                    self.input(["cliclick", *["kd:" + name for name in modifiers], "c:%d,%d" % (x, y)], timeout=10)
                finally:
                    if modifiers:
                        run(["cliclick", *["ku:" + name for name in reversed(modifiers)]], timeout=5)
            elif op == "drag":
                start = self.point(action["from"], space, rect)
                end = self.point(action["to"], space, rect)
                duration = action.get("duration", 1.2)
                # cliclick 5.1 enforces a 100 ms minimum after each event,
                # despite its help advertising 20 ms. Avoid extra wait events:
                # 20 steps/s plus w commands made a short drag take seconds.
                steps = max(2, round(duration * 10) - 1)
                # Give the native window a pointer move before pressing, so hit
                # testing starts on the intended canvas rather than the old hover.
                args = ["cliclick", "-w", "100", "m:%d,%d" % start, "dd:%d,%d" % start]
                for i in range(1, steps + 1):
                    x = round(start[0] + (end[0] - start[0]) * i / steps)
                    y = round(start[1] + (end[1] - start[1]) * i / steps)
                    args.append("dm:%d,%d" % (x, y))
                try:
                    self.input(args + ["du:%d,%d" % end], timeout=duration + 10)
                finally:
                    # Release the synthetic button even when a drag fails.
                    run(["cliclick", "du:%d,%d" % end], timeout=5)
        if op != "wait":
            time.sleep(.08)

    def choose_file(self, path):
        # The native panel belongs to an osascript child of this GUI process.
        # Never type into an arbitrary frontmost file dialog.
        if not path.is_file():
            raise ValueError("Missing picker input: " + str(path))
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            rows = run(["ps", "-axo", "pid=,ppid=,comm="], timeout=5).stdout.decode().splitlines()
            candidates = []
            for row in rows:
                pid, parent, executable = row.strip().split(None, 2)
                if int(parent) == self.pid and Path(executable).name == "osascript":
                    candidates.append(pid)
            if len(candidates) == 1:
                try:
                    result = run(["osascript", DEMO / "scripts/picker.applescript", candidates[0], path.name], timeout=15)
                except subprocess.CalledProcessError:
                    # Child creation can precede its panel/accessibility layout.
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(.2)
                    continue
                x, y = [int(value) for value in result.stdout.decode().strip().split(",")]
                run(["cliclick", f"c:{x},{y}", "w:200", f"dc:{x},{y}"], timeout=5)
                closed = time.monotonic() + 5
                while True:
                    try:
                        os.kill(int(candidates[0]), 0)
                    except ProcessLookupError:
                        break
                    if time.monotonic() >= closed:
                        raise RuntimeError("Native picker did not accept the fixture; inspect the owned window")
                    time.sleep(.1)
                time.sleep(.3)
                self.window("place")
                return
            time.sleep(.2)
        raise RuntimeError("Expected one native picker owned by the demo application")

    def actions(self, actions):
        for action in actions:
            self.action(action)


def launch_command(binary, case, work, source):
    return [str(binary), "--foreground", "--new-window", "--work-dir", str(work), "--data-dir", str(work.parent / "profile" / str(os.getpid())), str(source)], None


def record_case(binary, case, run_folder):
    folder = run_folder / case["id"]
    work = folder / "work"
    work.mkdir(parents=True)
    marker = "demo-" + str(os.getpid()) + "-" + case["id"]
    if case.get("repository_file"):
        # Open the real read-only Markdown so relative images resolve in place.
        unique = confined(ROOT, case["repository_file"])
        marker = unique.name
    else:
        # Copy editing inputs so native dialogs start in a disposable folder.
        for source in (DEMO / "fixtures").iterdir():
            if source.is_file():
                shutil.copy2(source, work / source.name)
        source = work / case["fixture"]
        unique = work / (marker + source.suffix)
        source.rename(unique)
    command, environment = launch_command(binary, case, work, unique)
    log = (folder / "launch.log").open("wb")
    try:
        launcher = subprocess.Popen(command, cwd=ROOT, env=environment, stdout=log, stderr=subprocess.STDOUT)
    except OSError:
        log.close()
        raise
    session = Session(marker, folder)
    session.pid = launcher.pid
    deadline = time.monotonic() + 25
    try:
        while True:
            try:
                session.window("place")
                break
            except subprocess.CalledProcessError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("Demo window not found; check Accessibility/Automation permissions and launch.log")
                time.sleep(.5)
        time.sleep(1)
        session.window("place")
        session.window("assert")
        session.actions([{"op": "key", "key": "0", "modifiers": ["command"]}])
        session.settle()
        if case.get("setup"):
            session.actions(case["setup"])
            session.settle()
        output = folder / ("result." + case["format"])
        clip = None
        if case["format"] == "png":
            session.actions(case.get("actions", []))
            session.settle()
            raw = folder / "raw.png"
            session.screenshot(raw)
        else:
            raw = folder / "raw.mov"
            duration = case.get("seconds", 6)
            capture_started = time.monotonic()
            session.input_times.clear()
            capture = subprocess.Popen(["screencapture", "-x", "-v", "-V" + str(duration), "-R" + session.region(), str(raw)], stdout=subprocess.DEVNULL, stderr=log)
            try:
                time.sleep(.8)
                if capture.poll() is not None:
                    raise RuntimeError("Screen recording stopped before actions; check Screen Recording permission")
                began = time.monotonic()
                session.actions(case.get("actions", []))
                if case.get("clip"):
                    if not session.input_times:
                        raise RuntimeError("A trimmed capture needs a mouse or typing action")
                    clip = clip_interval(session.input_times, capture_started, duration, case["clip"])
                elif time.monotonic() - began > duration - 1.3:
                    raise RuntimeError("Actions exceed recording duration; increase seconds in cases.json")
                capture.wait(timeout=duration + 10)
                if capture.returncode:
                    raise RuntimeError("Screen recording failed; see launch.log")
            finally:
                if capture.poll() is None:
                    capture.terminate()
                    capture.wait(timeout=5)
        run(encode_args(raw, output, case["format"], clip), timeout=120)
        metadata = json.loads(run(["ffprobe", "-v", "quiet", "-show_streams", "-show_format", "-of", "json", output], timeout=15).stdout)
        if case["format"] != "png":
            # Sample the whole clip even when it is shorter than two seconds.
            length = float(metadata["format"]["duration"])
            sheet = f"fps={6 / length:.6f},scale=320:-1,tile=3x2"
            run(["ffmpeg", "-v", "error", "-y", "-i", output, "-vf", sheet, "-frames:v", "1", folder / "contact-sheet.png"], timeout=60)
        session.actions(case.get("cleanup", []))
        session.actions([{"op": "key", "key": "escape"}, {"op": "key", "key": "v", "modifiers": ["command", "shift"]}])
        session.actions([{"op": "key", "key": "z", "modifiers": ["command"]}] * case.get("cleanup_undo", 0))
        # Close only our window. Never quit/kill a shared Proview application.
        # AXClose can invalidate the window reference, so its trailing bounds read may fail.
        try:
            session.window("close")
        except subprocess.CalledProcessError:
            pass
        time.sleep(.5)
        try:
            session.window("assert")
        except subprocess.CalledProcessError as error:
            message = error.stderr.decode(errors="replace")
            if "found 0" not in message:
                raise RuntimeError("Could not confirm demo window closure; inspect it before continuing") from error
        else:
            raise RuntimeError("Demo window remains open (possible unsaved-change dialog); inspect before continuing")
        target = confined(ROOT / "docs/screenshots", case["target"])
        publish_output(output, target)
        metadata["format"]["filename"] = str(target)
        return {"id": case["id"], "status": "captured", "output": str(target.relative_to(ROOT)), "sha256": sha256(target), "visual_review": "pending", "expected": case["expected"], "clip": clip, "media": metadata}
    finally:
        log.close()
        # A forwarding launcher usually exits immediately; a fresh GUI process owns its
        # own lifetime. Never terminate either process to clean up a single demo window.
        if launcher.poll() is not None:
            launcher.wait()


def publish_output(candidate, target):
    # Encoding/probing/cleanup must succeed first; preserve the previous README
    # asset on any failure. Both locations are on the repository filesystem.
    target.parent.mkdir(parents=True, exist_ok=True)
    os.replace(candidate, target)


def generate(binary, selected):
    folder = ROOT / "build/demo"
    report = {"catalog_sha256": sha256(DEMO / "cases.json"), "fixtures_sha256": sha256(DEMO / "fixtures/manifest.json"), "binary_sha256": sha256(binary), "results": []}
    with workspace_lock(folder):
        reset_workspace(folder)
        folder.mkdir(parents=True, exist_ok=True)
        print("Capturing demo windows. Keep this desktop free until the runner finishes.", flush=True)
        try:
            for case in selected:
                print("Recording " + case["id"], flush=True)
                try:
                    if case["automation"] == "pending":
                        raise RuntimeError("Case " + case["id"] + " is defined but needs agent automation: " + case["automation_note"])
                    report["results"].append(record_case(binary, case, folder))
                except Exception as error:
                    report["results"].append({"id": case["id"], "status": "failed", "error": error_details(error)})
                    raise
        finally:
            (folder / "report.json").write_text(json.dumps(report, indent=2) + "\n")
            print("Report: " + str(folder / "report.json"))
    print("Final media updated in docs/screenshots. Review the Git diff and captured media.")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("cases", nargs="*", help="case IDs, or all (default)")
    parser.add_argument("--list", action="store_true")
    parser.add_argument("--doctor", action="store_true")
    parser.add_argument("--dry-run", action="store_true", help="validate and print recipes without desktop access")
    parser.add_argument("--binary", type=Path, default=ROOT / "build/proview")
    args = parser.parse_args(argv)
    catalog = load_catalog()
    verify_fixtures()
    selected = select_cases(catalog, args.cases)
    if args.list:
        for case in selected:
            print("%-22s %-4s %-8s %s" % (case["id"], case["format"], case["automation"], case["target"]))
        return 0
    if args.dry_run:
        print(json.dumps({"window": catalog["window"], "binary": str(args.binary.resolve()), "desktop_access": False, "cases": selected}, indent=2))
        return 0
    problems = doctor(args.binary.resolve())
    if problems:
        for problem in problems:
            print("ERROR: " + problem, file=sys.stderr)
        return 1
    if args.doctor:
        return 0
    generate(args.binary.resolve(), selected)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as error:
        print("ERROR: " + error_details(error), file=sys.stderr)
        raise SystemExit(1)

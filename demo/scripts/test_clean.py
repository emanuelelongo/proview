import contextlib
import io
from pathlib import Path
import tempfile
import unittest

from clean import clean, workspace_lock


class CleanupTests(unittest.TestCase):
    def test_workspace_is_disposable_but_readme_media_and_other_builds_are_kept(self):
        with tempfile.TemporaryDirectory() as temporary, contextlib.redirect_stdout(io.StringIO()):
            repo = Path(temporary)
            build = repo / "build"
            root = build / "demo"
            case = root / "pdf-page-drag/work"
            case.mkdir(parents=True)
            (case / "showcase.pdf").write_bytes(b"copy")
            (root / "report.json").write_text("{}")
            old = root / "20261017T211926779329Z"
            old.mkdir()
            (old / "old.gif").write_bytes(b"old local candidate")
            (build / "proview").write_bytes(b"executable")
            final = repo / "docs/screenshots/pdf.gif"
            final.parent.mkdir(parents=True)
            final.write_bytes(b"final")
            clean(root, dry_run=True)
            self.assertTrue(case.exists())
            clean(root)
            self.assertEqual(list(root.iterdir()), [])
            self.assertEqual(final.read_bytes(), b"final")
            self.assertTrue((build / "proview").is_file())
            self.assertEqual(clean(root), [])

    def test_recording_and_cleaning_cannot_overlap(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "demo"
            root.mkdir()
            artifact = root / "raw.mov"
            artifact.write_bytes(b"raw")
            with workspace_lock(root):
                with self.assertRaisesRegex(ValueError, "workspace is busy"):
                    clean(root)
                self.assertTrue(artifact.exists())
            self.assertFalse((root.parent / ".demo-recording").exists())
            # Old runner markers are recognized until that recording finishes.
            (root / ".recording").write_text("123")
            with self.assertRaisesRegex(ValueError, "recording is active"):
                clean(root)
            self.assertTrue(artifact.exists())

    def test_symlinks_never_delete_outside_targets(self):
        with tempfile.TemporaryDirectory() as temporary, contextlib.redirect_stdout(io.StringIO()):
            base = Path(temporary)
            outside = base / "outside"
            outside.mkdir()
            original = outside / "keep.txt"
            original.write_text("keep")
            root = base / "demo"
            root.mkdir()
            try:
                (root / "work").symlink_to(outside, target_is_directory=True)
            except OSError:
                self.skipTest("Symlinks unavailable")
            clean(root)
            self.assertEqual(original.read_text(), "keep")
            self.assertFalse((root / "work").is_symlink())
            root.rmdir()
            root.symlink_to(outside, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, "symlinked"):
                clean(root)

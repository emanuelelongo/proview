import hashlib
import io
import os
from pathlib import Path
import tempfile
import tarfile
from unittest import TestCase, mock

import build
import ocr_runtime


class OCRRuntimeTests(TestCase):
    def test_cold_source_cache_extracts_files_and_rejects_escaping_links(self):
        for escaping in (False, True):
            with self.subTest(escaping=escaping), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                archive = root / "source.tar.gz"
                with tarfile.open(archive, "w:gz") as source:
                    member = tarfile.TarInfo("source-1.0/file.txt")
                    if escaping:
                        member.type = tarfile.SYMTYPE
                        member.linkname = "../../outside"
                        source.addfile(member)
                    else:
                        member.size = 6
                        source.addfile(member, io.BytesIO(b"source"))
                digest = hashlib.sha256(archive.read_bytes()).hexdigest()
                sources = {"source": ("owner/repo", "v1.0", "source-1.0", digest, "license")}
                with mock.patch("ocr_runtime.ROOT", root), mock.patch("ocr_runtime.SOURCES", sources), mock.patch(
                    "ocr_runtime.urllib.request.urlopen", return_value=io.BytesIO(archive.read_bytes()),
                ):
                    if escaping:
                        with self.assertRaises(tarfile.FilterError):
                            ocr_runtime.sources()
                    else:
                        extracted = ocr_runtime.sources()
                        self.assertEqual((extracted / "source-1.0/file.txt").read_bytes(), b"source")

    def test_download_checks_digest_and_reuses_verified_source(self):
        data = b"pinned source archive"
        digest = hashlib.sha256(data).hexdigest()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "source.tar.gz"
            with mock.patch("ocr_runtime.urllib.request.urlopen", return_value=io.BytesIO(b"wrong")), self.assertRaises(RuntimeError):
                ocr_runtime.download("https://example.test/source", path, digest)
            self.assertFalse(path.exists())
            with mock.patch("ocr_runtime.urllib.request.urlopen", return_value=io.BytesIO(data)):
                ocr_runtime.download("https://example.test/source", path, digest)
            with mock.patch("ocr_runtime.urllib.request.urlopen") as request:
                ocr_runtime.download("https://example.test/source", path, digest)
                request.assert_not_called()
            self.assertEqual(path.read_bytes(), data)

    def test_windows_runtime_rejects_non_system_dlls(self):
        with mock.patch("ocr_runtime.shutil.which", return_value="objdump"), mock.patch(
            "ocr_runtime.subprocess.check_output", return_value="DLL Name: KERNEL32.dll\nDLL Name: libwinpthread-1.dll\n",
        ), self.assertRaises(RuntimeError):
            ocr_runtime.verify_dependencies("helper.exe", "windows")
        with mock.patch("ocr_runtime.shutil.which", return_value="objdump"), mock.patch(
            "ocr_runtime.subprocess.check_output", return_value="DLL Name: KERNEL32.dll\nDLL Name: msvcrt.dll\n",
        ):
            ocr_runtime.verify_dependencies("helper.exe", "windows")

    def test_bundled_native_build_embeds_and_checks_produced_executable(self):
        with mock.patch.dict(os.environ, {"GOOS": "windows", "GOARCH": "amd64", "TESSDATA_PREFIX": "external"}, clear=True), mock.patch(
            "sys.argv", ["build.py", "--bundle-ocr", "--windows-gui", "-o", "proview.exe"],
        ), mock.patch("build.git_metadata", return_value=("Unknown", "Unknown")), mock.patch(
            "ocr_runtime.build_runtime",
        ) as prepare, mock.patch("ocr_runtime.target_os", return_value="windows"), mock.patch(
            "platform.machine", return_value="AMD64",
        ), mock.patch("build.windows_icon_resources"), mock.patch("build.subprocess.call", side_effect=[0, 0]) as run:
            self.assertEqual(build.main(), 0)
            prepare.assert_called_once_with("windows", "amd64")
            command = run.call_args_list[0].args[0]
            self.assertIn("proview_ocr_bundle", command)
            check = run.call_args_list[1]
            self.assertEqual(check.args[0][-1], "--check-ocr")
            self.assertNotIn("TESSDATA_PREFIX", check.kwargs["env"])

    def test_failed_ocr_selftest_fails_build(self):
        with mock.patch.dict(os.environ, {"GOOS": "windows", "GOARCH": "amd64"}, clear=True), mock.patch(
            "sys.argv", ["build.py", "--bundle-ocr", "-o", "proview.exe"],
        ), mock.patch("build.git_metadata", return_value=("Unknown", "Unknown")), mock.patch(
            "ocr_runtime.build_runtime",
        ), mock.patch("ocr_runtime.target_os", return_value="windows"), mock.patch(
            "platform.machine", return_value="AMD64",
        ), mock.patch("build.windows_icon_resources"), mock.patch("build.subprocess.call", side_effect=[0, 1]):
            self.assertEqual(build.main(), 1)

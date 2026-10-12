"""Build a pinned, minimal OCR helper and embed its runtime in Proview.

Only PNG input is needed: Proview normalizes every image before recognition.
OCR libraries are static; platform system libraries retain their native ABI.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import subprocess
import tarfile
import urllib.request
import zipfile

from ocr_models import MODELS, REVISION, main as fetch_models

ROOT = Path.cwd()
SOURCES = {
    "zlib": ("madler/zlib", "v1.3.1", "zlib-1.3.1", "17e88863f3600672ab49182f217281b6fc4d3c762bde361935e436a95214d05c", "LICENSE"),
    "png": ("pnggroup/libpng", "v1.6.50", "libpng-1.6.50", "71158e53cfdf2877bc99bcab33641d78df3f48e6e0daad030afe9cb8c031aa46", "LICENSE"),
    "leptonica": ("DanBloomberg/leptonica", "1.85.0", "leptonica-1.85.0", "c01376bce0379d4ea4bc2ec5d5cbddaa49e2f06f88242619ab8c059e21adf233", "leptonica-license.txt"),
    "tesseract": ("tesseract-ocr/tesseract", "5.5.3", "tesseract-5.5.3", "9218e62793116d42a9f6d14cd9348518b27f382096eea3d0f2d1a24616bb5884", "LICENSE"),
}


WINDOWS_LICENSES = {
    "gcc-runtime-exception": ("https://raw.githubusercontent.com/gcc-mirror/gcc/releases/gcc-15.2.0/COPYING.RUNTIME", "9d6b43ce4d8de0c878bf16b54d8e7a10d9bd42b75178153e3af6a815bdc90f74"),
    "gcc-gpl3": ("https://raw.githubusercontent.com/gcc-mirror/gcc/releases/gcc-15.2.0/COPYING3", "8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903"),
    "mingw-w64": ("https://raw.githubusercontent.com/mingw-w64/mingw-w64/v12.0.0/COPYING", "99a69660981156c21336fdb5661f89341b013c94e4bf9e1c7467b4745718397f"),
    "winpthreads": ("https://raw.githubusercontent.com/mingw-w64/mingw-w64/v12.0.0/mingw-w64-libraries/winpthreads/COPYING", "63263614cdd29f2f93cba85e992f041b31f9fc7b4033692f31269489a8a1b177"),
}


def download(url, destination, digest):
    if destination.exists() and hashlib.sha256(destination.read_bytes()).hexdigest() == digest:
        return
    with urllib.request.urlopen(url, timeout=120) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != digest:
        raise RuntimeError("Source checksum mismatch: " + destination.name)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(".download")
    temporary.write_bytes(data)
    temporary.replace(destination)


def sources():
    directory = ROOT / "build/ocr-sources"
    for name, (repository, tag, folder, digest, _) in SOURCES.items():
        archive = directory / (name + ".tar.gz")
        download(f"https://codeload.github.com/{repository}/tar.gz/refs/tags/{tag}", archive, digest)
        if not (directory / folder).exists():
            with tarfile.open(archive) as source:
                source.extractall(directory, filter="data")
    return directory


def target_os():
    return {"Darwin": "darwin", "Windows": "windows", "Linux": "linux"}[platform.system()]


def compiler_wrapper(value, directory, name):
    # CGO cross-compilers can contain arguments (e.g. zig cc -target ...).
    if os.name == "nt" or Path(value).exists() or len(shlex.split(value)) == 1:
        return value
    wrapper = directory / name
    wrapper.write_text("#!/bin/sh\nexec " + shlex.join(shlex.split(value)) + ' "$@"\n')
    wrapper.chmod(0o755)
    return str(wrapper)


def cmake_options(goos, arch, work, prefix):
    options = ["-DCMAKE_BUILD_TYPE=Release", "-DBUILD_SHARED_LIBS=OFF",
               "-DCMAKE_POLICY_VERSION_MINIMUM=3.5", "-DCMAKE_INSTALL_LIBDIR=lib",
               f"-DCMAKE_INSTALL_PREFIX={prefix.as_posix()}", f"-DCMAKE_PREFIX_PATH={prefix.as_posix()}"]
    for variable, language in (("CC", "C"), ("CXX", "CXX")):
        value = os.environ.get("PROVIEW_OCR_" + variable) or os.environ.get(variable)
        if value:
            compiler = compiler_wrapper(value, work, variable.lower())
            options.append(f"-DCMAKE_{language}_COMPILER={compiler}")
    if goos == "darwin":
        options.extend(["-DCMAKE_SYSTEM_NAME=Darwin", f"-DCMAKE_SYSTEM_PROCESSOR={'arm64' if arch == 'arm64' else 'x86_64'}", f"-DCMAKE_OSX_ARCHITECTURES={'arm64' if arch == 'arm64' else 'x86_64'}",
                        "-DCMAKE_OSX_DEPLOYMENT_TARGET=11.0"])
    if goos == "windows":
        resource_compiler = shutil.which("x86_64-w64-mingw32-windres")
        if resource_compiler:
            options.append(f"-DCMAKE_RC_COMPILER={resource_compiler}")
        options.extend(["-DCMAKE_SYSTEM_NAME=Windows", "-DCMAKE_SYSTEM_PROCESSOR=x86_64",
                        "-DCMAKE_EXE_LINKER_FLAGS=-static -static-libgcc -static-libstdc++"])
    return options


def verify_dependencies(executable, goos):
    if goos == "darwin":
        dependencies = subprocess.check_output(["otool", "-L", executable], text=True).splitlines()[1:]
        if any(not line.strip().startswith(("/usr/lib/", "/System/Library/")) for line in dependencies):
            raise RuntimeError("OCR helper depends on non-system macOS libraries")
    elif goos == "linux":
        dependencies = subprocess.check_output(["ldd", executable], text=True)
        allowed = ("linux-vdso", "libstdc++", "libgcc_s", "libm.so", "libc.so", "libpthread", "libdl.so", "librt.so", "/lib", "ld-linux")
        for line in dependencies.splitlines():
            if line.strip() and ("not found" in line or not line.strip().startswith(allowed)):
                raise RuntimeError("Unexpected OCR helper dependency: " + line.strip())
    else:
        objdump = shutil.which("objdump") or shutil.which("x86_64-w64-mingw32-objdump")
        if not objdump:
            raise RuntimeError("objdump is required to verify Windows OCR dependencies")
        dependencies = subprocess.check_output([objdump, "-p", executable], text=True)
        # Static MinGW runtime: only DLLs supplied by Windows itself are allowed.
        allowed = {"kernel32.dll", "msvcrt.dll", "ucrtbase.dll", "advapi32.dll", "user32.dll", "shell32.dll", "ole32.dll", "ws2_32.dll", "bcrypt.dll"}
        for line in dependencies.splitlines():
            if "DLL Name:" in line:
                dll = line.split("DLL Name:", 1)[1].strip().lower()
                if dll not in allowed and not dll.startswith("api-ms-win-"):
                    raise RuntimeError("Unexpected OCR helper DLL: " + dll)


def build_runtime(goos, arch):
    if (goos, arch) not in {("darwin", "arm64"), ("darwin", "amd64"), ("linux", "arm64"), ("linux", "amd64"), ("windows", "amd64")}:
        raise ValueError("Unsupported OCR target")
    work = ROOT / "build" / f"ocr-{goos}-{arch}"
    work.mkdir(parents=True, exist_ok=True)
    prefix = work / "prefix"
    common = cmake_options(goos, arch, work, prefix)
    destination = ROOT / "internal/ocr/payloads" / f"{goos}-{arch}.zip"
    fingerprint = hashlib.sha256(json.dumps({
        "script": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "sources": SOURCES, "models": MODELS, "revision": REVISION,
        "options": common, "host": platform.platform(),
    }, sort_keys=True).encode()).hexdigest()
    stamp = work / "runtime-build.json"
    cached = json.loads(stamp.read_text()) if stamp.exists() else {}
    if cached.get("fingerprint") == fingerprint and destination.exists() and hashlib.sha256(destination.read_bytes()).hexdigest() == cached.get("archive_sha256"):
        print(f"OCR runtime reused: {destination}")
        return destination
    if cached.get("fingerprint") != fingerprint:
        for name in SOURCES:
            shutil.rmtree(work / name, ignore_errors=True)
    src = sources()
    fetch_models(ROOT / "build/ocr/tessdata")
    if goos == "windows":
        # GNU cross-linkers on case-sensitive filesystems require the lowercase
        # Windows import-library name used by both MinGW and Zig.
        cmake_file = src / SOURCES["tesseract"][2] / "CMakeLists.txt"
        original = cmake_file.read_text()
        patched = original.replace("set(LIB_Ws2_32 Ws2_32)", "set(LIB_Ws2_32 ws2_32)")
        if patched != original:
            temporary = cmake_file.with_suffix(".tmp")
            temporary.write_text(patched)
            temporary.replace(cmake_file)
    generator = "MinGW Makefiles" if os.name == "nt" else "Unix Makefiles"
    zlib_name = "libzlibstatic.a" if goos == "windows" else "libz.a"
    # CMake exports these paths into link interfaces; Windows backslashes can
    # otherwise be consumed as escapes when MinGW links Tesseract.
    libraries = [f"-DZLIB_LIBRARY={(prefix / 'lib' / zlib_name).as_posix()}", f"-DZLIB_INCLUDE_DIR={(prefix / 'include').as_posix()}"]
    builds = {
        "zlib": ["-DZLIB_BUILD_EXAMPLES=OFF"],
        "png": [*libraries, "-DPNG_SHARED=OFF", "-DPNG_STATIC=ON", "-DPNG_TESTS=OFF", "-DPNG_TOOLS=OFF"],
        "leptonica": [*libraries, f"-DPNG_LIBRARY={(prefix / 'lib/libpng16.a').as_posix()}", f"-DPNG_PNG_INCLUDE_DIR={(prefix / 'include').as_posix()}", "-DBUILD_PROG=OFF", "-DSW_BUILD=OFF", *[f"-DENABLE_{feature}=OFF" for feature in ("GIF", "JPEG", "TIFF", "WEBP", "OPENJPEG")]],
        "tesseract": [*libraries, "-DBUILD_TRAINING_TOOLS=OFF", "-DBUILD_TESTS=OFF", "-DOPENMP_BUILD=OFF", "-DGRAPHICS_DISABLED=ON", "-DDISABLED_LEGACY_ENGINE=ON", "-DDISABLE_TIFF=ON", "-DDISABLE_ARCHIVE=ON", "-DDISABLE_CURL=ON", "-DENABLE_NATIVE=OFF", "-DENABLE_PRECOMPILED_HEADERS=OFF", "-DLEPT_TIFF_RESULT=1", "-DLEPT_TIFF_COMPILE_SUCCESS=TRUE", f"-DLeptonica_DIR={(prefix / 'lib/cmake/leptonica').as_posix()}"],
    }
    for name, flags in builds.items():
        directory = work / name
        subprocess.run(["cmake", "-S", str(src / SOURCES[name][2]), "-B", str(directory), "-G", generator, *common, *flags], check=True)
        build_command = ["cmake", "--build", str(directory), "--parallel", str(min(8, os.cpu_count() or 2))]
        if name == "zlib":
            build_command.extend(["--target", "zlibstatic"])
        subprocess.run(build_command, check=True)
        if name == "zlib":
            # Upstream always defines a shared target too. Install only the static
            # archive and headers we actually need, avoiding DLL/resource tooling.
            (prefix / "lib").mkdir(parents=True, exist_ok=True)
            (prefix / "include").mkdir(parents=True, exist_ok=True)
            shutil.copy2(directory / zlib_name, prefix / "lib" / zlib_name)
            shutil.copy2(src / SOURCES[name][2] / "zlib.h", prefix / "include/zlib.h")
            shutil.copy2(directory / "zconf.h", prefix / "include/zconf.h")
        else:
            subprocess.run(["cmake", "--install", str(directory)], check=True)
    executable = prefix / "bin" / ("tesseract.exe" if goos == "windows" else "tesseract")
    verify_dependencies(str(executable), goos)
    if goos == "darwin":
        subprocess.run(["codesign", "--force", "--sign", "-", str(executable)], check=True)
    if goos == target_os():
        subprocess.run([str(executable), "--tessdata-dir", str(ROOT / "build/ocr/tessdata"), "--list-langs"], check=True)
    files = {"bin/" + executable.name: executable.read_bytes()}
    for language in MODELS:
        files[f"tessdata/{language}.traineddata"] = (ROOT / "build/ocr/tessdata" / (language + ".traineddata")).read_bytes()
    for name, (_, _, folder, _, license_file) in SOURCES.items():
        files[f"licenses/{name}.txt"] = (src / folder / license_file).read_bytes()
    # tessdata_fast is Apache-2.0, like Tesseract. Include its pinned license.
    license_url = f"https://raw.githubusercontent.com/tesseract-ocr/tessdata_fast/{REVISION}/LICENSE"
    model_license = src / "tessdata_fast-LICENSE"
    download(license_url, model_license, "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30")
    files["licenses/tessdata_fast.txt"] = model_license.read_bytes()
    if goos == "windows":
        for name, (url, digest) in WINDOWS_LICENSES.items():
            path = src / (name + "-LICENSE")
            download(url, path, digest)
            files[f"licenses/{name}.txt"] = path.read_bytes()
    manifest = {"os": goos, "arch": arch, "executable": "bin/" + executable.name,
                "files": {name: hashlib.sha256(data).hexdigest() for name, data in files.items()},
                "sources": {name: {"repository": item[0], "version": item[1], "sha256": item[3]} for name, item in SOURCES.items()}, "models_revision": REVISION}
    files["manifest.json"] = json.dumps(manifest, sort_keys=True, indent=2).encode()
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(".tmp")
    with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = (0o100700 if name.startswith("bin/") else 0o100600) << 16
            archive.writestr(info, data)
    temporary.replace(destination)
    stamp.write_text(json.dumps({"fingerprint": fingerprint, "archive_sha256": hashlib.sha256(destination.read_bytes()).hexdigest()}))
    print(f"OCR runtime ready: {destination} ({destination.stat().st_size:,} bytes)")
    return destination


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--os", choices=("darwin", "linux", "windows"), default=os.environ.get("GOOS") or target_os())
    parser.add_argument("--arch", choices=("amd64", "arm64"), default=os.environ.get("GOARCH") or ("arm64" if platform.machine().lower() in ("arm64", "aarch64") else "amd64"))
    options = parser.parse_args()
    build_runtime(options.os, options.arch)


if __name__ == "__main__":
    main()

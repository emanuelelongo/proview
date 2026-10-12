"""Build release assets on macOS, using Docker for Linux and Windows targets."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import zipfile


BUILD_ENV = (
    "PROVIEW_APP_NAME", "PROVIEW_BUILD_VERSION", "PROVIEW_REPOSITORY_URL",
    "PROVIEW_COMMIT_SHA", "PROVIEW_COMMIT_DATE",
    "PROVIEW_GOOGLE_CLIENT_ID", "PROVIEW_GOOGLE_CLIENT_SECRET",
)


def docker_command(image, platform, command, root):
    arguments = ["docker", "run", "--rm", "--platform", platform,
                 "--mount", f"type=bind,source={root},target=/workspace",
                 "--mount", f"type=volume,source=proview-go-mod,target=/go/pkg/mod",
                 "--mount", f"type=volume,source=proview-go-build-{image.rsplit(':', 1)[-1]},target=/root/.cache/go-build",
                 "--workdir", "/workspace",
                 "--env", "GOPATH=/go",
                 "--env", f"PROVIEW_HOST_UID={os.getuid()}",
                 "--env", f"PROVIEW_HOST_GID={os.getgid()}",
                 "--env", "GIT_CONFIG_COUNT=1", "--env", "GIT_CONFIG_KEY_0=safe.directory",
                 "--env", "GIT_CONFIG_VALUE_0=/workspace"]
    for variable in BUILD_ENV:
        arguments.extend(["--env", variable])
    # Keep generated files writable by the runner user, including on failed builds.
    cleanup = ('trap \'for path in build .task; do '
               'if [ -e "$path" ]; then chown -R "$PROVIEW_HOST_UID:$PROVIEW_HOST_GID" "$path"; fi; '
               'done\' EXIT; "$@"')
    return arguments + [image, "bash", "-ceu", cleanup, "build", *command]


def package(target, arch, tag, tooling):
    root = Path.cwd()
    output = root / "dist"
    output.mkdir(exist_ok=True)
    name = f"Proview-{tag}-{target}-{arch}"
    taskfile = "Taskfile.yaml"
    build = str(tooling / "packaging/build.py")
    if target == "macos":
        subprocess.run(["task", "--taskfile", taskfile, "package:macos", f"ARCH={arch}"], check=True)
        subprocess.run(["ditto", "-c", "-k", "--sequesterRsrc", "--keepParent",
                        f"build/macos-{arch}/Proview.app", str(output / (name + ".zip"))], check=True)
        return

    if target == "linux":
        platform = f"linux/{arch}"
        image = f"proview/self-hosted-linux:{arch}"
        baseline = "ubuntu:24.04" if arch == "arm64" else "ubuntu:22.04"
        subprocess.run(["docker", "build", "--platform", platform, "--build-arg",
                        f"BASE_IMAGE={baseline}", "-f", str(tooling / "docker/self-hosted-linux/Dockerfile"),
                        "-t", image, "."], check=True)
        # Test the native PDF engine in the target Linux userspace before packaging.
        # Rosetta's ELF mask rejects AppImage identification bytes at offset 8.
        # Patch a separate executable copy after Task verifies the original checksum.
        # libstdc++ copy_file fails on VirtioFS mode-0200 destinations. Keep
        # linuxdeploy's staging directory on the container filesystem.
        linux_package = (
            'go test ./internal/pdfengine; linuxdeploy_override=; '
            'appimage_stage=$(mktemp -d /tmp/proview-appimage.XXXXXX); '
            'trap \'rm -rf "$appimage_stage"\' EXIT; '
            'if [ "$2" = amd64 ]; then '
            'task --taskfile "$1" tools:linuxdeploy; '
            'cp build/tools/linuxdeploy-x86_64.AppImage build/tools/linuxdeploy-x86_64-rosetta.AppImage; '
            'dd if=/dev/zero bs=1 count=3 seek=8 conv=notrunc '
            'of=build/tools/linuxdeploy-x86_64-rosetta.AppImage; '
            'linuxdeploy_override=build/tools/linuxdeploy-x86_64-rosetta.AppImage; fi; '
            'task --taskfile "$1" package:linux ARCH="$2" '
            'APPDIR="$appimage_stage/Proview.AppDir" LINUXDEPLOY_OVERRIDE="$linuxdeploy_override"'
        )
        command = ["bash", "-ceu", linux_package,
                   "package", taskfile, arch]
        subprocess.run(docker_command(image, platform, command, root), check=True)
        appimage_arch = "aarch64" if arch == "arm64" else "x86_64"
        source = root / f"build/linux-{arch}/Proview-{os.environ['PROVIEW_VERSION']}-{appimage_arch}.AppImage"
        shutil.copy2(source, output / (name + ".AppImage"))
        return

    image = "proview/fyne-cross-windows:go1.26"
    subprocess.run(["docker", "build", "--platform", "linux/amd64", "-f",
                    str(tooling / "docker/fyne-cross-windows/Dockerfile"), "-t", image, "."], check=True)
    executable = "build/windows-amd64/proview.exe"
    command = ["bash", "-ceu",
               'mkdir -p build/windows-amd64; export GOOS=windows GOARCH=amd64 CGO_ENABLED=1 '
               'CC="zig cc -target x86_64-windows-gnu -Wl,--subsystem,windows" '
               'CXX="zig c++ -target x86_64-windows-gnu -Wl,--subsystem,windows"; '
               'go test -c ./internal/pdfengine -o build/windows-amd64/pdfengine.test.exe; '
               'python3 "$1" --bundle-ocr --windows-gui -o build/windows-amd64/proview.exe', "package", build]
    subprocess.run(docker_command(image, "linux/amd64", command, root), check=True)
    with zipfile.ZipFile(output / (name + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
        archive.write(executable, "proview.exe")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", choices=("macos", "linux", "windows"), required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    parser.add_argument("--tooling", type=Path, default=Path("."))
    options = parser.parse_args()
    if options.target == "windows" and options.arch != "amd64":
        parser.error("Windows release packages support only amd64")
    # Keep paths relative so the same tooling directory is available in Docker.
    tooling = options.tooling.resolve().relative_to(Path.cwd())
    package(options.target, options.arch, os.environ["PROVIEW_BUILD_VERSION"], tooling)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Verify release checksums, archive contents and the native packaged runtime.

Uses only Python's standard library and isolated temporary data; no model calls.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import socket
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def unused_address():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return "127.0.0.1:" + str(listener.getsockname()[1])


def verify_archives(assets, version, destination):
    checksums = {}
    for line in (assets / "checksums.txt").read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        name = name.lstrip("*")
        require(re.fullmatch(r"[0-9a-f]{64}", digest), "Invalid SHA-256 checksum")
        require(Path(name).name == name and name not in checksums, "Invalid checksum filename")
        checksums[name] = digest
        with (assets / name).open("rb") as source:
            actual = hashlib.sha256()
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                actual.update(chunk)
        require(actual.hexdigest() == digest, "Checksum mismatch: " + name)

    host = platform.system().lower()
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64"}.get(platform.machine())
    require(host in ("darwin", "linux") and arch, "Unsupported verification host")
    required = {"kairos-server", "kairos-daemon", "LICENSE", "THIRD_PARTY_NOTICES.txt",
                "docs/installation.md", "docs/installation.zh-CN.md",
                "examples/daemon/run.sh", "examples/daemon/workflow.json",
                "examples/daemon/blackboard.json", "examples/review-loop/run.sh",
                "examples/review-loop/workflow.json", "examples/review-loop/work-item.json"}
    for target_os in ("darwin", "linux"):
        for target_arch in ("amd64", "arm64"):
            name = "kairos_{}_{}_{}.tar.gz".format(version, target_os, target_arch)
            require(name in checksums, "Archive missing from checksums: " + name)
            with tarfile.open(assets / name) as archive:
                members = archive.getmembers()
                names = {member.name for member in members}
                require(required <= names, "Required packaged files missing: " + name)
                require(len(names) == len(members), "Duplicate archive paths: " + name)
                for member in members:
                    path = PurePosixPath(member.name)
                    require(not path.is_absolute() and ".." not in path.parts,
                            "Unsafe archive path: " + name)
                    require(member.isfile() or member.isdir(), "Archive contains a link or special file")
                    require(not ({".env", "auth.json", ".git", "core.db"} & set(path.parts)),
                            "Private runtime files in archive")
                for binary in ("kairos-server", "kairos-daemon"):
                    require(archive.getmember(binary).mode & 0o111, "Binary is not executable")
                if (target_os, target_arch) == (host, arch):
                    # All member paths and file types were checked above.
                    archive.extractall(destination)
    return host + "/" + arch


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
            raise RuntimeError("Packaged process did not stop gracefully")


def verify_runtime(package, version, data, environment):
    for binary in ("kairos-server", "kairos-daemon"):
        output = subprocess.check_output([str(package / binary), "--version"], env=environment,
                                         cwd=data, text=True, timeout=10).strip()
        require(output == binary + " " + version, "Unexpected packaged version: " + binary)
    address = unused_address()
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def get(path):
        with http.open("http://" + address + path, timeout=3) as response:
            return response.read()
    environment = dict(environment, KAIROS_AUTH_MODE="trusted", KAIROS_LISTEN_ADDR=address,
                       KAIROS_SQLITE_PATH=str(data / "core.db"), KAIROS_ARTIFACT_DIR=str(data / "artifacts"))
    with (data / "core.log").open("ab") as log:
        process = subprocess.Popen([str(package / "kairos-server")], cwd=data,
                                   env=environment, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 15
            while True:
                require(process.poll() is None, "Packaged Core exited during startup")
                try:
                    get("/healthz")
                    break
                except (OSError, urllib.error.URLError):
                    require(time.monotonic() < deadline, "Packaged Core startup timed out")
                    time.sleep(0.1)
            html = get("/").decode()
            assets = re.findall(r'(?:src|href)="(/assets/[^"?]+)', html)
            require(assets and '<div id="root">' in html, "Embedded console missing")
            for asset in assets:
                require(get(asset), "Embedded console asset is empty")
        finally:
            stop(process)
        require(process.returncode == 0, "Packaged Core shutdown failed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--assets", type=Path, required=True, help="Directory containing all four archives and checksums.txt")
    parser.add_argument("--version", required=True, help="Exact binary/archive version, without the leading v")
    args = parser.parse_args()
    environment = {key: value for key, value in os.environ.items() if not key.startswith("KAIROS_")}
    with tempfile.TemporaryDirectory(prefix="kairos-release-") as temporary:
        root = Path(temporary)
        package = root / "package"
        package.mkdir()
        target = verify_archives(args.assets.resolve(), args.version, package)
        data = root / "data"
        data.mkdir(mode=0o700)
        verify_runtime(package, args.version, data, environment)
        for example, variable in (("daemon", "KAIROS_DAEMON_EXAMPLE"), ("review-loop", "KAIROS_REVIEW_LOOP")):
            process = subprocess.Popen(["sh", str(package / "examples" / example / "run.sh")], cwd=package,
                                       env=dict(environment, TMPDIR=str(root), **{variable + "_SMOKE": "1", variable + "_ADDR": unused_address()}),
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                process.communicate(timeout=30)
                require(process.returncode == 0, "Packaged example setup failed: " + example)
            finally:
                stop(process)
        print(json.dumps({"target": target, "version": args.version, "checksums": "ok", "archives": 4,
                          "console_assets": "ok", "example_setup": "ok", "model_calls": 0}))


if __name__ == "__main__":
    main()

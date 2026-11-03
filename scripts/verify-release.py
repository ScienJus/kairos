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
import posixpath
import re
import socket
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


REQUIRED_FILES = {"kairos-server", "kairos-daemon", "LICENSE", "THIRD_PARTY_NOTICES.txt",
                  "README.md", "README.zh-CN.md", "docs/installation.md", "docs/installation.zh-CN.md",
                  ".codex/config.toml", ".agents/skills/kairos-agent/SKILL.md",
                  "examples/quickstart/run.sh", "examples/quickstart/workflow.json",
                  "examples/quickstart/work-item.json", "examples/quickstart/README.md",
                  "examples/quickstart/README.zh-CN.md",
                  "examples/daemon/run.sh", "examples/daemon/workflow.json",
                  "examples/daemon/blackboard.json", "examples/review-loop/run.sh",
                  "examples/review-loop/workflow.json", "examples/review-loop/work-item.json"}


def verify_entry_links(archive, names):
    for source in ("README.md", "README.zh-CN.md", "docs/installation.md", "docs/installation.zh-CN.md",
                   "examples/quickstart/README.md", "examples/quickstart/README.zh-CN.md"):
        text = archive.extractfile(source).read().decode("utf-8")
        links = re.findall(r'\]\(([^\s)]+)\)|<img[^>]+src="([^"]+)"', text)
        for markdown, image in links:
            target = markdown or image
            if target.startswith("#") or urllib.parse.urlsplit(target).scheme:
                continue
            path = urllib.parse.unquote(urllib.parse.urlsplit(target).path)
            resolved = posixpath.normpath(posixpath.join(posixpath.dirname(source), path))
            require(resolved in names, "Broken packaged link: " + source + " -> " + target)


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
    for target_os in ("darwin", "linux"):
        for target_arch in ("amd64", "arm64"):
            name = "kairos_{}_{}_{}.tar.gz".format(version, target_os, target_arch)
            require(name in checksums, "Archive missing from checksums: " + name)
            with tarfile.open(assets / name) as archive:
                members = archive.getmembers()
                names = {member.name for member in members}
                require(REQUIRED_FILES <= names, "Required packaged files missing: " + name + ": " + ", ".join(sorted(REQUIRED_FILES - names)))
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
                verify_entry_links(archive, names)
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


def verify_console(get):
    content_type, html = get("/")
    require(content_type == "text/html" and b'<div id="root">' in html, "Embedded console missing")
    assets = re.findall(r'(?:src|href)="(/assets/[^"?]+)', html.decode())
    require(assets, "Embedded console assets missing")
    for asset in assets:
        content_type, body = get(asset)
        require(body and body != html and content_type != "text/html", "Console asset is empty or HTML: " + asset)
        if asset.endswith(".js"):
            require(content_type in ("text/javascript", "application/javascript"),
                    "Unexpected JavaScript Content-Type: " + asset)
        elif asset.endswith(".css"):
            require(content_type == "text/css", "Unexpected CSS Content-Type: " + asset)


def verify_runtime(package, version, data, environment):
    for binary in ("kairos-server", "kairos-daemon"):
        output = subprocess.check_output([str(package / binary), "--version"], env=environment,
                                         cwd=data, text=True, timeout=10).strip()
        require(output == binary + " " + version, "Unexpected packaged version: " + binary)
    address = unused_address()
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def get(path):
        with http.open("http://" + address + path, timeout=3) as response:
            return response.headers.get_content_type(), response.read()
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
            verify_console(get)
        finally:
            stop(process)
        require(process.returncode == 0, "Packaged Core shutdown failed")


def verify_quickstart(package, data, environment):
    address = unused_address()
    base = "http://" + address
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    # The demo must isolate itself even when launched from a configured deployment.
    environment = dict(environment, TMPDIR=str(data), KAIROS_QUICKSTART_ADDR=address,
                       KAIROS_AUTH_MODE="authenticated", KAIROS_POSTGRES_DSN="postgres://invalid.invalid/unavailable")
    log_path = data / "quickstart.log"
    with log_path.open("wb") as log:
        process = subprocess.Popen(["sh", str(package / "examples/quickstart/run.sh")], cwd=data,
                                   env=environment, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 15
            while "Ask each session:" not in log_path.read_text():
                require(process.poll() is None, "Packaged quickstart exited: " + log_path.read_text())
                require(time.monotonic() < deadline, "Packaged quickstart startup timed out")
                time.sleep(0.1)
            require('mcp_servers.kairos.url="' + base + '/mcp"' in log_path.read_text(),
                    "Quickstart command does not use its listening address")
            headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
                       "X-Kairos-Actor-Id": "package-verifier", "X-Kairos-Actor-Kind": "agent",
                       "X-Kairos-Actor-Role": "contributor"}
            def rpc(method, params, notification=False):
                payload = {"jsonrpc": "2.0", "method": method, "params": params}
                if not notification:
                    payload["id"] = 1
                request = urllib.request.Request(base + "/mcp", headers=headers,
                    data=json.dumps(payload).encode())
                with http.open(request, timeout=3) as response:
                    body = response.read()
                    return json.loads(body)["result"] if body else None
            rpc("initialize", {"protocolVersion": "2025-03-26", "capabilities": {},
                               "clientInfo": {"name": "package-verifier", "version": "1"}})
            rpc("notifications/initialized", {}, notification=True)
            result = rpc("tools/call", {"name": "find_work", "arguments": {}})
            require(not result.get("isError") and len(result["structuredContent"]["candidates"]) == 2,
                    "Packaged quickstart did not expose its two Agent Tasks through MCP")
        finally:
            stop(process)


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
        verify_quickstart(package, data, environment)
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
                          "console_assets": "ok", "quickstart_mcp": "ok", "example_setup": "ok", "model_calls": 0}))


if __name__ == "__main__":
    main()

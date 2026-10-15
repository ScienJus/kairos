#!/usr/bin/env python3
"""Verify release checksums, archive contents and the native packaged runtime.

Uses only Python's standard library and isolated temporary data; no model calls.
"""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import secrets
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


class Runtime:
    def __init__(self, package, data, environment):
        self.package = package
        self.data = data
        self.environment = environment
        self.admin = secrets.token_hex(32)
        self.base = "http://" + unused_address()
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.process = None
        self.log = None

    def request(self, method, path, payload=None, token=None, raw=False, mcp=False):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        if method == "POST" and not mcp:
            headers["Idempotency-Key"] = secrets.token_hex(16)
        if mcp:
            headers.update({"Accept": "application/json, text/event-stream",
                            "MCP-Protocol-Version": "2025-03-26"})
        request = urllib.request.Request(self.base + path, method=method, headers=headers,
                                         data=json.dumps(payload).encode() if payload is not None else None)
        with self.http.open(request, timeout=5) as response:
            body = response.read()
        return body if raw else json.loads(body)

    def api(self, method, path, payload=None, token=None):
        return self.request(method, "/api/v1" + path, payload, token)["data"]

    def tool(self, name, arguments, token):
        result = self.request("POST", "/mcp", {"jsonrpc": "2.0", "id": 1,
                              "method": "tools/call", "params": {"name": name, "arguments": arguments}},
                              token, mcp=True)
        require("error" not in result, "MCP protocol failure: " + name)
        require(not result["result"].get("isError"), "MCP tool failed: " + name)
        return result["result"]["structuredContent"]

    def start(self):
        self.log = (self.data / "core.log").open("ab")
        environment = dict(self.environment, KAIROS_AUTH_MODE="authenticated", KAIROS_ADMIN_TOKEN=self.admin,
                           KAIROS_LISTEN_ADDR=self.base.removeprefix("http://"),
                           KAIROS_SQLITE_PATH=str(self.data / "core.db"),
                           KAIROS_ARTIFACT_DIR=str(self.data / "artifacts"))
        self.process = subprocess.Popen([str(self.package / "kairos-server")], cwd=self.data,
                                        env=environment, stdout=self.log, stderr=self.log)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            require(self.process.poll() is None, "Packaged Core exited during startup")
            try:
                self.request("GET", "/healthz", raw=True)
                return
            except (OSError, urllib.error.URLError):
                time.sleep(0.1)
        raise RuntimeError("Packaged Core startup timed out")

    def stop(self):
        if self.process is not None:
            self.process.terminate()
            try:
                status = self.process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
                raise RuntimeError("Packaged Core did not stop gracefully")
            finally:
                self.process = None
                self.log.close()
            require(status == 0, "Packaged Core shutdown failed")


def verify_runtime(package, version, data, environment):
    for binary in ("kairos-server", "kairos-daemon"):
        output = subprocess.check_output([str(package / binary), "--version"], env=environment,
                                         cwd=data, text=True, timeout=10).strip()
        require(output == binary + " " + version, "Unexpected packaged version: " + binary)
    runtime = Runtime(package, data, environment)
    try:
        runtime.start()
        html = runtime.request("GET", "/", raw=True).decode()
        assets = re.findall(r'(?:src|href)="(/assets/[^"?]+)', html)
        require(assets and '<div id="root">' in html, "Embedded console missing")
        for asset in assets:
            require(runtime.request("GET", asset, raw=True), "Embedded console asset is empty")
        try:
            runtime.api("GET", "/session")
            raise RuntimeError("Authenticated Core accepted an anonymous session")
        except urllib.error.HTTPError as error:
            require(error.code == 401, "Unexpected unauthenticated response")

        human = runtime.api("POST", "/identities", {"kind": "human", "id": "release-reviewer"}, runtime.admin)["token"]
        agent = runtime.api("POST", "/identities", {"kind": "agent", "id": "release-agent", "role": "backend"}, runtime.admin)["token"]
        initialized = runtime.request("POST", "/mcp", {"jsonrpc": "2.0", "id": 1, "method": "initialize",
                                      "params": {"protocolVersion": "2025-03-26", "capabilities": {},
                                                 "clientInfo": {"name": "release-verifier", "version": "1"}}}, agent, mcp=True)
        require("result" in initialized, "MCP initialization failed")
        definition = json.loads((package / "examples/daemon/workflow.json").read_text())
        definition["graph"]["tasks"][0]["review_policy"] = "required"
        runtime.api("POST", "/definitions/workflows/daemon-workflow/versions", definition, human)
        work = runtime.api("POST", "/work-items", json.loads((package / "examples/daemon/workflow-work-item.json").read_text()), human)
        context_path = "/work-items/" + work["id"] + "/context"
        task = runtime.api("GET", context_path, token=human)["tasks"][0]
        task_id = task["id"]
        previous = []
        for attempt in (1, 2):
            context = runtime.tool("get_task_context", {"task_id": task_id}, agent)
            for field in ("staged_artifacts", "input_artifacts", "shared_artifacts"):
                require(context[field] == [], "Unexpected empty Artifact representation: " + field)
            if attempt == 2:
                require(context["task"]["reviews"][0]["feedback"] == "Add the correction.", "Rejection feedback lost")
            claim = runtime.tool("claim_task", {"task_id": task_id, "operation_id": "claim-" + str(attempt)}, agent)["claim"]
            content = ("Release artifact " + str(attempt) + ": 验收\n").encode()
            artifact = runtime.tool("upload_artifact", {"task_id": task_id, "claim_id": claim["id"],
                                    "operation_id": "upload-" + str(attempt), "name": "result",
                                    "content_base64": base64.b64encode(content).decode()}, agent)["artifact"]
            arguments = {"task_id": task_id, "claim_id": claim["id"], "artifact_ids": [artifact["id"]], "result": "Ready for review."}
            context = runtime.tool("get_task_context", {"task_id": task_id}, agent)
            groups = context["workflow"]["choice_groups"]
            if groups:
                require(any(group["id"] == "exit:execute" for group in groups), "Unexpected terminal choice")
                arguments["transition"] = {"choice_group_id": "exit:execute"}
            runtime.tool("submit_task", arguments, agent)
            view = runtime.api("GET", context_path, token=human)
            task = next(task for task in view["tasks"] if task["id"] == task_id)
            require(task["status"] == "in_review", "Submission did not enter Review")
            review = task["reviews"][-1]
            previous.append((artifact["id"], content))
            if attempt == 1:
                runtime.api("POST", "/tasks/" + task_id + "/reviews/" + review["id"] + "/decision",
                            {"decision": "rejected", "feedback": "Add the correction."}, human)

        runtime.stop()
        runtime.start()
        view = runtime.api("GET", context_path, token=human)
        task = next(task for task in view["tasks"] if task["id"] == task_id)
        require(len(task["submissions"]) == 2 and task["reviews"][0]["status"] == "rejected", "Review history lost after restart")
        require(task["reviews"][-1]["id"] == review["id"] and task["status"] == "in_review", "Pending Review lost after restart")
        runtime.api("POST", "/tasks/" + task_id + "/reviews/" + review["id"] + "/decision",
                    {"decision": "approved", "feedback": "Release fixture accepted."}, human)
        runtime.stop()
        runtime.start()
        view = runtime.api("GET", context_path, token=human)
        require(view["work_item"]["status"] == "completed" and view["active_claims"] == [], "Completed state lost after restart")
        require(len(view["artifacts"]) == 2, "Submitted Artifact history changed")
        for artifact_id, content in previous:
            require(runtime.request("GET", "/api/v1/artifacts/" + artifact_id + "/content", token=human, raw=True) == content,
                    "Artifact bytes changed after restart")
    finally:
        runtime.stop()


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
            subprocess.run(["sh", str(package / "examples" / example / "run.sh")], cwd=package,
                           env=dict(environment, TMPDIR=str(root), **{variable + "_SMOKE": "1", variable + "_ADDR": unused_address()}),
                           check=True, timeout=30, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        print(json.dumps({"target": target, "version": args.version, "checksums": "ok", "archives": 4,
                          "console_assets": "ok", "authenticated_http_mcp": "ok", "review_resubmission": "ok",
                          "restart_persistence": "ok", "example_setup": "ok", "model_calls": 0}))


if __name__ == "__main__":
    main()

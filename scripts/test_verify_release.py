"""Regressions for package onboarding files, links and console asset fallbacks."""

import importlib.util
import hashlib
import io
from pathlib import Path
import tarfile
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).with_name("verify-release.py"))
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class PackageOnboardingTest(unittest.TestCase):
    def make_archives(self, root, missing=None, broken_link=False):
        checksums = []
        for target_os in ("darwin", "linux"):
            for arch in ("amd64", "arm64"):
                name = "kairos_test_{}_{}.tar.gz".format(target_os, arch)
                with tarfile.open(root / name, "w:gz") as archive:
                    for path in sorted(verifier.REQUIRED_FILES - {missing}):
                        body = b"[Missing guide](docs/missing.md)" if broken_link and path == "README.md" else b""
                        member = tarfile.TarInfo(path)
                        member.size = len(body)
                        member.mode = 0o755 if path in ("kairos-server", "kairos-daemon") else 0o644
                        archive.addfile(member, io.BytesIO(body))
                checksums.append(hashlib.sha256((root / name).read_bytes()).hexdigest() + "  " + name)
        (root / "checksums.txt").write_text("\n".join(checksums))

    def test_complete_onboarding_package(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_archives(root)
            destination = root / "extracted"
            destination.mkdir()
            verifier.verify_archives(root, "test", destination)
            self.assertTrue((destination / ".codex/config.toml").is_file())
            self.assertTrue((destination / "examples/quickstart/run.sh").is_file())

    def test_missing_onboarding_files_are_rejected(self):
        for missing in ("examples/quickstart/run.sh", "examples/quickstart/workflow.json",
                        "examples/quickstart/work-item.json", ".codex/config.toml",
                        ".agents/skills/kairos-agent/SKILL.md"):
            with self.subTest(missing=missing), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                self.make_archives(root, missing=missing)
                with self.assertRaisesRegex(RuntimeError, "Required packaged files missing:.*" + missing):
                    verifier.verify_archives(root, "test", root / "extracted")

    def test_broken_entry_link_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_archives(root, broken_link=True)
            with self.assertRaisesRegex(RuntimeError, "Broken packaged link: README.md -> docs/missing.md"):
                verifier.verify_archives(root, "test", root / "extracted")


class ConsoleAssetsTest(unittest.TestCase):
    def setUp(self):
        self.html = b'<div id="root"></div><script src="/assets/app.js"></script><link href="/assets/app.css">'
        self.responses = {"/": ("text/html", self.html), "/assets/app.js": ("text/javascript", b"export {};"),
                          "/assets/app.css": ("text/css", b"body { margin: 0; }")}

    def test_valid_assets(self):
        for mime in ("text/javascript", "application/javascript"):
            with self.subTest(mime=mime):
                self.responses["/assets/app.js"] = (mime, b"export {};")
                verifier.verify_console(self.responses.__getitem__)

    def test_missing_asset_html_fallback(self):
        for asset in ("/assets/app.js", "/assets/app.css"):
            with self.subTest(asset=asset):
                responses = dict(self.responses, **{asset: ("text/html", self.html)})
                with self.assertRaisesRegex(RuntimeError, "Console asset is empty or HTML: " + asset):
                    verifier.verify_console(responses.__getitem__)

    def test_html_body_with_asset_mime(self):
        self.responses["/assets/app.js"] = ("text/javascript", self.html)
        with self.assertRaisesRegex(RuntimeError, "Console asset is empty or HTML"):
            verifier.verify_console(self.responses.__getitem__)

    def test_wrong_asset_mime(self):
        for asset, reason in (("/assets/app.js", "JavaScript"), ("/assets/app.css", "CSS")):
            with self.subTest(asset=asset):
                responses = dict(self.responses, **{asset: ("text/plain", b"not an executable asset")})
                with self.assertRaisesRegex(RuntimeError, "Unexpected " + reason + " Content-Type: " + asset):
                    verifier.verify_console(responses.__getitem__)


if __name__ == "__main__":
    unittest.main()

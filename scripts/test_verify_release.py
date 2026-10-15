"""Regression checks for the release integrity gate, without building binaries."""

import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).with_name("verify-release.py"))
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class ReleaseIntegrityTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.output = self.root / "extracted"
        self.output.mkdir()

    def make_assets(self, omit=None, extra=None):
        files = ["kairos-server", "kairos-daemon", "LICENSE", "THIRD_PARTY_NOTICES.txt",
                 "docs/installation.md", "docs/installation.zh-CN.md",
                 "examples/daemon/run.sh", "examples/daemon/workflow.json",
                 "examples/daemon/blackboard.json", "examples/review-loop/run.sh",
                 "examples/review-loop/workflow.json", "examples/review-loop/work-item.json"]
        checksums = []
        for os_name in ("darwin", "linux"):
            for architecture in ("amd64", "arm64"):
                path = self.assets / ("kairos_test_{}_{}.tar.gz".format(os_name, architecture))
                with tarfile.open(path, "w:gz") as archive:
                    for name in files:
                        if name == omit:
                            continue
                        member = tarfile.TarInfo(name)
                        member.size = 1
                        member.mode = 0o755 if name.startswith("kairos-") else 0o644
                        archive.addfile(member, io.BytesIO(b"x"))
                    if extra:
                        archive.addfile(extra)
                checksums.append(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name)
        (self.assets / "checksums.txt").write_text("\n".join(checksums) + "\n")

    def verify(self):
        return verifier.verify_archives(self.assets, "test", self.output)

    def test_valid_set_extracts_native_package(self):
        self.make_assets()
        self.assertIn(self.verify(), ("darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64"))
        self.assertEqual((self.output / "kairos-server").read_bytes(), b"x")

    def test_changed_archive_rejected_before_extraction(self):
        self.make_assets()
        archive = next(self.assets.glob("*.tar.gz"))
        archive.write_bytes(archive.read_bytes() + b"changed")
        with self.assertRaisesRegex(RuntimeError, "Checksum mismatch"):
            self.verify()
        self.assertEqual(list(self.output.iterdir()), [])

    def test_checksum_valid_archive_still_requires_both_binaries(self):
        self.make_assets(omit="kairos-daemon")
        with self.assertRaisesRegex(RuntimeError, "Required packaged files missing"):
            self.verify()

    def test_parent_path_rejected_before_extraction(self):
        self.make_assets(extra=tarfile.TarInfo("../outside"))
        with self.assertRaisesRegex(RuntimeError, "Unsafe archive path"):
            self.verify()
        self.assertFalse((self.root / "outside").exists())

    def test_link_rejected_before_extraction(self):
        link = tarfile.TarInfo("alias")
        link.type = tarfile.SYMTYPE
        link.linkname = "/outside"
        self.make_assets(extra=link)
        with self.assertRaisesRegex(RuntimeError, "link or special file"):
            self.verify()


if __name__ == "__main__":
    unittest.main()

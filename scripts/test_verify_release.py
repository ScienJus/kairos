"""Regressions for missing console assets that return HTTP 200 SPA HTML."""

import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).with_name("verify-release.py"))
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


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

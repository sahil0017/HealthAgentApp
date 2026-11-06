import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "publish_apk_release", Path(__file__).resolve().parents[1] / "publish-apk-release.py"
)
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)

SHA = "a" * 40
TAG = "v1.0.0-build.100"


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        apk = self.directory / f"sehat-{TAG}.apk"
        apk.write_bytes(b"test apk")
        (self.directory / "SHA256SUMS.txt").write_text(
            f"{hashlib.sha256(apk.read_bytes()).hexdigest()}  {apk.name}\n"
        )
        self.calls = []
        self.refs = [{"ref": f"refs/tags/{TAG}", "object": {"type": "commit", "sha": SHA}}]
        self.releases = []

    def gh(self, *args):
        self.calls.append(args)
        if "matching-refs" in " ".join(args):
            return json.dumps(self.refs)
        if "--slurp" in args:
            return json.dumps([self.releases])
        if args[:2] == ("release", "download"):
            name = args[args.index("--pattern") + 1]
            destination = Path(args[args.index("--dir") + 1]) / name
            destination.write_bytes((self.directory / name).read_bytes())
        return ""

    def publish(self):
        with patch.object(publisher, "gh", self.gh):
            publisher.publish("owner/repo", SHA, TAG, "debug", "123", self.directory)

    def release(self, tag, assets=(), draft=False):
        return {
            "tag_name": tag,
            "draft": draft,
            "prerelease": False,
            "assets": [{"name": name} for name in assets],
        }

    def test_publishes_and_promotes_new_build(self):
        self.releases = [self.release("v1.0.0", ["sehat-v1.0.0-build.95.apk"])]
        self.publish()
        self.assertTrue(any(call[:2] == ("release", "create") and "--draft" in call for call in self.calls))
        self.assertEqual(2, sum(call[:2] == ("release", "upload") for call in self.calls))
        self.assertIn(("release", "edit", TAG, "--repo", "owner/repo", "--latest"), self.calls)
        self.assertIn("Debug-signed", next(call for call in self.calls if call[:2] == ("release", "create"))[-1])

    def test_out_of_order_build_is_not_promoted(self):
        self.releases = [self.release("v1.0.0-build.101")]
        self.publish()
        self.assertNotIn(("release", "edit", TAG, "--repo", "owner/repo", "--latest"), self.calls)

    def test_release_notes_include_prior_apk_downloads(self):
        self.releases = [
            self.release("v1.0.0", ["sehat-v1.0.0-build.95.apk"]),
            self.release("v1.0.0-build.99", ["sehat-v1.0.0-build.99.apk"]),
        ]
        self.publish()
        notes = next(call for call in self.calls if call[:2] == ("release", "create"))[-1]
        self.assertIn("https://github.com/owner/repo/releases/download/v1.0.0/sehat-v1.0.0-build.95.apk", notes)
        self.assertLess(notes.index("[v1.0.0-build.99]"), notes.index("[v1.0.0-build.95]"))

    def test_retry_compares_existing_assets_without_overwriting(self):
        names = [f"sehat-{TAG}.apk", "SHA256SUMS.txt"]
        self.releases = [self.release(TAG, names)]
        self.publish()
        self.assertEqual(2, sum(call[:2] == ("release", "download") for call in self.calls))
        self.assertFalse(any(call[:2] == ("release", "upload") for call in self.calls))
        self.assertFalse(any(call[:2] == ("release", "create") for call in self.calls))

    def test_retry_recovers_missing_asset_and_draft(self):
        self.releases = [self.release(TAG, [f"sehat-{TAG}.apk"], draft=True)]
        self.publish()
        self.assertEqual(1, sum(call[:2] == ("release", "upload") for call in self.calls))
        self.assertIn(("release", "edit", TAG, "--repo", "owner/repo", "--draft=false", "--latest=false"), self.calls)

    def test_refuses_mismatched_tag(self):
        self.refs[0]["object"]["sha"] = "b" * 40
        with self.assertRaisesRegex(ValueError, "Existing tag"):
            self.publish()
        self.assertFalse(any(call[:2] == ("release", "create") for call in self.calls))

    def test_refuses_bad_checksum(self):
        (self.directory / "SHA256SUMS.txt").write_text("bad\n")
        with self.assertRaisesRegex(ValueError, "Invalid checksum"):
            self.publish()
        self.assertEqual([], self.calls)

    def test_refuses_different_existing_asset(self):
        self.releases = [self.release(TAG, [f"sehat-{TAG}.apk"])]
        original = self.gh

        def gh_with_changed_asset(*args):
            result = original(*args)
            if args[:2] == ("release", "download"):
                name = args[args.index("--pattern") + 1]
                (Path(args[args.index("--dir") + 1]) / name).write_bytes(b"different")
            return result

        with patch.object(publisher, "gh", gh_with_changed_asset):
            with self.assertRaisesRegex(ValueError, "refusing to overwrite"):
                publisher.publish("owner/repo", SHA, TAG, "debug", "123", self.directory)
        self.assertFalse(any(call[:2] == ("release", "upload") for call in self.calls))

    def test_creates_missing_tag_at_expected_commit(self):
        self.refs = []
        self.publish()
        self.assertIn(
            ("api", "repos/owner/repo/git/refs", "-f", f"ref=refs/tags/{TAG}", "-f", f"sha={SHA}"),
            self.calls,
        )


if __name__ == "__main__":
    unittest.main()

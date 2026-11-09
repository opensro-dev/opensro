"""
test_seed_publish.py - seed publish: the pin is checked before gh runs

Publish must never upload a byte the manifest does not pin, never
create the release before every asset verifies, and never overwrite
an asset a release already holds (a refresh is a new dated tag). The
gh calls run against a fake runner; the seeds are real, built with
revtool_seed.encode_seed.

    py -3 -m unittest tools.re.revtool.test_seed_publish
"""

from __future__ import annotations

import hashlib
import io
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import revtool_seed  # noqa: E402


def _seed_file(directory, target):
	payload = {
		"format": revtool_seed.SEED_FORMAT,
		"version": revtool_seed.SEED_VERSION,
		"target": target,
		"binary_sha256": "ab" * 32,
		"binary_sha_source": "fixture",
		"functions": [[0x401000, "sub_401000"], [0x403000, "CIFMainFrame_Draw"]],
		"ledger": ['{"_t": "_meta", "target_sha256": "' + "ab" * 32 + '"}'],
	}
	path = Path(directory) / f"revtool-seed-{target}.xz"
	blob = revtool_seed.encode_seed(payload)
	path.write_bytes(blob)
	pin = {
		"target": target,
		"file": path.name,
		"sha256": hashlib.sha256(blob).hexdigest(),
		"bytes": len(blob),
		"binary_sha256": "ab" * 32,
		"functions": 2,
		"ledger_lines": 1,
	}
	return path, pin


class FakeGh:
	"""Records argv; release view answers from the fixture state."""

	def __init__(self, exists=False, assets=()):
		self.calls = []
		self.exists = exists
		self.assets = list(assets)

	def __call__(self, argv):
		self.calls.append(list(argv))
		if argv[2] == "view":
			if not self.exists:
				return 1, "", "not found"
			return 0, json.dumps({"assets": [{"name": n} for n in self.assets]}), ""
		return 0, "", ""


class PublishTest(unittest.TestCase):
	def setUp(self):
		self.tmp = tempfile.TemporaryDirectory()
		self.addCleanup(self.tmp.cleanup)
		self.dir = Path(self.tmp.name)
		self.paths, self.pins = [], []
		for target in ("client", "server"):
			path, pin = _seed_file(self.dir, target)
			self.paths.append(path)
			self.pins.append(pin)
		self.manifest = self.dir / "seed-manifest.json"
		self.manifest.write_text(json.dumps({"assets": self.pins}), encoding="utf-8")

	def publish(self, gh, dry_run=False):
		out = io.StringIO()
		with redirect_stdout(out):
			url = revtool_seed.publish_assets(
				str(self.manifest), str(self.dir), "revtool-seed-2026-11-09",
				"opensro-dev/opensro", run=gh, dry_run=dry_run)
		return url, out.getvalue()

	def test_dry_run_verifies_and_runs_nothing(self):
		gh = FakeGh()
		url, text = self.publish(gh, dry_run=True)
		self.assertEqual(gh.calls, [])
		self.assertIn("verified revtool-seed-client.xz", text)
		self.assertIn("[dry-run] gh release create", text)
		create = next(line for line in text.splitlines() if line.startswith("[dry-run] gh release create"))
		self.assertIn("--latest=false", create.split())
		self.assertEqual(url, "https://github.com/opensro-dev/opensro/releases/tag/revtool-seed-2026-11-09")

	def test_creates_then_uploads_when_the_release_is_missing(self):
		gh = FakeGh(exists=False)
		self.publish(gh)
		self.assertEqual([c[2] for c in gh.calls], ["view", "create", "upload"])
		# A seed release never becomes the repository's Latest.
		self.assertIn("--latest=false", gh.calls[1])
		upload = gh.calls[-1]
		self.assertIn("revtool-seed-2026-11-09", upload)
		self.assertEqual([Path(p).name for p in upload if p.endswith(".xz")],
						 ["revtool-seed-client.xz", "revtool-seed-server.xz"])

	def test_uploads_only_when_the_release_already_stands_empty(self):
		gh = FakeGh(exists=True)
		self.publish(gh)
		self.assertEqual([c[2] for c in gh.calls], ["view", "upload"])

	def test_refuses_when_the_release_already_holds_an_asset(self):
		gh = FakeGh(exists=True, assets=["revtool-seed-client.xz"])
		with self.assertRaises(revtool_seed.SeedError):
			self.publish(gh)
		self.assertEqual([c[2] for c in gh.calls], ["view"])

	def test_refuses_bytes_the_pin_does_not_name(self):
		self.paths[0].write_bytes(self.paths[0].read_bytes() + b" ")
		gh = FakeGh()
		with self.assertRaises(revtool_seed.SeedError):
			self.publish(gh)
		self.assertEqual(gh.calls, [])

	def test_refuses_a_missing_seed_before_gh(self):
		os.remove(self.paths[1])
		gh = FakeGh()
		with self.assertRaises(revtool_seed.SeedError):
			self.publish(gh)
		self.assertEqual(gh.calls, [])


if __name__ == "__main__":
	unittest.main()

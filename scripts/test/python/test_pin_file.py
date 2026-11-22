"""
===========================================================================

test_pin_file.py - a pin file is read whole and right, or the import stops

The seven importers that check a later client's archives read their pin
files through pin_file.read_pin_field. A pin that is missing, unreadable,
of another format or without its field must stop the import, never read
as empty: an unchecked archive is what the pin exists to prevent.

===========================================================================
"""
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pin_file  # noqa: E402
from prepare_client_resources import file_digest  # noqa: E402

FORMAT = "opensro-vsro-archive-pins-v1"


class ReadPinField(unittest.TestCase):
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)

	def pin(self, value):
		# One file per case: the cases are built together, before any is read.
		path = Path(self.directory.name) / f"pins-{len(list(Path(self.directory.name).iterdir()))}.json"
		path.write_text(value if isinstance(value, str) else json.dumps(value), encoding="utf-8")
		return path

	# ================
	# test_the_field_is_returned
	# ================
	def test_the_field_is_returned(self):
		archives = {"Media": "a" * 64}
		self.assertEqual(pin_file.read_pin_field(self.pin({"format": FORMAT, "archives": archives}), FORMAT, "archives"),
			archives)
		single = self.pin({"format": FORMAT, "sha256": "b" * 64})
		self.assertEqual(pin_file.read_pin_field(single, FORMAT, "sha256", str), "b" * 64)

	# ================
	# test_a_bad_pin_stops_the_import
	# ================
	def test_a_bad_pin_stops_the_import(self):
		cases = {
			"missing": (Path(self.directory.name) / "absent.json", "is unreadable"),
			"broken": (self.pin("{"), "is unreadable"),
			"not an object": (self.pin([1]), "is not a"),
			"foreign format": (self.pin({"format": "other", "archives": {}}), "is not a"),
			"no field": (self.pin({"format": FORMAT}), "is not a"),
			"wrong kind": (self.pin({"format": FORMAT, "archives": ["Media"]}), "is not a"),
		}
		for name, (path, message) in cases.items():
			with self.subTest(name), self.assertRaisesRegex(SystemExit, message):
				pin_file.read_pin_field(path, FORMAT, "archives")


class RequirePinnedArchive(unittest.TestCase):
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		(self.root / "Media.pk2").write_bytes(b"archive")
		self.digest = file_digest(self.root / "Media.pk2")

	def pins(self, archives):
		path = self.root / "pins.json"
		path.write_text(json.dumps({"format": FORMAT, "archives": archives}), encoding="utf-8")
		return path

	# ================
	# test_a_matching_archive_is_returned
	# ================
	def test_a_matching_archive_is_returned(self):
		path = pin_file.require_pinned_archive(self.pins({"Media": self.digest}), FORMAT, self.root, "Media", "x")
		self.assertEqual(path, self.root / "Media.pk2")

	# ================
	# test_an_unchecked_archive_stops_the_import
	#
	# Unpinned, absent and changed archives each stop before a byte of
	# them is read.
	# ================
	def test_an_unchecked_archive_stops_the_import(self):
		cases = {
			"unpinned": ({"Data": self.digest}, "Media", "pins no Media.pk2"),
			"absent": ({"Data": self.digest}, "Data", "Data.pk2 not found; name the folder"),
			"changed": ({"Media": "0" * 64}, "Media", "does not match its pin in pins.json"),
		}
		for label, (archives, name, message) in cases.items():
			with self.subTest(label), self.assertRaisesRegex(SystemExit, message):
				pin_file.require_pinned_archive(self.pins(archives), FORMAT, self.root, name, "name the folder")


if __name__ == "__main__":
	unittest.main()

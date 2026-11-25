"""
===========================================================================

test_find_duplicates.py - the duplicate finder counts copies, not links

A hard link shares its storage, so it is no waste; a byte copy is. A loose
file a pack also stores is counted once per file.

===========================================================================
"""
import json
import os
import tempfile
import unittest
from pathlib import Path

import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)

import sys  # noqa: E402

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tools"))

import find_duplicates  # noqa: E402

PAYLOAD = b"x" * 70000


class FindDuplicatesTests(unittest.TestCase):
	# ================
	# test_a_copy_is_waste_and_a_hard_link_is_not
	# ================
	def test_a_copy_is_waste_and_a_hard_link_is_not(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			(root / "a").mkdir()
			(root / "b").mkdir()
			(root / "a" / "one.bin").write_bytes(PAYLOAD)
			(root / "b" / "copy.bin").write_bytes(PAYLOAD)
			os.link(root / "a" / "one.bin", root / "a" / "link.bin")
			(root / "a" / "other.bin").write_bytes(PAYLOAD[:-1] + b"y")
			groups = find_duplicates.identical_groups(find_duplicates.walk(root, 1))
			summary = find_duplicates.report(root, groups, [], 5)
		self.assertEqual(summary["identical"]["groups"], 1)
		self.assertEqual(summary["identical"]["wasted_bytes"], len(PAYLOAD))

	# ================
	# test_a_loose_file_a_pack_stores_is_counted
	# ================
	def test_a_loose_file_a_pack_stores_is_counted(self):
		with tempfile.TemporaryDirectory() as folder:
			public = Path(folder)
			packs = public / "assets" / "packs"
			packs.mkdir(parents=True)
			(public / "assets" / "model.bin").write_bytes(PAYLOAD)
			manifest = packs / "manifest.json"
			manifest.write_text(json.dumps({"assets": [
				{"path": "/assets/model.bin", "packPath": "/assets/packs/p.bin"},
				{"path": "/assets/gone.bin", "packPath": "/assets/packs/p.bin"},
				{"path": "/assets/unpacked.bin"}
			]}), encoding="utf-8")
			packed = find_duplicates.packed_loose(manifest)
		self.assertEqual([(path.name, size) for path, size in packed], [("model.bin", len(PAYLOAD))])


if __name__ == "__main__":
	unittest.main()

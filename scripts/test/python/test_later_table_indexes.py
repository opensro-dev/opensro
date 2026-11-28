"""
===========================================================================

test_later_table_indexes.py - the checked-in shard-index census (#4762)

scripts/data/later-table-indexes.json records which textdata files are
shard indexes in the v1.150, v1.696 and VSRO clients, and what each
lists. Its shape is checked everywhere. Each client whose files this
machine holds (the native extraction, SRO_V1696_ROOT, SRO_VSRO_ROOT) is
read again and must match it exactly; a client the machine lacks is
skipped by name.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import unittest
from pathlib import Path

import import_vsro_alexandria as tables
import later_tables

CENSUS = Path(__file__).resolve().parents[2] / "data" / "later-table-indexes.json"
CLIENTS = ("v1.150", "v1.696", "vsro")
V1696_ENV, VSRO_ENV = "SRO_V1696_ROOT", "SRO_VSRO_ROOT"


# ================
# read_census
# ================
def read_census():
	return json.loads(CENSUS.read_text(encoding="utf-8"))


# ================
# CensusShapeTests
# ================
class CensusShapeTests(unittest.TestCase):
	def test_every_client_lists_folded_txt_shards_once(self):
		census = read_census()
		self.assertEqual(census["format"], "opensro-later-table-indexes-v1")
		self.assertEqual(sorted(census["clients"]), sorted(CLIENTS))
		for client, matrix in census["clients"].items():
			for name, listed in matrix.items():
				with self.subTest(client=client, name=name):
					self.assertTrue(listed)
					for shard in [name] + listed:
						self.assertEqual(shard, shard.lower())
						self.assertTrue(shard.endswith(".txt"))
					self.assertEqual(len(set(listed)), len(listed))
					# No client nests an index (#4730): no listed shard
					# is itself an index of the same client.
					self.assertFalse(set(listed) & set(matrix))

	def test_the_v1150_tree_indexes_its_three_object_tables(self):
		self.assertEqual(sorted(read_census()["clients"]["v1.150"]), ["characterdata.txt", "itemdata.txt", "skilldata.txt"])


# ================
# LiveCensusTests
#
# The census against each client archive this machine holds.
# ================
class LiveCensusTests(unittest.TestCase):
	def check(self, client, archive):
		self.assertEqual(later_tables.index_matrix(archive), read_census()["clients"][client])

	def test_the_native_v1150_tree(self):
		root = tables.EXTRACTED_ROOT / "Media_extracted" / tables.TEXTDATA
		if not root.is_dir():
			self.skipTest(f"no native extraction at {root}")
		self.check("v1.150", later_tables.Archive.tree(root))

	def test_the_v1696_client(self):
		root = os.environ.get(V1696_ENV, "").strip()
		if not root or not (Path(root) / "Media.pk2").is_file():
			self.skipTest(f"{V1696_ENV} names no v1.696 client")
		with later_tables.Archive.open(root) as source:
			self.check("v1.696", source)

	def test_the_vsro_client(self):
		root = os.environ.get(VSRO_ENV, "").strip()
		if not root or not (Path(root) / "Media.pk2").is_file():
			self.skipTest(f"{VSRO_ENV} names no VSRO client")
		with later_tables.Archive.open(root) as source:
			self.check("vsro", source)


if __name__ == "__main__":
	unittest.main()

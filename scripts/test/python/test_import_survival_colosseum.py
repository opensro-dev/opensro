"""
===========================================================================

test_import_survival_colosseum.py - exact venue selection and native catalogue safety

Synthetic metadata only; the owned archive import separately validates every
model dependency with the production publisher's parsers.

===========================================================================
"""
import sys
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import import_survival_colosseum as arena


# ================
# ColosseumImportTests
# ================
class ColosseumImportTests(unittest.TestCase):

	# ================
	# test_repository_assets_have_complete_pinned_inputs
	# ================
	def test_repository_assets_have_complete_pinned_inputs(self):
		manifest = json.loads((arena.ASSET_ROOT / arena.ASSET_MANIFEST).read_text(encoding="utf8"))
		self.assertEqual(manifest["region"], 25580)
		self.assertEqual(manifest["archives"], json.loads(arena.PIN_FILE.read_text(encoding="utf8")))
		self.assertGreater(len(manifest["files"]), 0)
		for key in manifest["files"]:
			archive, name = key.split("/", 1)
			self.assertNotEqual(Path(name).suffix.lower(), ".pk2")
			arena.read_asset_input(arena.ASSET_ROOT, manifest, archive, name)

	# ================
	# test_repository_input_refuses_corruption_and_traversal
	# ================
	def test_repository_input_refuses_corruption_and_traversal(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			(root / "Map").mkdir()
			(root / "Map/terrain.m").write_bytes(b"terrain")
			manifest = {"files": {"Map/terrain.m": hashlib.sha256(b"terrain").hexdigest()}}
			self.assertEqual(arena.read_asset_input(root, manifest, "Map", "TERRAIN.M"), b"terrain")
			(root / "Map/terrain.m").write_bytes(b"changed")
			with self.assertRaises(ValueError):
				arena.read_asset_input(root, manifest, "Map", "terrain.m")
			with self.assertRaises(ValueError):
				arena.read_asset_input(root, manifest, "Map", "absent.m")
			with self.assertRaises(ValueError):
				arena.read_asset_input(root, manifest, "Map", "../../outside.m")

	# ================
	# test_higher_level_rows_reuse_native_models_and_skills
	# ================
	def test_higher_level_rows_reuse_native_models_and_skills(self):
		base = ["0"] * 120
		base[0], base[1], base[2], base[57] = "1", "3874", "MOB_RM_ROCKY", "88"
		base[52], base[59], base[89] = "mob/roc/rocky.bsr", "18722", "3575"
		rows = arena.arena_character_rows([base])
		self.assertEqual([int(row[57]) for row in rows], list(range(89, 98)))
		self.assertEqual(len({row[1] for row in rows}), 9)
		self.assertTrue(all(row[52] == base[52] and row[89:99] == base[89:99] for row in rows))
		self.assertGreater(int(rows[-1][59]), int(rows[0][59]))
		self.assertEqual(base[57], "88")
		collision = list(base)
		collision[1], collision[2] = rows[0][1], "MOB_OTHER"
		with self.assertRaises(ValueError):
			arena.arena_character_rows([base, collision])
		with self.assertRaises(ValueError):
			arena.arena_character_rows([])

	# ================
	# test_region_requires_matching_authored_starts
	# ================
	def test_region_requires_matching_authored_starts(self):
		rows = [f"1\t{i}\tSTRUCTURE_POS_COLOSSEUM_SAFEZONE_{i:02}\t0\t0\t25580\t0\t0\t0\t0\t0\t0\t116" for i in range(10)]
		raw = "\r\n".join(rows).encode("cp949")
		self.assertEqual(arena.colosseum_region(raw), 25580)
		with self.assertRaises(ValueError):
			arena.colosseum_region(raw.replace(b"25580", b"25581", 1))
		with self.assertRaises(ValueError):
			arena.colosseum_region(rows[0].encode("cp949"))

	# ================
	# test_objects_preserve_native_and_reject_conflicts
	# ================
	def test_objects_preserve_native_and_reject_conflicts(self):
		native = b'JMXVOBJI1000\r\n1\r\n00000 0x00000000 "res/native.bsr"\r\n'
		imported = b'JMXVOBJI1000\r\n2\r\n00000 0x00000000 "res/changed.bsr"\r\n00001 0x00000000 "res/arena.bsr"\r\n'
		result = arena.merge_objects(native, imported, {1})
		self.assertIn(b'res/native.bsr', result)
		self.assertIn(b'res/arena.bsr', result)
		self.assertNotIn(b'res/changed.bsr', result)
		with self.assertRaises(ValueError):
			arena.merge_objects(native, imported, {0})
		with self.assertRaises(ValueError):
			arena.merge_objects(native, imported, {2})


if __name__ == "__main__":
	unittest.main()

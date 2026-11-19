"""
===========================================================================

test_import_survival_colosseum.py - exact venue selection and native catalogue safety

Synthetic metadata only; the owned archive import separately validates every
model dependency with the production publisher's parsers.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
import hashlib
import json
import tempfile
import unittest
from contextlib import ExitStack
from unittest.mock import patch
from pathlib import Path

import import_survival_colosseum as arena


# ================
# ColosseumImportTests
# ================
class ColosseumImportTests(unittest.TestCase):

	# ================
	# test_archives_require_exact_hashes_before_parsing_or_writing
	# ================
	def test_archives_require_exact_hashes_before_parsing_or_writing(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			client, output = root / "client", root / "output"
			client.mkdir()
			pins = {name: hashlib.sha256(name.encode()).hexdigest() for name in ("Map", "Media", "Data", "Particles")}
			pin_file = root / "pins.json"
			pin_file.write_text(json.dumps(pins), encoding="utf8")
			with patch.object(arena, "PIN_FILE", pin_file), patch.object(arena, "PROJECT_PIN_FILE", root / "absent.json"), patch.object(arena.source, "archive_index") as parse:
				with self.assertRaisesRegex(ValueError, "Missing.*Map.pk2"):
					arena.build_variant(client, root / "native", output)
				for name in pins:
					(client / f"{name}.pk2").write_bytes(name.encode())
				(client / "Particles.pk2").write_bytes(b"wrong build")
				with self.assertRaisesRegex(ValueError, "Wrong .*Particles.pk2\\nActual SHA-256"):
					arena.build_variant(client, root / "native", output)
				parse.assert_not_called()
				self.assertFalse(output.exists())
			(client / "Particles.pk2").write_bytes(b"")
			with ExitStack() as stack, self.assertRaisesRegex(ValueError, "Empty archive"):
				arena.open_archives(stack, client, pins)
			(client / "Particles.pk2").write_bytes(b"Particles")
			with ExitStack() as stack:
				archives, chosen = arena.open_archives(stack, client, pins)
				self.assertEqual({name: bytes(data) for name, data in archives.items()}, {name: name.encode() for name in pins})
				self.assertEqual(chosen, pins)
			self.assertIn("v1.696", arena.archive_requirements(pins))

	# ================
	# test_the_projects_pinned_client_is_accepted
	#
	# The shard's variant build verifies its own v1.696 pins; the arena
	# import accepts that client too, and still refuses one matching
	# neither set (#3931).
	# ================
	def test_the_projects_pinned_client_is_accepted(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			client = root / "client"
			client.mkdir()
			for name in arena.ARCHIVES:
				(client / f"{name}.pk2").write_bytes(b"project " + name.encode())
			own = {name: hashlib.sha256(b"other " + name.encode()).hexdigest() for name in arena.ARCHIVES}
			project = {name: hashlib.sha256(b"project " + name.encode()).hexdigest() for name in arena.ARCHIVES}
			project["Music"] = hashlib.sha256(b"music").hexdigest()
			(root / "own.json").write_text(json.dumps(own), encoding="utf8")
			(root / "project.json").write_text(json.dumps({"format": "opensro-vsro-archive-pins-v1", "archives": project}), encoding="utf8")
			with patch.object(arena, "PIN_FILE", root / "own.json"), patch.object(arena, "PROJECT_PIN_FILE", root / "project.json"):
				accepted = arena.accepted_pins()
				self.assertEqual(len(accepted), 2)
				self.assertNotIn("Music", accepted[1])
				with ExitStack() as stack:
					_, chosen = arena.open_archives(stack, client, accepted)
					self.assertEqual(chosen, accepted[1])
				(client / "Map.pk2").write_bytes(b"neither")
				with ExitStack() as stack, self.assertRaisesRegex(ValueError, "Wrong .*Map.pk2"):
					arena.open_archives(stack, client, accepted)

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

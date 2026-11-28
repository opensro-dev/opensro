"""
===========================================================================
test_later_shard_bands.py - one importer owns one shard file.

Every optional layer writes its kinds into a shard of its own and, on
its off path, unlinks that shard whether or not it ever wrote it. Two
layers naming one shard file therefore delete each other's rows: a
build with the first on and the second off loses the first. These
tests pin the band rule the importers' headers state. Discovery reads
both declaration shapes: a module-level shard assignment, and a source
profile's shard entries (the pet import's per-source dicts), across the
character, skill and item tables.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import importlib
import os
import re
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import arabia_import as arabia  # noqa: E402
import cap110_monsters as cap110  # noqa: E402
import colosseum_import as colosseum  # noqa: E402
import constantinople_import as constantinople  # noqa: E402
import export_world_import as export_world  # noqa: E402
import gears_stone_import as gears  # noqa: E402
import god_import as god  # noqa: E402
import honor_items as honor  # noqa: E402
import hunting2_import as hunting2  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import magic_cube_import as magic_cubes  # noqa: E402
import muhan_import as muhan  # noqa: E402
import pet_import as pets  # noqa: E402
import pharaoh_temple_import as pharaoh_temple  # noqa: E402
import reset_scroll_import as reset_scrolls  # noqa: E402
import roc_import as roc  # noqa: E402
import skytemple_import as skytemple  # noqa: E402
import styria_import as styria  # noqa: E402
import styria_items_import as styria_items  # noqa: E402
import switch_scroll_import as switch_scrolls  # noqa: E402
import winter_event_import as winter  # noqa: E402

SCRIPTS = Path(__file__).resolve().parents[2]
# A module-level shard assignment, the shape most importers' headers use.
SHARD_ASSIGNMENT = re.compile(r"^(CHARACTER|SKILL|ITEM)_SHARD = ", re.MULTILINE)
# A source profile's shard entries (pet_import's per-source dicts): the
# other shape a staged shard is declared in.
PROFILE_SHARD_KEYS = ("char_shard", "skill_shard", "item_shard")
PROFILE_ASSIGNMENT = re.compile(r'"(?:char_shard|skill_shard|item_shard)"\s*:')


# ================
# shard_owners
#
# Every scripts/ module that stages its own character, skill or item shard,
# found by its module-level assignment and confirmed by importing it, so
# a new importer joins the band rule without anyone listing it (a hand
# list let a missing entry pass, opensro-web's #3762 reading).
# ================
def shard_owners():
	owners = []
	for path in sorted(SCRIPTS.glob("*.py")):
		source = path.read_text(encoding="utf-8")
		if not SHARD_ASSIGNMENT.search(source) and not PROFILE_ASSIGNMENT.search(source):
			continue
		module = importlib.import_module(path.stem)
		if shard_names(module):
			owners.append(module)
	return tuple(owners)


LAYER_FLAGS = (arabia.FLAG, muhan.FLAG, winter.FLAG)


# ================
# shard_names
# ================
def shard_names(module):
	# One module names a validation pattern ITEM_SHARD (cap110_item_import):
	# only a string constant declares a staged file.
	names = [getattr(module, name) for name in ("CHARACTER_SHARD", "SKILL_SHARD", "ITEM_SHARD")
		if isinstance(getattr(module, name, None), str)]
	for value in vars(module).values():
		if isinstance(value, dict):
			names += [value[key] for key in PROFILE_SHARD_KEYS if isinstance(value.get(key), str)]
	return names


SHARD_OWNERS = shard_owners()


# ================
# TestShardBands
# ================
class TestShardBands(unittest.TestCase):
	def test_the_owners_include_every_known_importer(self):
		# The discovery itself: the importers the band rule was written for.
		for module in (arabia, cap110, colosseum, constantinople, export_world, gears, god, honor, hunting2,
			jupiter, magic_cubes, muhan, pets, pharaoh_temple, reset_scrolls, roc, skytemple, styria,
			styria_items, switch_scrolls, winter):
			self.assertIn(module, SHARD_OWNERS)

	def test_every_shard_has_one_owner(self):
		owners = {}
		for module in SHARD_OWNERS:
			for name in shard_names(module):
				self.assertNotIn(name, owners, f"{name} is owned by both {owners.get(name)} and {module.__name__}")
				owners[name] = module.__name__

	def test_muhan_off_leaves_the_arabia_shards(self):
		# The next release's posture: Arabia on, Muhan off. Muhan runs
		# after Arabia in variant_later_layers, so its off reap must not
		# touch what Arabia just staged.
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			native.mkdir()
			text_root = out / muhan.TEXT_ROOT
			text_root.mkdir(parents=True)
			(text_root / arabia.CHARACTER_SHARD).write_bytes(b"arabia kinds")
			(text_root / arabia.SKILL_SHARD).write_bytes(b"arabia skills")
			(text_root / "skilldata.txt").write_bytes(cap110.encode_rows([["skilldata_5000.txt"], [arabia.SKILL_SHARD]]))
			env = {key: value for key, value in os.environ.items() if key not in LAYER_FLAGS}
			with mock.patch.dict(os.environ, env, clear=True):
				self.assertEqual(muhan.add_muhan(native, out), {"enabled": False})
			self.assertEqual((text_root / arabia.CHARACTER_SHARD).read_bytes(), b"arabia kinds")
			self.assertEqual((text_root / arabia.SKILL_SHARD).read_bytes(), b"arabia skills")
			manifest = {row[0] for row in arabia.tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row}
			self.assertIn(arabia.SKILL_SHARD, manifest)

	def test_winter_off_leaves_the_muhan_shards(self):
		# The winter layer runs after Muhan in variant_later_layers, so
		# with the Christmas event off its reap must not touch what Muhan
		# just staged.
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			native.mkdir()
			text_root = out / winter.later_world_import.TEXT_ROOT
			text_root.mkdir(parents=True)
			(text_root / muhan.CHARACTER_SHARD).write_bytes(b"muhan kinds")
			(text_root / muhan.SKILL_SHARD).write_bytes(b"muhan skills")
			(text_root / "skilldata.txt").write_bytes(cap110.encode_rows([["skilldata_5000.txt"], [muhan.SKILL_SHARD]]))
			env = {key: value for key, value in os.environ.items() if key not in LAYER_FLAGS}
			with mock.patch.dict(os.environ, env, clear=True):
				self.assertFalse(winter.add_winter_monsters(native, out)["enabled"])
			self.assertEqual((text_root / muhan.CHARACTER_SHARD).read_bytes(), b"muhan kinds")
			self.assertEqual((text_root / muhan.SKILL_SHARD).read_bytes(), b"muhan skills")
			manifest = {row[0] for row in cap110.tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row}
			self.assertIn(muhan.SKILL_SHARD, manifest)


if __name__ == "__main__":
	unittest.main()

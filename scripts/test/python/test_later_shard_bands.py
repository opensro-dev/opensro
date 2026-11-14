"""
===========================================================================
test_later_shard_bands.py - one importer owns one shard file.

Every optional layer writes its kinds into a shard of its own and, on
its off path, unlinks that shard whether or not it ever wrote it. Two
layers naming one shard file therefore delete each other's rows: a
build with the first on and the second off loses the first. These
tests pin the band rule the importers' headers state.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import arabia_import as arabia  # noqa: E402
import cap110_monsters as cap110  # noqa: E402
import constantinople_import as constantinople  # noqa: E402
import god_import as god  # noqa: E402
import hunting2_import as hunting2  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import muhan_import as muhan  # noqa: E402
import skytemple_import as skytemple  # noqa: E402

# Every module that stages its own character (and skill) shard.
SHARD_OWNERS = (arabia, cap110, constantinople, god, hunting2, jupiter, muhan, skytemple)
LAYER_FLAGS = (arabia.FLAG, muhan.FLAG)


# ================
# shard_names
# ================
def shard_names(module):
	return [getattr(module, name) for name in ("CHARACTER_SHARD", "SKILL_SHARD") if hasattr(module, name)]


# ================
# TestShardBands
# ================
class TestShardBands(unittest.TestCase):
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


if __name__ == "__main__":
	unittest.main()

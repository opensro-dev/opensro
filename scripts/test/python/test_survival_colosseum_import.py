"""
===========================================================================
test_survival_colosseum_import.py - the survival arena's Colosseum layer.

Port-only, not native. The layer is the later-world engine's region merge
over the one region the Colosseum's safezone rows name: these tests pin
what is its own - the region it admits, that it runs as a later layer,
and the off path that reaps what an on build enrolled and reads no archive.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import cap110_monsters as cap110  # noqa: E402
import later_world_import  # noqa: E402
import survival_colosseum_import as arena  # noqa: E402
import variant_later_layers  # noqa: E402


# ================
# safezone_table
#
# teleportdata bytes with eight Colosseum safezone starts in one region.
# ================
def safezone_table(region):
	rows = [["1", str(100 + i), f"{arena.colosseum.SAFEZONE_PREFIX}{i}", "x", "x", str(region)] + ["0"] * 7
		for i in range(8)]
	return cap110.encode_rows(rows)


# ================
# SurvivalColosseumImportTest
# ================
class SurvivalColosseumImportTest(unittest.TestCase):
	def test_admits_the_region_the_server_resolves(self):
		self.assertEqual(arena.arena_region(safezone_table(25580)), 25580)

	def test_refuses_a_client_whose_colosseum_is_elsewhere(self):
		with self.assertRaisesRegex(ValueError, "not 25580"):
			arena.arena_region(safezone_table(25581))

	def test_is_a_later_layer(self):
		self.assertIn(("survivalColosseum", arena.add_survival_colosseum), variant_later_layers.LATER_LAYERS)

	def test_off_reaps_what_the_marker_enrolled_and_reads_no_archive(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			native.mkdir()
			out.mkdir()
			record = {"files": [], "sectors": [], "regions": [arena.ARENA_REGION], "prefixes": []}
			(out / arena.MARKER).write_text(json.dumps({"enabled": True, "regionMerge": record}))
			with mock.patch.dict(os.environ, {arena.FLAG: ""}), \
				mock.patch.object(later_world_import, "reap_regions") as reap, \
				mock.patch.object(later_world_import, "reap_monsters"), \
				mock.patch.object(later_world_import, "merge_world") as merge:
				self.assertEqual(arena.add_survival_colosseum(native, out), {"enabled": False})
			reap.assert_called_once()
			self.assertIs(reap.call_args.args[0], arena.SPEC)
			self.assertEqual(reap.call_args.args[3], record)
			merge.assert_not_called()

	def test_on_merges_the_colosseum_through_the_engine(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			with mock.patch.dict(os.environ, {arena.FLAG: "on"}), \
				mock.patch.object(later_world_import, "require_pin"), \
				mock.patch.object(arena, "open", mock.mock_open(), create=True), \
				mock.patch.object(arena.mmap, "mmap") as mapped, \
				mock.patch.object(arena.tables, "archive_index", return_value={}), \
				mock.patch.object(arena.tables, "table_text", return_value=safezone_table(25580)), \
				mock.patch.object(later_world_import, "merge_world", return_value={"enabled": True}) as merge:
				mapped.return_value.__enter__.return_value = b""
				self.assertEqual(arena.add_survival_colosseum(native, out, v1696_root=temp), {"enabled": True})
			spec, _native, _out, _root, regions, _monsters = merge.call_args.args
			self.assertIs(spec, arena.SPEC)
			self.assertEqual(regions, [25580])


if __name__ == "__main__":
	unittest.main()

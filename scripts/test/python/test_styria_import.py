"""
===========================================================================
test_styria_import.py - the Styria Clash arena map layer (#3272).

Port-only, not native. The layer is the later-world engine's region
merge over the four RN_ARENA_JOB_WAR regions, with no population: these
tests pin what is its own - which regions it admits, the marker its
empty monster pass writes, and the off path that reaps what an on build
enrolled and reads no archive.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import later_world_import  # noqa: E402
import styria_import as styria  # noqa: E402


# ================
# region_rows
#
# regioncode rows (service, id, code) for the given ids under one code.
# ================
def region_rows(ids, code=styria.REGION_CODE):
	return [["1", str(region_id), code] for region_id in ids]


# ================
# StyriaImportTest
# ================
class StyriaImportTest(unittest.TestCase):
	def test_admits_exactly_the_four_arena_regions(self):
		rows = region_rows(styria.ARENA_REGIONS) + region_rows([25000], "RN_OTHER")
		self.assertEqual(styria.arena_regions(rows), list(styria.ARENA_REGIONS))

	def test_refuses_a_client_with_another_region_set(self):
		with self.assertRaisesRegex(ValueError, "not"):
			styria.arena_regions(region_rows(styria.ARENA_REGIONS[:3]))

	def test_refuses_a_dungeon_region(self):
		with self.assertRaisesRegex(ValueError, "dungeon"):
			styria.arena_regions(region_rows(list(styria.ARENA_REGIONS) + [0x8000 | 5]))

	def test_the_empty_monster_pass_keeps_the_region_record(self):
		with tempfile.TemporaryDirectory() as temp:
			marker = Path(temp) / styria.MARKER
			marker.write_text(json.dumps({"regionMerge": {"regions": [31985]}, "stale": 1}))
			summary = styria.write_marker(temp)
			self.assertEqual(summary, {"enabled": True, "characters": 0})
			self.assertEqual(json.loads(marker.read_text()), {"regionMerge": {"regions": [31985]}, "enabled": True})

	def test_off_reaps_what_the_marker_enrolled_and_reads_no_archive(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			native.mkdir()
			out.mkdir()
			record = {"files": [], "sectors": [], "regions": list(styria.ARENA_REGIONS), "prefixes": []}
			(out / styria.MARKER).write_text(json.dumps({"enabled": True, "regionMerge": record}))
			# The engine's own reaps are later_world_import's tests; this pins
			# that the layer hands them its spec and the enrolled record.
			with mock.patch.dict(os.environ, {styria.FLAG: ""}), \
				mock.patch.object(later_world_import, "reap_regions") as reap, \
				mock.patch.object(later_world_import, "reap_monsters") as reap_kinds, \
				mock.patch.object(later_world_import, "merge_world") as merge:
				self.assertEqual(styria.add_styria(native, out, v1696_root=None), {"enabled": False})
			reap.assert_called_once()
			self.assertIs(reap.call_args.args[0], styria.SPEC)
			self.assertEqual(reap.call_args.args[3], record)
			reap_kinds.assert_called_once()
			self.assertIs(reap_kinds.call_args.args[0], styria.SPEC)
			merge.assert_not_called()

	def test_on_merges_the_regions_through_the_engine(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			with mock.patch.dict(os.environ, {styria.FLAG: "on"}), \
				mock.patch.object(styria, "require_v1696_pin"), \
				mock.patch.object(styria, "open", mock.mock_open(), create=True), \
				mock.patch.object(styria.mmap, "mmap") as mapped, \
				mock.patch.object(styria.tables, "archive_index", return_value={}), \
				mock.patch.object(styria.tables, "table_text", return_value=""), \
				mock.patch.object(styria.tables, "table_rows", return_value=region_rows(styria.ARENA_REGIONS)), \
				mock.patch.object(later_world_import, "merge_world", return_value={"enabled": True}) as merge:
				mapped.return_value.__enter__.return_value = b""
				self.assertEqual(styria.add_styria(native, out, v1696_root=temp), {"enabled": True})
			spec, _native, _out, _root, regions, _monsters = merge.call_args.args
			self.assertIs(spec, styria.SPEC)
			self.assertEqual(regions, list(styria.ARENA_REGIONS))


if __name__ == "__main__":
	unittest.main()

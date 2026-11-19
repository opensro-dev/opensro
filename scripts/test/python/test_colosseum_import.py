"""
===========================================================================
test_colosseum_import.py - the Survival Arena's Colosseum map layer (#3931).

Port-only, not native. The layer is the later-world engine's map-only
path over the one region the v1.696 safezone teleport rows name: these
tests pin what is its own - the region rule and the table it reads. The
switch, the pin and the merge are the engine's (test_later_world_import).
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest
from unittest import mock

import colosseum_import as colosseum  # noqa: E402
import later_world_import  # noqa: E402


# ================
# safezone_rows
#
# teleportdata rows (service, id, codename, ..., region in column 5).
# ================
def safezone_rows(region, count=colosseum.MIN_SAFEZONES, prefix=colosseum.SAFEZONE_PREFIX):
	return [["1", str(9000 + i), f"{prefix}{i:02}", "x", "x", str(region)] for i in range(count)]


# ================
# ColosseumImportTest
# ================
class ColosseumImportTest(unittest.TestCase):
	def test_the_safezones_name_the_server_s_arena_region(self):
		rows = safezone_rows(colosseum.COLOSSEUM_REGION) + [["1", "1", "GATE_OTHER", "x", "x", "25000"]]
		self.assertEqual(colosseum.colosseum_regions(rows), [colosseum.COLOSSEUM_REGION])

	def test_another_region_refuses(self):
		with self.assertRaisesRegex(ValueError, r"not \[25580\]"):
			colosseum.colosseum_regions(safezone_rows(25581))

	def test_two_regions_refuse(self):
		rows = safezone_rows(colosseum.COLOSSEUM_REGION) + safezone_rows(25581, 1)
		with self.assertRaisesRegex(ValueError, "regions"):
			colosseum.colosseum_regions(rows)

	def test_a_cut_client_with_too_few_starts_refuses(self):
		with self.assertRaisesRegex(ValueError, "fewer than"):
			colosseum.colosseum_regions(safezone_rows(colosseum.COLOSSEUM_REGION, colosseum.MIN_SAFEZONES - 1))

	def test_a_disabled_row_is_not_a_start(self):
		rows = safezone_rows(colosseum.COLOSSEUM_REGION)
		rows[0][0] = "0"
		with self.assertRaisesRegex(ValueError, "fewer than"):
			colosseum.colosseum_regions(rows)

	def test_the_layer_rides_the_arena_switch_on_the_map_path(self):
		self.assertEqual(colosseum.SPEC.flag, "SRO_SOOK_SURVIVAL_ARENA")
		with mock.patch.object(later_world_import, "add_map_world", return_value={"enabled": True}) as add:
			self.assertEqual(colosseum.add_colosseum("n", "o", v1696_root="r"), {"enabled": True})
		spec, native, out, root, rule = add.call_args.args
		self.assertIs(spec, colosseum.SPEC)
		self.assertEqual((native, out, root), ("n", "o", "r"))
		tables = {"teleportdata.txt": safezone_rows(colosseum.COLOSSEUM_REGION)}
		self.assertEqual(rule(tables.__getitem__), [colosseum.COLOSSEUM_REGION])


if __name__ == "__main__":
	unittest.main()

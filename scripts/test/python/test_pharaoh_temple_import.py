"""
===========================================================================
test_pharaoh_temple_import.py - the Pharaoh Temple's monster import.

The temple admits its 26 named kinds alone (the cap-110 desert field
shares their MOB_SD_ prefix), projects its nests as authored plus the
one v1.188 Osiris exception, and keeps a capped hive whole: the four
priest nests of the Sanctum of Punishment share one cap of three,
never three each. Off reads no archive. The shard band and the
engine's monster pass are pinned in test_later_shard_bands and
test_later_world_import.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import pharaoh_temple_import as temple  # noqa: E402

CONTROLS = {"SightRange": 100, "ChangeTarget": 2, "AggressType": 0}

# The Sanctum of Punishment: region 0x800C, source world 53.
PUNISHMENT_REGION = 32780
PUNISHMENT_WORLD = 53


# ================
# later_row
# ================
def later_row(code, level, grade=0):
	row = ["0"] * 104
	row[1], row[2] = "9000", code
	row[15], row[57] = str(grade), str(level)
	return row


# ================
# nest
# ================
def nest(nest_id, code, world=PUNISHMENT_WORLD, region=PUNISHMENT_REGION, cap=0):
	return {
		"Codename": code, "NestID": nest_id, "WorldID": world, "Continent": "SD_TEMPLE",
		"RegionID": region, "X": 100.0, "Y": 50.0, "Z": 200.0, "Radius": 30, "GenerateRadius": 20,
		"MaxCount": 1, "RespawnDelayMinSec": 10, "RespawnDelayMaxSec": 20, "Respawn": True,
		"InitialDir": 0, "SpawnType": 0, "NestFlag": 0, "ChampionGenPercentage": 0,
		"Controls": dict(CONTROLS), "Champion": None,
		"Hive": {"OverwriteMaxTotalCount": cap, "KeepMonsterCountType": 0},
	}


# ================
# PharaohTempleAdmissionTest
# ================
class PharaohTempleAdmissionTest(unittest.TestCase):
	def test_a_named_kind_in_its_window_is_admitted(self):
		self.assertTrue(temple.family_admits("MOB_SD_ANUBIS", later_row("MOB_SD_ANUBIS", 107, 3)))
		self.assertTrue(temple.family_admits("MOB_SD_PRIEST_GUARD", later_row("MOB_SD_PRIEST_GUARD", 108)))

	def test_the_desert_field_shares_the_prefix_and_stays_out(self):
		# A cap-110 field kind at a temple level and grade: the name, not
		# the prefix, admits.
		self.assertFalse(temple.family_admits("MOB_SD_APIS", later_row("MOB_SD_APIS", 107)))

	def test_a_named_kind_outside_its_window_or_grades_is_refused(self):
		self.assertFalse(temple.family_admits("MOB_SD_ANUBIS", later_row("MOB_SD_ANUBIS", 104, 3)))
		self.assertFalse(temple.family_admits("MOB_SD_ANUBIS", later_row("MOB_SD_ANUBIS", 111, 3)))
		self.assertFalse(temple.family_admits("MOB_SD_ANUBIS", later_row("MOB_SD_ANUBIS", 107, 8)))

	def test_osiris_enters_by_name_at_its_source_grade_alone(self):
		# The v1.188 row (grade 8, level 110) enters; the keepers' grade
		# on the Osiris row does not, and the named exception opens no
		# grade for the family (Anubis at 8 stays refused, above).
		self.assertTrue(temple.family_admits(temple.OSIRIS_KIND, later_row(temple.OSIRIS_KIND, 110, 8)))
		self.assertFalse(temple.family_admits(temple.OSIRIS_KIND, later_row(temple.OSIRIS_KIND, 110, 6)))
		self.assertFalse(temple.family_admits(temple.OSIRIS_KIND, later_row(temple.OSIRIS_KIND, 104, 8)))

	def test_haroeris_enters_at_the_keeper_grade_alone(self):
		# The summoned unique is a keeper-grade kind (grade 3, level 109
		# in the pinned client): the keepers' rule admits him, and no
		# named grade opens for him (his grade-8 decoy stays out).
		self.assertTrue(temple.family_admits(temple.HAROERIS_KIND, later_row(temple.HAROERIS_KIND, 109, 3)))
		self.assertFalse(temple.family_admits(temple.HAROERIS_KIND, later_row(temple.HAROERIS_KIND, 109, 8)))
		self.assertFalse(temple.family_admits(temple.HAROERIS_KIND, later_row(temple.HAROERIS_KIND, 104, 3)))

	def test_only_the_temple_worlds_claim_and_the_exceptions_by_name(self):
		rows = [nest(1, "MOB_SD_ANUBIS"), nest(2, "MOB_SD_APIS", world=1, region=24929)]
		self.assertEqual(temple._claim_groups(rows, False),
			{"temple": {"MOB_SD_ANUBIS", "MOB_SD_OSIRIS_3", "MOB_SD_HAROERIS"}})

	def test_the_spec_places_on_a_standing_tomb_row(self):
		self.assertTrue(temple.SPEC.place_standing)


# ================
# PharaohTemplePopulationTest
# ================
class PharaohTemplePopulationTest(unittest.TestCase):
	def test_a_capped_hive_projects_whole_under_one_shared_cap(self):
		rows = [nest(29551, "MOB_SD_PRIEST_HEALING", cap=3), nest(29552, "MOB_SD_PRIEST_POWER", cap=3),
			nest(29553, "MOB_SD_PRIEST_GUARD", cap=3), nest(29554, "MOB_SD_PRIEST_HEALING", cap=3),
			nest(29591, "MOB_SD_ANUBIS")]
		population = temple._build_populations(rows, False)["temple"]
		by_nest = {row["NestID"]: row for row in population["Rows"]}
		self.assertEqual(sorted(by_nest), [29551, 29552, 29553, 29554, 29591, 40998, 40999])
		keys = {(by_nest[nest_id].get("HiveKey"), by_nest[nest_id].get("HiveMaxCount"))
			for nest_id in (29551, 29552, 29553, 29554)}
		self.assertEqual(keys, {(f"pharaoh-temple:{PUNISHMENT_WORLD}:{PUNISHMENT_REGION}:3", 3)})
		# Each nest keeps its own authored count; the hive caps the room.
		self.assertEqual({by_nest[nest_id]["MaxCount"] for nest_id in (29551, 29552, 29553, 29554)}, {1})
		self.assertNotIn("HiveKey", by_nest[29591])

	def test_the_projection_leaves_the_export_rows_untouched(self):
		rows = [nest(29551, "MOB_SD_PRIEST_HEALING", cap=3)]
		temple._build_populations(rows, False)
		self.assertEqual(rows[0]["Hive"]["OverwriteMaxTotalCount"], 3)

	def test_another_world_stays_out_of_the_population(self):
		rows = [nest(1, "MOB_SD_ANUBIS"), nest(2, "MOB_SD_APIS", world=1, region=24929)]
		population = temple._build_populations(rows, False)["temple"]
		self.assertEqual([row["NestID"] for row in population["Rows"]], [1, 40998, 40999])

	def test_haroeris_rides_as_the_authored_row(self):
		# No source places him: he stands authored in his sanctum on
		# the keepers' pattern (one alive, their 10,800 s respawn),
		# with controls re-keyed to his own client object.
		row = temple.haroeris_nest()
		self.assertEqual((row["Codename"], row["NestID"]), ("MOB_SD_HAROERIS", 40998))
		self.assertEqual((row["WorldID"], row["RegionID"]), (55, 32782))
		self.assertEqual((row["Radius"], row["GenerateRadius"], row["MaxCount"]), (50, 10, 1))
		self.assertEqual((row["RespawnDelayMinSec"], row["RespawnDelayMaxSec"]), (10800, 10800))
		self.assertTrue(row["Respawn"])
		self.assertNotIn("HiveKey", row)
		controls = row["Controls"]
		self.assertEqual((controls["ID"], controls["ObjectID"]), (38691, 26681))
		self.assertEqual((controls["SightRange"], controls["Flags"], controls["AggressOnHoming"]), (600, 0, 1))
		self.assertEqual(controls["DiversionBasisData"], [0, 0, 0, 0, 0, 30, 0, 0])

	def test_osiris_rides_as_the_v1188_row(self):
		# Nest 40999 as the cap-110 reader sees it: the field world, the
		# Temple region, an 8-hour respawn, and the whole tactics row.
		row = temple.osiris_nest()
		self.assertEqual((row["Codename"], row["NestID"]), ("MOB_SD_OSIRIS_3", 40999))
		self.assertEqual((row["WorldID"], row["RegionID"]), (1, 32784))
		self.assertEqual((row["Radius"], row["GenerateRadius"], row["MaxCount"]), (50, 10, 1))
		self.assertEqual((row["RespawnDelayMinSec"], row["RespawnDelayMaxSec"]), (28800, 28900))
		self.assertTrue(row["Respawn"])
		self.assertNotIn("HiveKey", row)
		controls = row["Controls"]
		self.assertEqual((controls["ID"], controls["ObjectID"]), (57573, 38210))
		self.assertEqual((controls["SightRange"], controls["Flags"], controls["AggressOnHoming"]), (200, 112, 2))
		self.assertEqual(controls["DiversionBasisData"], [0, 0, 0, 0, 0, 30, 0, 0])


# ================
# PharaohTempleSwitchTest
# ================
class PharaohTempleSwitchTest(unittest.TestCase):
	def test_off_reads_no_archive_and_reports_off(self):
		with tempfile.TemporaryDirectory() as temp:
			base = Path(temp)
			(base / "native").mkdir()
			(base / "out").mkdir()
			with mock.patch.dict(os.environ, {temple.FLAG: "off"}), \
					mock.patch.object(temple.later_world_import, "client_root",
						side_effect=AssertionError("off read the client")):
				self.assertEqual(temple.add_pharaoh_temple_monsters(base / "native", base / "out"),
					{"enabled": False})

	def test_on_without_a_client_refuses_by_its_variable(self):
		with tempfile.TemporaryDirectory() as temp, \
				mock.patch.dict(os.environ, {temple.FLAG: "on", temple.ROOT_ENV: ""}):
			with self.assertRaisesRegex(SystemExit, temple.ROOT_ENV):
				temple.add_pharaoh_temple_monsters(Path(temp), Path(temp) / "out")


if __name__ == "__main__":
	unittest.main()

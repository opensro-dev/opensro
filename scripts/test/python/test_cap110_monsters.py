"""
===========================================================================
test_cap110_monsters.py - reference identity and optional equipment boundaries.
===========================================================================
"""
import copy
import json
import tempfile
import unittest
from pathlib import Path

import cap110_monsters as monsters
import cap110_monster_loot as loot


# ================
# skill
# ================
def skill(identity, code, original=0):
	row = ["0"] * 118
	row[0], row[1], row[3], row[6] = "1", str(identity), code, str(original)
	return row


# ================
# Cap110MonstersTest
# ================
class Cap110MonstersTest(unittest.TestCase):
	# ================
	# test_only_the_pinned_backup_export_is_admitted
	# ================
	def test_only_the_pinned_backup_export_is_admitted(self):
		with tempfile.TemporaryDirectory() as root:
			summary = Path(root) / "summary.json"
			summary.write_text(json.dumps({"backup_sha256": monsters.SOURCE_BACKUP_SHA256.upper()}), encoding="utf-8")
			monsters.require_pinned_export(root)
			summary.write_text(json.dumps({"backup_sha256": "0" * 64}), encoding="utf-8")
			with self.assertRaisesRegex(ValueError, monsters.SOURCE_BACKUP_NAME):
				monsters.require_pinned_export(root)
			summary.write_text("{}", encoding="utf-8")
			with self.assertRaisesRegex(ValueError, "records backup none"):
				monsters.require_pinned_export(root)

	# ================
	# test_apis_admission_preserves_grade_and_excludes_other_uniques
	# ================
	def test_apis_admission_preserves_grade_and_excludes_other_uniques(self):
		later = {}
		for code, grade, level in [("MOB_SD_APIS", 8, 103), ("MOB_SD_FIELD", 0, 110),
			("MOB_SD_OTHER_UNIQUE", 8, 103), ("MOB_SD_TOO_HIGH", 0, 111)]:
			row = ["0"] * 104
			row[2], row[15], row[57] = code, str(grade), str(level)
			later[code] = row
		refs = monsters.select_field_references(later, set(later))
		self.assertEqual(set(refs), {"MOB_SD_APIS", "MOB_SD_FIELD"})
		self.assertEqual(refs["MOB_SD_APIS"][15], "8")

	# ================
	# test_earlier_skill_addition_does_not_take_a_published_identity
	# ================
	def test_earlier_skill_addition_does_not_take_a_published_identity(self):
		native = [skill(100, "SKILL_CH_EXISTING")]
		source = [skill(2, "MSKILL_SD_NEW"), skill(12, "MSKILL_SD_OLD")]
		rows, remap = monsters.merge_skills(native, source, [2, 12], {"MSKILL_SD_OLD": 101})
		self.assertEqual(remap, {2: 102, 12: 101})
		self.assertEqual({r[3]: int(r[1]) for r in rows}, {"MSKILL_SD_NEW": 102, "MSKILL_SD_OLD": 101})

	# ================
	# test_character_additions_reserve_all_published_identities
	# ================
	def test_character_additions_reserve_all_published_identities(self):
		previous = {"MOB_SD_OLD": 101, "MOB_SD_RESERVED": 104}
		got = monsters.allocate_identities({100}, ["MOB_SD_NEW", "MOB_SD_OLD"], previous)
		self.assertEqual(got, {"MOB_SD_NEW": 105, "MOB_SD_OLD": 101})
		for invalid in ({"a": 101, "b": 101}, {"a": 100}, {"a": 0}, {"a": True}, {"a": monsters.MAX_ID + 1}):
			with self.subTest(previous=invalid), self.assertRaises(ValueError):
				monsters.allocate_identities({100}, ["MOB_SD_NEW"], invalid)
		with self.assertRaises(ValueError):
			monsters.allocate_identities({100}, ["MOB_SD_NEW"], {"old": monsters.MAX_ID})

	# ================
	# test_originals_join_by_codename_and_published_ids_survive
	# ================
	def test_originals_join_by_codename_and_published_ids_survive(self):
		native = [skill(3, "MSKILL_BASE"), skill(100, "SKILL_EU_EXISTING")]
		source = [skill(9, "MSKILL_BASE"), skill(12, "MSKILL_SD_ATTACK", 9)]
		rows, identities = monsters.merge_skills(native, source, [12], {})
		self.assertEqual(identities, {9: 3, 12: 101})
		self.assertEqual(rows[0][6], "3")
		rebuilt, remap = monsters.merge_skills(native + [skill(150, "SKILL_CH_OTHER")], source, [12], {"MSKILL_SD_ATTACK": 101})
		self.assertEqual(rebuilt, rows)
		self.assertEqual(remap, identities)

	# ================
	# test_missing_dependency_and_identity_collision_refuse
	# ================
	def test_missing_dependency_and_identity_collision_refuse(self):
		with self.assertRaises(ValueError):
			monsters.merge_skills([], [skill(12, "MSKILL_SD_ATTACK", 9)], [12], {})
		with self.assertRaises(ValueError):
			monsters.merge_skills([skill(101, "SKILL_CH_OTHER")], [skill(12, "MSKILL_SD_ATTACK")], [12], {"MSKILL_SD_ATTACK": 101})

	# ================
	# test_tomb_codes_admit_elite_and_unique_while_the_field_does_not
	# ================
	def test_tomb_codes_admit_elite_and_unique_while_the_field_does_not(self):
		later = {}
		for code, grade, level in [("MOB_SD_ORDINARY", 0, 103), ("MOB_SD_ELITE", 6, 104), ("MOB_SD_BOSS", 8, 110),
			("MOB_SD_CHAMPION", 1, 104), ("MOB_SD_LOW_ELITE", 6, 99)]:
			row = ["0"] * 104
			row[2], row[15], row[57] = code, str(grade), str(level)
			later[code] = row
		field = monsters.select_field_references(later, set(later))
		self.assertEqual(set(field), {"MOB_SD_ORDINARY"})
		tomb = monsters.select_field_references(later, set(later), {"MOB_SD_ELITE", "MOB_SD_BOSS", "MOB_SD_CHAMPION", "MOB_SD_LOW_ELITE"})
		self.assertEqual(set(tomb), {"MOB_SD_ORDINARY", "MOB_SD_ELITE", "MOB_SD_BOSS"})

	# ================
	# test_tomb_population_is_its_own_world_set_and_must_be_complete
	# ================
	def test_tomb_population_is_its_own_world_set_and_must_be_complete(self):
		def spawn(nest, code, world, region):
			return {"nest_id": nest, "codename": code, "game_world_id": world, "service": 1, "region_id_unsigned": region,
				"tactics_id": 1, "hive_id": 1, "x": 1.0, "y": 0.0, "z": 2.0, "radius": 400, "generate_radius": 300,
				"max_total_count": 4, "delay_min": 6, "delay_max": 8, "respawn": 1, "direction_unsigned": 0,
				"champion_percent": 0, "nest_flag": 0, "hive_flag": 0, "spawn_type": 0}
		tactics = {1: {"dwTacticsID": 1, "dwObjID": 7, "btAIQoS": 0, "nMaxStamina": 0, "btMaxStaminaVariance": 0,
			"nSightRange": 130, "btAggressType": 0, "AggressData": 0, "btChangeTarget": 2, "btHelpRequestTo": 0,
			"btHelpResponseTo": 0, "btBattleStyle": 0, "BattleStyleData": 0, "btDiversionBasis": 0, "btDiversionKeepBasis": 0,
			"btKeepDistance": 0, "KeepDistanceData": 0, "btTraceType": 0, "btTraceBoundary": 0, "TraceData": 0,
			"btHomingType": 0, "HomingData": 0, "btAggressTypeOnHoming": 0, "btFleeType": 0, "dwChampionTacticsID": 0,
			"AdditionOptionFlag": 0, **{f"DiversionBasisData{i}": 0 for i in range(1, 9)},
			**{f"DiversionKeepBasisData{i}": 0 for i in range(1, 9)}}}
		hives = {1: {"dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}}
		spawns = [spawn(1, "MOB_SD_FIELD", 1, 100), spawn(2, "MOB_SD_TOMB", 57, 29631), spawn(3, "MOB_SD_TOMB", 58, 31423)]
		refs = {"MOB_SD_FIELD": 1, "MOB_SD_TOMB": 2}
		field = monsters.project_population(spawns, refs, tactics, hives)
		self.assertEqual([(r["NestID"], r["WorldID"]) for r in field["Rows"]], [(1, 1)])
		self.assertEqual(field["Version"], 2)
		tomb = monsters.project_population(spawns, refs, tactics, hives, monsters.TOMB_WORLDS, strict=True)
		self.assertEqual([(r["NestID"], r["RegionID"], r["WorldID"]) for r in tomb["Rows"]], [(2, 29631, 57), (3, 31423, 58)])
		with self.assertRaises(ValueError):
			monsters.project_population(spawns, {"MOB_SD_FIELD": 1}, tactics, hives, monsters.TOMB_WORLDS, strict=True)
		self.assertEqual(len(monsters.project_population(spawns, {"MOB_SD_FIELD": 1}, tactics, hives, monsters.TOMB_WORLDS)["Rows"]), 0)

	# ================
	# test_basic_skills_the_projection_omits_join_native_by_codename
	# ================
	def test_basic_skills_the_projection_omits_join_native_by_codename(self):
		native = [skill(1, "SKILL_PUNCH_01"), skill(100, "SKILL_CH_EXISTING")]
		source = [skill(12, "MSKILL_SD_ATTACK")]
		client = {1: skill(1, "SKILL_PUNCH_01"), 7: skill(7, "SKILL_NOT_NATIVE")}
		calls = []
		def load():
			calls.append(1)
			return client
		self.assertIs(monsters.add_native_client_skills(source, [12], native, load), source)
		self.assertEqual(calls, [], "nothing missing, so the client is not read")
		joined = monsters.add_native_client_skills(source, [12, 1], native, load)
		self.assertEqual([int(r[1]) for r in joined], [12, 1])
		rows, remap = monsters.merge_skills(native, joined, [12, 1], {})
		self.assertEqual(remap[1], 1)
		self.assertEqual([r[3] for r in rows], ["MSKILL_SD_ATTACK"])
		still_missing = monsters.add_native_client_skills(source, [12, 7], native, load)
		with self.assertRaises(ValueError):
			monsters.merge_skills(native, still_missing, [12, 7], {})

	# ================
	# test_tomb_regions_seed_terrain_only_when_asked
	# ================
	def test_tomb_regions_seed_terrain_only_when_asked(self):
		import json
		import tempfile
		from pathlib import Path
		rows = [{"game_world_id": 1, "service": 1, "codename": "MOB_SD_FIELD", "region_id_unsigned": (74 << 8) | 57},
			{"game_world_id": 57, "service": 1, "codename": "MOB_SD_TOMB", "region_id_unsigned": 29631},
			{"game_world_id": 5, "service": 1, "codename": "MOB_SD_OTHER", "region_id_unsigned": 1}]
		with tempfile.TemporaryDirectory() as root:
			Path(root, "all-spawns.json").write_text(json.dumps(rows), encoding="utf-8")
			self.assertEqual(monsters.outdoor_regions(root), {(57, 74)})
			self.assertEqual(monsters.outdoor_regions(root, True), {(57, 74), (29631 & 255, 29631 >> 8)})

	# ================
	# test_equipment_classes_keep_native_lower_levels
	# ================
	def test_equipment_classes_keep_native_lower_levels(self):
		code = "ITEM_CH_SWORD_11_A"
		item = ["0"] * 160
		item[0], item[2], item[14], item[15], item[33], item[61], item[158] = "1", code, "0", "0", "101", "31", "9"
		item[9:13] = ["3", "1", "6", "2"]
		base = {"equipment.json": {"version": 2, "widths": {"normal": 36, "rare": 60},
			"normal": [[0.0] * 36 for _ in range(180)], "rare": [[0.0] * 60 for _ in range(180)], "items": []},
			"properties.json": {"items": {}, "magic": [], "assignments": []}}
		before = copy.deepcopy(base)
		assignment = {"CodeName128": code, "Service": 1, "Prob_Relative": 100, "Prob_Absolute": 100}
		classes = [{"MonLevel": level, "ProbGroup31": 0.01} for level in range(101, 111)]
		got = loot.compile_field_equipment(base, [item], [assignment], classes, classes)
		self.assertEqual(base, before)
		self.assertEqual(got["Equipment"]["normal"][:100], base["equipment.json"]["normal"][:100])
		self.assertEqual(got["Equipment"]["normal"][109][30], 0.01)
		self.assertEqual(got["Equipment"]["items"][0]["group"], 30)
		with self.assertRaises(ValueError):
			loot.compile_field_equipment(base, [], [assignment], classes, classes)
		with self.assertRaises(ValueError):
			loot.compile_field_equipment(base, [item], [assignment], classes[:-1], classes)


if __name__ == "__main__":
	unittest.main()

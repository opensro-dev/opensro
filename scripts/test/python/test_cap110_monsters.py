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
	# test_family_claims_from_spawns
	# ================
	def family_claims(self, spawns, families):
		claims = {}
		for family in families:
			for code in monsters.family_codes(spawns, family):
				claims.setdefault(code, []).append(family)
		return claims

	# ================
	# later_row
	# ================
	def later_row(self, code, grade, level):
		row = ["0"] * 104
		row[2], row[15], row[57] = code, str(grade), str(level)
		return row

	# ================
	# test_qinshi_family_admits_its_window_and_prefixes
	# ================
	def test_qinshi_family_admits_its_window_and_prefixes(self):
		later = {}
		for code, grade, level in [("MOB_TQ_SOLDIER", 0, 81), ("MOB_TQ_LOW", 0, 80), ("MOB_TQ_TOP", 3, 110),
			("MOB_TQ_HIGH", 0, 111), ("MOB_TQ_CHAMPION", 1, 95), ("MOB_QT_TQ_MIRROR", 6, 100), ("MOB_QT_GHOST", 8, 91)]:
			later[code] = self.later_row(code, grade, level)
		spawns = [{"codename": code, "game_world_id": 1, "service": 1} for code in later]
		spawns.append({"codename": "MOB_TQ_INACTIVE", "game_world_id": 1, "service": 0})
		later["MOB_TQ_INACTIVE"] = self.later_row("MOB_TQ_INACTIVE", 0, 90)
		claims = self.family_claims(spawns, (monsters.QINSHI_FAMILY,))
		refs = monsters.select_references(later, claims)
		self.assertEqual(set(refs), {"MOB_TQ_SOLDIER", "MOB_TQ_TOP", "MOB_QT_TQ_MIRROR", "MOB_QT_GHOST"})

	# ================
	# test_temple_family_claims_only_its_worlds_and_window
	# ================
	def test_temple_family_claims_only_its_worlds_and_window(self):
		later = {}
		for code, grade, level in [("MOB_GOD_LOW", 0, 34), ("MOB_GOD_BAND", 1, 35), ("MOB_GOD_TOP", 8, 109),
			("MOB_GOD_OVER", 0, 110), ("MOB_GOD_OUTWORLD", 0, 60), ("MOB_GOD_RARE2", 2, 60), ("MOB_GOD_FIELD", 0, 60)]:
			later[code] = self.later_row(code, grade, level)
		spawns = [{"codename": "MOB_GOD_LOW", "game_world_id": 63, "service": 1},
			{"codename": "MOB_GOD_BAND", "game_world_id": 63, "service": 1},
			{"codename": "MOB_GOD_TOP", "game_world_id": 84, "service": 1},
			{"codename": "MOB_GOD_OVER", "game_world_id": 70, "service": 1},
			{"codename": "MOB_GOD_OUTWORLD", "game_world_id": 62, "service": 1},
			{"codename": "MOB_GOD_RARE2", "game_world_id": 70, "service": 1},
			{"codename": "MOB_GOD_FIELD", "game_world_id": 1, "service": 1},
			{"codename": "MOB_SD_FIELD", "game_world_id": 63, "service": 1}]
		later["MOB_SD_FIELD"] = self.later_row("MOB_SD_FIELD", 0, 60)
		claims = self.family_claims(spawns, (monsters.TEMPLE_FAMILY,))
		refs = monsters.select_references(later, claims)
		self.assertEqual(set(refs), {"MOB_GOD_BAND", "MOB_GOD_TOP"})

	# ================
	# test_family_tables_match_the_agreed_rows
	# ================
	def test_family_tables_match_the_agreed_rows(self):
		by_name = {family["Name"]: family for family in monsters.FAMILIES}
		self.assertEqual(monsters.FAMILIES[0]["Name"], "field")
		qinshi = by_name["qinshi"]
		self.assertEqual((qinshi["Flag"], qinshi["File"], qinshi["Worlds"], qinshi["MinLevel"], qinshi["MaxLevel"], qinshi["Grades"]),
			("SRO_CAP110_QINSHI", "cap110-qinshi.txt", (1,), 81, 110, (0, 3, 6, 8)))
		temple = by_name["temple"]
		self.assertEqual((temple["Flag"], temple["File"], temple["MinLevel"], temple["MaxLevel"], temple["Grades"]),
			("SRO_CAP110_TEMPLE", "cap110-temple.txt", 35, 109, (0, 1, 3, 6, 8)))
		self.assertEqual((temple["Worlds"][0], temple["Worlds"][-1], len(temple["Worlds"])), (63, 84, 22))
		flags = {family["Flag"] for family in monsters.FAMILIES}
		self.assertEqual(len(flags), len(monsters.FAMILIES))

	# ================
	# test_family_projection_matches_legacy_field_and_pharaoh_outputs
	# ================
	def test_family_projection_matches_legacy_field_and_pharaoh_outputs(self):
		later = {}
		for code, grade, level in [("MOB_SD_FIELD", 0, 105), ("MOB_SD_TOMB", 6, 104), ("MOB_SD_LOW", 0, 99)]:
			later[code] = self.later_row(code, grade, level)
		def spawn(nest, code, world):
			return {"nest_id": nest, "codename": code, "game_world_id": world, "service": 1, "region_id_unsigned": 100,
				"tactics_id": 1, "hive_id": 1, "x": 1.0, "y": 0.0, "z": 2.0, "radius": 400, "generate_radius": 300,
				"max_total_count": 4, "delay_min": 6, "delay_max": 8, "respawn": 1, "direction_unsigned": 0,
				"champion_percent": 0, "nest_flag": 0, "hive_flag": 0, "spawn_type": 0}
		spawns = [spawn(1, "MOB_SD_FIELD", 1), spawn(2, "MOB_SD_TOMB", 57), spawn(3, "MOB_SD_LOW", 1)]
		legacy = monsters.select_field_references(later, {"MOB_SD_FIELD", "MOB_SD_TOMB", "MOB_SD_LOW"}, {"MOB_SD_TOMB"})
		claims = self.family_claims(spawns, (monsters.FIELD_FAMILY, monsters.TOMB_FAMILY))
		self.assertEqual(monsters.select_references(later, claims), legacy)
		tactics = {1: {"dwTacticsID": 1, "dwObjID": 7, "btAIQoS": 0, "nMaxStamina": 0, "btMaxStaminaVariance": 0,
			"nSightRange": 130, "btAggressType": 0, "AggressData": 0, "btChangeTarget": 2, "btHelpRequestTo": 0,
			"btHelpResponseTo": 0, "btBattleStyle": 0, "BattleStyleData": 0, "btDiversionBasis": 0, "btDiversionKeepBasis": 0,
			"btKeepDistance": 0, "KeepDistanceData": 0, "btTraceType": 0, "btTraceBoundary": 0, "TraceData": 0,
			"btHomingType": 0, "HomingData": 0, "btAggressTypeOnHoming": 0, "btFleeType": 0, "dwChampionTacticsID": 0,
			"AdditionOptionFlag": 0, **{f"DiversionBasisData{i}": 0 for i in range(1, 9)},
			**{f"DiversionKeepBasisData{i}": 0 for i in range(1, 9)}}}
		hives = {1: {"dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}}
		refs = {"MOB_SD_FIELD": 1, "MOB_SD_TOMB": 2, "MOB_SD_LOW": 3}
		general_field = monsters.project_population(spawns, refs, tactics, hives, monsters.FIELD_FAMILY["Worlds"],
			codes=monsters.family_codes(spawns, monsters.FIELD_FAMILY))
		self.assertEqual(general_field, monsters.project_population(spawns, refs, tactics, hives))
		general_tomb = monsters.project_population(spawns, refs, tactics, hives, monsters.TOMB_FAMILY["Worlds"],
			strict=True, codes=monsters.family_codes(spawns, monsters.TOMB_FAMILY))
		self.assertEqual(general_tomb, monsters.project_population(spawns, refs, tactics, hives, monsters.TOMB_WORLDS, strict=True))

	# ================
	# test_qinshi_strict_projection_skips_other_families_but_refuses_its_own
	# ================
	def test_qinshi_strict_projection_skips_other_families_but_refuses_its_own(self):
		def spawn(nest, code):
			return {"nest_id": nest, "codename": code, "game_world_id": 1, "service": 1, "region_id_unsigned": 32772,
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
		spawns = [spawn(1, "MOB_SD_FIELD"), spawn(2, "MOB_TQ_SOLDIER")]
		codes = monsters.family_codes(spawns, monsters.QINSHI_FAMILY)
		self.assertEqual(codes, {"MOB_TQ_SOLDIER"})
		rows = monsters.project_population(spawns, {"MOB_TQ_SOLDIER": 1}, tactics, hives,
			monsters.QINSHI_FAMILY["Worlds"], strict=True, codes=codes)
		self.assertEqual([(r["NestID"], r["WorldID"]) for r in rows["Rows"]], [(2, 1)])
		with self.assertRaisesRegex(ValueError, "MOB_TQ_SOLDIER"):
			monsters.project_population(spawns, {}, tactics, hives,
				monsters.QINSHI_FAMILY["Worlds"], strict=True, codes=codes)

	# ================
	# test_family_switches_require_the_field_flag
	# ================
	def test_family_switches_require_the_field_flag(self):
		import os
		from unittest import mock
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			for flag in ("SRO_CAP110_QINSHI", "SRO_CAP110_TEMPLE", "SRO_PHARAOH_TOMB"):
				with self.subTest(flag=flag), mock.patch.dict(os.environ, {flag: "on", "SRO_CAP110_MONSTERS": ""}):
					with self.assertRaisesRegex(ValueError, "requires SRO_CAP110_MONSTERS"):
						monsters.add_monsters(base / "vsro", base / "native", base / "output")

	# ================
	# test_shipped_flags_at_their_defaults_do_not_gate_the_families
	#
	# Today's default build: the shipped tomb switch is on (its registry
	# default) and every cap-110 switch is off. The families ride none of
	# the shipped switches, so the run is the plain off path.
	# ================
	def test_shipped_flags_at_their_defaults_do_not_gate_the_families(self):
		import os
		from unittest import mock
		env = {"SRO_QINSHI_TOMB": "on", "SRO_PHARAOH_TOMB": "",
			"SRO_CAP110_MONSTERS": "", "SRO_CAP110_QINSHI": "", "SRO_CAP110_TEMPLE": ""}
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			with mock.patch.dict(os.environ, env):
				self.assertEqual(monsters.add_monsters(base / "vsro", base / "native", base / "output"),
					{"enabled": False})

	# ================
	# test_disabled_family_rows_are_the_stale_ones
	# ================
	def test_disabled_family_rows_are_the_stale_ones(self):
		rows = [["1", "UIO_OBJ_MOB_SD_FIELD"], ["2", "UIO_OBJ_MOB_TQ_SOLDIER"], ["3"]]
		self.assertEqual(monsters.drop_symboled_rows(rows, {"UIO_OBJ_MOB_TQ_SOLDIER"}),
			[["1", "UIO_OBJ_MOB_SD_FIELD"], ["3"]])
		text = ("#section\tskill\r\nMOB_SD_FIELD\ta\r\n#section\tcharacterinfo\r\n"
			"MOB_SD_FIELD\tb\r\nMOB_TQ_SOLDIER\tc\r\n#section\tother\r\nMOB_TQ_SOLDIER\td\r\n")
		scrubbed = monsters.drop_characterinfo_rows(text, {"MOB_TQ_SOLDIER"})
		self.assertIn("MOB_SD_FIELD\tb", scrubbed)
		self.assertNotIn("MOB_TQ_SOLDIER\tc", scrubbed)
		self.assertIn("MOB_TQ_SOLDIER\td", scrubbed)
		self.assertEqual(monsters.characterinfo_codenames(scrubbed), {"MOB_SD_FIELD"})

	# ================
	# test_new_family_species_do_not_rekey_published_identities
	# ================
	def test_new_family_species_do_not_rekey_published_identities(self):
		previous = {"MOB_SD_FIELD": 120001, "MOB_TQ_SOLDIER": 120002}
		got = monsters.allocate_identities({100}, ["MOB_GOD_NEW", "MOB_SD_FIELD", "MOB_TQ_SOLDIER"], previous)
		self.assertEqual(got["MOB_SD_FIELD"], 120001)
		self.assertEqual(got["MOB_TQ_SOLDIER"], 120002)
		self.assertNotIn(got["MOB_GOD_NEW"], set(previous.values()))
		self.assertGreater(got["MOB_GOD_NEW"], 120002)

	# ================
	# nest_fixture
	# ================
	def nest_fixture(self, nest, code, world=1, spawn_type=0):
		return {"nest_id": nest, "codename": code, "game_world_id": world, "service": 1, "region_id_unsigned": 100,
			"tactics_id": 1, "hive_id": 1, "x": 1.0, "y": 0.0, "z": 2.0, "radius": 400, "generate_radius": 300,
			"max_total_count": 4, "delay_min": 6, "delay_max": 8, "respawn": 1, "direction_unsigned": 0,
			"champion_percent": 0, "nest_flag": 0, "hive_flag": 0, "spawn_type": spawn_type}

	# ================
	# tactics_fixture
	# ================
	def tactics_fixture(self):
		return {1: {"dwTacticsID": 1, "dwObjID": 7, "btAIQoS": 0, "nMaxStamina": 0, "btMaxStaminaVariance": 0,
			"nSightRange": 130, "btAggressType": 0, "AggressData": 0, "btChangeTarget": 2, "btHelpRequestTo": 0,
			"btHelpResponseTo": 0, "btBattleStyle": 0, "BattleStyleData": 0, "btDiversionBasis": 0, "btDiversionKeepBasis": 0,
			"btKeepDistance": 0, "KeepDistanceData": 0, "btTraceType": 0, "btTraceBoundary": 0, "TraceData": 0,
			"btHomingType": 0, "HomingData": 0, "btAggressTypeOnHoming": 0, "btFleeType": 0, "dwChampionTacticsID": 0,
			"AdditionOptionFlag": 0, **{f"DiversionBasisData{i}": 0 for i in range(1, 9)},
			**{f"DiversionKeepBasisData{i}": 0 for i in range(1, 9)}}}

	# ================
	# test_temple_projection_carries_spawn_type
	# ================
	def test_temple_projection_carries_spawn_type(self):
		spawns = [self.nest_fixture(1, "MOB_GOD_SOLDIER", 70), self.nest_fixture(2, "MOB_GOD_CAPTAIN", 70, spawn_type=1)]
		hives = {1: {"dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}}
		refs = {"MOB_GOD_SOLDIER": 1, "MOB_GOD_CAPTAIN": 2}
		rows = monsters.project_population(spawns, refs, self.tactics_fixture(), hives,
			monsters.TEMPLE_FAMILY["Worlds"], carry_spawn_type=True)["Rows"]
		self.assertNotIn("SpawnType", rows[0])
		self.assertEqual(rows[1]["SpawnType"], 1)
		with self.assertRaisesRegex(ValueError, "unsupported field spawn flags"):
			monsters.project_population(spawns, refs, self.tactics_fixture(), hives, monsters.TEMPLE_FAMILY["Worlds"])

	# ================
	# test_strict_codes_answer_for_the_window_only
	# ================
	def test_strict_codes_answer_for_the_window_only(self):
		later = {"MOB_TQ_TRAP": self.later_row("MOB_TQ_TRAP", 0, 30),
			"MOB_TQ_SOLDIER": self.later_row("MOB_TQ_SOLDIER", 0, 95)}
		codes = {"MOB_TQ_TRAP", "MOB_TQ_SOLDIER", "MOB_TQ_ABSENT"}
		strict = monsters.family_strict_codes(monsters.QINSHI_FAMILY, codes, later)
		self.assertEqual(strict, {"MOB_TQ_SOLDIER"})
		spawns = [self.nest_fixture(1, "MOB_TQ_TRAP"), self.nest_fixture(2, "MOB_TQ_SOLDIER")]
		hives = {1: {"dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}}
		rows = monsters.project_population(spawns, {"MOB_TQ_SOLDIER": 1}, self.tactics_fixture(), hives,
			(1,), strict=True, codes=strict)["Rows"]
		self.assertEqual([r["Codename"] for r in rows], ["MOB_TQ_SOLDIER"])
		with self.assertRaisesRegex(ValueError, "MOB_TQ_SOLDIER"):
			monsters.project_population(spawns, {}, self.tactics_fixture(), hives,
				(1,), strict=True, codes=strict)

	# ================
	# test_claimed_species_without_a_later_row_refuses_by_name
	# ================
	def test_claimed_species_without_a_later_row_refuses_by_name(self):
		with self.assertRaisesRegex(ValueError, "MOB_TQ_GHOST"):
			monsters.select_references({}, {"MOB_TQ_GHOST": [monsters.QINSHI_FAMILY]})

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

"""
===========================================================================
test_cap110_monsters.py - reference identity and optional equipment boundaries.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
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
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# archive_bytes
# ================
def archive_bytes(files):
	from test_client_preparation import ArchiveBuilder
	builder = ArchiveBuilder()
	root = builder.folder([])
	server = builder.folder([])
	silkroad = builder.folder([])
	textdata = builder.folder([])
	builder.blocks[root] = [(1, b"server_dep", server, 0)]
	builder.blocks[server] = [(1, b"silkroad", silkroad, 0)]
	builder.blocks[silkroad] = [(1, b"textdata", textdata, 0)]
	builder.blocks[textdata] = [(2, name.encode("ascii"), *builder.add_payload(raw)) for name, raw in files.items()]
	return builder.build()


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
	# test_family_tables_match_the_agreed_rows
	# ================
	def test_family_tables_match_the_agreed_rows(self):
		by_name = {family["Name"]: family for family in monsters.FAMILIES}
		self.assertEqual(monsters.FAMILIES[0]["Name"], "field")
		qinshi = by_name["qinshi"]
		self.assertEqual((qinshi["Flag"], qinshi["File"], qinshi["Worlds"], qinshi["MinLevel"], qinshi["MaxLevel"], qinshi["Grades"]),
			("SRO_CAP110_QINSHI", "cap110-qinshi.txt", (1,), 81, 110, (0, 3, 6, 8)))
		# The temple bands are god_import's, not a family here (its
		# kinds and nests ride the 190000 shard, not this allocator).
		self.assertNotIn("temple", by_name)
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
			# The temple switch's own pairing refusal is god_import's
			# (test_god_import); the families here refuse without the field.
			for flag in ("SRO_CAP110_QINSHI", "SRO_PHARAOH_TOMB"):
				with self.subTest(flag=flag), mock.patch.dict(os.environ, {flag: "on", "SRO_CAP110_MONSTERS": ""}):
					with self.assertRaisesRegex(ValueError, "requires SRO_CAP110_MONSTERS"):
						monsters.add_monsters(base / "vsro", base / "native", base / "output")

	# ================
	# test_shipped_flags_at_their_defaults_do_not_gate_the_families
	#
	# Today's default build: the shipped tomb switch is on (its registry
	# default) and every cap-110 switch is off. The families ride none of
	# the shipped switches - and the temple switch gates god_import's
	# build, not these families - so the run is the plain off path.
	# ================
	def test_shipped_flags_at_their_defaults_do_not_gate_the_families(self):
		import os
		from unittest import mock
		env = {"SRO_QINSHI_TOMB": "on", "SRO_PHARAOH_TOMB": "",
			"SRO_CAP110_MONSTERS": "", "SRO_CAP110_QINSHI": "", "SRO_CAP110_TEMPLE": "on"}
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
	# test_family_off_rebuild_reaps_what_the_first_build_enrolled
	#
	# Build level: the first enabled build writes the new family's
	# characterinfo and name rows and must enrol them in the marker the
	# same build (the pre-existing text is the native copy, so nothing
	# about them is discoverable later); a rebuild with the family off
	# then reaps exactly those rows and shrinks both marker lists.
	# ================
	def test_family_off_rebuild_reaps_what_the_first_build_enrolled(self):
		import os
		from unittest import mock

		def later_monster(code, symbol, level):
			row = self.later_row(code, 0, level)
			row[5] = symbol
			return row

		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			vsro, native, output, source = base / "vsro", base / "native", base / "output", base / "source"
			text_root = output / monsters.TEXT_ROOT
			text_root.mkdir(parents=True)
			vsro.mkdir()
			native.mkdir()
			source.mkdir()
			native_char = ["0"] * 104
			native_char[1], native_char[2] = "100", "MOB_NATIVE"
			(text_root / "characterdata_5000.txt").write_bytes(utf16([native_char]))
			(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
			(text_root / "skilldata_5000.txt").write_bytes(utf16([]))
			(text_root / "textdataname.txt").write_bytes(utf16([["1", "UIO_OBJ_MOB_NATIVE", "Native"]]))
			(text_root / "skilleffect.txt").write_bytes(utf16([
				["#section", "characterinfo"], ["MOB_NATIVE"] + ["0"] * 12, ["#section", "skillaniset"]]))
			media = {
				"characterdata.txt": utf16([["characterdata_6000.txt"]]),
				"characterdata_6000.txt": utf16([
					later_monster("MOB_SD_FIELD", "UIO_OBJ_MOB_SD_FIELD", 105),
					later_monster("MOB_TQ_SOLDIER", "UIO_OBJ_MOB_TQ_SOLDIER", 95)]),
				"skilldata.txt": utf16([["skilldata_6000.txt"]]),
				"skilldata_6000.txt": utf16([]),
				"textdata_object.txt": utf16([
					["1", "UIO_OBJ_MOB_SD_FIELD", "x", "x", "Field monster", "f", "x", "a", "b", "c"],
					["2", "UIO_OBJ_MOB_TQ_SOLDIER", "x", "x", "Tomb soldier", "t", "x", "a", "b", "c"]]),
				"skilleffect.txt": utf16([
					["#section", "characterinfo"],
					["MOB_SD_FIELD"] + ["0"] * 12,
					["MOB_TQ_SOLDIER"] + ["0"] * 12,
					["#section", "skillaniset"]]),
			}
			(vsro / "Media.pk2").write_bytes(archive_bytes(media))
			(source / "summary.json").write_text(
				json.dumps({"backup_sha256": monsters.SOURCE_BACKUP_SHA256}), encoding="utf-8")
			(source / "all-spawns.json").write_text(json.dumps(
				[self.nest_fixture(1, "MOB_SD_FIELD"), self.nest_fixture(2, "MOB_TQ_SOLDIER")]), encoding="utf-8")
			(source / "Tab_RefNest.json").write_text(json.dumps(
				[{"dwNestID": 1, "nChampionGenPercentage": 0}, {"dwNestID": 2, "nChampionGenPercentage": 0}]), encoding="utf-8")
			(source / "Tab_RefTactics.json").write_text(json.dumps([self.tactics_fixture()[1]]), encoding="utf-8")
			(source / "Tab_RefHive.json").write_text(json.dumps(
				[{"dwHiveID": 1, "dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}]), encoding="utf-8")
			for name in ("vsro-outdoor-monster-skills", "vsro-outdoor-drop-assignments",
				"vsro-outdoor-drop-classes", "vsro-outdoor-rare-classes"):
				(source / (name + ".json")).write_text("[]", encoding="utf-8")
			env = {"SRO_CAP110_MONSTERS": "on", "SRO_PHARAOH_TOMB": "", "SRO_CAP110_TEMPLE": ""}
			marker = output / monsters.MARKER
			with mock.patch.object(loot, "stage_field_equipment", return_value=0):
				with mock.patch.dict(os.environ, {**env, "SRO_CAP110_QINSHI": "on"}):
					first = monsters.add_monsters(vsro, native, output, source)
				self.assertEqual(first["characterInfo"], ["MOB_SD_FIELD", "MOB_TQ_SOLDIER"])
				enrolled = json.loads(marker.read_text(encoding="utf-8"))
				self.assertEqual(enrolled["characterInfo"], ["MOB_SD_FIELD", "MOB_TQ_SOLDIER"])
				self.assertIn("UIO_OBJ_MOB_TQ_SOLDIER", enrolled["nameSymbols"])
				self.assertTrue((text_root / monsters.QINSHI_FILE).exists())
				with mock.patch.dict(os.environ, {**env, "SRO_CAP110_QINSHI": ""}):
					second = monsters.add_monsters(vsro, native, output, source)
			self.assertEqual(second["characterInfo"], ["MOB_SD_FIELD"])
			self.assertNotIn("UIO_OBJ_MOB_TQ_SOLDIER", second["nameSymbols"])
			effect_text = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			self.assertEqual(monsters.characterinfo_codenames(effect_text), {"MOB_NATIVE", "MOB_SD_FIELD"})
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("UIO_OBJ_MOB_SD_FIELD", names)
			self.assertNotIn("UIO_OBJ_MOB_TQ_SOLDIER", names)
			self.assertFalse((text_root / monsters.QINSHI_FILE).exists())
			self.assertTrue((text_root / monsters.POPULATION_FILE).exists())

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
	# test_projection_carries_spawn_type_when_asked
	# ================
	def test_projection_carries_spawn_type_when_asked(self):
		spawns = [self.nest_fixture(1, "MOB_GOD_SOLDIER", 70), self.nest_fixture(2, "MOB_GOD_CAPTAIN", 70, spawn_type=1)]
		hives = {1: {"dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}}
		refs = {"MOB_GOD_SOLDIER": 1, "MOB_GOD_CAPTAIN": 2}
		rows = monsters.project_population(spawns, refs, self.tactics_fixture(), hives,
			(70,), carry_spawn_type=True)["Rows"]
		self.assertNotIn("SpawnType", rows[0])
		self.assertEqual(rows[1]["SpawnType"], 1)
		with self.assertRaisesRegex(ValueError, "unsupported field spawn flags"):
			monsters.project_population(spawns, refs, self.tactics_fixture(), hives, (70,))

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

	# ================
	# test_export_without_assignments_stages_the_native_catalog
	#
	# #1745 D3: the pinned export has no drop assignments; the staged
	# catalog is then the native compilation, classes and items unchanged.
	# ================
	def test_export_without_assignments_stages_the_native_catalog(self):
		from unittest import mock
		base = {"equipment.json": {"version": 2, "widths": {"normal": 2, "rare": 2},
			"normal": [[0.0, 0.5]] * 180, "rare": [[0.25, 0.0]] * 180,
			"items": [{"codename": "ITEM_NATIVE", "group": 1}]},
			"properties.json": {"items": {"ITEM_NATIVE": {"codename": "ITEM_NATIVE", "type": [3, 1, 6, 2],
				"country": 0, "rarity": 0, "class": 2, "maxMagic": 4, "extra": 1}}, "magic": [], "assignments": []}}
		with tempfile.TemporaryDirectory() as temporary:
			text_root, source = Path(temporary) / "text", Path(temporary) / "source"
			text_root.mkdir()
			source.mkdir()
			self.assertFalse(loot.has_assignments(source))
			with mock.patch.object(loot, "compile_catalogs", return_value=copy.deepcopy(base)):
				self.assertEqual(loot.stage_field_equipment(text_root, source), 1)
			staged = json.loads((text_root / loot.EQUIPMENT_FILE).read_text(encoding="utf-8"))
		self.assertEqual(staged["Equipment"], base["equipment.json"])
		self.assertNotIn("extra", staged["Properties"]["items"]["ITEM_NATIVE"])

	# ================
	# test_client_only_build_mirrors_d10_and_joins_backup_gear
	#
	# #3405: with no export, D11 takes D10's banding over 102-110, the
	# backup's live expansion rows join the installed items only, the
	# rare rows above 94 stay unrollable, and an empty row is refused.
	# ================
	def test_client_only_build_mirrors_d10_and_joins_backup_gear(self):
		normal = [[0.0] * 36 for _ in range(180)]
		for level, groups in ((91, (27,)), (94, (28,)), (95, (28, 29)), (98, (29,)), (101, (29,))):
			for group in groups:
				normal[level - 1][group] = 0.0035
		for level in (92, 93):
			normal[level - 1] = list(normal[90])
		for level in (96, 97):
			normal[level - 1] = list(normal[94])
		for level in (99, 100):
			normal[level - 1] = list(normal[97])
		rows = {r["MonLevel"]: r for r in loot.mirrored_normal_classes(normal)}
		self.assertEqual(rows[101]["ProbGroup30"], 0.0035)
		self.assertEqual(rows[102]["ProbGroup31"], 0.0035)
		self.assertEqual(rows[105]["ProbGroup32"], 0.0035)
		self.assertEqual((rows[106]["ProbGroup32"], rows[106]["ProbGroup33"]), (0.0035, 0.0035))
		self.assertEqual(rows[110]["ProbGroup33"], 0.0035)
		self.assertNotIn(0.0035, [rows[105]["ProbGroup31"], rows[110]["ProbGroup32"]])

		code, missing, older = "ITEM_CH_SWORD_11_A", "ITEM_EU_SWORD_11_A", "ITEM_CH_BLADE_10_A"
		drops = {"assignments": [
			{"service": 1, "codename": code, "item": 1, "weight": 140, "absolute": 100, "class": -1, "lsn": [1]},
			{"service": 1, "codename": missing, "item": 2, "weight": 100, "absolute": 100, "class": -1, "lsn": [1]},
			{"service": 1, "codename": older, "item": 3, "weight": 100, "absolute": 100, "class": -1, "lsn": [1]},
			{"service": 0, "codename": older, "item": 3, "weight": 100, "absolute": 100, "class": -1, "lsn": [2]},
			{"service": 1, "codename": "ITEM_CH_SWORD_09_A", "item": 4, "weight": 100, "absolute": 100,
				"class": -1, "lsn": [1]},
		]}
		got = loot.backup_assignments(drops, {code, older, "ITEM_CH_SWORD_09_A"})
		self.assertEqual(got, [{"CodeName128": code, "Service": 1, "Prob_Relative": 140, "Prob_Absolute": 100}])

		item = ["0"] * 160
		item[0], item[2], item[10], item[33], item[61], item[158] = "1", code, "1", "101", "31", "9"
		item[9:13] = ["3", "1", "6", "2"]
		rare = [[0.0] * 60 for _ in range(180)]
		rare[101][29] = 1e-9
		base = {"equipment.json": {"version": 2, "widths": {"normal": 36, "rare": 60},
			"normal": normal, "rare": rare, "items": []}, "properties.json": {"items": {}, "magic": [], "assignments": []}}
		compiled = loot.compile_field_equipment(base, [item], got, list(rows.values()), loot.authored_classes(rare))
		loot.require_expansion_rows(compiled)
		self.assertGreater(compiled["Equipment"]["normal"][104][31], 0)
		self.assertEqual([i["group"] for i in compiled["Equipment"]["items"]], [30])
		self.assertFalse(any(p > 1e-6 for p in compiled["Equipment"]["rare"][101]))
		compiled["Equipment"]["normal"][104] = [0.0] * 36
		with self.assertRaisesRegex(ValueError, "level 105"):
			loot.require_expansion_rows(compiled)

	# ================
	# test_no_export_names_no_assignments
	# ================
	def test_no_export_names_no_assignments(self):
		self.assertFalse(loot.has_assignments(None))

	# ================
	# test_client_placements_join_npcpos_by_later_identity
	#
	# The client-only claim source (#3246): npcpos names the later
	# character identity; the join lands on its codename, only MOB_SD_
	# rows place, a duplicate row stands once, and a dungeon region
	# arrives signed and masks back to its 0x8000 form.
	# ================
	def test_client_placements_join_npcpos_by_later_identity(self):
		later = {"MOB_SD_FIELD": self.later_row("MOB_SD_FIELD", 0, 105),
			"NPC_SD_MERCHANT": self.later_row("NPC_SD_MERCHANT", 0, 100)}
		later["MOB_SD_FIELD"][1], later["NPC_SD_MERCHANT"][1] = "7001", "7002"
		npcpos = utf16([
			["7001", "19001", "10.5", "0.0", "20.25"],
			["7001", "19001", "10.5", "0.0", "20.25"],
			["7002", "19001", "1.0", "0.0", "2.0"],
			["7001", "-32767", "3.0", "0.0", "4.0"],
			["9999", "19001", "5.0", "0.0", "6.0"],
		])
		self.assertEqual(monsters.npcpos_placements(npcpos, later), [
			("MOB_SD_FIELD", 19001, 10.5, 0.0, 20.25),
			("MOB_SD_FIELD", 0x8001, 3.0, 0.0, 4.0),
		])

	# ================
	# test_client_nest_radii_are_the_evidence_commonest
	# ================
	def test_client_nest_radii_are_the_evidence_commonest(self):
		# The synthesized radii are the commonest of the native population
		# evidence's respawning nests, as the respawn window and sight are.
		evidence = (Path(__file__).resolve().parents[3] / "apps" / "server" / "internal" / "game" /
			"world" / "monster" / "data" / "v1188_population_evidence.tsv")
		radii, generate = {}, {}
		for line in evidence.read_text(encoding="utf-8").splitlines():
			columns = line.split("\t")
			if line.startswith("#") or len(columns) != 22 or columns[11] != "1":
				continue
			radii[float(columns[5])] = radii.get(float(columns[5]), 0) + 1
			generate[float(columns[6])] = generate.get(float(columns[6]), 0) + 1
		self.assertEqual(max(radii, key=radii.get), monsters.CLIENT_NEST_RADIUS)
		self.assertEqual(max(generate, key=generate.get), monsters.CLIENT_NEST_GENERATE_RADIUS)

	# ================
	# test_client_population_stands_at_the_placements
	#
	# One outdoor placement of an admitted species is one nest on its
	# point with the synthesized ordinary posture; dungeon placements
	# and unadmitted species build none, and nest identities are
	# sequential over the sorted placements.
	# ================
	def test_client_population_stands_at_the_placements(self):
		placements = [
			("MOB_SD_FIELD", 19002, 30.0, 0.0, 40.0),
			("MOB_SD_FIELD", 19001, 10.5, 0.0, 20.25),
			("MOB_SD_FIELD", 0x8001, 3.0, 0.0, 4.0),
			("MOB_SD_LOW", 19001, 1.0, 0.0, 2.0),
		]
		population = monsters.client_field_population(placements, {"MOB_SD_FIELD": 1})
		self.assertEqual(population["Version"], 2)
		rows = population["Rows"]
		self.assertEqual([(r["NestID"], r["RegionID"], r["X"], r["Z"]) for r in rows],
			[(1, 19001, 10.5, 20.25), (2, 19002, 30.0, 40.0)])
		row = rows[0]
		# A nest with controls but radius 0 is always outside its home for
		# the server (InsideHome: distance < radius), so it never wanders
		# or pursues: the synthesized posture carries a real radius.
		self.assertEqual((row["WorldID"], row["MaxCount"], row["Radius"], row["GenerateRadius"]),
			(1, 1, monsters.CLIENT_NEST_RADIUS, monsters.CLIENT_NEST_GENERATE_RADIUS))
		self.assertGreater(row["Radius"], 0)
		self.assertEqual((row["Respawn"], row["RespawnDelayMinSec"], row["RespawnDelayMaxSec"]), (True, 1, 3))
		self.assertIsNone(row["Champion"])
		self.assertEqual(row["Controls"]["SightRange"], monsters.CLIENT_NEST_SIGHT_RANGE)
		self.assertEqual(row["Controls"]["AggressType"], 0)
		exported = monsters.tactics_controls(self.tactics_fixture()[1])
		self.assertEqual(set(row["Controls"]), set(exported))

	# ================
	# test_client_outdoor_regions_seed_the_placed_sectors
	#
	# Stage 1's client-only seeds: the sectors the admitted species'
	# outdoor placements stand in. The below-window species and the
	# dungeon placement seed nothing. The manifest names its shard in
	# the client's mixed case (CharacterData_5000.txt in the real one),
	# which must still resolve against the lower-case archive index.
	# ================
	def test_client_outdoor_regions_seed_the_placed_sectors(self):
		with tempfile.TemporaryDirectory() as root:
			vsro = Path(root)
			field = self.later_row("MOB_SD_FIELD", 0, 105)
			field[1], field[5] = "7001", "UIO_OBJ_MOB_SD_FIELD"
			low = self.later_row("MOB_SD_LOW", 0, 99)
			low[1], low[5] = "7003", "UIO_OBJ_MOB_SD_LOW"
			media = {
				"characterdata.txt": utf16([["CharacterData_6000.txt"]]),
				"characterdata_6000.txt": utf16([field, low]),
				"npcpos.txt": utf16([
					["7001", "19001", "1.0", "0.0", "2.0"],
					["7001", "-32767", "1.0", "0.0", "2.0"],
					["7003", "19002", "1.0", "0.0", "2.0"]]),
			}
			(vsro / "Media.pk2").write_bytes(archive_bytes(media))
			self.assertEqual(monsters.client_outdoor_regions(vsro), {(19001 & 255, 19001 >> 8)})

	# ================
	# build_fixture
	#
	# A native output tree and a VSRO Media carrying one admitted field
	# species (MOB_SD_FIELD, id 7001) and one below the window
	# (MOB_SD_LOW, id 7003), placed by npcpos outdoors and in a dungeon
	# region. Returns (vsro, native, output).
	# ================
	def build_fixture(self, base):
		vsro, native, output = base / "vsro", base / "native", base / "output"
		text_root = output / monsters.TEXT_ROOT
		text_root.mkdir(parents=True)
		vsro.mkdir()
		native.mkdir()

		def later_monster(code, identity, symbol, level):
			row = self.later_row(code, 0, level)
			row[1], row[5] = str(identity), symbol
			return row

		native_char = ["0"] * 104
		native_char[1], native_char[2] = "100", "MOB_NATIVE"
		(text_root / "characterdata_5000.txt").write_bytes(utf16([native_char]))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / "skilldata_5000.txt").write_bytes(utf16([]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "UIO_OBJ_MOB_NATIVE", "Native"]]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_NATIVE"] + ["0"] * 12, ["#section", "skillaniset"]]))
		media = {
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16([
				later_monster("MOB_SD_FIELD", 7001, "UIO_OBJ_MOB_SD_FIELD", 105),
				later_monster("MOB_SD_LOW", 7003, "UIO_OBJ_MOB_SD_LOW", 99)]),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([]),
			"npcpos.txt": utf16([
				["7001", "19001", "10.5", "0.0", "20.25"],
				["7001", "19001", "11.5", "0.0", "21.25"],
				["7001", "-32767", "3.0", "0.0", "4.0"],
				["7003", "19001", "1.0", "0.0", "2.0"]]),
			"textdata_object.txt": utf16([
				["1", "UIO_OBJ_MOB_SD_FIELD", "x", "x", "Field monster", "f", "x", "a", "b", "c"],
				["2", "UIO_OBJ_MOB_SD_LOW", "x", "x", "Low monster", "l", "x", "a", "b", "c"]]),
			# The v1.188 characterinfo rows are 16 wide, as the shipped
			# MOB_SD_MONT row (sound, height 3.5, ..., three trailing columns).
			"skilleffect.txt": utf16([
				["#section", "characterinfo"],
				["MOB_SD_FIELD", "MOB_TK_BONEGENERAL", "3.5"] + ["none"] * 5 +
					["0,14,-3.6", "hit_2_metal", "none", "0", "0", "note", "", ""],
				["MOB_SD_LOW"] + ["0"] * 15,
				["#section", "skillaniset"]]),
		}
		(vsro / "Media.pk2").write_bytes(archive_bytes(media))
		return vsro, native, output

	# ================
	# test_client_only_build_writes_the_field
	#
	# #3246: with no database export named, the field builds from the
	# client's characterdata and npcpos alone: the admitted species
	# converts and its outdoor placements become the nests.
	# ================
	def test_client_only_build_writes_the_field(self):
		import os
		from unittest import mock
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output = self.build_fixture(Path(root))
			env = {"SRO_CAP110_MONSTERS": "on", "SRO_PHARAOH_TOMB": "", "SRO_CAP110_QINSHI": "", "SRO_CAP110_TEMPLE": ""}
			with mock.patch.object(loot, "stage_field_equipment", return_value=0) as staged:
				with mock.patch.dict(os.environ, env):
					os.environ.pop(monsters.SOURCE_ENV, None)
					summary = monsters.add_monsters(vsro, native, output)
			self.assertIsNone(staged.call_args.args[1])
			self.assertEqual(summary["source"], "client")
			self.assertEqual(summary["equipmentLoot"], "native")
			self.assertNotIn("sourceBackupSha256", summary)
			text_root = output / monsters.TEXT_ROOT
			population = json.loads((text_root / monsters.POPULATION_FILE).read_text(encoding="utf-8"))
			self.assertEqual([(r["Codename"], r["RegionID"], r["X"], r["Z"]) for r in population["Rows"]],
				[("MOB_SD_FIELD", 19001, 10.5, 20.25), ("MOB_SD_FIELD", 19001, 11.5, 21.25)])
			shard = (text_root / monsters.CHARACTER_SHARD).read_bytes().decode("utf-16")
			self.assertIn("MOB_SD_FIELD", shard)
			self.assertNotIn("MOB_SD_LOW", shard)
			marker = json.loads((output / monsters.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["source"], "client")
			# The admitted species carries its own characterinfo height
			# (the name board's anchor), cut to the native width.
			effect = (text_root / "skilleffect.txt").read_bytes().decode("utf-16").splitlines()
			field = [line.split("	") for line in effect if line.startswith("MOB_SD_FIELD	")]
			self.assertEqual(len(field), 1)
			self.assertEqual(field[0][:3], ["MOB_SD_FIELD", "MOB_TK_BONEGENERAL", "3.5"])
			self.assertEqual(len(field[0]), monsters.characters.SKILL_EFFECT_COLUMNS)
			self.assertIn("MOB_SD_FIELD", marker["characterInfo"])

	# ================
	# test_export_still_wins_when_it_is_named
	#
	# The export remains the enrichment (#3246): named, its authored
	# nests are the population and the marker keeps its hash, even
	# though the client tables could build the field alone.
	# ================
	def test_export_still_wins_when_it_is_named(self):
		import os
		from unittest import mock
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			vsro, native, output = self.build_fixture(base)
			source = base / "source"
			source.mkdir()
			(source / "summary.json").write_text(
				json.dumps({"backup_sha256": monsters.SOURCE_BACKUP_SHA256}), encoding="utf-8")
			(source / "all-spawns.json").write_text(json.dumps([self.nest_fixture(9, "MOB_SD_FIELD")]), encoding="utf-8")
			(source / "Tab_RefNest.json").write_text(
				json.dumps([{"dwNestID": 9, "nChampionGenPercentage": 0}]), encoding="utf-8")
			(source / "Tab_RefTactics.json").write_text(json.dumps([self.tactics_fixture()[1]]), encoding="utf-8")
			(source / "Tab_RefHive.json").write_text(
				json.dumps([{"dwHiveID": 1, "dwOverwriteMaxTotalCount": 0, "btKeepMonsterCountType": 0}]), encoding="utf-8")
			for name in ("vsro-outdoor-monster-skills", "vsro-outdoor-drop-classes", "vsro-outdoor-rare-classes"):
				(source / (name + ".json")).write_text("[]", encoding="utf-8")
			env = {"SRO_CAP110_MONSTERS": "on", "SRO_PHARAOH_TOMB": "", "SRO_CAP110_QINSHI": "", "SRO_CAP110_TEMPLE": ""}
			with mock.patch.object(loot, "stage_field_equipment", return_value=0):
				with mock.patch.dict(os.environ, env):
					summary = monsters.add_monsters(vsro, native, output, source)
			self.assertEqual(summary["sourceBackupSha256"], monsters.SOURCE_BACKUP_SHA256)
			self.assertNotIn("source", summary)
			population = json.loads((output / monsters.TEXT_ROOT / monsters.POPULATION_FILE).read_text(encoding="utf-8"))
			self.assertEqual([(r["NestID"], r["RegionID"], r["Radius"]) for r in population["Rows"]], [(9, 100, 400)])

	# ================
	# test_client_mode_refuses_the_other_families
	#
	# The tomb and qinshi worlds are the export's alone: with no export
	# named they refuse by name before any archive is read.
	# ================
	def test_client_mode_refuses_the_other_families(self):
		import os
		from unittest import mock
		with tempfile.TemporaryDirectory() as root:
			base = Path(root)
			with mock.patch.dict(os.environ, {"SRO_CAP110_MONSTERS": "on", "SRO_PHARAOH_TOMB": "on"}):
				os.environ.pop(monsters.SOURCE_ENV, None)
				with self.assertRaisesRegex(ValueError, "SRO_CAP110_MONSTER_DATABASE"):
					monsters.add_monsters(base / "vsro", base / "native", base / "output")


if __name__ == "__main__":
	unittest.main()

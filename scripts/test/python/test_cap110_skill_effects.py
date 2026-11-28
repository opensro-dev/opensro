"""
===========================================================================

test_cap110_skill_effects.py - the animation-set merge without licensed data

Synthetic skilleffect sections exercise section-aware insertion, native
precedence, the service-column offset and the unknown-animation-state refusal.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import later_fixtures  # noqa: E402
import later_tables  # noqa: E402
import sys
import tempfile
import unittest
from pathlib import Path

import cap110_skill_effects as effects  # noqa: E402

NATIVE = (
	"#section\tskillaniset2\t\t\t\r\n"
	"1\tfoo\tSKILL_A\tANI_SKILL_1\t0\r\n"
	"1\tbar\tSKILL_B\tANI_SKILL_2\t0\r\n"
	"0\tz\tSKILL_DISABLED\tANI_SKILL_3\t0\r\n"
	"#section\tskilleffectset\t\t\r\n"
	"1\tEFFECT_X\t0\r\n"
)
LATER = (
	"#section\tskillaniset2\t\t\t\r\n"
	"1\tnew\tSKILL_C\tANI_SKILL_5\t0\r\n"
	"1\told\tSKILL_A\tANI_SKILL_1\t0\r\n"
	"#section\tskilleffectset\t\t\r\n"
	"1\tEFFECT_Y\t0\r\n"
)


# ================
# MergeTests
# ================
class MergeTests(unittest.TestCase):
	# ================
	# test_removal_preserves_npc_and_effect_sections
	# ================
	def test_removal_preserves_npc_and_effect_sections(self):
		base = NATIVE + "#section\tcharacterInfo\r\nNPC_SD_M_AREA_ARMOR\tPLAYER\t1.6\r\n"
		merged, _ = effects.merge_skill_effect_text(base, effects.select_later_rows(LATER, {"SKILL_C"}))
		self.assertEqual(effects.remove_presentation(merged, {"SKILL_C"}), base)

	# ================
	# test_only_absent_bases_are_inserted
	# ================
	def test_only_absent_bases_are_inserted(self):
		self.assertEqual(effects.native_bases(NATIVE), {"SKILL_A", "SKILL_B"})
		selected = effects.select_later_rows(LATER, {"SKILL_A", "SKILL_C"})
		merged, inserted = effects.merge_skill_effect_text(NATIVE, selected)
		self.assertEqual(inserted, 1)
		self.assertIn("SKILL_C", merged)
		self.assertEqual(merged.count("SKILL_A"), 1)

	# ================
	# test_rows_land_inside_their_section
	# ================
	def test_rows_land_inside_their_section(self):
		selected = effects.select_later_rows(LATER, {"SKILL_C"})
		merged, _inserted = effects.merge_skill_effect_text(NATIVE, selected)
		self.assertLess(merged.index("SKILL_C"), merged.index("skilleffectset"))
		self.assertIn("EFFECT_X", merged)

	# ================
	# test_disabled_and_unknown_service_rows_are_skipped
	# ================
	def test_disabled_and_unknown_service_rows_are_skipped(self):
		bases = {base for _section, base, _line in effects.animset_rows(LATER)}
		self.assertEqual(bases, {"SKILL_C", "SKILL_A"})
		self.assertNotIn("SKILL_DISABLED", effects.native_bases(NATIVE))

	# ================
	# test_motions_the_client_lacks_are_named
	#
	# The client's table holds ANI_SKILL_1..100 and ANI_ATTACK1..9; a
	# motion past them is named, and it is never a stage fault.
	# ================
	def test_motions_the_client_lacks_are_named(self):
		later = {"skillaniset2": [("SKILL_C", "1\tnew\tSKILL_C\tANI_SKILL_99,ANI_ATTACK12\t0"),
			("SKILL_D", "1\tnew\tSKILL_D\tANI_SKILL_100,ANI_READY04,ANI_SKILL_101\t0")]}
		self.assertEqual(effects.unknown_skill_states(later),
			{"SKILL_C": ["ANI_ATTACK12"], "SKILL_D": ["ANI_SKILL_101"]})
		self.assertEqual(effects.unplayable_bases(later, NATIVE), {})
		# A player skill refuses on either: a lost motion or a stage fault.
		cells = stage_row("SKILL_C", "SHOT", "x.efp").split("\t")
		cells[14] = "MOV_PIERCE,0,0,0"
		later["skilleffectset"] = [("SKILL_C", "\t".join(cells))]
		self.assertEqual(effects.player_skill_faults(later, NATIVE),
			{"SKILL_C": ["ANI_ATTACK12", "MOV_PIERCE"], "SKILL_D": ["ANI_SKILL_101"]})


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in rows).encode("utf-16-le")


# ================
# stage_row
#
# A skilleffectset row in the native layout, key and step first, its stage
# object in the directory (17) and name (18) cells.
# ================
def stage_row(key, step, name, boolean="FALSE"):
	cells = ["s", key, step, "q", boolean] + ["xxx"] * 23
	cells[13], cells[14] = "AT_STOP", "MOV_NONE,0,0,0"
	cells[17], cells[18] = "monster\\arabia\\", name
	return "\t".join(cells)


# A VSRO characterinfo row: 16 cells where the native record keeps 12.
VSRO_INFO = "\t".join(["MOB_NEW", "MOB_NEW", "3.5"] + ["none"] * 9 + ["0", "note", "", ""])
TREE = (
	"#section\tcharacterInfo\r\n"
	"MOB_OLD\tMOB_OLD\t2\tnone\tnone\tnone\tnone\tnone\tnone\tnone\tnone\tnone\t\r\n"
	"#section\tskillaniset2\r\n"
	"1\tfoo\tSKILL_A\tANI_SKILL_1\tFALSE\r\n"
	"0\tplaceholder\tSKILL_C\tANI_SKILL_1\tFALSE\r\n"
	"#section\tskilleffectset\r\n"
	+ stage_row("SKILL_A", "SHOT", "native.efp") + "\r\n"
)
LATER_TEXT = (
	"#section\tcharacterInfo\r\n" + VSRO_INFO + "\r\n"
	"#section\tskillaniset2\r\n"
	"1\tnew\tSKILL_C\tANI_SKILL_1\tFALSE\r\n"
	"1\tlater\tSKILL_A\tANI_SKILL_1\tFALSE\r\n"
	"#section\tskilleffectset\r\n"
	+ stage_row("SKILL_C", "READY", "skill_c_ready.efp") + "\r\n"
	+ stage_row("SKILL_A", "READY", "later_a.efp") + "\r\n"
)


# ================
# LaterTextTests
#
# v1.696 ships skilleffect.txt as a stub and keeps the rows in shard
# indexes with a service and an id cell first (#4730).
# ================
class LaterTextTests(unittest.TestCase):
	# ================
	# test_the_stub_is_rebuilt_from_the_shards_in_the_native_layout
	# ================
	def test_the_stub_is_rebuilt_from_the_shards_in_the_native_layout(self):
		files = {
			"skilleffect.txt": utf16([["0"]]),
			**later_fixtures.sharded("characterinfo.txt", {"characterinfo_001.txt": [["1", "5", "MOB_NEW", "MOB_NEW", "3.5"] + ["none"] * 10]}),
			"skillaniset.txt": utf16([["1", "9001", "new", "SKILL_C", "ANI_SKILL_5", "0"],
				["0", "9002", "off", "SKILL_D", "ANI_SKILL_5", "0"]]),
			"skilleffectset.txt": utf16([["1", "77", "x", "STAGE_A", "READY", "q", "FLASE"],
				["0", "78", "x", "STAGE_OFF", "READY", "q", "0"]]),
		}
		text = effects.later_skill_effect_text(later_tables.Archive.of(files))
		rows = {section: [line.split("\t") for s, line in effects.data_lines(text) if s == section]
			for section in ("characterinfo", "skillaniset2", "skilleffectset")}
		self.assertEqual(rows["characterinfo"][0][:3], ["MOB_NEW", "MOB_NEW", "3.5"])
		# The animation sets keep their own service cell, switched-off rows too.
		self.assertEqual(rows["skillaniset2"][0], ["1", "new", "SKILL_C", "ANI_SKILL_5", "FALSE"])
		self.assertEqual(rows["skillaniset2"][1][0], "0")
		# The stages have no service cell: a switched-off one leaves, and
		# the misspelt boolean reads as the native FALSE.
		self.assertEqual(rows["skilleffectset"], [["x", "STAGE_A", "READY", "q", "FALSE"]])

	# ================
	# test_a_table_with_its_sections_is_read_as_it_is
	# ================
	def test_a_table_with_its_sections_is_read_as_it_is(self):
		raw = b"\xff\xfe" + LATER_TEXT.encode("utf-16-le")
		self.assertEqual(effects.later_skill_effect_text(later_tables.Archive.of({"skilleffect.txt": raw})), LATER_TEXT)


# ================
# StageTests
#
# One import's skill presentation: animation sets, their effect stages
# and the kinds' characterinfo rows join; off returns the tree as it was.
# ================
class StageTests(unittest.TestCase):
	# ================
	# test_stages_join_with_their_skill_and_leave_byte_for_byte
	# ================
	def test_stages_join_with_their_skill_and_leave_byte_for_byte(self):
		with tempfile.TemporaryDirectory() as temp:
			root = Path(temp)
			path = root / effects.SKILL_EFFECT_TABLE
			path.write_bytes(b"\xff\xfe" + TREE.encode("utf-16-le"))
			stage = effects.LaterEffects(root, LATER_TEXT, {"SKILL_A", "SKILL_C"}, lambda code: code == "MOB_NEW",
				{"MOB_NEW"}, {})
			marker = effects.stage_later_effects(stage)
			text = path.read_bytes().decode("utf-16")

			self.assertEqual(marker["effectBases"], ["SKILL_C"])
			self.assertEqual(marker["characterInfo"], ["MOB_NEW"])
			self.assertIn("skill_c_ready.efp", text)
			# Native wins: a native skill keeps its own set and gains no stages.
			self.assertNotIn("later_a.efp", text)
			self.assertNotIn("\tlater\t", text)
			# The 16-wide VSRO row joins, cut to the native record.
			info = [line for line in text.split("\r\n") if line.startswith("MOB_NEW")]
			self.assertEqual(len(info), 1)
			self.assertEqual(len(info[0].split("\t")), 13)

			effects.reap_later_effects(root, marker)
			self.assertEqual(path.read_bytes().decode("utf-16"), TREE)

	# ================
	# test_the_native_placeholder_of_a_staged_base_stays
	# ================
	def test_the_native_placeholder_of_a_staged_base_stays(self):
		staged = TREE.replace("#section\tskilleffectset", "1\tnew\tSKILL_C\tANI_SKILL_1\tFALSE\r\n#section\tskilleffectset")
		got = effects.remove_presentation(staged, {"SKILL_C"})
		self.assertEqual(got, TREE)

	# ================
	# test_what_the_client_cannot_play_is_left_out
	#
	# A stage moving by a kind the client lacks with no client homolog,
	# and two damage events in one phase, are rules the build enforces
	# for the client; the import leaves such a skill out and records it.
	# ================
	def test_what_the_client_cannot_play_is_left_out(self):
		def staged(key, kind, start="0", damage="FALSE"):
			cells = stage_row(key, "SHOT", "x.efp", damage).split("\t")
			cells[3], cells[14] = start, kind + ",0,0,0"
			return (key, "\t".join(cells))
		selected = {"skilleffectset": [staged("SKILL_PX", "MOV_PIERCE"), staged("SKILL_MX", "MOV_UPR"),
			staged("SKILL_DX", "MOV_NONE", "2", "TRUE"), staged("SKILL_DX", "MOV_NONE", "2", "TRUE"),
			staged("SKILL_EX", "MOV_NONE", "12", "TRUE")]}
		self.assertEqual(effects.unplayable_bases(selected, TREE), {"SKILL_PX": ["MOV_PIERCE"],
			"SKILL_DX": ["two damage events in phase 2"], "SKILL_EX": ["damage event phase 12"]})
		# A client homolog of the same family, phase and action translates.
		homolog = TREE + "\t".join(staged("SKILL_PX_A", "MOV_UP")[1].split("\t")) + "\r\n"
		self.assertEqual(effects.unplayable_bases({"skilleffectset": [staged("SKILL_PX_B", "MOV_PIERCE")]}, homolog), {})

		with tempfile.TemporaryDirectory() as temp:
			root = Path(temp)
			(root / effects.SKILL_EFFECT_TABLE).write_bytes(b"\xff\xfe" + TREE.encode("utf-16-le"))
			# Two damage stages in one phase: the skill keeps nothing.
			cells = stage_row("SKILL_C", "HIT", "x.efp", "TRUE").split("\t")
			cells[3] = "1"
			twin = "\t".join(cells)
			later = LATER_TEXT + twin + "\r\n" + twin + "\r\n"
			marker = effects.stage_later_effects(effects.LaterEffects(root, later, {"SKILL_C"}, lambda code: False, set(), {}))
			self.assertEqual(marker["unplayableSkills"], {"SKILL_C": ["two damage events in phase 1"]})
			self.assertEqual(marker["effectBases"], [])
			self.assertEqual((root / effects.SKILL_EFFECT_TABLE).read_bytes().decode("utf-16"), TREE)

		with tempfile.TemporaryDirectory() as temp:
			root = Path(temp)
			(root / effects.SKILL_EFFECT_TABLE).write_bytes(b"\xff\xfe" + TREE.encode("utf-16-le"))
			# A motion the client lacks joins: the client drops that token.
			later = LATER_TEXT.replace("1\tnew\tSKILL_C\tANI_SKILL_1", "1\tnew\tSKILL_C\tANI_ATTACK12")
			marker = effects.stage_later_effects(effects.LaterEffects(root, later, {"SKILL_C"}, lambda code: False, set(), {}))
			self.assertNotIn("unplayableSkills", marker)
			self.assertIn("ANI_ATTACK12", (root / effects.SKILL_EFFECT_TABLE).read_bytes().decode("utf-16"))

	# ================
	# test_the_rows_name_their_stage_files
	# ================
	def test_the_rows_name_their_stage_files(self):
		cells = stage_row("SKILL_C", "READY", "Skill_C_Ready.efp").split("\t")
		cells[26] = "monster\\hit.wav"
		self.assertEqual(effects.row_resources([("skilleffectset", cells)]),
			{("Particles", "monster/arabia/skill_c_ready.efp"), ("Data", "prim/snd/monster/hit.wav")})


if __name__ == "__main__":
	unittest.main()

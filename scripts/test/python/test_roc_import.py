"""
===========================================================================
test_roc_import.py - the Roc range import's contract (#3399 steps 1 and 3c).

On, the eleven admitted kinds stage in the import's own shard with their
skills and names; a kind the pinned client authors differently, or one
the native tree already carries, refuses by name. Off, exactly what the
marker enrolled is reaped - and a native name a later kind reuses
(MOB_RM_TAHOMET_ROC names itself SN_MOB_RM_TAHOMET) stays.

Step 1b: the four gates and their buildings merge, the summit link
carries its key item under the variant's identity - or stays out while
the variant lacks the item - and the wait room is the one region handed
to the region merge. merge_world itself has its own tests over real
archives (test_later_world_import); here it is stubbed to run the
staging step it is given.

Step 3c: the six MEETROC questdata rows join under their source ids and
leave by codename when off, restoring the native table byte for byte;
the quest kinds' committed nests (scripts/data/roc/quest-nests.json)
become roc-monsters.txt, and a nest naming an unstaged kind refuses.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import import_vsro_alexandria as tables  # noqa: E402
import roc_import as roc  # noqa: E402
from test_hunting2_import import archive_bytes, later_row, skill_row, utf16  # noqa: E402

NATIVE_TAHOMET_SYMBOL = "SN_MOB_RM_TAHOMET"
ROC_SKILL = 7101
# The key item: its id in the source client, and the one a variant
# that carries it gives it.
KEY_ITEM = "ITEM_QNO_RM_MEETROC_6_01"
SOURCE_KEY_ID = "23973"
VARIANT_KEY_ID = "50100"
MOUNTAIN_BUILDING = "24296"
NATIVE_QUEST = ["1", "1", "QTUTORIAL_CH", "1", "x", "SN_QTUTORIAL_CH", "SN_PAY", "xxx", "SN_PAYCON", "SN_NN", "SN_NC"]
SOURCE_QUEST_IDS = {"QNO_RM_MEETROC_1": "91", "QNO_RM_MEETROC_2": "92", "QNO_RM_MEETROC_6": "96",
	"QNO_RM_MEETROC_ECT_1": "374", "QNO_RM_MEETROC_ECT_2": "375", "QNO_RM_MEETROC_ECT_3": "376"}


# ================
# quest_row
#
# One questdata row in the 11-column layout v1.150 and the VSRO client share.
# ================
def quest_row(identity, code):
	return ["1", identity, code, "90", "x", "SN_" + code, "SN_PAY_" + code, "xxx", "SN_PAYCON_" + code,
		"SN_NN_" + code, "SN_NC_" + code]


SOURCE_QUESTS = [quest_row(identity, code) for code, identity in SOURCE_QUEST_IDS.items()] + [
	quest_row("90", "QNO_RM_VILLAGECHIEF_1")]


# ================
# contents_row
#
# A questcontentsdata row in the client's seventeen-column layout.
# ================
def contents_row(code, columns=17):
	row = [code, "x", "3", "xxx", "1", "SN_CON_" + code] + ["xxx"] * 7 + ["0", "80", "xxx", "0"]
	return row[:columns]


# v1.150 ships the contents rows of MEETROC_1, _2 and _6; the client
# ships all six.
NATIVE_CONTENTS = [contents_row(code, 13) for code in ("QTUTORIAL_CH", "QNO_RM_MEETROC_1", "QNO_RM_MEETROC_2", "QNO_RM_MEETROC_6")]
SOURCE_CONTENTS = [contents_row(code) for code in SOURCE_QUEST_IDS]
GUARDIANS = {"MOB_QT_%02d_ROCKY_CLON2" % n for n in range(1, 8)}
# v1.150's interface table carries the countdown's other lines; the
# client's adds _AGAIN, in its fifteen-column layout.
NATIVE_UI = [["1", "UIIT_MSG_QUEST_ROC_BERSERKER_START", "k", "", "", "s", "t", "j", "The Roc has gone berserk.", "v", ""]]
AGAIN = "UIIT_MSG_QUEST_ROC_BERSERKER_AGAIN"
SOURCE_UI = [["1", AGAIN, "k", "", "t", "s", "d", "j", "Roc's Berserk mode is advanced.", "v", "", "", "", "", ""],
	["1", "UIIT_MSG_QUEST_ROC_BERSERKER_START", "k", "", "t", "s", "d", "j", "started", "v", "", "", "", "", ""]]


# ================
# gate_row / building_row / link_row
#
# The source client's Roc rows, in its own column layout (13, 58, 21).
# ================
def gate_row(identity, code, building, world):
	return ["1", identity, code, building, "SN_ZONE_" + identity, "23406", "1382", "4056", "1558", "300", "0", "0", world]


def building_row(identity, code):
	row = ["0"] * 58
	row[0], row[1], row[2], row[5] = "1", identity, code, "SN_" + code
	return row


def link_row(source, target, conditions):
	row = ["1", source, target, "0", "0", "0"] + ["0"] * 15
	for at, condition in enumerate(conditions):
		row[6 + 3 * at:9 + 3 * at] = condition
	return row


SOURCE_GATES = [
	gate_row("160", "GATE_RC_MOUNTAIN_GATE", MOUNTAIN_BUILDING, "1"),
	gate_row("161", "GATE_RC_TAHOMET_GATE", "0", "41"),
	gate_row("162", "GATE_RC_ROC_GATE", "0", "40"),
	gate_row("163", "GATE_RC_ROC_FRONT_GATE", "24297", "1"),
]
SOURCE_LINKS = [
	link_row("160", "162", [["4", SOURCE_KEY_ID, "0"], ["5", SOURCE_KEY_ID, "0"], ["1", "80", "102"], ["6", "0", "0"]]),
	link_row("162", "160", []),
]


# ================
# symbol_of
#
# The name symbol each fixture kind carries: the arena's Tahomet reuses
# the native Tahomet's, as the VSRO client authors it.
# ================
def symbol_of(code):
	return NATIVE_TAHOMET_SYMBOL if code == "MOB_RM_TAHOMET_ROC" else "SN_" + code


# ================
# RocImportTest
# ================
class RocImportTest(unittest.TestCase):
	def setUp(self):
		tables._verified_archives.clear()

	# ================
	# fixture
	#
	# A scratch world: a fake VSRO root (one Media.pk2) carrying the
	# eleven kinds (Roc with one skill) and the client's questdata, a
	# native root, and an output textdata with one native kind whose
	# name the arena's Tahomet reuses and one native quest. overrides
	# replaces kind rows by codename; quests replaces the client's
	# questdata rows.
	# ================
	def fixture(self, base, overrides=None, quests=None, contents=None, ui=None):
		vsro, native, output = base / "vsro", base / "native", base / "output"
		text_root = output / roc.TEXT_ROOT
		text_root.mkdir(parents=True)
		vsro.mkdir()
		(native / roc.TEXT_ROOT).mkdir(parents=True)
		(native / roc.TEXT_ROOT / roc.UI_TABLE).write_bytes(utf16(NATIVE_UI))
		native_char = ["0"] * 104
		native_char[1], native_char[2], native_char[5] = "100", "MOB_RM_TAHOMET", NATIVE_TAHOMET_SYMBOL
		guardians = []
		for n in range(1, 8):
			row = ["0"] * 104
			row[1], row[2] = str(5871 + n), "MOB_QT_%02d_ROCKY_CLON2" % n
			guardians.append(row)
		(text_root / "characterdata_5000.txt").write_bytes(utf16([native_char] + guardians))
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / "skilldata_5000.txt").write_bytes(utf16([]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", NATIVE_TAHOMET_SYMBOL, "Tahomet"]]))
		(text_root / "skilleffect.txt").write_bytes(utf16([
			["#section", "characterinfo"], ["MOB_RM_TAHOMET"] + ["0"] * 12, ["#section", "skillaniset"]]))
		(text_root / "teleportdata.txt").write_bytes(utf16([gate_row("1", "GATE_CH", "2094", "1")]))
		(text_root / "teleportbuilding.txt").write_bytes(utf16([]))
		(text_root / "teleportlink.txt").write_bytes(utf16([]))
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([["1", "7", "ITEM_ETC_NATIVE"]]))
		(text_root / roc.QUEST_TABLE).write_bytes(utf16([NATIVE_QUEST]))
		(text_root / roc.CONTENTS_TABLE).write_bytes(utf16(NATIVE_CONTENTS))
		rows = []
		for at, (code, (grade, level)) in enumerate(roc.KINDS.items()):
			rows.append(later_row(code, symbol_of(code), level, grade=grade, identity=9000 + at))
		rows[0][89] = str(ROC_SKILL)
		for code, row in (overrides or {}).items():
			rows = [row if old[2] == code else old for old in rows]
		media = {
			"characterdata.txt": utf16([["characterdata_6000.txt"]]),
			"characterdata_6000.txt": utf16(rows),
			"skilldata.txt": utf16([["skilldata_6000.txt"]]),
			"skilldata_6000.txt": utf16([skill_row(ROC_SKILL, "MSKILL_RM_ROC_TEST")]),
			"textdata_object.txt": utf16([[str(at), symbol_of(code), "x", "x", code, "f", "x", "a", "b", "c"]
				for at, code in enumerate(roc.KINDS) if code != "MOB_RM_TAHOMET_ROC"]
				+ [["1", "SN_STORE_RC_TAHOMET_GATE", "k", "", "", "s", "t", "j", "Summit Gate", "v"]]),
			"teleportdata.txt": utf16(SOURCE_GATES),
			"teleportbuilding.txt": utf16([building_row(MOUNTAIN_BUILDING, "STORE_RC_TAHOMET_GATE"),
				building_row("24297", "STORE_ROC_FRONT_GATE")]),
			"teleportlink.txt": utf16(SOURCE_LINKS),
			"itemdata.txt": utf16([["itemdata_20000.txt"]]),
			"itemdata_20000.txt": utf16([["1", SOURCE_KEY_ID, KEY_ITEM]]),
			"questdata.txt": utf16(SOURCE_QUESTS if quests is None else quests),
			"questcontentsdata.txt": utf16(SOURCE_CONTENTS if contents is None else contents),
			roc.UI_TABLE: utf16(SOURCE_UI if ui is None else ui),
			"skilleffect.txt": utf16([["#section", "characterinfo"]]
				+ [[code] + ["0"] * 12 for code in roc.KINDS] + [["#section", "skillaniset"]]),
		}
		media_bytes = archive_bytes(media)
		(vsro / "Media.pk2").write_bytes(media_bytes)
		pins = {"Media": hashlib.sha256(media_bytes).hexdigest()}
		return vsro, native, output, text_root, pins

	# ================
	# run_import
	#
	# One add_roc call. On, the region merge is stubbed to run the staging
	# step and record what it was handed; the staging summary returns.
	# ================
	def run_import(self, vsro, native, output, pins, flag):
		self.merged = []
		self.pinned = []

		def merge_world(spec, native_root, out, root, regions, build_monsters):
			self.merged.append((spec.label, list(regions)))
			return {"enabled": True, "regions": len(regions), "monsters": build_monsters()}

		with mock.patch.object(tables, "archive_pins", return_value=pins), \
			mock.patch.object(tables, "require_vsro_pin", lambda root, names: self.pinned.append(tuple(names))), \
			mock.patch.object(roc.later_world_import, "merge_world", merge_world), \
			mock.patch.dict(os.environ, {roc.FLAG: flag}):
			result = roc.add_roc(vsro, native, output)
		return result["monsters"] if result.get("enabled") else result

	# ================
	# links
	# ================
	def links(self, text_root):
		return [row for row in tables.table_rows((text_root / "teleportlink.txt").read_bytes()) if len(row) > 2]

	# ================
	# test_on_merges_the_wait_room_and_pins_every_archive
	# ================
	def test_on_merges_the_wait_room_and_pins_every_archive(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(self.merged, [("roc", [roc.WAIT_REGION])])
			self.assertEqual(self.pinned, [roc.MERGE_ARCHIVES])

	# ================
	# test_on_adds_the_four_gates_and_their_buildings
	# ================
	def test_on_adds_the_four_gates_and_their_buildings(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			codes = [row[2] for row in tables.table_rows((text_root / "teleportdata.txt").read_bytes()) if len(row) > 2]
			self.assertEqual(codes, ["GATE_CH"] + list(roc.GATE_CODES))
			buildings = [row[2] for row in tables.table_rows((text_root / "teleportbuilding.txt").read_bytes())
				if len(row) > 2]
			self.assertEqual(sorted(buildings), ["STORE_RC_TAHOMET_GATE", "STORE_ROC_FRONT_GATE"])
			self.assertEqual(summary["gates"]["buildings"], ["STORE_RC_TAHOMET_GATE", "STORE_ROC_FRONT_GATE"])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("SN_STORE_RC_TAHOMET_GATE", names)

	# ================
	# test_the_summit_link_stays_out_without_its_key_item
	#
	# The variant does not carry the key item yet (step 3 imports it):
	# the summit link is left out, never opened without its condition,
	# and the marker says why. The way back is unconditioned and stays.
	# ================
	def test_the_summit_link_stays_out_without_its_key_item(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual([row[1:3] for row in self.links(text_root)], [["162", "160"]])
			self.assertEqual(summary["gates"]["omittedLinks"], [{"from": "160", "to": "162", "items": [SOURCE_KEY_ID]}])
			self.assertEqual(summary["gates"]["keyItems"], {})
			self.assertTrue(any("key item" in limit for limit in summary["limits"]))

	# ================
	# test_the_summit_link_names_the_variant_key_item
	# ================
	def test_the_summit_link_names_the_variant_key_item(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"], ["itemdata_25000.txt"]]))
			(text_root / "itemdata_25000.txt").write_bytes(utf16([["1", VARIANT_KEY_ID, KEY_ITEM]]))
			summary = self.run_import(vsro, native, output, pins, "on")
			summit = next(row for row in self.links(text_root) if row[1:3] == ["160", "162"])
			self.assertEqual(summit[6:18],
				["4", VARIANT_KEY_ID, "0", "5", VARIANT_KEY_ID, "0", "1", "80", "102", "6", "0", "0"])
			self.assertEqual(summary["gates"]["keyItems"], {SOURCE_KEY_ID: VARIANT_KEY_ID})
			self.assertNotIn("omittedLinks", summary["gates"])
			self.assertFalse(any("key item" in limit for limit in summary["limits"]))

	# ================
	# test_a_building_identity_the_variant_holds_refuses
	# ================
	def test_a_building_identity_the_variant_holds_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(text_root / "itemdata_5000.txt").write_bytes(utf16([["1", MOUNTAIN_BUILDING, "ITEM_MALL_HAT"]]))
			with self.assertRaisesRegex(ValueError, f"{MOUNTAIN_BUILDING} \\(ITEM_MALL_HAT\\)"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_on_stages_the_eleven_kinds
	# ================
	def test_on_stages_the_eleven_kinds(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(sorted(summary["characters"]), sorted(roc.KINDS))
			self.assertTrue(all(identity > 100 for identity in summary["characters"].values()))
			self.assertEqual(list(summary["skills"]), ["MSKILL_RM_ROC_TEST"])
			shard = (text_root / roc.CHARACTER_SHARD).read_bytes().decode("utf-16")
			converted = [line.split("\t") for line in shard.splitlines() if "\tMOB_RM_ROC\t" in line][0]
			self.assertEqual(len(converted), 120)
			# The skill slots (89-98 of the converted row) name the merged identity.
			self.assertIn(str(summary["skills"]["MSKILL_RM_ROC_TEST"]), converted[89:99])
			self.assertNotIn(str(ROC_SKILL), converted[89:99])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("SN_MOB_RM_ROC", names)
			# The native symbol the arena's Tahomet reuses is not the import's.
			self.assertNotIn(NATIVE_TAHOMET_SYMBOL, summary["nameSymbols"])
			self.assertIn("SN_MOB_RM_ROC", summary["nameSymbols"])
			manifest = [row[0] for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertIn(roc.SKILL_SHARD, manifest)

	# ================
	# test_rebuild_keeps_identities
	# ================
	def test_rebuild_keeps_identities(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			first = self.run_import(vsro, native, output, pins, "on")
			second = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(first["characters"], second["characters"])
			self.assertEqual(first["nameSymbols"], second["nameSymbols"])
			# A second pass replaces the first's gates, never stands them twice.
			codes = [row[2] for row in tables.table_rows((text_root / "teleportdata.txt").read_bytes()) if len(row) > 2]
			self.assertEqual(codes, ["GATE_CH"] + list(roc.GATE_CODES))
			self.assertEqual(len(self.links(text_root)), 1)
			self.assertEqual(self.quest_codes(text_root), ["QTUTORIAL_CH"] + list(roc.QUEST_CODES))

	# ================
	# test_off_reaps_and_keeps_the_native_name
	# ================
	def test_off_reaps_and_keeps_the_native_name(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(self.run_import(vsro, native, output, pins, ""), {"enabled": False})
			for name in (roc.CHARACTER_SHARD, roc.SKILL_SHARD, roc.MARKER):
				self.assertFalse((text_root / name).exists() or (output / name).exists())
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn(NATIVE_TAHOMET_SYMBOL, names)
			self.assertNotIn("SN_MOB_RM_ROC", names)
			manifest = [row[0] for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row]
			self.assertNotIn(roc.SKILL_SHARD, manifest)

	# ================
	# test_rewards_join_installed_items_and_leave_off
	#
	# Step 5b: with a degree-11 piece of the Roc's group 12 installed,
	# on writes roc-rewards.txt; off removes it with the rest.
	# ================
	def test_rewards_join_installed_items_and_leave_off(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			piece = ["0"] * 13
			piece[0], piece[1], piece[2], piece[9:13] = "1", "8", "ITEM_CH_BLADE_11_A", ["3", "1", "6", "2"]
			(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"], ["itemdata_110000.txt"]]))
			(text_root / "itemdata_110000.txt").write_bytes(utf16([piece]))
			summary = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(summary["rewards"]["random"], 8)
			written = json.loads((text_root / roc.roc_rewards.REWARDS_FILE).read_text(encoding="utf-8"))
			self.assertEqual({rule["Monster"] for rule in written["Random"]} - set(roc.KINDS), set())
			self.run_import(vsro, native, output, pins, "")
			self.assertFalse((text_root / roc.roc_rewards.REWARDS_FILE).exists())

	# ================
	# quest_codes
	# ================
	def quest_codes(self, text_root):
		return [row[2] for row in tables.table_rows((text_root / roc.QUEST_TABLE).read_bytes()) if len(row) > 2]

	# ================
	# test_on_stages_the_six_quests_under_their_source_ids
	# ================
	def test_on_stages_the_six_quests_under_their_source_ids(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			self.assertEqual(summary["quests"], list(roc.QUEST_CODES))
			rows = tables.table_rows((text_root / roc.QUEST_TABLE).read_bytes())
			self.assertEqual(rows[0], NATIVE_QUEST)
			self.assertEqual({row[2]: row[1] for row in rows[1:]}, SOURCE_QUEST_IDS)
			# The client's other quests are not the chain's.
			self.assertNotIn("QNO_RM_VILLAGECHIEF_1", self.quest_codes(text_root))

	# ================
	# test_on_writes_the_quest_kinds_nests
	# ================
	def test_on_writes_the_quest_kinds_nests(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			population = json.loads((text_root / roc.POPULATION_FILE).read_text(encoding="utf-8"))
			committed = json.loads(roc.QUEST_NESTS.read_text(encoding="utf-8"))["population"]
			self.assertEqual(population, committed)
			self.assertEqual({row["Codename"] for row in population["Rows"]},
				{"MOB_QT_RM_FEATHERMASK_01", "MOB_QT_RM_ROCKY_CLON_01"} | GUARDIANS)
			self.assertEqual(summary["population"]["nests"], len(committed["Rows"]))

	# ================
	# test_nests_naming_an_unstaged_kind_refuse
	# ================
	def test_nests_naming_an_unstaged_kind_refuse(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			document = json.loads(roc.QUEST_NESTS.read_text(encoding="utf-8"))
			document["population"]["Rows"][0]["Codename"] = "MOB_RM_STRAY"
			stray = Path(root) / "quest-nests.json"
			stray.write_text(json.dumps(document), encoding="utf-8")
			with mock.patch.object(roc, "QUEST_NESTS", stray), self.assertRaisesRegex(ValueError, "MOB_RM_STRAY"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_a_quest_id_the_native_table_holds_refuses
	# ================
	def test_a_quest_id_the_native_table_holds_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(text_root / roc.QUEST_TABLE).write_bytes(utf16([NATIVE_QUEST, quest_row("92", "QNO_NATIVE_92")]))
			with self.assertRaisesRegex(ValueError, "QNO_RM_MEETROC_2 \\(id 92\\) already stands"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_a_quest_the_client_lacks_refuses
	# ================
	def test_a_quest_the_client_lacks_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root), quests=SOURCE_QUESTS[:5])
			with self.assertRaisesRegex(ValueError, "no Roc quest QNO_RM_MEETROC_ECT_3"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_off_restores_the_native_questdata
	#
	# On, then off: the quest rows leave by the codenames the marker
	# enrolled, the table is the native bytes again, and the nests'
	# file is gone.
	# ================
	def test_off_restores_the_native_questdata(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			before = (text_root / roc.QUEST_TABLE).read_bytes()
			contents = (text_root / roc.CONTENTS_TABLE).read_bytes()
			self.run_import(vsro, native, output, pins, "on")
			self.run_import(vsro, native, output, pins, "")
			self.assertEqual((text_root / roc.QUEST_TABLE).read_bytes(), before)
			self.assertEqual((text_root / roc.CONTENTS_TABLE).read_bytes(), contents)
			self.assertFalse((text_root / roc.POPULATION_FILE).exists())

	# ================
	# test_on_stages_only_the_contents_rows_v1150_lacks
	#
	# The three ECT rows come from the client, cut to v1.150's thirteen
	# columns; the native MEETROC_1 row stays as v1.150 ships it.
	# ================
	def test_on_stages_only_the_contents_rows_v1150_lacks(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			ect = ["QNO_RM_MEETROC_ECT_1", "QNO_RM_MEETROC_ECT_2", "QNO_RM_MEETROC_ECT_3"]
			self.assertEqual(summary["contents"], ect)
			rows = tables.table_rows((text_root / roc.CONTENTS_TABLE).read_bytes())
			self.assertEqual(rows[:len(NATIVE_CONTENTS)], NATIVE_CONTENTS)
			self.assertEqual([row[0] for row in rows[len(NATIVE_CONTENTS):]], ect)
			self.assertTrue(all(len(row) == roc.CONTENTS_COLUMNS for row in rows))

	# ================
	# test_a_contents_row_the_client_lacks_refuses
	# ================
	def test_a_contents_row_the_client_lacks_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root), contents=SOURCE_CONTENTS[:-1])
			with self.assertRaisesRegex(ValueError, "row for Roc quest QNO_RM_MEETROC_ECT_3"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_off_reads_no_archive
	# ================
	def test_off_reads_no_archive(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			(vsro / "Media.pk2").unlink()
			self.assertEqual(self.run_import(vsro, native, output, pins, ""), {"enabled": False})

	# ================
	# test_moved_kind_refuses
	# ================
	def test_moved_kind_refuses(self):
		moved = later_row("MOB_RM_ROC", "SN_MOB_RM_ROC", 120, grade=3, identity=9000)
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root), {"MOB_RM_ROC": moved})
			with self.assertRaisesRegex(ValueError, "MOB_RM_ROC"):
				self.run_import(vsro, native, output, pins, "on")

	# ================
	# test_native_kind_refuses
	# ================
	def test_native_kind_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			row = ["0"] * 120
			row[1], row[2] = "200", "MOB_RM_ROC"
			(text_root / "characterdata_7000.txt").write_bytes(utf16([row]))
			with self.assertRaisesRegex(ValueError, "already stands"):
				self.run_import(vsro, native, output, pins, "on")



	# ================
	# test_on_adds_the_berserk_again_line
	#
	# The one countdown line v1.150 lacks joins the interface table in the
	# v1.150 layout, with the client's English; the native rows stay.
	# ================
	def test_on_adds_the_berserk_again_line(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			summary = self.run_import(vsro, native, output, pins, "on")
			rows = tables.table_rows((text_root / roc.UI_TABLE).read_bytes())
			self.assertEqual(summary["uiTexts"], [AGAIN])
			self.assertEqual([row[1] for row in rows], ["UIIT_MSG_QUEST_ROC_BERSERKER_START", AGAIN])
			self.assertEqual(rows[1][8], "Roc's Berserk mode is advanced.")
			self.run_import(vsro, native, output, pins, "on")
			rows = tables.table_rows((text_root / roc.UI_TABLE).read_bytes())
			self.assertEqual([row[1] for row in rows].count(AGAIN), 1)

	# ================
	# test_off_takes_the_berserk_again_line_out
	# ================
	def test_off_takes_the_berserk_again_line_out(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root))
			self.run_import(vsro, native, output, pins, "on")
			self.run_import(vsro, native, output, pins, "")
			rows = tables.table_rows((text_root / roc.UI_TABLE).read_bytes())
			self.assertEqual([row[1] for row in rows], ["UIIT_MSG_QUEST_ROC_BERSERKER_START"])

	# ================
	# test_a_client_without_the_again_line_refuses
	# ================
	def test_a_client_without_the_again_line_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			vsro, native, output, text_root, pins = self.fixture(Path(root), ui=SOURCE_UI[1:])
			with self.assertRaisesRegex(ValueError, AGAIN):
				self.run_import(vsro, native, output, pins, "on")

if __name__ == "__main__":
	unittest.main()

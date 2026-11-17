"""
===========================================================================
test_roc_rewards.py - the Roc kinds' database rewards joined to a variant (#3399 step 5b).

The committed evidence keeps a rule only where the variant installs its
items: a group keeps its installed members at their authored weights, a
rule with none waits and is listed, and with nothing kept no file is
written. A rule the server cannot represent refuses, and evidence from
another backup refuses.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import copy
import json
import tempfile
import unittest
from pathlib import Path

import roc_rewards as rewards  # noqa: E402
from test_hunting2_import import utf16  # noqa: E402

BLADE, BOW = "ITEM_CH_BLADE_11_A", "ITEM_CH_BOW_11_A"
SET_PIECE = "ITEM_ROC_CH_M_CLOTHES_AA_SET"
SCROLL = "ITEM_ETC_E051123_HIT_SCROLL"


# ================
# item_row
# ================
def item_row(identity, code, types=("3", "1", "6", "2")):
	row = ["0"] * 13
	row[0], row[1], row[2] = "1", str(identity), code
	row[9:13] = types
	return row


# ================
# evidence
# ================
def evidence():
	return json.loads(rewards.SOURCE.read_text(encoding="utf-8"))


# ================
# RocRewardsTest
# ================
class RocRewardsTest(unittest.TestCase):
	# ================
	# test_the_evidence_is_the_pinned_backup
	# ================
	def test_the_evidence_is_the_pinned_backup(self):
		source = evidence()
		self.assertEqual(source["sha256"], rewards.SOURCE_SHA256)
		roc = [row for row in source["random"] if row["monster"] == "MOB_RM_ROC"]
		self.assertEqual([(row["group"], row["distinct"], row["min"], row["max"]) for row in roc],
			[(1, True, 3, 3), (12, False, 2, 3), (13, False, 1, 1)])
		self.assertEqual(len(source["groups"]["1"]), 72)
		self.assertEqual({row["item"] for row in source["fixed"]}, {
			"ITEM_ETC_E051123_AGILITY_SCROLL", "ITEM_ETC_E051123_EVATION_SCROLL", SCROLL,
			"ITEM_ETC_E051123_SPEED_UP_SCROLL"})

	# ================
	# test_a_group_keeps_its_installed_members
	# ================
	def test_a_group_keeps_its_installed_members(self):
		installed = {code: item_row(n, code) for n, code in enumerate((BLADE, BOW))}
		rules, waiting = rewards.project_rewards(evidence(), installed)
		group = str(rewards.GROUP_BASE + 12)
		self.assertEqual([member["Codename"] for member in rules["Groups"][group]], [BLADE, BOW])
		self.assertEqual(rules["Groups"][group][0]["Probability"], evidence()["groups"]["12"][0]["probability"])
		self.assertEqual(list(rules["Groups"]), [group])
		roc = [rule for rule in rules["Random"] if rule["Monster"] == "MOB_RM_ROC"]
		self.assertEqual([(rule["Group"], rule["Min"], rule["Max"]) for rule in roc], [(int(group), 2, 3)])
		self.assertEqual(len(rules["Random"]), 8)
		self.assertEqual(rules["Fixed"], [])
		self.assertIn("MOB_RM_ROC/group 1", waiting)
		self.assertIn("MOB_RM_ROC/group 13", waiting)
		self.assertIn("MOB_RM_ROC/" + SCROLL, waiting)

	# ================
	# test_installed_scrolls_and_sets_join
	# ================
	def test_installed_scrolls_and_sets_join(self):
		installed = {SET_PIECE: item_row(1, SET_PIECE), SCROLL: item_row(2, SCROLL, ("3", "3", "13", "1"))}
		rules, waiting = rewards.project_rewards(evidence(), installed)
		self.assertEqual([(rule["Item"], rule["Min"], rule["Max"]) for rule in rules["Fixed"]], [(SCROLL, 5, 6)])
		# Group 1 draws three distinct pieces: with one installed the server's
		# draw would run out, so the rule waits until three are.
		self.assertEqual(rules["Random"], [])
		self.assertIn("MOB_RM_ROC/group 1", waiting)
		for identity, family in enumerate(("HEAVY", "LIGHT"), 3):
			code = SET_PIECE.replace("CLOTHES", family)
			installed[code] = item_row(identity, code)
		rules, waiting = rewards.project_rewards(evidence(), installed)
		self.assertNotIn("MOB_RM_ROC/group 1", waiting)
		self.assertEqual(len(rules["Groups"][str(rewards.GROUP_BASE + 1)]), 3)
		self.assertEqual(rules["Random"], [{"Monster": "MOB_RM_ROC", "Group": rewards.GROUP_BASE + 1,
			"Distinct": True, "Min": 3, "Max": 3, "Probability": 1.0}])

	# ================
	# test_options_on_equipment_refuse
	# ================
	def test_options_on_equipment_refuse(self):
		with self.assertRaisesRegex(ValueError, "options name equipment"):
			rewards.project_rewards(evidence(), {SCROLL: item_row(2, SCROLL)})

	# ================
	# test_unrepresentable_rules_refuse
	# ================
	def test_unrepresentable_rules_refuse(self):
		for mutate, message in (
			(lambda source: source["fixed"][0].update(rent="RENT_A"), "rent code"),
			(lambda source: source["random"][0].update(params=[1, 0]), "random parameters"),
			(lambda source: source["random"][0].update(min=4, max=3), "out of range"),
			(lambda source: source.update(sha256="0" * 64), "pinned"),
		):
			source = copy.deepcopy(evidence())
			mutate(source)
			with self.assertRaisesRegex(ValueError, message):
				rewards.project_rewards(source, {})

	# ================
	# test_add_rewards_writes_only_what_it_keeps
	# ================
	def test_add_rewards_writes_only_what_it_keeps(self):
		with tempfile.TemporaryDirectory() as root:
			text_root = Path(root)
			(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"], ["itemdata_110000.txt"]]))
			(text_root / "itemdata_5000.txt").write_bytes(utf16([item_row(7, "ITEM_ETC_NATIVE", ("3", "3", "1", "1"))]))
			(text_root / "itemdata_110000.txt").write_bytes(utf16([item_row(8, BLADE)]))
			summary = rewards.add_rewards(text_root)
			written = json.loads((text_root / rewards.REWARDS_FILE).read_text(encoding="utf-8"))
			self.assertEqual(summary["random"], 8)
			self.assertEqual(written["Version"], rewards.REWARDS_VERSION)
			self.assertEqual({member["Codename"] for listed in written["Groups"].values() for member in listed}, {BLADE})
			(text_root / "itemdata_110000.txt").write_bytes(utf16([]))
			summary = rewards.add_rewards(text_root)
			self.assertEqual((summary["fixed"], summary["random"]), (0, 0))
			self.assertFalse((text_root / rewards.REWARDS_FILE).exists())


if __name__ == "__main__":
	unittest.main()

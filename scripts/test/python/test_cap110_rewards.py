"""
===========================================================================
test_cap110_rewards.py - the monsters' database rewards and their items.
===========================================================================
"""
import json
from pathlib import Path
import tempfile
import unittest

import cap110_items as items
import cap110_monsters as monsters
import cap110_rewards as rewards


# ================
# fixed
# ================
def fixed(monster, item, ratio=0.5, low=1, high=1, **changes):
	row = {"monster": monster, "item": item, "group_type": 0, "plus": 0, "amount_min": low, "amount_max": high,
		"ratio": ratio, "rent": "xxx", **{"m" + str(index): 0 for index in range(1, 10)}}
	row.update(changes)
	return row


# ================
# random
# ================
def random(monster, group, ratio=0.12, **changes):
	row = {"service": 1, "monster": monster, "item_group": group, "distinct_items": 0, "amount_min": 1,
		"amount_max": 2, "ratio": ratio, "param1": 0, "param2": 0}
	row.update(changes)
	return row


# ================
# member
# ================
def member(group, item, ratio=1.0, **changes):
	row = {"service": 1, "item_group": group, "item": item, "ratio": ratio, "magic_group": 0}
	row.update(changes)
	return row


# ================
# item
#
# A later-client item row of the given kind.
# ================
def item(code, kind=("3", "3", "11", "4"), identity=100, service="1"):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME], row[items.PLACEHOLDER_NAME] = service, str(identity), code, "korean"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = kind
	for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
		row[column], row[column + 1] = "-1", items.UNUSED
	return row


# ================
# RewardsTest
# ================
class RewardsTest(unittest.TestCase):
	# ================
	# test_only_the_installed_monsters_rules_are_projected
	# ================
	def test_only_the_installed_monsters_rules_are_projected(self):
		population = {"MOB_SD_SPHINX"}
		got = rewards.project_rewards(
			[fixed("MOB_SD_SPHINX", "ITEM_A", 0.416), fixed("MOB_CH_TIGER", "ITEM_B")],
			[random("MOB_SD_SPHINX", 2), random("MOB_CH_TIGER", 2), random("MOB_SD_SPHINX", 4, service=0)],
			[member(2, "ITEM_ETC_SD_TOKEN_01"), member(4, "ITEM_ETC_SD_TOKEN_03")], population)
		self.assertEqual([r["Item"] for r in got["Fixed"]], ["ITEM_A"])
		self.assertEqual([(r["Monster"], r["Group"]) for r in got["Random"]], [("MOB_SD_SPHINX", rewards.GROUP_BASE + 2)])
		self.assertEqual(list(got["Groups"]), [str(rewards.GROUP_BASE + 2)])
		self.assertEqual(got["Groups"][str(rewards.GROUP_BASE + 2)], [{"Codename": "ITEM_ETC_SD_TOKEN_01", "Probability": 1.0}])
		self.assertEqual(rewards.reward_items(got), ["ITEM_A", "ITEM_ETC_SD_TOKEN_01"])

	# ================
	# test_an_installed_qinshi_population_earns_its_rules
	#
	# The qinshi nest file's presence is the ask (#1800 step 2): its
	# monsters join the rewarded population, a family never installed
	# contributes nothing.
	# ================
	def test_an_installed_qinshi_population_earns_its_rules(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			(root / monsters.POPULATION_FILE).write_text(
				json.dumps({"Rows": [{"Codename": "MOB_SD_SPHINX"}]}), encoding="utf-8")
			(root / monsters.QINSHI_FILE).write_text(
				json.dumps({"Rows": [{"Codename": "MOB_TQ_TOMBGUARD"}, {"Codename": "MOB_QT_TQ_SCOUT"}]}), encoding="utf-8")
			population = rewards.population_codes(root)
		self.assertEqual(population, {"MOB_SD_SPHINX", "MOB_TQ_TOMBGUARD", "MOB_QT_TQ_SCOUT"})
		got = rewards.project_rewards(
			[fixed("MOB_TQ_TOMBGUARD", "ITEM_ETC_ARCHEMY_STONE"), fixed("MOB_GOD_UNRELATED", "ITEM_B")],
			[random("MOB_QT_TQ_SCOUT", 7)], [member(7, "ITEM_ETC_SD_TOKEN_01")], population)
		self.assertEqual([r["Monster"] for r in got["Fixed"]], ["MOB_TQ_TOMBGUARD"])
		self.assertEqual([(r["Monster"], r["Group"]) for r in got["Random"]], [("MOB_QT_TQ_SCOUT", rewards.GROUP_BASE + 7)])

	# ================
	# test_no_qinshi_file_no_qinshi_rules
	# ================
	def test_no_qinshi_file_no_qinshi_rules(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			(root / monsters.POPULATION_FILE).write_text(
				json.dumps({"Rows": [{"Codename": "MOB_SD_SPHINX"}]}), encoding="utf-8")
			population = rewards.population_codes(root)
		got = rewards.project_rewards([fixed("MOB_TQ_TOMBGUARD", "ITEM_A")], [], [], population)
		self.assertEqual(got["Fixed"], [])

	# ================
	# test_imported_groups_never_share_a_native_group_id
	# ================
	def test_imported_groups_never_share_a_native_group_id(self):
		got = rewards.project_rewards([], [random("MOB_SD_A", 2)], [member(2, "ITEM_X")], {"MOB_SD_A"})
		self.assertTrue(all(int(group) >= rewards.GROUP_BASE for group in got["Groups"]))
		self.assertEqual(got["Random"][0]["Group"], rewards.GROUP_BASE + 2)

	# ================
	# test_rules_the_server_cannot_represent_refuse
	# ================
	def test_rules_the_server_cannot_represent_refuse(self):
		population = {"MOB_SD_A"}
		for row in (fixed("MOB_SD_A", "ITEM_X", group_type=1), fixed("MOB_SD_A", "ITEM_X", m3=17),
			fixed("MOB_SD_A", "ITEM_X", rent="RENT_1"), fixed("MOB_SD_A", "ITEM_X", ratio=0),
			fixed("MOB_SD_A", "ITEM_X", ratio=1.5), fixed("MOB_SD_A", "ITEM_X", low=3, high=2), fixed("MOB_SD_A", None)):
			with self.subTest(row=row), self.assertRaises(ValueError):
				rewards.project_rewards([row], [], [], population)
		for row, members in ((random("MOB_SD_A", 2, param1=5), [member(2, "ITEM_X")]), (random("MOB_SD_A", 9), [member(2, "ITEM_X")]),
			(random("MOB_SD_A", 2), [member(2, None)]),):
			with self.subTest(row=row), self.assertRaises(ValueError):
				rewards.project_rewards([], [row], members, population)

	# ================
	# test_the_magic_group_column_does_not_refuse_an_etc_reward
	#
	# The column carries the group's own id for every row; it only shapes
	# equipment, which the item-kind check keeps out.
	# ================
	def test_the_magic_group_column_does_not_refuse_an_etc_reward(self):
		got = rewards.project_rewards([], [random("MOB_SD_A", 2)], [member(2, "ITEM_ETC_SD_TOKEN_01", magic_group=2)], {"MOB_SD_A"})
		self.assertEqual(len(got["Groups"]), 1)

	# ================
	# test_native_items_win_and_other_kinds_refuse
	# ================
	def test_native_items_win_and_other_kinds_refuse(self):
		later = {"ITEM_NEW": item("ITEM_NEW"), "ITEM_NATIVE": item("ITEM_NATIVE"), "ITEM_SWORD": item("ITEM_SWORD", ("3", "1", "6", "2")),
			"ITEM_OFF": item("ITEM_OFF", service="0")}
		native = {"ITEM_NATIVE": item("ITEM_NATIVE"), "ITEM_NATIVE_SWORD": item("ITEM_NATIVE_SWORD", ("3", "1", "6", "2"))}
		picked = rewards.select_imports(["ITEM_NATIVE", "ITEM_NEW"], later, native)
		self.assertEqual([row[items.CODENAME] for row in picked], ["ITEM_NEW"])
		for code in ("ITEM_SWORD", "ITEM_OFF", "ITEM_MISSING", "ITEM_NATIVE_SWORD"):
			with self.subTest(code=code), self.assertRaises(ValueError):
				rewards.select_imports([code], later, native)
		linked = item("ITEM_LINKED")
		linked[items.PARAM_FIRST], linked[items.PARAM_FIRST + 1] = "2", "ITEM_ELEMENT"
		linked[items.PARAM_FIRST + 2], linked[items.PARAM_FIRST + 3] = "7", "a text label"
		with self.assertRaises(ValueError):
			rewards.select_imports(["ITEM_LINKED"], {"ITEM_LINKED": linked}, {})
		# An element link to a native item, and text labels, are the authored shape.
		picked = rewards.select_imports(["ITEM_LINKED"], {"ITEM_LINKED": linked}, {"ITEM_ELEMENT": item("ITEM_ELEMENT")})
		self.assertEqual([row[items.CODENAME] for row in picked], ["ITEM_LINKED"])

	# ================
	# test_new_identities_are_fresh_and_published_ones_survive
	# ================
	def test_new_identities_are_fresh_and_published_ones_survive(self):
		selected = [item("ITEM_A", identity=7), item("ITEM_B", identity=8)]
		first = rewards.convert_rows(selected, {50, 60}, {})
		self.assertEqual([int(row[items.IDENTITY]) for row in first], [61, 62])
		self.assertTrue(all(row[items.PLACEHOLDER_NAME] == items.UNUSED for row in first))
		# A rebuild with a new item sorted ahead keeps the published identities.
		again = rewards.convert_rows([item("ITEM_0", identity=6)] + selected, {50, 60}, {"ITEM_A": 61, "ITEM_B": 62})
		self.assertEqual({row[items.CODENAME]: int(row[items.IDENTITY]) for row in again}, {"ITEM_0": 63, "ITEM_A": 61, "ITEM_B": 62})
		self.assertEqual(selected[0][items.IDENTITY], "7", "the source rows are not edited")
		with self.assertRaises(ValueError):
			rewards.convert_rows(selected, {61}, {"ITEM_A": 61})


if __name__ == "__main__":
	unittest.main()

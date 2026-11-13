"""
===========================================================================

test_cap110_skill_database.py - database skill projection and stable joins

Synthetic rows test explicit SQL field names, independent source identities,
cross-source chains, rebuild stability, and refusal to drop published content.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
import json
import tempfile
import unittest
from pathlib import Path

import cap110_skill_database as database
import cap110_skills as skills
from test_cap110_skills import make_skill


# ================
# DatabaseSkillTests
# ================
class DatabaseSkillTests(unittest.TestCase):
	# ================
	# test_wrong_tier_snapshot_is_refused
	# ================
	def test_wrong_tier_snapshot_is_refused(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "skills.json"
			path.write_text(json.dumps([dict(zip(database.DATABASE_COLUMNS, make_skill(mastery_level="100")))]), encoding="utf-8")
			with self.assertRaises(ValueError):
				database.load_database_rows(path)

	# ================
	# test_client_rows_above_the_expansion_cap_are_not_imported
	# ================
	def test_client_rows_above_the_expansion_cap_are_not_imported(self):
		native = [make_skill(id_="10", mastery_level="90")]
		later = [make_skill(id_="20", code="SKILL_CH_SWORD_B_01", mastery_level="111")]
		self.assertEqual(skills.merge_skills(native, later)[0], [])

	# ================
	# test_projection_uses_field_names_not_json_order
	# ================
	def test_projection_uses_field_names_not_json_order(self):
		row = make_skill()
		record = dict(reversed(list(zip(database.DATABASE_COLUMNS, row))))
		self.assertEqual(database.project_database_rows([record]), [row])
		del record["Param50"]
		with self.assertRaises(ValueError):
			database.project_database_rows([record])

	# ================
	# test_projection_refuses_embedded_table_delimiters
	# ================
	def test_projection_refuses_embedded_table_delimiters(self):
		record = dict(zip(database.DATABASE_COLUMNS, make_skill()))
		record["Basic_Name"] = "name\tforged row"
		with self.assertRaises(ValueError):
			database.project_database_rows([record])

	# ================
	# test_database_rank_joins_client_group_and_chain_without_changing_ids
	# ================
	def test_database_rank_joins_client_group_and_chain_without_changing_ids(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		client = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="20", code="SKILL_CH_SWORD_A_02", group="600", level="2", mastery_level="100"),
		]
		rows = [
			make_skill(id_="101", code="SKILL_CH_SWORD_A_01", group="800", mastery_level="90"),
			make_skill(id_="102", code="SKILL_CH_SWORD_A_02", group="800", level="2", mastery_level="100"),
			make_skill(id_="20", code="SKILL_CH_SWORD_A_03", group="800", level="3", mastery_level="110", chain_next="102"),
		]
		client_added, prior = skills.merge_skills(native, client)
		added, report = database.merge_database_tier(native, client, rows, prior)
		self.assertEqual(added[0], client_added[0])
		self.assertEqual(added[1][skills.GROUP], "500")
		self.assertEqual(added[1][skills.CHAIN_NEXT], added[0][skills.IDENTITY])
		self.assertNotEqual(added[1][skills.IDENTITY], added[0][skills.IDENTITY])
		rebuilt, again = database.merge_database_tier(native, client, rows, report)
		self.assertEqual(rebuilt, added)
		self.assertEqual(again["idMap"], report["idMap"])
		with self.assertRaises(ValueError):
			database.merge_database_tier(native, client, [], report)

	# ================
	# test_off_database_path_leaves_client_import_unchanged
	# ================
	def test_off_database_path_leaves_client_import_unchanged(self):
		native = [make_skill(id_="10", mastery_level="90")]
		client = [make_skill(id_="20", code="SKILL_CH_SWORD_B_01", mastery_level="100")]
		self.assertEqual(database.merge_database_tier(native, client, [], {}), skills.merge_skills(native, client))


if __name__ == "__main__":
	unittest.main()

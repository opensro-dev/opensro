"""
===========================================================================

test_element_resist_evidence.py - the element resist snapshot's row filter

Synthetic export tables: the importer keeps a later-only character row
whose six resists leave the level curve, and drops a row on the curve, a
row v1.150 already holds, a structure at none and a non-character.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import unittest

import alexandria_npcs as npcs  # noqa: E402
import import_element_resist_evidence as evidence  # noqa: E402

CHAR_HEADER = ["ID", "Lvl"] + list(evidence.RESIST_FIELDS)
COMMON_HEADER = ["CodeName128", "TypeID1", "TypeID3", "Link"]


# ================
# export
#
# One (header map, rows) pair, as read_export returns it.
# ================
def export(header, rows):
	return {name: i for i, name in enumerate(header)}, [[str(cell) for cell in row] for row in rows]


# ================
# OffCurveTests
# ================
class OffCurveTests(unittest.TestCase):
	def rows(self, native=frozenset()):
		chars = export(CHAR_HEADER, [
			[1, 100, 100, 100, 91, 91, 91, 91],  # a unique above its curve (90)
			[2, 50, 82, 82, 82, 82, 82, 82],  # on the curve
			[3, 24, 0, 0, 0, 0, 0, 0],  # below its curve (48)
			[4, 30, 0, 0, 0, 0, 0, 0],  # a structure: none is the curve
			[5, 60, 1, 1, 1, 1, 1, 1],  # linked from a non-character
		])
		common = export(COMMON_HEADER, [
			["MOB_BOSS", "1", "1", 1],
			["MOB_PLAIN", "1", "1", 2],
			["MOB_MASK", "1", "1", 3],
			["STRUCTURE_GATE", "1", npcs.STRUCTURE_TYPE, 4],
			["ITEM_X", "3", "1", 5],
		])
		return evidence.off_curve_rows(chars, common, native)

	def test_only_the_rows_off_the_curve_are_kept(self):
		self.assertEqual(self.rows(), {"MOB_BOSS": [100, 100, 91, 91, 91, 91], "MOB_MASK": [0] * npcs.RESISTANCE_COUNT})

	def test_a_row_v1150_holds_keeps_its_native_values(self):
		self.assertEqual(list(self.rows(native={"MOB_BOSS"})), ["MOB_MASK"])

	def test_the_snapshot_text_is_json_the_module_reads(self):
		text = evidence.snapshot_text({"t": "0" * 64}, self.rows())
		snapshot = json.loads(text)
		self.assertEqual(snapshot["format"], npcs.CARRIED_RESISTS_FORMAT)
		self.assertEqual(snapshot["rows"]["MOB_BOSS"], [100, 100, 91, 91, 91, 91])
		self.assertEqual(text.count("\n"), 1 + len(snapshot["rows"]) + 1)


if __name__ == "__main__":
	unittest.main()

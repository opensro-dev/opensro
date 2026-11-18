"""
===========================================================================
test_import_isror_tomb_balance.py - the tomb balance import's contract.

Each qinshi_tomb_nests.tsv row takes radius, generateRadius, delays and cap
from ISRO-R's active nest of the same id. Two nests at one anchor are told
apart by the archived balance the row carries. A restored unique the
active table leaves to a trigger keeps its row. A row with no single match
refuses the whole import. A second run changes nothing.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import import_isror_tomb_balance as tomb  # noqa: E402

FLOOR3 = 32773
FLOOR6 = 32770
HEADER = "# Qin-Shi Emperor Tomb nests\n# codename\tregion\tx\ty\tz\t..."


# ================
# row
#
# A 22-column TSV row with the given balance; the other columns are fixed.
# ================
def row(codename, region, anchor, balance):
	radius, generate, delay_min, delay_max, max_count = balance
	x, y, z = anchor
	columns = [codename, str(region), x, y, z, str(radius), str(generate), "10", str(delay_min), str(delay_max), str(max_count)]
	columns += ["1", "1", "100", "0", "2", "1", "1", "100", "0", "2", "0"]
	return "\t".join(columns)


# ================
# nest
# ================
def nest(nest_id, region, anchor, balance):
	return (nest_id, region, float(anchor[0]), float(anchor[1]), float(anchor[2]), balance)


SHARED = ("100.5", "0", "200.25")
ALONE = ("-50", "1.5", "75")
UNIQUE = ("10", "0", "10")
ARCHIVED = [
	nest(1, FLOOR3, SHARED, (900, 500, 6, 8, 16)),
	nest(2, FLOOR3, SHARED, (900, 500, 12, 16, 12)),
	nest(3, FLOOR3, ALONE, (1000, 500, 80, 160, 12)),
	nest(4, FLOOR6, UNIQUE, (150, 100, 8, 10, 0)),
]
ACTIVE = [
	nest(1, FLOOR3, SHARED, (20, 40, 6, 8, 16)),
	nest(2, FLOOR3, SHARED, (20, 40, 12, 16, 12)),
	nest(3, FLOOR3, ALONE, (20, 40, 1, 2, 5)),
	nest(4, FLOOR6, UNIQUE, (150, 100, 8, 10, 0)),
]


# ================
# tsv
# ================
def tsv(rows):
	return HEADER + "\n" + "\n".join(rows) + "\n"


class TombBalance(unittest.TestCase):
	def setUp(self):
		self.text = tsv([
			# The two shared-anchor rows, archived balance, radii already live (#3874).
			row("MOB_A", FLOOR3, SHARED, (20, 40, 12, 16, 12)),
			row("MOB_B", FLOOR3, SHARED, (20, 40, 6, 8, 16)),
			row("MOB_C", FLOOR3, ALONE, (1000, 500, 80, 160, 12)),
			# A restored unique: the native trigger's 1800..3600 s and cap 1.
			row("MOB_UNIQUE", FLOOR6, UNIQUE, (150, 100, 1800, 3600, 1)),
		])

	def balance(self, text, codename):
		for line in text.split("\n"):
			if line.startswith(codename + "\t"):
				columns = line.split("\t")
				return tuple(int(columns[c]) for c in tomb.BALANCE_COLUMNS)
		self.fail("no row " + codename)

	def test_rows_take_the_active_balance_of_their_own_nest(self):
		updated, changed, kept = tomb.rewrite(self.text, ARCHIVED, ACTIVE)
		self.assertEqual(self.balance(updated, "MOB_C"), (20, 40, 1, 2, 5))
		# Shared anchor: each row keeps its own nest, never its neighbour's.
		self.assertEqual(self.balance(updated, "MOB_A"), (20, 40, 12, 16, 12))
		self.assertEqual(self.balance(updated, "MOB_B"), (20, 40, 6, 8, 16))
		self.assertEqual((changed, kept), (1, 1))

	def test_a_restored_unique_keeps_its_trigger_balance(self):
		updated, _, _ = tomb.rewrite(self.text, ARCHIVED, ACTIVE)
		self.assertEqual(self.balance(updated, "MOB_UNIQUE"), (150, 100, 1800, 3600, 1))

	def test_a_second_run_changes_nothing(self):
		once, _, _ = tomb.rewrite(self.text, ARCHIVED, ACTIVE)
		twice, changed, _ = tomb.rewrite(once, ARCHIVED, ACTIVE)
		self.assertEqual(twice, once)
		self.assertEqual(changed, 0)
		self.assertEqual(once.count(tomb.PROVENANCE), 1)

	def test_a_row_without_a_single_nest_refuses_the_import(self):
		stray = tsv([row("MOB_STRAY", FLOOR3, ("999", "0", "999"), (1000, 500, 6, 8, 10))])
		with self.assertRaises(SystemExit):
			tomb.rewrite(stray, ARCHIVED, ACTIVE)
		# A shared-anchor row whose balance names neither nest is not guessed.
		unknown = tsv([row("MOB_A", FLOOR3, SHARED, (900, 500, 7, 9, 3))])
		with self.assertRaises(SystemExit):
			tomb.rewrite(unknown, ARCHIVED, ACTIVE)


if __name__ == "__main__":
	unittest.main()

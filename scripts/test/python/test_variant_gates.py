"""
===========================================================================

test_variant_gates.py - gate building ids stay inside the server's band

The server spawns a gate building at GID 250000 + its id and refuses one
at or past the ground items' 300000 ("teleport identity overflow"). A
building moved off an id another object holds, or authored fresh, takes a
free id under 50000, never "highest object + 1".

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import arabia_gates  # noqa: E402
import variant_gates  # noqa: E402


class BuildingIdTests(unittest.TestCase):
	# ================
	# test_a_clashing_building_moves_under_the_limit
	# ================
	def test_a_clashing_building_moves_under_the_limit(self):
		buildings = [["1", "27751", "STORE_GATE_A"], ["1", "27752", "STORE_GATE_B"]]
		moved = variant_gates.gate_buildings(buildings, {27751, 49999, 51299})
		self.assertEqual(moved, {"27751": "49998"})
		self.assertEqual([row[1] for row in buildings], ["49998", "27752"])

	# ================
	# test_a_source_id_past_the_band_moves_and_a_free_one_stays
	# ================
	def test_a_source_id_past_the_band_moves_and_a_free_one_stays(self):
		ids = arabia_gates.renumbered_buildings(["40790", "51295"], {"40000", "51299"})
		self.assertEqual(ids, {"40790": "40790", "51295": "49999"})
		self.assertTrue(all(int(i) < variant_gates.BUILDING_ID_LIMIT for i in ids.values()))


if __name__ == "__main__":
	unittest.main()

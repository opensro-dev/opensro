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
import json
import os
import tempfile
import unittest
from pathlib import Path

import arabia_gates  # noqa: E402
import published_ids  # noqa: E402
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
		ids = arabia_gates.renumbered_buildings([["1", "40790", "STORE_A"], ["1", "51295", "STORE_B"]], {"40000", "51299"})
		self.assertEqual(ids, {"40790": "40790", "51295": "49999"})
		self.assertTrue(all(int(i) < variant_gates.BUILDING_ID_LIMIT for i in ids.values()))

	# ================
	# test_a_pinned_building_keeps_its_released_id
	#
	# #4024: an appointed rebirth gate names its building by id. A pinned
	# code takes its released id although the pin also lists that id as
	# taken, a clashing source id that is not pinned steps over it, and a
	# code the pin does not hold is numbered as before.
	# ================
	def test_a_pinned_building_keeps_its_released_id(self):
		with tempfile.TemporaryDirectory() as folder:
			path = Path(folder) / "pin.json"
			path.write_text(json.dumps({"format": published_ids.PIN_FORMAT, "release": "abc", "skills": {},
				"items": {"ITEM_MALL_AVATAR": 27753}, "characters": {"STORE_SD_GATE1": 49997}}), encoding="utf-8")
			saved = os.environ[published_ids.PIN_VARIABLE]
			os.environ[published_ids.PIN_VARIABLE] = str(path)
			try:
				buildings = [["1", "27753", "STORE_SD_GATE1"], ["1", "27754", "STORE_SD_GATE2"]]
				moved = variant_gates.gate_buildings(buildings, {27753, 27754, 49997, 49999})
				self.assertEqual(moved, {"27753": "49997", "27754": "49998"})
				ids = variant_gates.building_identities([["1", "0", "STORE_NEW"]], {0, 49997, 49998, 49999})
				self.assertEqual(ids, {"0": "49996"})
			finally:
				os.environ[published_ids.PIN_VARIABLE] = saved


if __name__ == "__main__":
	unittest.main()

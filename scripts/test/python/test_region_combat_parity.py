"""
===========================================================================

test_region_combat_parity.py - the client and server region-combat tables agree

scripts/build/import_region_combat.py writes both tables from one parse
of _RefRegion. The research backup it reads is not in a public clone, so
this test pins the two checked-in outputs to each other instead: the
same source digest, row count and ranges, in the same order.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import pathlib
import re
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[3]
SERVER_TABLE = ROOT / "apps/server/internal/game/world/regioncombat_generated.go"
CLIENT_TABLE = ROOT / "apps/client-next/src/engine/foundation/gameplay/region-combat-ranges.ts"
GO_RANGE = re.compile(r"\{0x([0-9a-f]+), 0x([0-9a-f]+), (true|false)\}")
TS_RANGE = re.compile(r"\{ first: 0x([0-9a-f]+), last: 0x([0-9a-f]+), allowed: (true|false) \}")
HEADER = re.compile(r"_RefRegion source SHA256: ([0-9a-f]{64})\n// Catalog rows: (\d+)")


# ================
# table
#
# A generated table's header (digest, rows) and its ranges.
# ================
def table(path, ranges):
	text = path.read_text(encoding="utf-8")
	header = HEADER.search(text)
	if not header:
		raise AssertionError(f"{path.name} has no generator header")
	return header.groups(), ranges.findall(text)


class RegionCombatParityTest(unittest.TestCase):

	def test_the_client_table_is_the_server_table(self):
		server_header, server = table(SERVER_TABLE, GO_RANGE)
		client_header, client = table(CLIENT_TABLE, TS_RANGE)
		self.assertTrue(server, "the server table holds no ranges")
		self.assertEqual(client_header, server_header)
		self.assertEqual(client, server)


if __name__ == "__main__":
	unittest.main()

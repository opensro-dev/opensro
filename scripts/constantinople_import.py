r"""
===========================================================================

constantinople_import.py - the Constantinople fortress area (v1.696)

PORT-ONLY, NOT NATIVE. Off unless SRO_FORT_CONSTANTINOPLE is "1", "on"
or "true". Called by build_alexandria_variant.py after the Sky Temple
import; the server reads the same switch. Build and server must agree,
and the flag-off build is byte-identical to the build without this
import: everything below joins only while the switch is on, and turning
it off reaps the shard, the copied files and the owned table rows.

The source is the operator's own v1.696 client (SRO_V1696_ROOT, default
inputs/sro_1696 beside the game root); its archives are never committed
and are verified against the pin file before anything reads them (the
Sky Temple import's verify_archives).

What the source authors, and what this import does with it:

	regions   the 36 RN_FORT_CONSTANTINOPLE sectors (a 6 x 6 block,
	          sectors x 90-95, z 59-64). Terrain, navmesh, tile and
	          object catalogues merge over the variant's current ones
	          exactly as the Sky Temple's do; regioncode and
	          textzonename rows join where the current row is absent
	          or a placeholder.
	fortress  the 49 INS_FORT_CT event zones join eventzonedata.txt,
	          the FORTRESS_CONSTANTINOPLE row joins siegefortress.txt,
	          and the seven structure characters the shipped tables
	          lack (the fort stone, the two gates and the four gate
	          pulleys) join as characterdata_210000.txt. The zones'
	          placements (navmesh\objectstring.ifo) are staged as
	          objectstring.portonly.ifo beside the native file; the
	          server bundle's structure-zones projection merges it
	          when it exists. The fortress then stands up through the
	          port's existing fortress line: the authority catalog
	          reads siegefortress.txt, appendFortressStructures joins
	          zones, placements and structure records, and
	          Authority.ForWorld binds world 5 (INS_FORT_CT, already in
	          the shipped gameworld catalogue) to fortress id 4. The
	          three fortress staff NPCs npcpos places in the yard are
	          shipped characters already; staging their spawns is a
	          population change and stays a follow-up, not this import.

The engine is later_fortress_import.add_fortress; this module names the
fortress (SPEC) and keeps the entry points the variant's later layers
and the tests call.

The 210000 identity band: 120000 is the cap-110 family's, 160000 the
Sky Temple's, 170000 Jupiter's, 180000 the second hunting grounds',
190000 GOD's temples and the gears stones', 200000 Arabia's and
Muhan's. 210000 is this fortress's.

===========================================================================
"""
import argparse
import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import later_fortress_import as fortress  # noqa: E402
import skytemple_import as sky  # noqa: E402

FLAG = "SRO_FORT_CONSTANTINOPLE"
MARKER = ".opensro-constantinople.json"
CHARACTER_SHARD = "characterdata_210000.txt"
PORTONLY_IFO = "objectstring.portonly.ifo"
TEXT_ROOT = fortress.TEXT_ROOT

REGION_CODE = "RN_FORT_CONSTANTINOPLE"
FORTRESS_CODE = "FORTRESS_CONSTANTINOPLE"
FORTRESS_NAME_SYMBOL = "SN_FORTRESS_CONSTANTINOPLE"
FORTRESS_WORLD = "INS_FORT_CT"
EXPECTED_REGIONS = 36
EXPECTED_ZONES = 49

SPEC = fortress.FortressSpec(
	label="constantinople",
	source_label="v1.696",
	flag=FLAG,
	marker=MARKER,
	character_shard=CHARACTER_SHARD,
	portonly_ifo=PORTONLY_IFO,
	region_code=REGION_CODE,
	fortress_code=FORTRESS_CODE,
	name_symbol=FORTRESS_NAME_SYMBOL,
	world=FORTRESS_WORLD,
	expected_regions=EXPECTED_REGIONS,
	expected_zones=EXPECTED_ZONES,
	open_archive=lambda root, name: sky.open_archive(root, name),
	verify_archives=lambda root: sky.verify_archives(root),
	default_root=lambda: sky.source_root(),
)


# ================
# enabled
# ================
def enabled(env=os.environ):
	return fortress.enabled(SPEC, env)


# ================
# fortress_regions
# ================
def fortress_regions(regioncode_rows):
	return fortress.fortress_regions(SPEC, regioncode_rows)


# ================
# fortress_zones
# ================
def fortress_zones(eventzone_rows):
	return fortress.fortress_zones(SPEC, eventzone_rows)


# ================
# objectstring_lines
# ================
def objectstring_lines(text, zone_names):
	return fortress.objectstring_lines(SPEC, text, zone_names)


# ================
# add_constantinople
# ================
def add_constantinople(native, output, source=None, closure=None):
	return fortress.add_fortress(SPEC, native, output, source, closure or sky.copy_resource_closure)


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	parser.add_argument("--source-root")
	args = parser.parse_args()
	print(json.dumps(add_constantinople(args.native_extracted, args.output, args.source_root), indent=2))


if __name__ == "__main__":
	main()

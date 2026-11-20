r"""
===========================================================================

hotan_fortress_import.py - the Hotan fortress (v1.188)

PORT-ONLY, NOT NATIVE to v1.150. Off unless SRO_FORT_HOTAN is "1", "on" or
"true" when the variant builds; the server reads the same switch.

v1.150 ships the world (INS_FORT_HT, gameworld row 4) and the official's
character, but none of the 40 RN_FORT_HOTAN sectors, no fortress row and
no zones. This stages the sectors, the FORTRESS_HOTAN row (id 3), its 52
zones and their placements, the fort stone and big gate the shipped
tables lack, and the v1.188 standings of the fortress staff v1.150 ships
but never places: the official in Hotan town, and the manager, battle
aide, smith and trainer at the fortress.

The source is the one pinned VSRO client (SRO_VSRO_ROOT, its Map,
Media, Data and Particles archives verified against
vsro_archive_pins.json); the engine is later_fortress_import.add_fortress,
which records everything it adds in .opensro-hotan-fortress.json and removes
exactly that when the switch is off.

The 300000 identity band: one importer owns one shard file in a build
(test_later_shard_bands.py).

===========================================================================
"""
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import import_vsro_alexandria as tables  # noqa: E402
import later_fortress_import as fortress  # noqa: E402

FLAG = "SRO_FORT_HOTAN"
MARKER = ".opensro-hotan-fortress.json"
CHARACTER_SHARD = "characterdata_300000.txt"
PORTONLY_IFO = "objectstring.hotan.portonly.ifo"
ARCHIVES = ("Map", "Media", "Data", "Particles")


# ================
# vsro_root
#
# The pinned client the variant build names; a switch on without it
# stops the build by name.
# ================
def vsro_root(env=os.environ):
	configured = env.get(tables.ROOT_ENV, "").strip()
	if not configured:
		raise SystemExit(f"{FLAG} is on: set {tables.ROOT_ENV} to the VSRO client folder.")
	return Path(configured)


# The big gate's row (v1.188 and v1.696 alike) names
# Bldg\guild\ht\big_gate_ht.bsr, but both clients ship that file in
# the fortress's own folder, Bldg\guild\hotang\ (beside its bridge,
# plaza and castle gate). Same file, corrected folder.
MODEL_FALLBACKS = (
	(r"Bldg\guild\ht\big_gate_ht.bsr", r"Bldg\guild\hotang\big_gate_ht.bsr"),
)

SPEC = fortress.FortressSpec(
	label="hotan fortress",
	source_label="v1.188",
	flag=FLAG,
	marker=MARKER,
	character_shard=CHARACTER_SHARD,
	portonly_ifo=PORTONLY_IFO,
	region_code="RN_FORT_HOTAN",
	fortress_code="FORTRESS_HOTAN",
	name_symbol="SN_FORTRESS_HOTAN",
	world="INS_FORT_HT",
	expected_regions=40,
	expected_zones=52,
	open_archive=lambda root, name: tables.open_archive(root, name),
	verify_archives=lambda root: tables.require_vsro_pin(root, ARCHIVES),
	default_root=lambda: vsro_root(),
	object_texts=fortress.vsro_object_texts,
	model_fallbacks=MODEL_FALLBACKS,
	staff_codes=("NPC_KT_FORTRESS_OFFICIAL", "NPC_KT_FORTRESS_MANAGER", "NPC_KT_FORTRESS_BATTLEAIDE",
		"NPC_KT_FORTRESS_SMITH", "NPC_KT_FORTRESS_TRAINER"),
)


# ================
# add
#
# The Alexandria builder's pass, after the Roc's.
# ================
def add(vsro_root, native, output, closure=None):
	return fortress.add_fortress(SPEC, native, output, vsro_root or None,
		closure or fortress.shipped_closure)

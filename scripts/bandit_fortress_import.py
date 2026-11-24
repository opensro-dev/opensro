r"""
===========================================================================

bandit_fortress_import.py - the Bandit Fortress by Jangan (v1.188)

Later-native content, imported by every variant build: the later
client has the fortress, so the game does (owner, #3586). There is no
switch; the spec's flag is None and the engine imports it on every run.

v1.150 ships the ground (its 16 RN_FORT_BIJEOKDAN sectors beside the
Jangan fortress) and the world (INS_FORT_BJ, gameworld row 7), but not
the fortress: siegefortress.txt holds Jangan alone, the 43 INS_FORT_BJ
event zones are absent, and the sectors are the ground before the
fortress was built. This stages the v1.188 sectors over them, the
FORTRESS_BIJEOKDAN row (id 6), its zones and their placements, the fort
stone and small gate the shipped tables lack, and the fortress's
world-map page (1002) with its icons and art. Its registration desk is
Jangan's: the row names NPC_CH_FORTRESS_OFFICIAL, who already stands
in Jangan.

The source is the one pinned VSRO client (SRO_VSRO_ROOT, its Map,
Media, Data and Particles archives verified against
vsro_archive_pins.json); the engine is later_fortress_import.add_fortress,
which records everything it adds in .opensro-bandit-fortress.json.

The 290000 identity band: one importer owns one shard file in a build
(test_later_shard_bands.py).

===========================================================================
"""
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import import_vsro_alexandria as tables  # noqa: E402
import later_fortress_import as fortress  # noqa: E402

MARKER = ".opensro-bandit-fortress.json"
CHARACTER_SHARD = "characterdata_290000.txt"
PORTONLY_IFO = "objectstring.bijeokdan.portonly.ifo"
ARCHIVES = ("Map", "Media", "Data", "Particles")


# ================
# vsro_root
#
# The pinned client the variant build names; a standalone run without
# it stops by name (the variant build itself always names one: stage 1
# reads the same client).
# ================
def vsro_root(env=os.environ):
	configured = env.get(tables.ROOT_ENV, "").strip()
	if not configured:
		raise SystemExit(f"set {tables.ROOT_ENV} to the VSRO client folder.")
	return Path(configured)


# The structure rows name two files no client ships: fort_stone_bj.bsr
# and small_gate_bj.bsr are in no Data.pk2 of v1.150, v1.188, v1.657 or
# v1.696. Each client ships the Bandit fortress's own models under
# other names, and the rows map onto them one for one:
# - Fort stone: every client ships exactly eight fort stones (bd, ct,
#   dw, er, hd, ht, ja, sk) for exactly eight siege worlds (INS_FORT_BJ,
#   CT, DW, ER, HM, HT, JA, SK). The two "-dan" fortresses carry the "d":
#   Bijeokdan's stone is fort_stone_bd, Heukmakdan's fort_stone_hd.
# - Gate: Bldg\guild\bj\ holds one gate, big_gate_bj.bsr. The later
#   maps' object.ifo (v1.188, v1.657 and v1.696 alike) places it as
#   object 1979. The Bandit rows author no big gate, only the small one.
# The rows' paths are authoring slips, as Hotan's big gate is. Only the
# drawn model changes; the structure's row, HP and zones are authored.
MODEL_FALLBACKS = (
	(r"Artifact\Guild\fort_stone\fort_stone_bj.bsr", r"Artifact\Guild\fort_stone\fort_stone_bd.bsr"),
	(r"Bldg\guild\bj\small_gate_bj.bsr", r"Bldg\guild\bj\big_gate_bj.bsr"),
)

SPEC = fortress.FortressSpec(
	label="bandit fortress",
	source_label="v1.188",
	flag=None,
	marker=MARKER,
	character_shard=CHARACTER_SHARD,
	portonly_ifo=PORTONLY_IFO,
	region_code="RN_FORT_BIJEOKDAN",
	fortress_code="FORTRESS_BIJEOKDAN",
	name_symbol="SN_FORTRESS_BIJEOKDAN",
	world="INS_FORT_BJ",
	expected_regions=16,
	expected_zones=43,
	map_page=1002,
	open_archive=lambda root, name: tables.open_archive(root, name),
	verify_archives=lambda root: tables.require_vsro_pin(root, ARCHIVES),
	default_root=lambda: vsro_root(),
	object_texts=fortress.vsro_object_texts,
	model_fallbacks=MODEL_FALLBACKS,
	# The fortress's own staff (the second Chinese set); the official
	# is Jangan's, shared as in the v1.188 tables, which stand only one.
	staff_codes=("NPC_CH_FORTRESS_MANAGER2", "NPC_CH_FORTRESS_BATTLEAIDE2", "NPC_CH_FORTRESS_SMITH2"),
)


# ================
# add
#
# The Alexandria builder's pass, after the Roc's.
# ================
def add(vsro_root, native, output, closure=None):
	return fortress.add_fortress(SPEC, native, output, vsro_root or None,
		closure or fortress.shipped_closure)

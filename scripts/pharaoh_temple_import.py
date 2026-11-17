"""
===========================================================================

pharaoh_temple_import.py - the Pharaoh Temple's monsters and uniques.

The job temple's seven dungeons (pharaoh_temple.py: the Temple and six
sanctums, regions 0x800A-0x8010) ride SRO_PHARAOH_TEMPLE. That module
stages the dungeons and gates from the VSRO client; this one stages
what lives in them: 24 kinds and their nests, the five sanctum keepers
(Selkis, Neith, Anubis, Isis, Seth) among them.

Native source: the v1.188 server places the same 24 kinds with the same
nest counts in these regions (tactics-recovered Tab_RefNest, 105 nests
joined through Tab_RefTactics). Its recovered tactics keep 10 of the
fields the nest controls carry, so the rows come from the pinned
ISRO-R population export instead (temple-population.json, worlds 51-56
and 60: the temple groups), whose tactics are whole. The kinds come
from the pinned v1.696 client, as the other later worlds' do.

The export's NPC rows (guild agents, the Mentuhotep and Berenice
stands) are roster content and stay out. Not here: v1.188's one
MOB_SD_OSIRIS_3 nest (a world-1 hive in the Temple region, 8-hour
respawn), which the ISRO-R export does not carry and whose controls
need the v1.188 database's full tactics row; and Haroeris, whom
neither source places by nest.

No region merge: the dungeons are VSRO Data the variant build already
stages. The import is the shared monster pass (later_world_import.
add_monsters) with a band of its own, 270000. Port-only, not native,
as the temple itself is.

===========================================================================
"""
import os
from pathlib import Path

import import_vsro_alexandria as tables
import later_world_import

FLAG = "SRO_PHARAOH_TEMPLE"
ROOT_ENV = "SRO_V1696_ROOT"
SOURCE_ENV = "SRO_PHARAOH_TEMPLE_DATABASE"
MARKER = ".opensro-pharaoh-temple.json"
# The 270000 identity band, after the export-world areas' 250000 and
# Roc's 260000 (#3762). One
# importer owns one shard file in a build (test_later_shard_bands).
CHARACTER_SHARD = "characterdata_270000.txt"
SKILL_SHARD = "skilldata_270000.txt"
POPULATION_FILE = "pharaoh-temple.txt"
POPULATION_NAME = "temple-population.json"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")

# The ISRO-R shard backup every later population export is cut from.
POPULATION_SOURCE_SHA256 = "89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45"

# The source worlds of the temple groups (GROUP_TEMPLE_SELK .. _SETH,
# then GROUP_TEMPLE_DEFAULT), the same ids pharaohtemple.Dungeons keeps
# as each dungeon's SourceWorld on the server.
TEMPLE_WORLDS = (51, 52, 53, 54, 55, 56, 60)

# The temple's kinds by name: a closed set, so the shared MOB_SD_ prefix
# (the cap-110 desert field's) never draws the field's rows into this
# import's characterinfo or reap.
TEMPLE_KINDS = (
	"MOB_SD_ANUBIS", "MOB_SD_BLOODKHEPRI", "MOB_SD_DEVIL_SANDMAN", "MOB_SD_HARSAPHETH", "MOB_SD_HARSMESS",
	"MOB_SD_IMOUTHES", "MOB_SD_ISIS", "MOB_SD_KEISAS_BERSERK", "MOB_SD_NEITH", "MOB_SD_PRIEST_CHASTISEMENT",
	"MOB_SD_PRIEST_FEAR", "MOB_SD_PRIEST_FLAME", "MOB_SD_PRIEST_GALE", "MOB_SD_PRIEST_GUARD",
	"MOB_SD_PRIEST_HEALING", "MOB_SD_PRIEST_POWER", "MOB_SD_PRIEST_REVENGE", "MOB_SD_SANDSCOUT",
	"MOB_SD_SELKION", "MOB_SD_SELKIS", "MOB_SD_SEPTU", "MOB_SD_SETH", "MOB_SD_SPECTOR_DEMOLITION",
	"MOB_SD_SPECTOR_GREED",
)
# The level window and grades of those kinds in the v1.696 client (a
# sanity bound on the conversion, not a spawn filter): ordinary
# priests and guardians, and the five keepers at grade 3.
MIN_LEVEL = 105
MAX_LEVEL = 110
GRADES = (0, 3)


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_PHARAOH_TEMPLE names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# archive_pins
# ================
def archive_pins():
	"""The pinned v1.696 archive hashes (v1696_archive_pins.json beside this tool)."""
	return later_world_import.archive_pins(SPEC)


# ================
# family_admits
#
# One of the temple's named kinds, inside its level window, at a grade
# the temple uses.
# ================
def family_admits(code, row):
	if code not in TEMPLE_KINDS:
		return False
	return MIN_LEVEL <= int(row[57]) <= MAX_LEVEL and int(row[15]) in GRADES


# ================
# _claim_groups
# ================
def _claim_groups(rows, sub_enabled):
	return {"temple": {row["Codename"] for row in rows if row["WorldID"] in TEMPLE_WORLDS}}


# ================
# _admit
# ================
def _admit(code, row, families):
	return family_admits(code, row)


# ================
# hive_key
#
# The shared-cap key of a capped temple nest. The export carries a
# capped hive's total, not its id; the temple's capped nests share one
# hive per room in the native table (v1.188 Tab_RefNest: nests
# 29551-29554, Anubis's healing, power and guard priests, are all hive
# 639 with a total of 3), so world, region and cap name the hive.
# ================
def hive_key(row, cap):
	return f"pharaoh-temple:{row['WorldID']}:{row['RegionID']}:{cap}"


# ================
# _build_populations
#
# The engine's projection, except for capped nests: it keeps one nest
# per capped codename (no shared cap in the rows it writes), which here
# would turn a hive of three priests across four nests into nine. The
# temple's capped nests project whole, joined by a shared hive key the
# server caps as one (NestRow.HiveKey / HiveMaxCount, as Arabia's do).
# ================
def _build_populations(rows, sub_enabled):
	caps = {}
	plain = []
	for row in rows:
		cap = (row.get("Hive") or {}).get("OverwriteMaxTotalCount") or 0
		if cap:
			caps[row["NestID"]] = (hive_key(row, cap), cap)
			row = {**row, "Hive": {**row["Hive"], "OverwriteMaxTotalCount": 0}}
		plain.append(row)
	population = later_world_import.project_population(SPEC, plain, TEMPLE_WORLDS)
	for projected in population["Rows"]:
		if projected["NestID"] in caps:
			projected["HiveKey"], projected["HiveMaxCount"] = caps[projected["NestID"]]
	return {"temple": population}


# ================
# _summarize_populations
# ================
def _summarize_populations(populations, rows, claimed, refs, sub_enabled):
	population = populations["temple"]
	return {"nests": len(population["Rows"]), "regions": sorted({row["RegionID"] for row in population["Rows"]}),
		"keepers": sorted(code for code, row in refs.items() if int(row[15]) == 3)}


SPEC = later_world_import.WorldImport(
	label="pharaoh temple",
	flag=FLAG,
	requires=None,
	root_env=ROOT_ENV,
	source_env=SOURCE_ENV,
	population_name=POPULATION_NAME,
	source_sha256=POPULATION_SOURCE_SHA256,
	character_shard=CHARACTER_SHARD,
	skill_shard=SKILL_SHARD,
	marker=MARKER,
	pin_file=PIN_FILE,
	pin_sentence="The Pharaoh Temple's kinds come from one pinned v1.696 client; its archives are never committed.",
	region_label="Pharaoh Temple",
	worlds_required=TEMPLE_WORLDS,
	population_files=(("temple", POPULATION_FILE),),
	info_prefixes=TEMPLE_KINDS,
	claim_groups=_claim_groups,
	admit=_admit,
	build_populations=_build_populations,
	summarize_populations=_summarize_populations,
)


# ================
# add_pharaoh_temple_monsters
# ================
def add_pharaoh_temple_monsters(native, out, v1696_root=None, source_root=None):
	"""
	Stage the Pharaoh Temple's kinds and nests from the pinned v1.696
	client and the pinned ISRO-R export. Port-only, not native; off,
	what the marker records is reaped and no archive is read.
	"""
	enabled = flag_enabled()
	if enabled:
		v1696_root = later_world_import.client_root(v1696_root, ROOT_ENV)
	return later_world_import.add_monsters(SPEC, v1696_root, native, out, source_root, enabled, False,
		archive_pins)

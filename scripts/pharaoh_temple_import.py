"""
===========================================================================

pharaoh_temple_import.py - the Pharaoh Temple's monsters and uniques.

The job temple's seven dungeons (pharaoh_temple.py: the Temple and six
sanctums, regions 0x800A-0x8010) ride SRO_PHARAOH_TEMPLE. That module
stages the dungeons and gates from the VSRO client; this one stages
what lives in them: 26 kinds and their nests, the five sanctum keepers
(Selkis, Neith, Anubis, Isis, Seth) among them.

Native source: the v1.188 server places the same kinds with the same
nest counts in these regions (tactics-recovered Tab_RefNest, 105 nests
joined through Tab_RefTactics). Its recovered tactics keep 10 of the
fields the nest controls carry, so the rows come from the pinned
ISRO-R population export instead (temple-population.json, worlds 51-56
and 60: the temple groups), whose tactics are whole. The kinds come
from the pinned v1.696 client, as the other later worlds' do.

The export's NPC rows (guild agents, the Mentuhotep and Berenice
stands) are roster content and stay out. Two nests ride as named
exceptions the export does not carry (osiris_nest, haroeris_nest
below): the Temple's MOB_SD_OSIRIS_3, whose hive stands in the field
world, not under a temple group - its controls are the later
database's full tactics row, read whole - and MOB_SD_HAROERIS, whom
no source places by nest and no tactics row survives for: he stands
authored in his own sanctum on the sanctum keepers' pattern, the
inference recorded at haroeris_nest (#3787).

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
import later_world_population

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
	"MOB_SD_ANUBIS", "MOB_SD_BLOODKHEPRI", "MOB_SD_DEVIL_SANDMAN", "MOB_SD_HAROERIS", "MOB_SD_HARSAPHETH",
	"MOB_SD_HARSMESS",
	"MOB_SD_IMOUTHES", "MOB_SD_ISIS", "MOB_SD_KEISAS_BERSERK", "MOB_SD_NEITH", "MOB_SD_OSIRIS_3",
	"MOB_SD_PRIEST_CHASTISEMENT",
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

# The one unique among the temple's kinds, at the source grade its
# tomb brothers carry (8), not the keepers' 3: admitted by name, as
# the field admits Apis, without opening the grade to the family.
OSIRIS_KIND = "MOB_SD_OSIRIS_3"
OSIRIS_SOURCE_GRADE = 8
NAMED_GRADES = {OSIRIS_KIND: OSIRIS_SOURCE_GRADE}

# The temple's summoned unique (#3787): a keeper-grade kind (grade 3,
# level 109 in the pinned client) with no nest in any source. His
# authored nest id is chosen beside the Osiris row's native 40999;
# the ids read whole from the client at conversion time.
HAROERIS_KIND = "MOB_SD_HAROERIS"
HAROERIS_NEST_ID = 40998


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
	grade = int(row[15])
	return MIN_LEVEL <= int(row[57]) <= MAX_LEVEL and (grade in GRADES or NAMED_GRADES.get(code) == grade)


# ================
# _claim_groups
# ================
def _claim_groups(rows, sub_enabled):
	# The export names no Osiris or Haroeris nest, so their kinds are
	# claimed by name: the exception nests below name them, and a claimed
	# kind is converted here or placed on the tomb pass's standing row
	# (place_standing).
	return {"temple": {row["Codename"] for row in rows if row["WorldID"] in TEMPLE_WORLDS}
		| {OSIRIS_KIND, HAROERIS_KIND}}


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
# osiris_nest
#
# The one temple nest no population export carries, as the cap-110
# row the server reads. v1.188 groups it under the field world, not a
# temple group (Tab_RefNest 40999: hive 2761, world 1, cap 1; the
# Temple region 0x8010, an 8-hour respawn), so the ISRO-R export has
# no row for it and the projection would drop the one it was handed.
# Its controls are the v1.188 database's full tactics row (57573,
# naming the kind), read whole from the Eternity shard backup where
# the export's were read from ISRO-R's. The hive caps its one nest at
# one, which the row's own count already says, so no hive key rides.
# The server names this exception (monster/pharaoh_temple.go).
# ================
def osiris_nest():
	return {
		"Codename": OSIRIS_KIND, "NestID": 40999, "WorldID": 1,
		"RegionID": 32784, "X": -1735.0, "Y": -43.0, "Z": 1428.0,
		"Radius": 50, "GenerateRadius": 10, "MaxCount": 1,
		"RespawnDelayMinSec": 28800, "RespawnDelayMaxSec": 28900, "Respawn": True,
		"InitialDir": 0, "ChampionGenPercentage": 0,
		"Controls": {
			"ID": 57573, "ObjectID": 38210, "AIQoS": 0, "MaxStamina": 500, "StaminaVariance": 50,
			"SightRange": 200, "AggressType": 0, "AggressData": 0, "ChangeTarget": 2,
			"HelpRequest": 2, "HelpResponse": 2, "BattleStyle": 0, "BattleStyleData": 0,
			"DiversionBasis": 5, "DiversionKeepBasis": 4, "KeepDistance": 0, "KeepDistanceData": 0,
			"TraceType": 0, "TraceBoundary": 1, "TraceData": 500, "HomingType": 0, "HomingData": 0,
			"AggressOnHoming": 2, "FleeType": 0, "ChampionID": 0, "Flags": 112,
			"DiversionBasisData": [0, 0, 0, 0, 0, 30, 0, 0],
			"DiversionKeepBasisData": [0, 0, 0, 0, 0, 0, 0, 0],
		},
		"Champion": None,
	}


# ================
# haroeris_nest
#
# The one temple unique no population export carries, as the cap-110
# row the server reads. Neither the ISRO-R export nor the recovered
# native nest and tactics tables place MOB_SD_HAROERIS by nest (#3787:
# he is a summoned unique, and the chain that calls him survives only
# as later-client objects no pinned source converts), so no native row
# can be read whole the way the Osiris row was. Recorded inference,
# in the open: he stands in his own sanctum (GROUP_TEMPLE_HARO, world
# 55, region 32782) at its specter nests' anchor, on the sanctum
# keepers' pattern - one alive, the keepers' 10,800 s respawn, a
# 50-unit ground - with controls mirrored from the paired keeper's
# export row (Seth: the schedule names the two together), re-keyed to
# Haroeris's own client object (26681) and an authored tactics id
# beside Seth's (38691, free in the export), so no champion or help
# link can mistake the two. His authored nest id (40998) sits beside
# the Osiris row's native 40999. The server admits him by the keepers'
# own rule - MOB_SD_ at keeper grade inside the temple window - and
# places the row under its sanctum's source world, so no loader
# exception rides along (monster/pharaoh_temple.go).
# ================
def haroeris_nest():
	return {
		"Codename": HAROERIS_KIND, "NestID": HAROERIS_NEST_ID, "WorldID": 55,
		"RegionID": 32782, "X": 9612.509765625, "Y": 101.83999633789062, "Z": 29.829999923706055,
		"Radius": 50, "GenerateRadius": 10, "MaxCount": 1,
		"RespawnDelayMinSec": 10800, "RespawnDelayMaxSec": 10800, "Respawn": True,
		"InitialDir": 0, "ChampionGenPercentage": 0,
		"Controls": {
			"ID": 38691, "ObjectID": 26681, "AIQoS": 0, "MaxStamina": 150, "StaminaVariance": 50,
			"SightRange": 600, "AggressType": 0, "AggressData": 0, "ChangeTarget": 2,
			"HelpRequest": 2, "HelpResponse": 2, "BattleStyle": 0, "BattleStyleData": 0,
			"DiversionBasis": 5, "DiversionKeepBasis": 4, "KeepDistance": 0, "KeepDistanceData": 0,
			"TraceType": 0, "TraceBoundary": 1, "TraceData": 500, "HomingType": 0, "HomingData": 0,
			"AggressOnHoming": 1, "FleeType": 0, "ChampionID": 0, "Flags": 0,
			"DiversionBasisData": [0, 0, 0, 0, 0, 30, 0, 0],
			"DiversionKeepBasisData": [0, 0, 0, 0, 0, 0, 0, 0],
		},
		"Champion": None,
	}


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
	population = later_world_population.project_population(SPEC, plain, TEMPLE_WORLDS)
	for projected in population["Rows"]:
		if projected["NestID"] in caps:
			projected["HiveKey"], projected["HiveMaxCount"] = caps[projected["NestID"]]
	# The projection keeps the temple worlds' rows alone; the Osiris
	# and Haroeris exceptions (osiris_nest, haroeris_nest) ride beside
	# them, in nest order.
	population["Rows"].append(haroeris_nest())
	population["Rows"].append(osiris_nest())
	population["Rows"].sort(key=lambda row: row["NestID"])
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
	# The Osiris kind is the tomb pass's too (its world-59 nest): when
	# that pass has staged it, this one places its nest on the
	# standing row instead of staging a second.
	place_standing=True,
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

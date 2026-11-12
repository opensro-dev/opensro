"""
===========================================================================

pharaoh_temple.py - the Pharaoh Temple (the job temple): its dungeons and entrances

PORT-ONLY, NOT NATIVE. Off unless SRO_PHARAOH_TEMPLE is "1", "on" or "true".
v1.150 has no temple. The VSRO client has one in the Storm and Cloud
Desert: seven dungeons (the Temple and six sanctums) behind two desert gates,
one for the merchant and hunter jobs, one for the thief job.

This module owns what the temple adds to the variant's tables, and
add_temple writes them into a variant tree, as the other optional
layers' importers own their file work (skytemple_import.py,
hunting2_import.py). The tables:

	dungeoninfo.txt    seven rows, region ids 10-16 (regions 0x800A-0x8010),
	                   appended in the native layout (region id, dof path).
	                   v1.150 uses ids 1 and 9 and the Qin-Shi rows 2-7
	                   (dungeonInfo.mjs); nothing collides.
	eventzonedata.txt  the two TELEPORT zones the Temple's way out is made of
	                   (GATE_TEMPLE_*_GATE, ids 234 and 235, after the native
	                   233): the temple gates have no object, a player walks
	                   into them, as the Donwhang cave's gates (#1018).
	teleport tables    the four gates, merged by build_alexandria_variant like
	                   TOMB_GATE_CODES; the codes are named here.

The dungeon files themselves (Dungeon\\Asiam\\*.dof and every /asiam/ model,
material, lightmap and animation they use) are VSRO Data.pk2 files the
variant already stages ("every VSRO file the native tree lacks"), from the
hash-pinned archive, so this module names the seven .dof files and checks
they arrived.

The Go server's counterpart is internal/game/world/pharaohtemple: the same
region ids and gate codes, kept in step by a test on each side.

===========================================================================
"""
import os
from pathlib import Path

import import_vsro_alexandria as stage1

FLAG = "SRO_PHARAOH_TEMPLE"
FLAG_OFF_VALUES = ("", "0", "off", "false")
FLAG_ON_VALUES = ("1", "on", "true")

# The dungeon region bit: dungeoninfo.txt names a region by its low 15 bits.
DUNGEON_REGION_BIT = 0x8000

# The seven dungeons by dungeoninfo id (the VSRO client's dungeoninfo.txt rows
# 10-16): id, .dof path as the table spells it, what it is. Region 0x8010 is
# the Temple itself; 0x800A-0x800F are its six sanctums.
TEMPLE_DUNGEON_ID = 16
DUNGEONS = (
	(10, "Dungeon\\Asiam\\R1_Cv\\pha_cv_selkis.dof", "Sanctum of Restriction"),
	(11, "Dungeon\\Asiam\\R1_Cv\\pha_cv_neith.dof", "Sanctum of the Blue Eye"),
	(12, "Dungeon\\Asiam\\R1_Cv\\pha_cv_anubis.dof", "Sanctum of Punishment"),
	(13, "Dungeon\\Asiam\\R1_Cv\\pha_cv_isis.dof", "Sanctum of Atonement"),
	(14, "Dungeon\\Asiam\\R1_Cv\\pha_cv_haro.dof", "Sanctum of Immortality"),
	(15, "Dungeon\\Asiam\\R2_Cv\\st_seth.dof", "Sanctum of Dark (Seth)"),
	(TEMPLE_DUNGEON_ID, "Dungeon\\Asiam\\R1_Cv\\pha_cv.dof", "Pharaoh Temple"),
)

# The gates: the two clicked desert gates (teleportbuilding objects in Storm
# and Cloud Desert) and the two walk-in gates inside the Temple. The link
# 171 -> 173 / 172 -> 174 and back carry a level 105 condition; the job
# check is not in the link table (the original runs it in code, slice 2).
DESERT_GATE_CODES = ("GATE_DESERT_MERCHANT_HUNTER_GATE", "GATE_DESERT_THIEF_GATE")
TEMPLE_ZONE_CODES = ("GATE_TEMPLE_MERCHANT_HUNTER_GATE", "GATE_TEMPLE_THIEF_GATE")
GATE_CODES = DESERT_GATE_CODES + TEMPLE_ZONE_CODES

# The destination world of every temple gate in the source (the Temple is
# database world 60, the sanctums 51-56). The port has no such worlds: the
# Temple is a dungeon region of the persistent world, as the Donwhang cave
# is, so the gates' world column is flattened to this.
SOURCE_WORLD_COLUMN_VALUE = "1"

# eventzonedata.txt: column 1 is the zone id, column 2 its codename.
EVENT_ZONE_ID_COLUMN = 1
EVENT_ZONE_CODE_COLUMN = 2


# ================
# temple_enabled
# ================
def temple_enabled(environ=None):
	"""
	Whether SRO_PHARAOH_TEMPLE is on. False (unset, or an off value) is the
	native build; anything else is refused, as the server refuses a value it
	does not know, so a typo cannot silently build the native tables.
	"""
	value = (os.environ if environ is None else environ).get(FLAG, "").strip().lower()
	if value in FLAG_OFF_VALUES:
		return False
	if value in FLAG_ON_VALUES:
		return True
	raise ValueError(f"{FLAG} must be off, on or unset, not {value!r}")


# ================
# dungeon_regions
# ================
def dungeon_regions():
	"""The seven dungeons' region ids (0x800A-0x8010), in dungeoninfo order."""
	return [DUNGEON_REGION_BIT | dungeon_id for dungeon_id, _, _ in DUNGEONS]


# ================
# dof_path
# ================
def dof_path(dungeon):
	"""The relative path of a dungeon's .dof under Data_extracted, lower case."""
	return "dungeon/" + dungeon[1].split("\\", 1)[1].replace("\\", "/").lower()


# ================
# dungeon_info_rows
# ================
def dungeon_info_rows():
	"""The seven rows in the native dungeoninfo.txt layout: id, tab, quoted path."""
	return [f'{dungeon_id}\t"{path}"' for dungeon_id, path, _ in DUNGEONS]


# ================
# merge_dungeon_info
# ================
def merge_dungeon_info(native_raw):
	"""
	The native dungeoninfo.txt (CP949) with the temple's seven rows appended;
	the native bytes stay first. A native table that already names one of the
	ids or paths is refused: the rows would collide, and the build must say so
	rather than guess. Returns the bytes unchanged when every row is already
	there exactly (a rebuild of an on variant).
	"""
	text = native_raw.decode("cp949")
	rows = dungeon_info_rows()
	if all(row in text.splitlines() for row in rows):
		return native_raw
	taken = {}
	for line in text.splitlines():
		cells = line.split("\t", 1)
		if len(cells) == 2 and cells[0].isdigit():
			taken[int(cells[0])] = cells[1].strip().lower()
	for dungeon_id, path, _ in DUNGEONS:
		if dungeon_id in taken:
			raise ValueError(f"dungeoninfo.txt already has region id {dungeon_id}: {taken[dungeon_id]}")
		if f'"{path}"'.lower() in taken.values():
			raise ValueError(f"dungeoninfo.txt already names {path}")
	if text and not text.endswith("\n"):
		text += "\r\n"
	return (text + "\r\n".join(rows) + "\r\n").encode("cp949")


# ================
# merge_event_zones
# ================
def merge_event_zones(native_raw, vsro_raw):
	"""
	The native eventzonedata.txt (UTF-16) with the VSRO rows of the two temple
	TELEPORT zones appended; the native bytes stay first. The VSRO ids (234,
	235) follow the native table's last, so they are carried as they are; a
	native table that already uses one of them, or already holds a zone of the
	same name, is refused. Returns the native bytes when both zones are already
	present exactly.
	"""
	native = stage1.table_rows(native_raw)
	vsro = {row[EVENT_ZONE_CODE_COLUMN]: row for row in stage1.table_rows(vsro_raw) if len(row) > EVENT_ZONE_CODE_COLUMN}
	missing = [code for code in TEMPLE_ZONE_CODES if code not in vsro]
	if missing:
		raise ValueError(f"VSRO eventzonedata.txt lacks {missing}")
	wanted = [vsro[code] for code in TEMPLE_ZONE_CODES]
	if all(row in native for row in wanted):
		return native_raw
	taken_ids = {row[EVENT_ZONE_ID_COLUMN] for row in native}
	taken_codes = {row[EVENT_ZONE_CODE_COLUMN] for row in native}
	for row in wanted:
		if row[EVENT_ZONE_ID_COLUMN] in taken_ids or row[EVENT_ZONE_CODE_COLUMN] in taken_codes:
			raise ValueError(f"eventzonedata.txt already has zone {row[EVENT_ZONE_ID_COLUMN]} {row[EVENT_ZONE_CODE_COLUMN]}")
	text = stage1.decode_table(native_raw)
	if not text.endswith("\n"):
		text += "\r\n"
	added = "\r\n".join("\t".join(row) for row in wanted) + "\r\n"
	return b"\xff\xfe" + (text.lstrip("﻿") + added).encode("utf-16-le")


# ================
# add_temple
# ================
def add_temple(vsro_root, native, out):
	"""
	Port-only, not native: the Pharaoh Temple's dungeon rows and walk-in zones
	(the tables above), while SRO_PHARAOH_TEMPLE is on. Appends the seven
	dungeoninfo.txt rows and the two TELEPORT event zones to the native tables
	and checks the seven .dof files the staging step carried from Data.pk2 are
	there. Off, the two tables are the native bytes: a variant an earlier on
	build left rows in is put back, and one that already matches is not
	touched. Returns the summary.
	"""
	import build_alexandria_variant as variant

	enabled = temple_enabled()
	dungeon_info = Path("Data_extracted") / "dungeon" / "dungeoninfo.txt"
	event_zones = Path("Media_extracted") / variant.TEXTDATA / "eventzonedata.txt"
	native_info = (native / dungeon_info).read_bytes()
	native_zones = (native / event_zones).read_bytes()
	if enabled:
		wanted_info = merge_dungeon_info(native_info)
		wanted_zones = merge_event_zones(
			native_zones, variant.read_vsro_file(vsro_root, "Media", variant.TEXTDATA + "eventzonedata.txt")
		)
	else:
		wanted_info, wanted_zones = native_info, native_zones
	for relative, content in ((dungeon_info, wanted_info), (event_zones, wanted_zones)):
		target = out / relative
		if not target.is_file() or target.read_bytes() != content:
			variant.replace_file(target, content)
	if not enabled:
		return {"enabled": False}
	staged = variant.lower_paths(out / "Data_extracted" / "dungeon")
	missing = [path for path in map(dof_path, DUNGEONS) if path.split("/", 1)[1] not in staged]
	if missing:
		raise SystemExit(f"the Pharaoh Temple's dungeon files are missing from the variant: {missing}")
	return {
		"enabled": True,
		"dungeons": [dungeon_id for dungeon_id, _, _ in DUNGEONS],
		"regions": dungeon_regions(),
		"gates": list(GATE_CODES),
		"eventZones": list(TEMPLE_ZONE_CODES)
	}

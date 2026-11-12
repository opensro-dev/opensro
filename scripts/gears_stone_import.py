"""
===========================================================================

gears_stone_import.py - the later client's gears magic stones (port-only).

Port-only, not native. Off unless SRO_MALL_CATEGORIES is "1", "on" or
"true" (the same switch the server's mall layout reads; an unset or
off build is byte-identical to today).

The later mall's Premium > Gears tab sells sixteen kinds of leveled
magic stone, ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_<kind>_<degree> at
degrees 1-7 (112 rows). v1.150 has no such rows, but the stones need
nothing new to work: each is a native magic stone (type 3/3/11/1)
whose Descriptions[0] names a shipped magicoption (MATTR_STR and its
kin, degrees 1-12 on the shipped table) at its degree, so the native
alchemy stone path (alchemy Stone) admits them unchanged once the
rows stand in itemdata. This import stages exactly those rows from
the pinned v1.696 client (SRO_V1696_ROOT, v1696_archive_pins.json),
byte for byte except their identities: the later client's numeric
ids are its own, so each row takes a fresh identity above every
published row, remembered in the marker so a rebuild never moves an
item. The server authors the mall listing itself
(commerce/gearsStoneGoods, Imported): a build without this import
lists none of the stones and boots the rest.

A switch off removes exactly what the marker records: the shard, its
manifest line and the marker itself. The client's texts for the rows
(the SN_/SD_ symbols) ride the later client's own text tables through
the variant's text merges; a row whose name text is absent still
imports (the avatar import's posture) and is reported in the marker.

===========================================================================
"""
import json
import mmap
import os
from pathlib import Path

import cap110_monsters as cap110
import import_vsro_alexandria as tables
import jupiter_import as jupiter
from prepare_client_resources import safe_target, write_atomically

FLAG = "SRO_MALL_CATEGORIES"
ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-gears-stones.json"
ITEM_SHARD = "itemdata_190000.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA

# The sixteen option kinds and their degree span, authored in one
# place (commerce.GearsStoneKinds mirrors this list for the listing).
KINDS = (
	"STR", "INT", "HP", "MP", "HR", "DUR", "ER", "SOLID",
	"BURN", "POISON", "FROSTBITE", "ZOMBIE", "ESHOCK",
	"EVADE_BLOCK", "EVADE_CRITICAL", "LUCK",
)
DEGREES = tuple(range(1, 8))
PREFIX = "ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_"
ITEM_COLUMNS = 160

# The rows' stone family: the two magic-stone types the native
# alchemy path admits (3/3/11/1 and 3/3/11/7; the solid kind rides 7).
STONE_TYPES = (["3", "3", "11", "1"], ["3", "3", "11", "7"])


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_MALL_CATEGORIES names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# stone_codenames
#
# The 112 admitted codenames, in kind then degree order.
# ================
def stone_codenames():
	return [f"{PREFIX}{kind}_{degree:02d}" for kind in KINDS for degree in DEGREES]


# ================
# select_stones
#
# The admitted rows of the later item tables: exactly the 112 gears
# stones, each a magic-stone row naming its kind's option at its
# degree. A missing, miscast or mislabelled row refuses: a partial
# import would list stones the game cannot use.
# ================
def select_stones(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > 2 and row[0] == "1":
			by_code.setdefault(row[2], row)
	selected = []
	for code in stone_codenames():
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no gears stone {code}")
		if len(row) != ITEM_COLUMNS or row[9:13] not in STONE_TYPES:
			raise ValueError(f"gears stone {code} is not a magic-stone row")
		kind, _, degree = code[len(PREFIX):].rpartition("_")
		if int(row[118]) != int(degree):
			raise ValueError(f"gears stone {code} names degree {row[118]}, want {degree}")
		if not row[119].startswith("MATTR_"):
			raise ValueError(f"gears stone {code} names no magic option ({row[119]!r})")
		selected.append(row)
	return selected


# ================
# add_stones
#
# The item pass: stage the 112 rows in their shard under marker-kept
# identities, or (switch off, or no later client) reap a previous
# import. The variant-side entry the Alexandria builder calls.
# ================
def add_stones(native, output, v1696_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("gears stone overlay must be separate from native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(output / TEXT_ROOT, ITEM_SHARD)
	manifest = safe_target(output / TEXT_ROOT, "itemdata.txt")
	if not flag_enabled():
		if shard.exists():
			shard.unlink()
		if manifest.exists():
			names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
			kept = [name for name in names if name.lower() != ITEM_SHARD]
			if len(kept) != len(names):
				write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
		marker.unlink(missing_ok=True)
		return {"enabled": False}
	v1696_root = Path(v1696_root or os.environ.get(ROOT_ENV, ""))
	if not str(v1696_root):
		raise SystemExit(f"Pass the v1.696 client folder or set {ROOT_ENV}.")
	jupiter.require_v1696_pin(v1696_root, ("Media",))
	with open(v1696_root / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())

		later_rows = [row for name in tables.table_rows(read("itemdata.txt"))
			for row in tables.table_rows(read(name[0]))]
	stones = select_stones(later_rows)
	# Identities stand above every published row of the merged item
	# tables (the variant's accumulated shards, hunting2's posture),
	# never in the character band other imports reserve.
	native_ids = set()
	stone_codes = {stone[2] for stone in stones}
	for path in sorted(text_root.glob("itemdata*.txt")):
		if path.name == ITEM_SHARD:
			continue
		for row in tables.table_rows(path.read_bytes()):
			if len(row) > 2 and row[0] == "1":
				native_ids.add(int(row[1]))
				if row[2] in stone_codes:
					raise ValueError(f"gears stone {row[2]} already stands in the native tables")
	allocated = cap110.allocate_identities(native_ids, [stone[2] for stone in stones], prior.get("items", {}))
	rows = []
	for stone in stones:
		row = list(stone)
		row[1] = str(allocated[stone[2]])
		rows.append(row)
	rows.sort(key=lambda row: int(row[1]))
	write_atomically(shard, cap110.encode_rows(rows))
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / "itemdata.txt").read_bytes()
	manifest_names = [row[0] for row in tables.table_rows(current) if row]
	manifest_names = [name for name in manifest_names if name.lower() != ITEM_SHARD] + [ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in manifest_names]))
	summary = {"enabled": True, "format": "opensro-gears-stones-v1", "portOnly": True,
		"items": {row[2]: int(row[1]) for row in rows}, "degrees": list(DEGREES),
		"kinds": list(KINDS), "source": "v1696",
		"limits": [
			"Listed in Premium > Gears only while SRO_MALL_CATEGORIES is on (commerce authors the listing).",
			"No drop or event reward is added; the rows arrive by mall purchase or GM grant alone.",
		]}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary

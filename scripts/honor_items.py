"""
===========================================================================

honor_items.py - the honor weapons the client authored and never shipped

Port-only, not native. Off unless SRO_HONOR_GOODS is "1", "on" or
"true"; an unset or off build is byte-identical to one without this
import. Called by the Alexandria builder after every other item import,
so its identities stand above all of theirs (#3548).

The Training Camp honor line is half-finished in the shipped data. The
v1.150 client's resinfo (itemrare.txt, itemoption.txt) authors the
Chinese honor weapons and shields for degrees 8 to 12, grades A to C,
with their own "_honor" seal effects. Its itemdata ships only the
degree 8 and 9 weapons, and the European degree 8 and 9 weapons with no
package and no resinfo row. iSRO-R, the last official server, ships the
same rows. This import finishes the line by the rule every shipped
honor row follows (84 rows, no exception): an honor item is its _RARE
row with

	CanTrade, CanBorrow and CanDrop 0
	required level  the rare's  -2 (A), +2 (B), +6 (C)
	price           a flat price per degree and grade, sell price 40%

It derives, from the rare rows the item tables hold (the cap-110 import
brings degrees 10 and 11):

- every honor weapon and shield of a kind its race authors (the
  resinfo names Chinese shields; Europe authors only weapons) up to
  degree 11, the port's last degree. Degree 11 exists as grade A only:
  no source authors a degree 11 B or C rare to derive from;
- the degree 10 and 11 repair stones (MATTR_REPAIR), copied from the
  degree 9 stone with their degree, and named by the degree 9 stone's
  text with its number changed, as native's degree 10 stones are;
- the resinfo rows the client lacks for these items (all European
  honor items, native ones included): the rare's glow group, and the
  rare's seal effect in its "_honor" variant when the client ships one.

Inference (no source prices degrees 10 and 11): the price tables
continue the native step from degree 8 to 9 linearly. That keeps an
honor weapon's gold price beside its rare's, as native's are.

The shops sell these rows (honor_shops.py). A switch off removes exactly
what the marker records: the shard, its manifest line, the texts and the
resinfo rows it added, and the marker.

===========================================================================
"""
import json
from pathlib import Path
import re

import alexandria_npcs as text_tables
import cap110_item_import as shared
import cap110_items as items
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import nasrun_import as nasrun
import sro_pk2
from build_alexandria_variant import build_switch
from prepare_client_resources import safe_target, write_atomically

FLAG = "SRO_HONOR_GOODS"
MARKER = ".opensro-honor-goods.json"
ITEM_SHARD = "itemdata_190002.txt"
TEXT_ROOT = shared.TEXT_ROOT
GLOW_ROOT = shared.GLOW_ROOT
NAME_TABLE = "textdataname.txt"
MANIFEST = "itemdata.txt"
PARTICLES = "Particles_extracted"

HONOR_SUFFIX = "_HONOR"
HONOR_CODE = re.compile(r"^ITEM_(CH|EU)_([A-Z]+)_(\d\d)_([ABC])_RARE_HONOR$")
DEGREES = (8, 9, 10, 11)
GRADES = ("A", "B", "C")
LEVEL_OFFSET = {"A": -2, "B": 2, "C": 6}
# itemdata cells the honor rule sets (VSRO _RefObjCommon order).
CAN_TRADE, CAN_BORROW, CAN_DROP = 16, 19, 20
PRICE, SELL_PRICE = 26, 31
SELL_SHARE = (2, 5)
DURABILITY = 63

# The shipped package prices (refpricepolicyofitem: gold, then honor).
HONOR_GOLD = {8: {"A": 2700000, "B": 3600000, "C": 4600000}, 9: {"A": 5800000, "B": 7600000, "C": 9500000}}
HONOR_POINTS = {8: {"A": 1000, "B": 1500, "C": 2500}, 9: {"A": 4000, "B": 6000, "C": 8000}}
STONE_GOLD = {8: 30000, 9: 50000}
STONE_POINTS = {8: 10, 9: 15}

STONE_PREFIX = "ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_"
STONE_SOURCE_DEGREE = 9
# The stone's degree: its class cell and its first parameter.
STONE_DEGREE_COLUMNS = (items.CLASS, items.PARAM_FIRST)
DEGREE_NUMBER = re.compile(r"(?<!\d)9(?!\d)")
EFFECT = ".efp"
HONOR_EFFECT = "_honor.efp"


# ================
# enabled
# ================
def enabled(environ=None):
	return build_switch(FLAG, environ)


# ================
# continued
#
# A native price table's value at degree, the native 8-to-9 step
# continued linearly past degree 9 (the inference in the banner).
# ================
def continued(table, degree):
	if degree in table:
		return table[degree]
	return table[9] + (table[9] - table[8]) * (degree - 9)


# ================
# honor_gold / honor_points / stone_gold / stone_points
# ================
def honor_gold(degree, grade):
	return continued({d: prices[grade] for d, prices in HONOR_GOLD.items()}, degree)


def honor_points(degree, grade):
	return continued({d: prices[grade] for d, prices in HONOR_POINTS.items()}, degree)


def stone_gold(degree):
	return continued(STONE_GOLD, degree)


def stone_points(degree):
	return continued(STONE_POINTS, degree)


# ================
# honor_code / stone_code
# ================
def honor_code(race, kind, degree, grade):
	return f"ITEM_{race}_{kind}_{degree:02d}_{grade}_RARE_HONOR"


def stone_code(degree):
	return f"{STONE_PREFIX}{degree:02d}"


# ================
# authored_kinds
#
# Each race's honor kinds: those any honor codename names, in the item
# tables or the client's resinfo.
# ================
def authored_kinds(codes):
	kinds = {}
	for code in codes:
		match = HONOR_CODE.match(code)
		if match:
			kinds.setdefault(match[1], set()).add(match[2])
	return {race: sorted(found) for race, found in kinds.items()}


# ================
# honor_row
#
# The honor item of a rare row, by the shipped rule.
# ================
def honor_row(rare, identity, degree, grade):
	row = list(rare)
	gold = honor_gold(degree, grade)
	row[items.IDENTITY] = str(identity)
	row[items.CODENAME] = rare[items.CODENAME] + HONOR_SUFFIX
	row[items.PLACEHOLDER_NAME] = items.UNUSED
	for column in (CAN_TRADE, CAN_BORROW, CAN_DROP):
		row[column] = "0"
	row[items.REQUIRED_LEVEL] = str(int(rare[items.REQUIRED_LEVEL]) + LEVEL_OFFSET[grade])
	row[PRICE] = str(gold)
	row[SELL_PRICE] = str(gold * SELL_SHARE[0] // SELL_SHARE[1])
	return row


# ================
# stone_row
# ================
def stone_row(source, identity, degree):
	row = list(source)
	row[items.IDENTITY] = str(identity)
	row[items.CODENAME] = stone_code(degree)
	row[items.NAME_KEY] = "SN_" + stone_code(degree)
	row[items.PLACEHOLDER_NAME] = items.UNUSED
	for column in STONE_DEGREE_COLUMNS:
		row[column] = str(degree)
	return row


# ================
# stone_text
#
# The degree 9 stone's name row with its number changed, in every
# language column (native's degree 10 stones read the same way).
# ================
def stone_text(source, degree):
	return [source[0], "SN_" + stone_code(degree)] + [DEGREE_NUMBER.sub(str(degree), cell) for cell in source[2:]]


# ================
# plan_rows
#
# The rows to derive: (codename, builder taking an identity), in a fixed
# order. rows maps every published codename to its row.
# ================
def plan_rows(rows, kinds):
	plan = []
	for race in sorted(kinds):
		for kind in kinds[race]:
			for degree in DEGREES:
				for grade in GRADES:
					code = honor_code(race, kind, degree, grade)
					rare = rows.get(code[:-len(HONOR_SUFFIX)])
					if code in rows or rare is None:
						continue
					plan.append((code, lambda identity, rare=rare, degree=degree, grade=grade: honor_row(rare, identity, degree, grade)))
	source = rows.get(stone_code(STONE_SOURCE_DEGREE))
	for degree in DEGREES:
		if degree > STONE_SOURCE_DEGREE and source is not None and stone_code(degree) not in rows:
			plan.append((stone_code(degree), lambda identity, degree=degree: stone_row(source, identity, degree)))
	return plan


# ================
# resinfo_lines
#
# A resinfo table as (codename, cells) lines, comments and blanks kept
# as (None, text).
# ================
def resinfo_lines(text):
	lines = []
	for line in text.splitlines():
		cells = line.split()
		lines.append((cells[0], cells) if cells and not cells[0].startswith("//") else (None, line))
	return lines


# ================
# resinfo_rows
#
# The glow and seal rows the client lacks for honor codes: the rare's
# glow group, and the rare's seal lines with each effect in its _honor
# variant when effects holds it (else the rare's own effect). Returns
# the two tables' added lines and the codes they cover.
# ================
def resinfo_rows(glow_text, rare_text, codes, effects):
	glow = {code: cells for code, cells in resinfo_lines(glow_text) if code}
	seals = {}
	for code, cells in resinfo_lines(rare_text):
		if code:
			seals.setdefault(code, []).append(cells)
	glow_added, seal_added = [], []
	for code in sorted(codes):
		rare = code[:-len(HONOR_SUFFIX)]
		if code not in glow and rare in glow:
			glow_added.append([code] + glow[rare][1:])
		if code not in seals:
			for cells in seals.get(rare, []):
				effect = cells[1][:-len(EFFECT)] + HONOR_EFFECT if cells[1].lower().endswith(EFFECT) else cells[1]
				seal_added.append([code, effect if effect.lower() in effects else cells[1]] + cells[2:])
	return glow_added, seal_added


# ================
# append_lines / strip_lines
#
# The resinfo tables are latin-1 text with CRLF lines.
# ================
def append_lines(text, rows):
	if not rows:
		return text
	tail = "" if text.endswith("\n") or not text else "\r\n"
	return text + tail + "".join("\t".join(row) + "\r\n" for row in rows)


def strip_lines(text, codes):
	if not codes:
		return text
	return "".join(line for line in text.splitlines(keepends=True) if not (line.split() and line.split()[0] in codes))


# ================
# effect_names
#
# Every effect program the client can load, as itemrare names them
# (folder\name.efp, lower case).
# ================
def effect_names(*roots):
	names = set()
	for root in roots:
		for path in Path(root).rglob("*.efp"):
			names.add(str(path.relative_to(root)).replace("/", "\\").lower())
	return names


# ================
# resinfo_paths
# ================
def resinfo_paths(native, output):
	paths = {}
	for name in (shared.GLOW_TABLE, shared.RARE_TABLE):
		target = safe_target(output / GLOW_ROOT, name)
		paths[name] = (target, target if target.exists() else native / GLOW_ROOT / name)
	return paths


# ================
# remove_import
#
# The off path: the shard and its manifest line, the texts and resinfo
# rows the marker records (a resinfo table it created leaves whole), and
# the marker.
# ================
def remove_import(native, output, prior, marker, shard, manifest):
	nasrun.strip_texts(safe_target(output / TEXT_ROOT, NAME_TABLE), set(prior.get("texts", [])))
	added, created = prior.get("resinfo", {}), set(prior.get("createdResinfo", []))
	for name, (target, source) in resinfo_paths(native, output).items():
		text = source.read_bytes().decode("latin1")
		stripped = strip_lines(text, set(added.get(name, [])))
		# A table this import created goes when it holds only native rows.
		if name in created and target.exists() and stripped.encode("latin1") == (native / GLOW_ROOT / name).read_bytes():
			target.unlink()
			if not any(target.parent.iterdir()):
				target.parent.rmdir()
		elif stripped != text:
			write_atomically(target, stripped.encode("latin1"))
	if shard.exists():
		shard.unlink()
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != ITEM_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	marker.unlink(missing_ok=True)


# ================
# add_honor_items
#
# The item pass the Alexandria builder runs after every other item
# import: stage the derived rows in their shard under marker-kept
# identities, their texts and resinfo rows, or (switch off) reap a
# previous import.
# ================
def add_honor_items(native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("honor goods output must be separate from the native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	shard = safe_target(text_root, ITEM_SHARD)
	manifest = safe_target(text_root, MANIFEST)
	# Tables this import created stay recorded as created across reruns.
	created = sorted(set(prior.get("createdResinfo", [])) | {name for name, (target, _) in resinfo_paths(native, output).items() if not target.exists()})
	remove_import(native, output, prior, marker, shard, manifest)
	if not enabled():
		return {"enabled": False}
	rows, occupied = {}, set()
	for path in sorted(text_root.glob("itemdata*.txt")):
		if sro_pk2.fold_ascii(path.name) == MANIFEST:
			continue
		for row in tables.table_rows(path.read_bytes()):
			if len(row) > items.CODENAME and row[items.SERVICE] == "1":
				rows[row[items.CODENAME]] = row
				occupied.add(int(row[items.IDENTITY]))
	for path in text_root.glob("characterdata_*.txt"):
		occupied |= {int(row[1]) for row in tables.table_rows(path.read_bytes()) if len(row) > 1 and row[1].isdigit()}
	paths = resinfo_paths(native, output)
	glow_text = paths[shared.GLOW_TABLE][1].read_bytes().decode("latin1")
	rare_text = paths[shared.RARE_TABLE][1].read_bytes().decode("latin1")
	resinfo_codes = {code for code, _ in resinfo_lines(rare_text) + resinfo_lines(glow_text) if code}
	plan = plan_rows(rows, authored_kinds(set(rows) | resinfo_codes))
	allocated = cap110.allocate_identities(occupied, [code for code, _ in plan], prior.get("items", {}))
	staged = sorted((build(allocated[code]) for code, build in plan), key=lambda row: int(row[items.IDENTITY]))
	# Names: honor rows reuse their rare's symbols; the new stones get
	# their degree 9 stone's row with its number changed.
	name_path = safe_target(text_root, NAME_TABLE)
	base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
	name_rows = {row[1]: row for row in tables.table_rows(base_names) if len(row) > 1}
	source_text = name_rows.get("SN_" + stone_code(STONE_SOURCE_DEGREE))
	texts = [stone_text(source_text, int(row[items.CLASS])) for row in staged
		if row[items.CODENAME].startswith(STONE_PREFIX) and source_text and row[items.NAME_KEY] not in name_rows]
	honor_codes = {code for code in rows if HONOR_CODE.match(code)} | {row[items.CODENAME] for row in staged if HONOR_CODE.match(row[items.CODENAME])}
	effects = effect_names(native / PARTICLES, output / PARTICLES)
	glow_added, seal_added = resinfo_rows(glow_text, rare_text, honor_codes, effects)
	write_atomically(shard, cap110.encode_rows(staged))
	if texts:
		write_atomically(name_path, text_tables.append_rows(base_names, texts))
	write_atomically(paths[shared.GLOW_TABLE][0], append_lines(glow_text, glow_added).encode("latin1"))
	write_atomically(paths[shared.RARE_TABLE][0], append_lines(rare_text, seal_added).encode("latin1"))
	current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / MANIFEST).read_bytes()
	names = [row[0] for row in tables.table_rows(current) if row] + [ITEM_SHARD]
	write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {
		"enabled": True, "format": "opensro-honor-goods-v1", "portOnly": True,
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in staged},
		"texts": sorted(row[1] for row in texts),
		"resinfo": {shared.GLOW_TABLE: sorted({row[0] for row in glow_added}), shared.RARE_TABLE: sorted({row[0] for row in seal_added})},
		"createdResinfo": created,
		"limits": [
			"Degree 11 honor items exist as grade A only: no source authors a degree 11 B or C rare.",
			"Degree 12 is outside the port's degrees, though the client's resinfo names it.",
			"Degree 10 and 11 prices continue the native 8-to-9 step linearly (an inference).",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary

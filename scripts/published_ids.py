"""
===========================================================================

published_ids.py - the ids a release published stay with their codes

A character's bag stores an item's reference id, its skill list stores
skill ids, its pets name their COS characters by id. The variant build
numbers every imported row afresh (build_alexandria_variant.clear_content
removes the importers' markers with the tree, #3815), and each importer
numbers above what it finds in the tree being built. Fresh numbering is
stable only while every flag and source stays the same: a flag that adds
rows to an early importer (SRO_ROC's set armour) moves every later
importer's ids, and an id a player holds would name another item.

This module owns the one rule every allocator follows, the rule the
character allocators already kept for monsters (#3850):

	a published id stays with its code;
	a published id whose code this import does not stage stays reserved,
	unless a row already in the tree holds it;
	a new code takes an id above every reserved one.

"Published" is what a release shipped: data/published-identities.json,
codename -> id per map, written by pin_published_identities.py from the
catalogue live serves. It is checked in, so a release built in a fresh
staging keeps the ids the last one shipped, and a flag turned off keeps
its ids reserved until it comes back.

Items and characters share one id space (_RefObjCommon: itemdata,
characterdata and teleportbuilding rows), so an object allocator reserves
both maps; skills are their own space.

===========================================================================
"""
from __future__ import annotations

import json
import os
from pathlib import Path

import import_vsro_alexandria as tables

# The pin a release refreshes (pin_published_identities.py).
PIN = Path(__file__).resolve().parent / "data" / "published-identities.json"
# Names another pin file; empty names none. The Python tests set it empty
# (test/python/script_paths.py): a fixture tree holds none of the published
# rows, so the real pin would reserve all of them and move every fixture id.
PIN_VARIABLE = "SRO_PUBLISHED_IDENTITIES"
PIN_FORMAT = "opensro-published-identities-v1"

# The pin's maps, by id space.
OBJECTS = ("characters", "items")
# The tables of the object id space (_RefObjCommon), by file name prefix.
# itemdata.txt and characterdata.txt are manifests and hold no row ids.
OBJECT_TABLES = ("itemdata_", "characterdata_", "teleportbuilding")
SKILLS = ("skills",)
MAPS = OBJECTS + SKILLS

# The wire width of a reference id (cap110_skills.MAX_ID, the same bound).
MAX_ID = 0xFFFFFFFF


# ================
# read_pin
#
# The pin document at path: {"release", "characters", "items", "skills"}.
# A missing map reads as empty; anything else malformed refuses, because a
# pin read wrong would renumber what players hold.
# ================
def read_pin(path):
	document = json.loads(Path(path).read_text(encoding="utf-8"))
	if not isinstance(document, dict) or document.get("format") != PIN_FORMAT:
		raise ValueError(f"{path} is not a {PIN_FORMAT} pin")
	pin = {"release": str(document.get("release") or "")}
	for name in MAPS:
		rows = document.get(name, {})
		if not isinstance(rows, dict) or any(type(v) is not int or not 0 < v <= MAX_ID for v in rows.values()):
			raise ValueError(f"{path}: {name} is not a codename -> id map")
		pin[name] = rows
	return pin


# ================
# pin_path
#
# The pin this process reads: PIN, the file PIN_VARIABLE names, or none.
# ================
def pin_path():
	named = os.environ.get(PIN_VARIABLE)
	if named is None:
		return PIN
	return Path(named) if named else None


# ================
# published
#
# codename -> id over the given maps of the pin (OBJECTS or SKILLS). A code
# named under two ids refuses. r202 shipped five ids under two codes at
# once (the Arabia monsters 50327-50331 and five pet scrolls), and the
# client keeps one object per id (#3994). An id pinned under an item and a
# character stays with the item, which a player can hold; the character is
# left out of the pin, so its importer numbers it afresh above every
# released id. Any other id pinned twice refuses.
# ================
def published(maps):
	path = pin_path()
	if path is None or not path.exists():
		return {}
	pin = read_pin(path)
	merged, holders = {}, {}
	for name in maps:
		for code, identity in pin[name].items():
			if merged.get(code, identity) != identity:
				raise ValueError(f"published identity {code} is pinned at two ids across {', '.join(maps)}")
			merged[code] = identity
			holders.setdefault(identity, []).append((name, code))
	for identity, named in holders.items():
		if len(named) < 2:
			continue
		if sorted(name for name, _ in named) != ["characters", "items"]:
			raise ValueError(f"published identity {identity} is pinned under {', '.join(code for _, code in named)}")
		for name, code in named:
			if name == "characters":
				del merged[code]
	return merged


# ================
# occupied_objects
#
# Every object id the tree at text_root already holds: the rows of each
# itemdata, characterdata and teleportbuilding table (_RefObjCommon is one
# id space and the client keeps one object per id). own names the shards
# the asking importer rewrites whole, which it leaves out because its
# marker retains their ids. Every object allocator asks this one owner: an
# importer that read its own table alone handed the gears stones the fox
# pets' ids, and a pet spawn read as a stone disconnected players (#3994).
# ================
def occupied_objects(text_root, own=()):
	occupied = set()
	for _, row in object_rows(text_root, own):
		occupied.add(int(row[1]))
	return occupied


# ================
# taken_objects
#
# The object ids a new object must not take: occupied_objects, and every
# released id (the pin), whose rows a later step of the build may add. A
# gate import that numbered its buildings past its own table alone put a
# Sky Temple gate on a fox pet's id, and the gate buildings sat on the
# released Halloween avatars' ids (#4004). A gate import that rewrites its
# buildings passes "teleportbuilding.txt" in own and adds its own set.
# ================
def taken_objects(text_root, own=()):
	return occupied_objects(text_root, own) | set(published(OBJECTS).values())


# ================
# shared_objects
#
# id -> codes for every object id two rows of the tree at text_root hold.
# The variant build refuses a tree that has any (#3994).
# ================
def shared_objects(text_root):
	holders = {}
	for _, row in object_rows(text_root):
		if len(row) > 2:
			holders.setdefault(int(row[1]), set()).add(row[2])
	return {identity: sorted(codes) for identity, codes in holders.items() if len(codes) > 1}


# ================
# object_rows
#
# (file name, row) for every id row of the object tables under text_root,
# less the shards own names.
# ================
def object_rows(text_root, own=()):
	text_root = Path(text_root)
	if not text_root.is_dir():
		return
	skipped = {name.lower() for name in own}
	for path in sorted(text_root.iterdir()):
		name = path.name.lower()
		if name in skipped or not name.startswith(OBJECT_TABLES) or not name.endswith(".txt"):
			continue
		for row in tables.table_rows(path.read_bytes()):
			if len(row) > 1 and row[1].isdigit():
				yield name, row


# ================
# reserved
#
# The released ids of every pinned code not in codes, less those a row in
# the tree (occupied) already holds: that row is the code's own, and
# reserving it again would refuse its own id.
# ================
def reserved(pinned, codes, occupied):
	staged = set(codes)
	return {identity for code, identity in pinned.items() if code not in staged} - set(occupied)


# ================
# kept
#
# codename -> released id for the codes that have one.
# ================
def kept(pinned, codes):
	return {code: pinned[code] for code in codes if code in pinned}


# ================
# overlay
#
# The module rule for one allocator: the ids to add to its taken set
# (every released id of a code it does not stage, unless a row in the tree
# holds it) and its previous map with the pin laid over it (a staged code
# keeps its released id; a previous id the pin gives another code is
# dropped, so that code is numbered anew above the pin). previous and the
# result are codename -> id. allocate uses it; an allocator with its own
# numbering loop (the equipment, the skill tier, the mall avatars, the
# pets) calls it directly.
# ================
def overlay(pinned, codes, occupied, previous):
	own = kept(pinned, codes)
	held = reserved(pinned, codes, occupied) - set(own.values())
	owned = held | set(own.values())
	merged = {code: identity for code, identity in previous.items() if code not in own and identity not in owned}
	merged.update(own)
	return held, merged


# ================
# allocate_identities
#
# codename -> id for codes: each code in previous keeps its id, every
# other one takes the next id above everything occupied or retained.
# Source order may insert a new code before an existing published row,
# which is why every retained id is reserved before any is handed out.
# ================
def allocate_identities(occupied, codes, previous):
	occupied = set(occupied)
	values = list(previous.values())
	if any(type(value) is not int or not 0 < value <= MAX_ID for value in values):
		raise ValueError("invalid published identity")
	retained = set(values)
	if len(retained) != len(values) or retained & occupied:
		raise ValueError("published identities collide")
	if len(codes) != len(set(codes)):
		raise ValueError("duplicate codename")
	occupied |= retained
	next_id = max(occupied | {0}) + 1
	identities = {}
	for code in codes:
		if code in previous:
			identities[code] = previous[code]
			continue
		if next_id > MAX_ID:
			raise ValueError("identity space exhausted")
		identities[code] = next_id
		next_id += 1
	return identities


# ================
# allocate
#
# allocate_identities under the pin of one id space (OBJECTS or SKILLS).
# pinned overrides the pin file (tests, and callers that read it once).
# ================
def allocate(space, occupied, codes, previous, pinned=None):
	pinned = published(space) if pinned is None else pinned
	occupied = set(occupied)
	held, merged = overlay(pinned, codes, occupied, previous)
	return allocate_identities(occupied | held, codes, merged)

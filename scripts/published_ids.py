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

# The pin a release refreshes (pin_published_identities.py).
PIN = Path(__file__).resolve().parent / "data" / "published-identities.json"
# Names another pin file; empty names none. The Python tests set it empty
# (test/python/script_paths.py): a fixture tree holds none of the published
# rows, so the real pin would reserve all of them and move every fixture id.
PIN_VARIABLE = "SRO_PUBLISHED_IDENTITIES"
PIN_FORMAT = "opensro-published-identities-v1"

# The pin's maps, by id space.
OBJECTS = ("characters", "items")
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
# named under two ids refuses. An id two maps give two codes is kept for
# both: r202 shipped five (the Arabia monsters 50327-50331 and five pet
# scrolls), and the pin reproduces what live holds rather than choose
# which one to move; shared() names them.
# ================
def published(maps):
	path = pin_path()
	if path is None or not path.exists():
		return {}
	pin = read_pin(path)
	merged = {}
	for name in maps:
		for code, identity in pin[name].items():
			if merged.get(code, identity) != identity:
				raise ValueError(f"published identity {code} is pinned at two ids across {', '.join(maps)}")
			merged[code] = identity
	return merged


# ================
# shared
#
# The ids pinned under more than one code: the live collisions published
# keeps. A staged code keeps such an id although a row of the other code
# already stands in the tree.
# ================
def shared(pinned):
	seen, twice = set(), set()
	for identity in pinned.values():
		(twice if identity in seen else seen).add(identity)
	return twice


# ================
# reserved
#
# The released ids of every pinned code not in codes, less those a row in
# the tree (occupied) already holds: that row is the code's own or a live
# collision, and reserving it again would refuse a build live shipped.
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
	# A live collision (shared) is never held against the code that keeps it.
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
	own = kept(pinned, codes)
	# A live collision the other code's row already holds is kept, as live
	# ships it; any other kept id a row holds still refuses as a collision.
	occupied -= shared(pinned) & set(own.values())
	return allocate_identities(occupied | held, codes, merged)

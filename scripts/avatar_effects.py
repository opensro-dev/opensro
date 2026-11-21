"""
===========================================================================

avatar_effects.py - the later client's worn auras of the imported mall avatars

Port-only, not native (behind SRO_MALL_AVATARS, as the avatars themselves).
The v1.696 client keeps most of its mall avatars' particles outside their
BSRs, in skilleffectset rows named after the avatar: Skeleton Knight's
body, forearm, calf, head and eye programs, BlueSoul Knight's nine. The
v1.150 client has no such rows and draws nothing for a worn avatar outside
its BSR. This module picks each imported avatar's steady loops (ACT_L /
AT_LOOP) out of those rows; mall_avatar_import.py copies their programs
and lists them in its marker, and the equipment-particle build publishes
them (equipmentParticles.mjs avatarWornEffects).

===========================================================================
"""
import collections
from pathlib import Path
import re

import cap110_item_import as shared
import cap110_items as items

SERVICE_ON = "1"
# The later client's worn-avatar particles (skilleffectset rows keyed by the
# avatar's name): column layout of one 30-cell row.
EFFECT_TABLE = "skilleffectset.txt"
EFFECT_COLUMNS = 30
EFFECT_SERVICE, EFFECT_KEY, EFFECT_PHASE, EFFECT_ATTACH = 0, 3, 4, 15
EFFECT_FOLDER, EFFECT_FILE, EFFECT_BONE, EFFECT_OFFSET, EFFECT_SCALE = 19, 20, 21, 22, 27
# Only the steady aura is imported: the effect the wearer keeps while the
# avatar is worn. Activation one-shots (ACT_S, DEACT, AT_ONE_FOLLOW) belong
# to the Devil's Spirit skills, which do not go through this import.
LOOP_PHASE, LOOP_ATTACH = "ACT_L", "AT_LOOP"
NO_OFFSET = "0,0,0"
NO_VALUE = "none"
EFFECT_SCALE_TOKEN = "SCT_EFFECT_SCALE"
# '#Bone': a bone with an offset in its own frame (loop_effect). '@' and '*'
# change the binding in ways a worn aura has no use for, and are refused.
BONE_POSITION_MARKER = "#"
BONE_MARKERS = ("#", "@", "*")
EFFECT_KEY_PREFIX = re.compile(r"^ITEM_(MALL_|EVENT_)?")


# ================
# effect_key
#
# The name a later skilleffectset row gives an avatar: its codename without
# the ITEM_ / ITEM_MALL_ / ITEM_EVENT_ prefix (ITEM_MALL_AVATAR_M_BLUE_SOULKNIGHT
# -> AVATAR_M_BLUE_SOULKNIGHT). INFERENCE from the v1.696 data, not from its
# binary: every one of its avatar rows names an item this way, and no item
# row carries the name in a column of its own.
# ================
def effect_key(code):
	return EFFECT_KEY_PREFIX.sub("", code, count=1)


# ================
# effect_keys
#
# Every name a later skilleffectset row may give one avatar, the most
# specific first; avatar_effects takes the first that names rows. Neither
# the v1.696 tables nor its binary hold the link (no item column, no
# string), so these are the spellings its data uses, INFERENCE from it:
# the codename's own (BlueSoul Knight), without the sex (the Skeleton
# Knight armour AVATAR_DARKLKNIGHT, the Fox dress, the Viking hat), with
# the sex moved before _HAT (AVATAR_DARKLKNIGHT_M_HAT), without the "_"
# before a trailing number (PIRATECREW_2 -> PIRATECREW2), and the worn
# model's file name (avatar_m_saytan.bsr for SAYTAN_DRESS).
# ================
def effect_keys(code, model=""):
	key = effect_key(code)
	found = re.fullmatch(r"AVATAR_([MW])_(.+)", key)
	keys = [key]
	if found:
		sex, rest = found.groups()
		keys.append("AVATAR_" + rest)
		if "_HAT" in rest:
			keys.append("AVATAR_" + rest.replace("_HAT", f"_{sex}_HAT", 1))
	keys += [re.sub(r"_(\d+)$", r"\1", name) for name in keys]
	stem = Path(model.replace("\\", "/")).stem.upper()
	if stem.startswith("AVATAR_"):
		keys.append(stem)
	return list(dict.fromkeys(keys))


# ================
# effect_offset
#
# A row's offset cell ("x,y,z") as three floats, or None when unreadable.
# ================
def effect_offset(cell):
	parts = cell.split(",")
	try:
		values = [float(part) for part in parts]
	except ValueError:
		return None
	return values if len(values) == 3 and all(abs(value) <= 1000 for value in values) else None


# ================
# loop_effect
#
# One later skilleffectset row as a worn-avatar particle, or a string
# naming why it is not one. The bone is a wearer bone ("none" is the
# character's origin); the effect lives in the Particles archive.
# ================
def loop_effect(row, has_particle):
	if row[EFFECT_PHASE] != LOOP_PHASE or row[EFFECT_ATTACH] != LOOP_ATTACH:
		return f"not a worn loop ({row[EFFECT_PHASE]} {row[EFFECT_ATTACH]})"
	offset = effect_offset(row[EFFECT_OFFSET])
	if offset is None:
		return f"offset {row[EFFECT_OFFSET]}"
	bone = row[EFFECT_BONE].strip()
	# '#Bone' with an offset: every such row places a pair of eyes
	# (Skeleton Knight, PirateCrew: y = +/-0.3 either side of Bip01 Head),
	# so the offset is read in the bone's own frame, as a BSR particle's
	# is. INFERENCE: the v1.150 parser has no '#' arm to compare with.
	if bone.startswith(BONE_POSITION_MARKER):
		bone = bone[len(BONE_POSITION_MARKER):]
	if bone.startswith(BONE_MARKERS) or not bone:
		return f"bone token {row[EFFECT_BONE]}"
	scale = 1.0
	if row[EFFECT_SCALE] != NO_VALUE:
		token, _, value = row[EFFECT_SCALE].partition(",")
		try:
			scale = float(value)
		except ValueError:
			scale = 0.0
		if token != EFFECT_SCALE_TOKEN or not 0 < scale <= 16:
			return f"scale {row[EFFECT_SCALE]}"
	if row[EFFECT_FILE].strip().lower() == NO_VALUE:
		return "no effect"
	path = shared.normalized_path(row[EFFECT_FOLDER] + row[EFFECT_FILE].strip())
	if not path.endswith(".efp") or not has_particle(path):
		return f"missing effect {path}"
	root = bone.lower() == NO_VALUE
	effect = {"effectPath": path, "bone": "" if root else bone, "root": root, "scale": scale}
	if any(offset):
		effect["offset"] = offset
	return effect


# ================
# avatar_effects
#
# Port-only, not native: the v1.150 client draws no particles for a worn
# avatar outside its BSR, but the later client keeps most of its mall
# avatars' auras in skilleffectset (BlueSoul Knight's nine forearm, calf,
# thigh, spine, eye and neck programs). Returns {codename: [effect]} for
# the imported avatars and the rows refused, each with its reason.
# ================
def avatar_effects(later_rows, avatars, has_particle):
	by_key = collections.defaultdict(list)
	for row in later_rows:
		if len(row) == EFFECT_COLUMNS and row[EFFECT_SERVICE] == SERVICE_ON:
			by_key[row[EFFECT_KEY].strip()].append(row)
	effects, refused = {}, []
	for avatar_row in avatars:
		code = avatar_row[items.CODENAME]
		key = next((key for key in effect_keys(code, avatar_row[items.MODEL]) if key in by_key), None)
		for row in by_key.get(key, []):
			loop_effect_row(row, code, has_particle, effects, refused)
	return effects, refused


# ================
# loop_effect_row
#
# One effect row of avatar code into effects, or into refused with its reason.
# ================
def loop_effect_row(row, code, has_particle, effects, refused):
	effect = loop_effect(row, has_particle)
	if isinstance(effect, str):
		refused.append({"code": code, "effect": row[EFFECT_FILE], "reason": effect})
		return
	effects.setdefault(code, []).append(effect)


# ================
# import_effects
#
# The auras of avatars (the source's own rows, and an earlier source's:
# the VSRO client keeps PirateCrew's in a table this import does not
# read) from later_rows, and their effect programs copied into output,
# native files first (copy_resource_closure). Returns (effects, refused,
# copied resource records).
# ================
def import_effects(later_rows, avatars, native, output, archives, native_particles):
	particle_index = archives["Particles"][1]
	effects, refused = avatar_effects(
		later_rows, avatars, lambda path: path in particle_index or path in native_particles,
	)
	programs = {("Particles", effect["effectPath"]) for rows in effects.values() for effect in rows}
	return effects, refused, shared.copy_resource_closure(programs, native, output, archives)

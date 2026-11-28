"""
===========================================================================

cap110_skill_effects.py - the later clients' skill presentation rows in the native skilleffect.txt

Port-only, not native. The later clients author animation sets
(skillaniset, skillaniset2), effect stages (skilleffectset) and character
profiles (characterInfo) for the skills and kinds the native v1.150 client
never shipped. This module is the one owner of moving those rows into the
variant's skilleffect.txt: every importer that stages later skills or kinds
(the cap-110 skill tier, the field monsters, Arabia, Sky Temple, the
later-world layers, the Devil's Spirit) goes through it.

Two facts a reader needs first:

  - v1.696 ships SkillEffect.txt as a stub ("0"). Its rows moved to the
    Skillaniset.txt, Skilleffectset.txt and characterinfo.txt shard
    indexes, each row two cells (service, id) wider than the native one
    (later_skill_effect_text rebuilds the native layout). VSRO v1.188
    still ships the whole table. Reading only skilleffect.txt left every
    v1.696 kind without an attack animation or effect (#4730).
  - An animation set without its effect stages plays the swing and
    nothing else: the stages are keyed by the same base name, so every
    merge carries both sections and every reap removes both.

Only a name the native table lacks is inserted, inside its own section; a
name native carries keeps the native rows. No engine vocabulary is
invented: a later row naming an unknown animation state is refused here.

===========================================================================
"""
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable

import alexandria_npcs as characters
import import_vsro_alexandria as tables
from prepare_client_resources import write_atomically

SECTION_HEADER = "#section"
ANIMSET_SECTIONS = ("skillaniset", "skillaniset2")
SERVICE_COLUMN = "skillaniset2"
EFFECT_SET_SECTION = "skilleffectset"
CHARACTER_INFO_SECTION = "characterinfo"
SKILL_EFFECT_TABLE = "skilleffect.txt"
# A skilleffectset row's base name (its skill's animation-set base).
EFFECT_SET_KEY = 1

# The v1.696 shard indexes that replace the skilleffect.txt sections, in
# the native table's section order, with the cells the native row keeps
# (both later layouts lead with service and a row id): skillaniset2 keeps
# its own service cell, the other two have none natively.
LATER_SECTIONS = (
	("characterinfo.txt", "characterInfo"),
	("Skillaniset.txt", "skillaniset2"),
	("Skilleffectset.txt", "skilleffectset"),
)
LATER_ID_COLUMNS = 2
SERVICE_OFF = "0"
# The one boolean cell both row kinds share, in the native layout:
# skillaniset2's overlap flag and skilleffectset's damage event. v1.150
# writes TRUE/FALSE, and the animation-set reader treats anything but
# FALSE as true (parseSkillEffect.mjs ANI_COL.overlap). v1.696 writes "0"
# where v1.150 wrote FALSE (947 of the 1516 rows both tables carry; never
# "1"), and five stage rows misspell FALSE as FLASE. Inferred: the later
# loader reads the cell as a number or a TRUE compare, so all three mean
# false; they become the native spelling.
BOOLEAN_COLUMN = 4
LATER_BOOLEANS = {"0": "FALSE", "1": "TRUE", "FLASE": "FALSE"}


# ================
# section_name
#
# The #section line's own name, lowercased, or None for a data row.
# ================
def section_name(line):
	if not line.startswith(SECTION_HEADER):
		return None
	columns = line.split("\t")
	return columns[1].strip().lower() if len(columns) > 1 else ""


# ================
# animset_base
#
# skillaniset2 prepends the loader-consumed Service column; skillaniset does
# not. The base name is the column after that.
# ================
def animset_base(section, columns):
	offset = 1 if section == SERVICE_COLUMN else 0
	if len(columns) <= offset + 1:
		return ""
	return columns[offset + 1].strip()


# ================
# row_key
#
# The base name a row of section belongs to: an animation set's base or
# an effect stage's skill, "" for a row of any other section.
# ================
def row_key(section, columns):
	if section in ANIMSET_SECTIONS:
		return animset_base(section, columns)
	if section == EFFECT_SET_SECTION and len(columns) > EFFECT_SET_KEY:
		return columns[EFFECT_SET_KEY].strip()
	return ""


# ================
# data_lines
#
# Yield (section, line) for every data row: headers, comments and blank
# lines are skipped.
# ================
def data_lines(text):
	section = None
	for line in text.splitlines():
		header = section_name(line)
		if header is not None:
			section = header
			continue
		if line.strip() and not line.startswith("//"):
			yield section, line


# ================
# animset_rows
#
# Yield (section, base, line) for every enabled animation-set row.
# ================
def animset_rows(text):
	for section, line in data_lines(text):
		if section not in ANIMSET_SECTIONS:
			continue
		columns = line.split("\t")
		if section == SERVICE_COLUMN and columns and columns[0].strip() == SERVICE_OFF:
			continue
		base = animset_base(section, columns)
		if base:
			yield section, base, line


# ================
# presentation_rows
#
# Yield (section, base, line) for every enabled animation-set row and
# every effect-stage row: the rows one skill's presentation is made of.
# ================
def presentation_rows(text):
	yield from animset_rows(text)
	for section, line in data_lines(text):
		if section == EFFECT_SET_SECTION:
			key = row_key(section, line.split("\t"))
			if key:
				yield section, key, line


# ================
# native_bases
# ================
def native_bases(text):
	return {base for _section, base, _line in animset_rows(text)}


# ================
# effect_set_keys
# ================
def effect_set_keys(text):
	return {key for section, key, _line in presentation_rows(text) if section == EFFECT_SET_SECTION}


# ================
# remove_presentation
#
# Remove only an import's rows: the enabled animation sets of bases and
# the effect stages of effect_bases. The native table keeps disabled
# placeholder rows (service 0) under names an import may enable, and
# effect stages under bases whose animation set it disables (the Qin-Shi
# tomb rows), so a disabled row and a stage the import did not insert
# stay: off returns the native text byte for byte. Other sections,
# including the Alexandria NPC characterInfo overlay, keep their rows.
# ================
def remove_presentation(text, bases, effect_bases=()):
	owned = {line for _section, base, line in animset_rows(text) if base in bases}
	owned |= {line for section, base, line in presentation_rows(text)
		if section == EFFECT_SET_SECTION and base in effect_bases}
	return "\r\n".join(line for line in text.splitlines() if line not in owned) + "\r\n"


# A skilleffectset row's cells the v1.150 adapter compares, in the native
# layout (skillEffectSetParseRow.ts): the stage's skill, phase, action
# and move (its kind first, then delay and speeds).
STAGE_SKILL, STAGE_PHASE, STAGE_ACTION, STAGE_MOVE = 1, 2, 13, 14
# The move kinds the v1.150 client's stage reader knows
# (skillEffectStage.ts SKILL_EFFECT_MOVE_TYPE_ID_BY_NAME).
CLIENT_MOVE_KINDS = frozenset(("MOV_NONE", "MOV_STRAIGHT", "MOV_UP", "MOV_UPR", "MOV_ROUND", "MOV_HWAN"))
# A stage's phase suffix (SKILL_X_A, SKILL_X_B ...), stripped to match a
# skill family across versions (parseSkillEffect.mjs skillEffectFamilyName).
FAMILY_SUFFIX = re.compile(r"_[A-Z]$")


# ================
# stage_move
#
# A stage row's (family, phase, action) and move kind, read as the
# build's parser reads them.
# ================
def stage_move(cells):
	cell = lambda at: cells[at].strip() if len(cells) > at else ""
	key = (FAMILY_SUFFIX.sub("", cell(STAGE_SKILL)), cell(STAGE_PHASE).upper(), cell(STAGE_ACTION).upper())
	return key, cell(STAGE_MOVE).split(",")[0].strip().upper()


# ================
# move_homologs
#
# The client move kinds the native stages of each (family, phase, action)
# use: the evidence the build's adapter (adaptSkillEffectSetsToClientNative)
# translates a later kind by, admitting a unanimous one only.
# ================
def move_homologs(text):
	found = {}
	for section, _base, line in presentation_rows(text):
		if section == EFFECT_SET_SECTION:
			key, kind = stage_move(line.split("\t"))
			if kind in CLIENT_MOVE_KINDS:
				found.setdefault(key, set()).add(kind)
	return found


# The client's own copy of the table, beside the server textdata in the
# Media tree (the variant links it in from the native extraction): the
# table the build's adapter takes its homologs from (paths.mjs
# clientV150ResinfoRoot). It carries none of the server table's
# MOV_PIERCE stages.
CLIENT_TABLE = Path("resinfo") / SKILL_EFFECT_TABLE
# A stage's start event (its animation phase) and damage flag, in the
# native layout; retail validates phases 0..9 (parseSkillEffect.mjs
# deriveTargetResultCapacity, GlobalEffectManager_ValidateDamageEffectEntry).
STAGE_START_EVENT, STAGE_DAMAGE = 3, 4
DAMAGE_PHASES = range(10)


# ================
# client_effect_text
#
# The client table for a server textdata folder, or fallback when the
# tree has none (a unit fixture).
# ================
def client_effect_text(text_root, fallback):
	parents = Path(text_root).parents
	# text_root is Media_extracted/server_dep/silkroad/textdata.
	path = parents[2] / CLIENT_TABLE if len(parents) > 2 else None
	return tables.decode_table(path.read_bytes()) if path is not None and path.is_file() else fallback


# ================
# damage_phase_faults
#
# The faults in one skill's stages the retail damage census refuses: a
# damage event in a phase outside 0..9, or two in one phase.
# ================
def damage_phase_faults(lines):
	counts, faults = {}, set()
	for line in lines:
		cells = line.split("\t")
		flag = cells[STAGE_DAMAGE].strip().upper() if len(cells) > STAGE_DAMAGE else ""
		if flag not in ("TRUE", "1"):
			continue
		try:
			phase = float(cells[STAGE_START_EVENT].strip() or 0)
		except ValueError:
			phase = 0.0
		if not phase.is_integer() or int(phase) not in DAMAGE_PHASES:
			faults.add(f"damage event phase {cells[STAGE_START_EVENT].strip()}")
			continue
		counts[int(phase)] = counts.get(int(phase), 0) + 1
		if counts[int(phase)] > 1:
			faults.add(f"two damage events in phase {int(phase)}")
	return faults


# ================
# unplayable_bases
#
# The selected bases whose stages the v1.150 client cannot load, by
# reason. Each is a rule the build enforces (a census of every v1.696 and
# VSRO row through the build's record table finds no other):
#
#   - a stage moving by a kind the client does not know and no client
#     homolog translates (v1.696's new MOV_PIERCE bow family; MOV_MOV_UPR,
#     its doubled spelling of MOV_UPR): adaptSkillEffectSetsToClientNative
#     refuses the row;
#   - two damage stages in one phase (the Arabia Geenie, Mustafa and
#     Harrison attacks): retail asserts iDmgCntTemp <= 1 at 0x916c4a
#     (GlobalEffectManager_ValidateDamageEffectEntry -> AssertFailed ->
#     BSLib_DebugAssert, int3), and the build refuses.
#
# A native defect is not copied: the skill keeps no presentation (as under
# the v1.696 stub) rather than stopping its whole import or the build
# (#4730). An animation set naming a motion the client lacks is not a
# fault: the client loads it and drops that token (animationNames, the
# data_ccd620 rule), so it joins as the client would read it.
# ================
def unplayable_bases(selected, client_text):
	homologs = move_homologs(client_text)
	found, stages = {}, {}
	for base, line in selected.get(EFFECT_SET_SECTION, []):
		stages.setdefault(base, []).append(line)
		key, kind = stage_move(line.split("\t"))
		if kind not in CLIENT_MOVE_KINDS and len(homologs.get(key, ())) != 1:
			found.setdefault(base, set()).add(kind or "no move kind")
	for base, lines in stages.items():
		faults = damage_phase_faults(lines)
		if faults:
			found.setdefault(base, set()).update(faults)
	return {base: sorted(reasons) for base, reasons in found.items()}


# The motion names the client's skill animation table holds (data_ccd620,
# 121 rows: skillEffectAnimationRegistry.ts); SkillAnimationSet_ParseRow
# counts a phase only for one of these.
CLIENT_SKILL_STATE = re.compile(
	r"ANI_ATTACK[1-9]|ANI_READY0[1-4]|ANI_WAIT0[1-4]|ANI_SKILL_(?:[1-9][0-9]?|100)|ANI_HAMMER|ANI_HANDLOOF|ANI_TROW")


# ================
# unknown_skill_states
#
# The motion names the selected animation sets use that the client's
# table lacks, by base. The client drops them; an import that must not
# lose a phase (a player skill) refuses on any.
# ================
def unknown_skill_states(selected):
	found = {}
	for section in ANIMSET_SECTIONS:
		for base, line in selected.get(section, []):
			unknown = {name for name in re.findall(r"ANI_[A-Z0-9_]+", line) if not CLIENT_SKILL_STATE.fullmatch(name)}
			if unknown:
				found.setdefault(base, set()).update(unknown)
	return {base: sorted(names) for base, names in found.items()}


# ================
# player_skill_faults
#
# What refuses a player skill import: a stage the client cannot load, or a
# motion it would drop. Unlike a monster's attack, the class would lose a
# skill or a phase of one it was given.
# ================
def player_skill_faults(selected, client_text):
	faults = unplayable_bases(selected, client_text)
	for base, names in unknown_skill_states(selected).items():
		faults[base] = sorted(set(faults.get(base, ())) | set(names))
	return faults


# ================
# select_later_rows
#
# The later animation-set and effect-stage rows of the wanted bases,
# grouped by section. Sorted by base for stable bytes; one base's stages
# keep their authored order (the sort is stable).
# ================
def select_later_rows(later_text, wanted):
	selected = {}
	for section, base, line in presentation_rows(later_text):
		if base in wanted:
			selected.setdefault(section, []).append((base, line))
	for rows in selected.values():
		rows.sort(key=lambda row: row[0])
	return selected


# ================
# merge_skill_effect_text
#
# Insert selected rows at the end of their own section, leaving every native
# byte otherwise intact. A later row whose base the native table already
# presents (an animation set, or stages of its own) is dropped: native wins,
# and a native skill never gains later stages.
# ================
def merge_skill_effect_text(native_text, selected):
	animated = native_bases(native_text)
	staged = animated | effect_set_keys(native_text)
	pending = {
		section: [line for base, line in rows if base not in (staged if section == EFFECT_SET_SECTION else animated)]
		for section, rows in selected.items()
	}
	inserted = 0
	out, section = [], None
	# ================
	# flush
	# ================
	def flush():
		nonlocal inserted
		rows = pending.get(section, [])
		out.extend(rows)
		inserted += len(rows)
	for line in native_text.splitlines():
		header = section_name(line)
		if header is not None:
			flush()
			section = header
		out.append(line)
	flush()
	return "\r\n".join(out) + "\r\n", inserted


#============================================================================
# The files a presentation row names
#============================================================================

# The resource cells of an animation-set row in the native skillaniset2
# layout (parseSkillEffect.mjs ANI_COL, one service cell further on):
# the damage, critical-damage, arrow-trail and arrow-force effects and
# the stationary object.
ANIMSET_RESOURCE_COLUMNS = (14, 19, 20, 21, 23)
# A skilleffectset row's resources (skillEffectSetParseRow.ts): the stage
# object is directory (17) + name (18); 23 is a second object; 26 and 27
# are the begin and end sounds, under the primitive sound root.
STAGE_DIRECTORY, STAGE_NAME, STAGE_SECONDARY = 17, 18, 23
STAGE_SOUND_COLUMNS = (26, 27)
SOUND_ROOT = "prim/snd/"
EMPTY_CELLS = ("", "none", "xxx")


# ================
# resource_path
#
# A resource cell as an archive path: forward slashes, lower case, no
# leading slash; "" for an empty cell.
# ================
def resource_path(value):
	text = (value or "").strip()
	if text.lower() in EMPTY_CELLS:
		return ""
	return text.replace("\\", "/").lstrip("/").lower()


# ================
# resource_seed
#
# The (archive, path) a skill resource lives at: an effect program in
# Particles, a model in Data, a sound under Data's primitive sound root;
# None for no resource.
# ================
def resource_seed(path, sound=False):
	if not path:
		return None
	if sound:
		return ("Data", SOUND_ROOT + path)
	if path.endswith(".efp"):
		return ("Particles", path)
	return ("Data", path)


# ================
# row_resources
#
# The (archive, path) seeds a set of presentation rows name, each row a
# (section, cells) pair in the native layout.
# ================
def row_resources(rows):
	seeds = set()
	for section, cells in rows:
		cell = lambda at: resource_path(cells[at]) if len(cells) > at else ""
		if section == EFFECT_SET_SECTION:
			stage = cell(STAGE_DIRECTORY) + cell(STAGE_NAME) if cell(STAGE_NAME) else ""
			found = [resource_seed(stage), resource_seed(cell(STAGE_SECONDARY))]
			found += [resource_seed(cell(at), sound=True) for at in STAGE_SOUND_COLUMNS]
		elif section == SERVICE_COLUMN:
			found = [resource_seed(cell(at)) for at in ANIMSET_RESOURCE_COLUMNS]
		else:
			found = []
		seeds.update(seed for seed in found if seed)
	return seeds


#============================================================================
# The later client's rows
#============================================================================


# ================
# later_native_row
#
# One later shard row in the native section's layout, or None for a row
# the later client switched off where the native layout has no service
# cell to say so.
# ================
def later_native_row(row, section):
	if section == SERVICE_COLUMN:
		cells = row[:1] + row[LATER_ID_COLUMNS:]
	elif row and row[0].strip() == SERVICE_OFF:
		return None
	else:
		cells = row[LATER_ID_COLUMNS:]
	if section != LATER_SECTIONS[0][1] and len(cells) > BOOLEAN_COLUMN:
		cells[BOOLEAN_COLUMN] = LATER_BOOLEANS.get(cells[BOOLEAN_COLUMN].strip(), cells[BOOLEAN_COLUMN])
	return cells


# ================
# later_skill_effect_text
#
# The later client's skill presentation as native skilleffect.txt text.
# read(name) returns a textdata file's bytes. A client whose skilleffect.txt
# carries its sections (VSRO) is read as it is; one that ships the stub
# (v1.696) is rebuilt from its shard indexes.
# ================
def later_skill_effect_text(read):
	whole = tables.decode_table(read(SKILL_EFFECT_TABLE))
	if any(section_name(line) is not None for line in whole.splitlines()):
		return whole
	lines = []
	for index, section in LATER_SECTIONS:
		lines.append(f"{SECTION_HEADER}\t{section}")
		for row in tables.later_table_rows(read, index):
			cells = later_native_row(row, section)
			if cells is not None:
				lines.append("\t".join(cells))
	return "\r\n".join(lines) + "\r\n"


# ================
# later_characterinfo_rows
#
# The characterinfo rows of a skilleffect text, cut to the native record.
# VSRO's rows are 16 wide (MOB_SD_MONT: sound, height 3.5, ..., then a 0, a
# note and two empty columns) where the native ones are 13; the first
# SKILL_EFFECT_KEPT columns are the same record. A width test of exactly 13
# admitted no row, and every species fell back to the default height, its
# name board at the waist; three importers' copies kept that test (#4730).
# ================
def later_characterinfo_rows(effect_text):
	rows = []
	for section, line in data_lines(effect_text):
		columns = line.split("\t")
		if section == CHARACTER_INFO_SECTION and len(columns) >= characters.SKILL_EFFECT_COLUMNS:
			rows.append(columns[:characters.SKILL_EFFECT_KEPT])
	return rows


# ================
# later_characterinfo
#
# The later client's characterinfo rows by codename (later_skill_effect_text).
# ================
def later_characterinfo(read):
	return {row[0]: row for row in later_characterinfo_rows(later_skill_effect_text(read))}


# ================
# characterinfo_codenames
# ================
def characterinfo_codenames(effect_text):
	return {line.split("\t")[0] for section, line in data_lines(effect_text) if section == CHARACTER_INFO_SECTION}


# ================
# drop_characterinfo_rows
#
# The effect text without the characterinfo rows of owned codenames.
# ================
def drop_characterinfo_rows(effect_text, owned):
	section, lines = None, []
	for line in effect_text.splitlines():
		header = section_name(line)
		if header is not None:
			section = header
		elif section == CHARACTER_INFO_SECTION and line.split("\t")[0] in owned:
			continue
		lines.append(line)
	return "\r\n".join(lines) + "\r\n"


# ================
# join_characterinfo
#
# The characterinfo rows of codes (from later_characterinfo) joined to the
# tree's skilleffect.txt at effect_path; a code the tree already carries,
# or one in staged, is left alone. Returns the codes whose rows joined.
# ================
def join_characterinfo(effect_path, later, codes, staged=()):
	known = characterinfo_codenames(tables.decode_table(effect_path.read_bytes())) | set(staged)
	info = [later[code] for code in sorted(codes) if code in later and code not in known]
	write_atomically(effect_path, characters.skill_effect_section(effect_path.read_bytes(), info))
	return [row[0] for row in info]


#============================================================================
# One import's pass
#============================================================================


# ================
# LaterEffects
#
# What one importer stages: the tree's textdata folder, the later client's
# presentation text (later_skill_effect_text), the animation-set bases of
# the skills it staged, which characterinfo rows it may claim, the kinds
# it staged, and its previous marker.
# ================
@dataclass
class LaterEffects:
	text_root: Path
	later_text: str
	bases: Iterable[str]
	info_owned: Callable[[str], bool]
	refs: Iterable[str]
	prior: dict


# ================
# stage_later_effects
#
# The one skilleffect.txt pass every later importer runs: the staged
# skills' animation sets and effect stages, then the staged kinds'
# characterinfo rows; a kind a previous build owned and this one no
# longer stages loses its row. Returns the marker fields the reap reads.
# ================
def stage_later_effects(stage):
	path = stage.text_root / SKILL_EFFECT_TABLE
	text = tables.decode_table(path.read_bytes())
	selected = select_later_rows(stage.later_text, set(stage.bases) - native_bases(text))
	unplayable = unplayable_bases(selected, client_effect_text(stage.text_root, text))
	if unplayable:
		print(f"skill effects: {len(unplayable)} later skills carry stages the v1.150 client cannot load and keep no "
			f"presentation: {', '.join(sorted(unplayable)[:8])}{' ...' if len(unplayable) > 8 else ''}")
		selected = {section: [(base, line) for base, line in rows if base not in unplayable]
			for section, rows in selected.items()}
	effect_bases = {base for base, _ in selected.get(EFFECT_SET_SECTION, [])} - native_bases(text) - effect_set_keys(text)
	text, _ = merge_skill_effect_text(text, selected)
	known = characterinfo_codenames(text)
	info = [row for row in later_characterinfo_rows(stage.later_text) if stage.info_owned(row[0]) and row[0] not in known]
	owned = known & set(stage.refs)
	stale = set(stage.prior.get("characterInfo", [])) - owned
	if stale:
		text = drop_characterinfo_rows(text, stale)
	write_atomically(path, characters.skill_effect_section(text.encode("utf-16"), info))
	return {
		"animationBases": sorted(set(stage.prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
		"effectBases": sorted(set(stage.prior.get("effectBases", [])) | effect_bases),
		"characterInfo": sorted(owned | {row[0] for row in info}),
		**({"unplayableSkills": unplayable} if unplayable else {}),
	}


# ================
# reap_later_effects
#
# The off path of stage_later_effects: the marker's animation sets, effect
# stages and characterinfo rows leave the tree's skilleffect.txt. A marker
# written before the stages were imported has no effectBases: none stand.
# ================
def reap_later_effects(text_root, prior):
	path = text_root / SKILL_EFFECT_TABLE
	text = remove_presentation(tables.decode_table(path.read_bytes()), set(prior.get("animationBases", [])),
		set(prior.get("effectBases", [])))
	text = drop_characterinfo_rows(text, set(prior.get("characterInfo", [])))
	write_atomically(path, b"\xff\xfe" + text.encode("utf-16-le"))

"""
===========================================================================

later_presentation.py - the files and sound rules the variant's imported skills and kinds play

Port-only, not native. Every later importer stages skill rows (animation
sets and effect stages, cap110_skill_effects.py) and characterinfo rows
for kinds the v1.150 client never shipped. Those rows name effect
programs (.efp in Particles), stage objects (.bsr in Data) and sounds
(prim/snd in Data), and the kinds play effectsound.txt rules keyed by
their profile objects and skills. No importer copied the stage files or
joined the sound rules (the winter event joined its own kinds' rules
alone), so an imported monster swung in silence with no effect (#4730).

This pass is the one owner of that closure. It runs after every import,
reads only the tree the imports left, and for the rows an import added -
a row whose base or codename the native tables lack - it:

  - joins the later clients' effectsound rules for the added profile
    objects and skills the tree has no rule for, in the native layout
    (an empty first cell, cp949): v1.696's first, then VSRO's for what
    v1.696 does not answer;
  - copies every file those rows and rules name that neither the native
    tree nor the variant holds, with its closure, from the v1.696 Data
    and Particles archives and then VSRO's.

Later-client content ships with holes: a file no archive holds is
recorded in the marker's gaps, never invented. The marker lists the files
and rules this pass wrote; the next run takes them out first, so the
pass's output is always a function of the tree in front of it.

===========================================================================
"""
import contextlib
import json
from pathlib import Path

import cap110_skill_effects as effects
import import_vsro_alexandria as tables
import later_tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

MARKER = ".opensro-later-presentation.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
SOUND_TABLE = "effectsound.txt"
SOUND_ENCODING = "cp949"
ARCHIVES = ("Data", "Particles")
SOUND_ARCHIVE = "Media"
FOLDERS = {"Data": "Data_extracted", "Particles": "Particles_extracted"}
SERVICE_OFF = "0"
# The holes the tolerant closure walk records (skytemple_import).
CLOSURE_GAPS = ("missingSounds", "missingEffects", "missingFiles", "unparsedFiles")

# An effectsound rule, its leading cell (the later clients' service, the
# native table's empty cell) dropped: object, handle, skill, three
# events, skip, folder, file (audioResources.mjs appendEffectSoundRules).
RULE_OBJECT, RULE_SKILL, RULE_FOLDER, RULE_FILE = 0, 2, 7, 8
# A characterinfo row's codename and the profile object it plays.
INFO_CODE, INFO_PROFILE = 0, 1


# ================
# added_rows
#
# The presentation rows of the variant's skilleffect text the native one
# lacks: (section, cells) for each animation set and effect stage whose
# base the native table does not present, and the characterinfo rows of
# codenames it does not carry.
# ================
def added_rows(text, native_text):
	animated = effects.native_bases(native_text)
	staged = animated | effects.effect_set_keys(native_text)
	rows = [(section, line.split("\t")) for section, base, line in effects.presentation_rows(text)
		if base not in (staged if section == effects.EFFECT_SET_SECTION else animated)]
	native_info = effects.characterinfo_codenames(native_text)
	info = [row for row in effects.later_characterinfo_rows(text) if row[INFO_CODE] not in native_info]
	return rows, info


# ================
# rule_cells
#
# The cells of an effectsound line after its leading cell, or None for a
# comment, a blank, a short line or a rule the later client switched off.
# ================
def rule_cells(line):
	cells = line.split("\t")
	if len(cells) <= RULE_FILE + 1 or cells[0].startswith("//") or cells[0].strip() == SERVICE_OFF:
		return None
	return cells[1:]


# ================
# join_sound_rules
#
# The later rules for the added objects and skills the current table has
# no rule for, as native lines, from each source in turn: a later source
# answers only what an earlier one did not.
# ================
def join_sound_rules(current, sources, objects, skills):
	ruled = [cells for cells in map(rule_cells, current.split("\r\n")) if cells]
	wanted_objects = set(objects) - {cells[RULE_OBJECT] for cells in ruled}
	wanted_skills = set(skills) - {cells[RULE_SKILL] for cells in ruled}
	joined = []
	for text in sources:
		answered = [cells for cells in map(rule_cells, text.split("\r\n"))
			if cells and (cells[RULE_OBJECT] in wanted_objects or cells[RULE_SKILL] in wanted_skills)]
		joined += ["\t" + "\t".join(cells) for cells in answered]
		wanted_objects -= {cells[RULE_OBJECT] for cells in answered}
		wanted_skills -= {cells[RULE_SKILL] for cells in answered}
	return joined


# ================
# drop_sound_rules
#
# The effectsound text without the given lines (the rules a previous run
# joined); every other line as it was.
# ================
def drop_sound_rules(text, lines):
	owned = set(lines)
	return "\r\n".join(line for line in text.split("\r\n") if line not in owned)


# ================
# rule_resources
# ================
def rule_resources(lines):
	seeds = set()
	for line in lines:
		cells = line.split("\t")[1:]
		seed = effects.resource_seed(effects.resource_path(cells[RULE_FOLDER] + cells[RULE_FILE]), sound=True)
		if seed:
			seeds.add(seed)
	return seeds


# ================
# later_source
#
# One later client's archives for the walk ({name: (mmap, index)}) and its
# effectsound text, closed when the pass is done; None for a root without
# the archives. Every archive is checked against its pin before a read.
# ================
@contextlib.contextmanager
def later_source(root, pins):
	names = ARCHIVES + (SOUND_ARCHIVE,)
	if root is None or not all((Path(root) / f"{name}.pk2").is_file() for name in names):
		yield None
		return
	tables.require_vsro_pin(root, names, pins)
	opened = {name: tables.open_archive(root, name) for name in names}
	try:
		media = opened[SOUND_ARCHIVE][2]
		source = later_tables.Archive.media(media, tables.archive_index(media))
		sound = source.file(SOUND_TABLE) if source.has(SOUND_TABLE) else b""
		yield {"archives": {name: (opened[name][2], tables.archive_index(opened[name][2])) for name in ARCHIVES},
			"sound": sound.decode(SOUND_ENCODING, errors="strict")}
	finally:
		for _, handle, data in opened.values():
			data.close()
			handle.close()


# ================
# reap
#
# Take out what the marker says a previous run wrote: its files (only
# while they still hold the bytes it wrote) and its sound rules.
# ================
def reap(out, prior):
	for record in prior.get("files", []):
		target = out / record["path"]
		if target.is_file() and sro_pk2.digest(target.read_bytes()) == record["sha256"]:
			target.unlink()
	rules = prior.get("soundRules", [])
	sound_path = out / TEXT_ROOT / SOUND_TABLE
	if rules and sound_path.is_file():
		text = sound_path.read_bytes().decode(SOUND_ENCODING)
		write_atomically(sound_path, drop_sound_rules(text, rules).encode(SOUND_ENCODING))


# ================
# present
#
# Whether a seed's file stands in the native tree or the variant.
# ================
def present(seed, native, out):
	archive, path = seed
	return any((root / FOLDERS[archive] / path).is_file() for root in (native, out))


# ================
# copy_missing
#
# Copy the seeds neither tree holds, with their closure, from each opened
# source in turn. Returns (files written, seeds no source held, the
# closure walk's own holes).
# ================
def copy_missing(seeds, opened, native, out):
	import skytemple_import
	pending = {seed for seed in seeds if not present(seed, native, out)}
	written, holes = [], set()
	for source in opened:
		if not pending:
			break
		result = skytemple_import.copy_resource_closure(sorted(pending), source["archives"], native, out, tolerate_gaps=True)
		written += result["files"]
		holes |= {hole for key in CLOSURE_GAPS for hole in result[key]}
		pending = {seed for seed in pending if not present(seed, native, out)}
	return written, pending, holes


# ================
# add_presentation
#
# The pass. sources are (root, pins) pairs, the v1.696 client first and
# VSRO second; a None root is skipped. Returns the summary the variant
# manifest records.
# ================
def add_presentation(native, out, sources):
	native, out = Path(native), Path(out)
	marker = out / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.is_file() else {}
	reap(out, prior)
	effect_path = out / TEXT_ROOT / effects.SKILL_EFFECT_TABLE
	if not effect_path.is_file():
		marker.unlink(missing_ok=True)
		return {"enabled": False}
	text = tables.decode_table(effect_path.read_bytes())
	native_text = tables.decode_table((native / TEXT_ROOT / effects.SKILL_EFFECT_TABLE).read_bytes())
	rows, info = added_rows(text, native_text)
	if not rows and not info:
		# Nothing imported: the tree keeps no marker, as before the pass.
		marker.unlink(missing_ok=True)
		return {"enabled": False}
	objects = {row[at] for row in info for at in (INFO_CODE, INFO_PROFILE) if len(row) > at and row[at]}
	skills = {cells[effects.EFFECT_SET_KEY] if section == effects.EFFECT_SET_SECTION else effects.animset_base(section, cells)
		for section, cells in rows}
	sound_path = out / TEXT_ROOT / SOUND_TABLE
	current = (sound_path if sound_path.is_file() else native / TEXT_ROOT / SOUND_TABLE).read_bytes().decode(SOUND_ENCODING)
	with contextlib.ExitStack() as stack:
		opened = [source for source in (stack.enter_context(later_source(root, pins)) for root, pins in sources) if source]
		rules = join_sound_rules(current, [source["sound"] for source in opened], objects, skills)
		if rules:
			kept = current if current.endswith("\r\n") else current + "\r\n"
			write_atomically(safe_target(out / TEXT_ROOT, SOUND_TABLE), (kept + "\r\n".join(rules) + "\r\n").encode(SOUND_ENCODING))
		written, missing, holes = copy_missing(effects.row_resources(rows) | rule_resources(rules), opened, native, out)
	gaps = {}
	if missing:
		gaps["missing"] = sorted(f"{archive}:{path}" for archive, path in missing)
	if holes:
		gaps["closureGaps"] = sorted(holes)
	files = [{"path": path, "sha256": sro_pk2.digest((out / path).read_bytes())} for path in sorted(set(written))]
	record = {"enabled": True, "rows": len(rows), "characterInfo": len(info), "soundRules": rules, "files": files, **gaps}
	write_atomically(marker, (json.dumps(record, indent=2) + "\n").encode("utf-8"))
	return {"enabled": True, "rows": len(rows), "soundRules": len(rules), "files": len(files), **gaps}

"""
===========================================================================

cap110_quest_monsters.py - the monsters the Blue Berserk chains spawn

Port-only, not native. The Army / Military Test trials, the Piece of Spirit
rituals and the bell summon of the Blue Berserk chains (levels 95 and 100)
spawn eleven monsters from quest logic: they have no nests. The v1.150 tree
holds their models and their animation records, but not their character rows
or their attack skills, which only the later (VSRO) client carries. This tool
imports the eleven character rows and the skills they use (with the chain
links of multi-hit attacks) into shards of their own.

	Camp Commander / Archer / Healer      MOB_QT_CH_SOLDIER_01_1, _01_2, _02_1, _02_2
	Army Officer / Archer / Healer        MOB_QT_EU_SOLDIER_01_1, _01_2, _02_1, _02_2
	Old Ghost (rituals 1_5 and 2_5)       MOB_QT_CH_CHARGHOST_01, _02
	Old Tombstone Ghost (the bell)        MOB_QT_CH_TOMBSTONE_01

The skill assignment is the later client's own, unchanged (the European level
95 officer uses the level 100 officer's attacks, as the later data has it).
They are part of the quest content, so they follow the quest switch:
SRO_CAP110_QUESTS, with SRO_LEVEL_CAP at 110. Off, the tool removes what a
previous on build left, so a reused variant cannot retain it.

Identities are fresh and kept in the marker so a rebuild never moves a row.
Names and most animation records are native already; only animation records
the native table lacks are merged. Nothing here places a monster or adds a
quest: the quests that spawn them are a later step. No retail table is
committed to Git.

===========================================================================
"""
import argparse
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import build_alexandria_variant as variant
import cap110_monsters as monsters
import cap110_skill_effects as effects
import cap110_rewards as rewards
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import write_atomically

FLAG = "SRO_CAP110_QUESTS"
MARKER = ".opensro-cap110-quest-monsters.json"
CHARACTER_SHARD = "characterdata_130000.txt"
SKILL_SHARD = "skilldata_130000.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
FORMAT = "opensro-cap110-quest-monsters-v1"
SKILL_COLUMNS = 118
FIRST_DEFAULT_SKILL = 89
END_DEFAULT_SKILL = 99
NAME_SYMBOL_COLUMN = 5
MONSTERS = (
	"MOB_QT_CH_CHARGHOST_01", "MOB_QT_CH_CHARGHOST_02", "MOB_QT_CH_SOLDIER_01_1", "MOB_QT_CH_SOLDIER_01_2",
	"MOB_QT_CH_SOLDIER_02_1", "MOB_QT_CH_SOLDIER_02_2", "MOB_QT_CH_TOMBSTONE_01",
	"MOB_QT_EU_SOLDIER_01_1", "MOB_QT_EU_SOLDIER_01_2", "MOB_QT_EU_SOLDIER_02_1", "MOB_QT_EU_SOLDIER_02_2",
)


# ================
# enabled
#
# The quest switch and the expansion level cap.
# ================
def enabled(env=os.environ):
	cap = env.get(variant.LEVEL_CAP_FLAG, "").strip().lower()
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON and cap in variant.LEVEL_CAP_EXPANSION_VALUES


# ================
# select_monsters
#
# The later client's rows of the quest monsters, converted to the v1.150
# layout. A monster the native tables already hold keeps its native row; a
# monster the later client lacks, or one it does not offer, refuses by name
# rather than shipping fewer monsters than the quests spawn.
# ================
def select_monsters(later, native_codes, codes=MONSTERS):
	refs = {}
	for code in codes:
		if code in native_codes:
			continue
		row = later.get(code)
		if row is None or row[0] != "1":
			raise ValueError("quest monster is not in the later client: " + code)
		refs[code] = characters.convert_character(row)
	return refs


# ================
# needed_skills
#
# The skill identities the converted monster rows name.
# ================
def needed_skills(refs):
	return {int(skill) for row in refs.values() for skill in row[FIRST_DEFAULT_SKILL:END_DEFAULT_SKILL] if int(skill)}


# ================
# remove_records
#
# The text minus the enabled animation records of the given bases. The native
# table keeps disabled placeholder rows (service 0) under the same names; the
# shared remove_animation_bases would take those too, so only the enabled rows
# this tool inserted leave, and the native text returns byte for byte.
# ================
def remove_records(text, bases):
	owned = {line for _section, base, line in effects.animset_rows(text) if base in bases}
	return "\r\n".join(line for line in text.splitlines() if line not in owned) + "\r\n"


# ================
# remove_owned
#
# What a previous on build left: both shards, the skill manifest line and the
# animation records this tool added (the marker lists their bases).
# ================
def remove_owned(text_root, prior):
	for name in (CHARACTER_SHARD, SKILL_SHARD):
		(text_root / name).unlink(missing_ok=True)
	manifest = text_root / "skilldata.txt"
	if manifest.exists():
		kept = [row for row in tables.table_rows(manifest.read_bytes()) if row[0].lower() != SKILL_SHARD]
		write_atomically(manifest, monsters.encode_rows(kept))
	bases = set(prior.get("animationBases", []))
	effect_path = text_root / "skilleffect.txt"
	if bases and effect_path.exists():
		write_atomically(effect_path, b"\xff\xfe" + remove_records(tables.decode_table(effect_path.read_bytes()), bases).encode("utf-16-le"))


# ================
# add_quest_monsters
# ================
def add_quest_monsters(vsro_root, native, output):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("quest monster overlay must be separate from native extraction")
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if not enabled():
		if marker.exists():
			remove_owned(text_root, prior)
			marker.unlink()
		return {"enabled": False}
	with open(Path(vsro_root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, index, name.lower())
		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt")) for row in tables.table_rows(read(shard[0]))}
		characters_native = [row for path in text_root.glob("characterdata_*.txt") if path.name != CHARACTER_SHARD
			for row in tables.table_rows(path.read_bytes())]
		refs = select_monsters(later, {row[2] for row in characters_native})
		# Items, characters and teleport buildings share one reference space.
		occupied = rewards.occupied_identities(text_root, [], CHARACTER_SHARD)
		allocated = monsters.allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in sorted(refs.items()):
			row[1] = str(allocated[code])
		native_skills = [row for path in text_root.glob("skilldata_*.txt") if path.name != SKILL_SHARD
			for row in tables.table_rows(path.read_bytes()) if len(row) == SKILL_COLUMNS]
		needed = needed_skills(refs)

		# ================
		# load_client_skills
		# ================
		def load_client_skills():
			return {int(row[1]): row for shard in tables.table_rows(read("skilldata.txt"))
				for row in tables.table_rows(read(shard[0])) if len(row) > 3 and row[1].isdigit()}
		client = load_client_skills()
		source = monsters.add_native_client_skills([row for row in client.values() if len(row) == SKILL_COLUMNS],
			needed, native_skills, lambda: client)
		added, skill_map = monsters.merge_skills(native_skills, source, needed, prior.get("skills", {}))
		for row in refs.values():
			row[FIRST_DEFAULT_SKILL:END_DEFAULT_SKILL] = [str(skill_map[int(skill)]) if int(skill) else "0"
				for skill in row[FIRST_DEFAULT_SKILL:END_DEFAULT_SKILL]]
		known_names = {row[1] for row in characters.translation_rows((text_root / "textdataname.txt").read_bytes()) if len(row) > 1}
		missing = {row[NAME_SYMBOL_COLUMN] for row in refs.values()} - known_names
		if missing:
			raise ValueError("missing quest monster names: " + ", ".join(sorted(missing)))
		effect_path = text_root / "skilleffect.txt"
		effect_text = tables.decode_table(effect_path.read_bytes())
		selected = effects.select_later_rows(tables.decode_table(read("skilleffect.txt")),
			{row[3] for row in added} - effects.native_bases(effect_text))
		unknown = effects.missing_animation_states([line for rows in selected.values() for _, line in rows], effect_text)
		if unknown:
			raise ValueError("quest monster animation states absent natively: " + str(sorted(unknown)))
		effect_text, merged_records = effects.merge_skill_effect_text(effect_text, selected)
		write_atomically(text_root / CHARACTER_SHARD, monsters.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		write_atomically(text_root / SKILL_SHARD, monsters.encode_rows(added))
		manifest = [row for row in tables.table_rows((text_root / "skilldata.txt").read_bytes()) if row[0].lower() != SKILL_SHARD]
		write_atomically(text_root / "skilldata.txt", monsters.encode_rows(manifest + [[SKILL_SHARD]]))
		write_atomically(effect_path, b"\xff\xfe" + effect_text.encode("utf-16-le"))
		summary = {"enabled": True, "format": FORMAT, "portOnly": True,
			"characters": {code: int(row[1]) for code, row in sorted(refs.items())},
			"alreadyNative": sorted(set(MONSTERS) - set(refs)),
			"skills": {row[3]: int(row[1]) for row in added},
			# A union with the previous build: a rebuild finds its own records already
			# in the text, selects none, and must still remember them for the off path.
			"animationBases": sorted(set(prior.get("animationBases", [])) | {base for rows in selected.values() for base, _ in rows}),
			"animationRecords": merged_records,
			"characterShardSha256": sro_pk2.digest((text_root / CHARACTER_SHARD).read_bytes()),
			"skillShardSha256": sro_pk2.digest((text_root / SKILL_SHARD).read_bytes()),
			"laterArchiveHash": tables.sha256_of(media)}
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
		return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", required=True)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	# Off, add_quest_monsters only removes its owned outputs and reads no archive.
	if enabled():
		tables.require_vsro_pin(args.vsro_root, ("Media",))
	print(json.dumps(add_quest_monsters(args.vsro_root, args.native_extracted, args.output), indent=2))


if __name__ == "__main__":
	main()

"""
===========================================================================

cap110_skill_import.py - stage and verify the optional skill tier

Port-only, not native. Off unless SRO_CAP110_SKILLS is 1/on/true. Called by
the Alexandria builder after the equipment import. The CLI builds an isolated
overlay for review; it never enables the running stack. Native files win, and
atomic replacement breaks hard links instead of writing through them. No
retail tables are committed to Git.

===========================================================================
"""
import argparse
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path
import re

import alexandria_npcs as text_tables
import cap110_skill_effects as skill_effects
import cap110_skills as skills
import cap110_skill_database as database_skills
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

# One of three switches that must agree (#1746): this one publishes the
# client tier, SRO_CAP110_SKILL_DATABASE adds the 101-110 database tier,
# and the server's SRO_LEVEL_CAP admits the levels. The gameworld warns
# at startup when the published table outruns the cap.
FLAG = "SRO_CAP110_SKILLS"
REPORT_NAME = ".opensro-cap110-skills.json"
EFFECT_REPORT_NAME = ".opensro-cap110-skill-effects.json"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA


# ================
# enabled
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# read_skill_tables
#
# Manifest entries are validated basenames, never arbitrary paths.
# ================
def read_skill_tables(read):
	names = [row[0].strip() for row in tables.table_rows(read("skilldata.txt"))]
	if not names or any(not re.fullmatch(r"skilldata_\d+\.txt", name, re.IGNORECASE) for name in names):
		raise ValueError("invalid skill manifest")
	if len(set(map(sro_pk2.fold_ascii, names))) != len(names):
		raise ValueError("duplicate skill manifest entry")
	return [row for name in names for row in tables.table_rows(read(sro_pk2.fold_ascii(name)))]


# ================
# add_skills
#
# A successful marker owns exactly one generated shard. Off removes that shard
# and restores the native manifest, preventing a stale import on rebuild.
# ================
def add_skills(vsro_root, native, output, database_export=None):
	native, output = native.resolve(), output.resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("skill output must be separate from the native extraction")
	marker = output / REPORT_NAME
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(output / TEXT_ROOT, skills.OUTPUT_SHARD)
	manifest = safe_target(output / TEXT_ROOT, "skilldata.txt")
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked skill import shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed imported shard")
	if not enabled():
		if marker.exists():
			text_path = output / TEXT_ROOT / "textdataname.txt"
			owned_names = set(prior.get("textSymbolsAdded", prior.get("nameSymbolsAdded", [])))
			if owned_names and text_path.exists():
				original = {r[1] for r in text_tables.translation_rows((native / TEXT_ROOT / "textdataname.txt").read_bytes()) if len(r) > 1}
				owned_names -= original
				rows = text_tables.translation_rows(text_path.read_bytes())
				if any(len(r) > 1 and r[1] in owned_names for r in rows):
					rows = [r for r in rows if len(r) < 2 or r[1] not in owned_names]
					write_atomically(text_path, b"\xff\xfe" + text_tables.table_text(rows).encode("utf-16-le"))
			if shard.exists():
				shard.unlink()
			write_atomically(manifest, (native / TEXT_ROOT / "skilldata.txt").read_bytes())
			marker.unlink()
		return {"enabled": False, "added": 0}
	with ExitStack() as stack:
		handle = stack.enter_context(open(Path(vsro_root) / "Media.pk2", "rb"))
		media = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
		index = {sro_pk2.fold_ascii(entry.path): entry for entry in sro_pk2.read_directory(media)}
		identity = tables.sha256_of(media)
		base = native / TEXT_ROOT
		base_paths = {sro_pk2.fold_ascii(path.name): path for path in base.iterdir() if path.is_file()}
		native_hashes = {}
		# ================
		# read_native
		# ================
		def read_native(name):
			raw = base_paths[name].read_bytes()
			native_hashes[name] = sro_pk2.digest(raw)
			return raw
		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)
		native_rows = read_skill_tables(read_native)
		later_rows = read_skill_tables(read_later)
		database_path = database_export or os.environ.get(database_skills.DATABASE_ENV)
		database_rows, database_hash = database_skills.load_database_rows(database_path) if database_path else ([], None)
		added, summary = database_skills.merge_database_tier(native_rows, later_rows, database_rows, prior)
		if database_hash:
			summary["databaseSnapshotSha256"] = database_hash
		text_path = output / TEXT_ROOT / "textdataname.txt"
		base_text = text_path.read_bytes() if text_path.exists() else read_native("textdataname.txt")
		known_text = {r[1] for r in text_tables.translation_rows(base_text) if len(r) > 8 and r[8] not in ("", "0", "xxx")}
		wanted_names = {r[62] for r in added if r[62] not in ("", "xxx")} - known_text
		wanted_text = {r[column] for r in added for column in (62, 64, 65) if r[column] not in ("", "xxx")} - known_text
		new_text = []
		for key in sorted(index):
			name = key.replace("\\", "/").rsplit("/", 1)[-1]
			if not wanted_text or not name.startswith("textdata_") or not name.endswith(".txt"):
				continue
			for row in text_tables.translation_rows(read_later(name)):
				if len(row) >= 10 and row[1] in wanted_text and row[8] not in ("", "0", "xxx"):
					new_text.append([row[0], row[1], row[2], "", "", row[5], row[4], row[7], row[8], row[9], ""])
					wanted_text.remove(row[1])
		if wanted_names & wanted_text:
			raise ValueError(f"missing imported skill names: {sorted(wanted_names & wanted_text)}")
		summary["textSymbolsAdded"] = sorted(set(prior.get("textSymbolsAdded", prior.get("nameSymbolsAdded", []))) | {r[1] for r in new_text})
		summary["missingDescriptionSymbols"] = sorted(wanted_text)
		if new_text:
			write_atomically(text_path, text_tables.append_rows(base_text, new_text))
		raw_shard = b"\xff\xfe" + text_tables.table_text(added).encode("utf-16-le")
		base_manifest = tables.decode_table(read_native("skilldata.txt")).rstrip("\r\n")
		if skills.OUTPUT_SHARD in sro_pk2.fold_ascii(base_manifest):
			raise ValueError("native skill manifest already owns the import shard")
		write_atomically(shard, raw_shard)
		write_atomically(manifest, b"\xff\xfe" + (base_manifest + "\r\n" + skills.OUTPUT_SHARD + "\r\n").encode("utf-16-le"))
		summary.update({
			"enabled": True, "format": "opensro-cap110-skills-v1", "portOnly": True,
			"laterArchiveHashes": {"Media": identity}, "shardSha256": sro_pk2.digest(raw_shard),
			"nativeSkillHashes": native_hashes, "limits": skills.report_limits()
		})
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
		return summary


# ================
# decode_skill_effect
#
# The native skill-effect source is UTF-16 with a BOM or a single-byte
# codepage; the production parser uses the same two-form detection.
# ================
def decode_skill_effect(raw):
	if raw[:2] in (b"\xff\xfe", b"\xfe\xff") or (len(raw) >= 4 and raw[1] == 0 and raw[3] == 0):
		return raw.decode("utf-16", errors="replace").lstrip("\ufeff"), True
	return raw.decode("latin1"), False


# ================
# add_skill_effects
#
# Merge the later client's animation sets for the imported ranks into the
# variant's skilleffect.txt. Other overlays retain ownership of their sections;
# off removes only the imported animation records named by our marker.
# ================
def add_skill_effects(vsro_root, native, output):
	native, output = native.resolve(), output.resolve()
	source = native / TEXT_ROOT / "skilleffect.txt"
	target = safe_target(output / TEXT_ROOT, "skilleffect.txt")
	marker = output / EFFECT_REPORT_NAME
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(output / TEXT_ROOT, skills.OUTPUT_SHARD)
	if not enabled():
		if target.exists() and prior:
			text, utf16 = decode_skill_effect(target.read_bytes())
			merged = skill_effects.remove_animation_bases(text, set(prior["bases"]))
			encoded = b"\xff\xfe" + merged.encode("utf-16-le") if utf16 else merged.encode("latin1")
			write_atomically(target, encoded)
			marker.unlink()
		return {"enabled": False, "inserted": 0}
	if not shard.exists():
		return {"enabled": True, "inserted": 0, "skipped": "no imported skill shard"}
	native_raw = source.read_bytes()
	native_text, _native_utf16 = decode_skill_effect(native_raw)
	target_raw = target.read_bytes() if target.exists() else native_raw
	target_text, utf16 = decode_skill_effect(target_raw)
	imported_bases = {
		row[5].strip() for row in tables.table_rows(shard.read_bytes())
		if row[0] == "1" and row[5].strip() not in ("", "xxx")
	}
	wanted = imported_bases - skill_effects.native_bases(native_text)
	with ExitStack() as stack:
		handle = stack.enter_context(open(Path(vsro_root) / "Media.pk2", "rb"))
		media = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
		index = {sro_pk2.fold_ascii(entry.path): entry for entry in sro_pk2.read_directory(media)}
		later_raw = tables.table_text(media, index, "skilleffect.txt")
	later_text, _later_utf16 = decode_skill_effect(later_raw)
	selected = skill_effects.select_later_rows(later_text, wanted)
	missing = skill_effects.missing_animation_states(
		[line for rows in selected.values() for _base, line in rows], native_text
	)
	if missing:
		raise ValueError(f"later animation sets use unknown native states: {sorted(missing)}")
	merged, inserted = skill_effects.merge_skill_effect_text(target_text, selected)
	encoded = (b"\xff\xfe" + merged.encode("utf-16-le")) if utf16 else merged.encode("latin1")
	if encoded != target_raw:
		write_atomically(target, encoded)
	owned_bases = sorted({base for rows in selected.values() for base, _line in rows})
	write_atomically(marker, (json.dumps({"bases": owned_bases}, indent=2) + "\n").encode("utf-8"))
	return {
		"enabled": True, "inserted": inserted, "importedBases": len(imported_bases),
		"missingBasesNeedingLaterRows": len(wanted),
	}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--native-extracted", type=Path, default=EXTRACTED_ROOT)
	parser.add_argument("--output-extracted", type=Path, default=REPO_ROOT / ".state" / "cap110-skills" / "extracted")
	parser.add_argument("--database-export", type=Path, help="owner-supplied _RefSkill JSON snapshot")
	args = parser.parse_args()
	if enabled():
		tables.require_vsro_pin(args.vsro_root, ("Media",))
	summary = add_skills(args.vsro_root, args.native_extracted, args.output_extracted, args.database_export)
	print(f"Skills: {summary['added']}; enabled: {summary['enabled']}")
	if summary["enabled"]:
		print(f"Trainable: {summary['learnableRanks']}; chain-sub: {summary['chainSubRanks']}; "
			f"new groups: {len(summary['newGroupIds'])}")
		print(f"Report: {args.output_extracted / REPORT_NAME}")


if __name__ == "__main__":
	main()

"""
===========================================================================

pack_avatars.py - pack a newer client's exported avatars and pets into one archive

Port-only, not native (owner ask #3254). Reads an already-extracted client
export (tables and resources on disk, nothing executed) and writes the
single pinned archive mall_avatar_import.py reads when SRO_AVATARS_ZIP
names it: the avatar item rows and their name texts under media/, and the
resource files the native tree lacks under data/, media/ and particles/,
mirroring the pk2 paths. The archive is written outside the repository
and never committed; its sha256 is pinned in avatars_archive_pins.json.

Rows are selected the way the importer selects (service-enabled avatar
type, not a test row), normalized to the native row width, and dropped -
always reported, never half packed - when a resource is absent, when the
model's materials would make the item-model builder refuse the whole
asset build (screened with the builder's own rule, avatar_model_screen),
or when any file of the model's closure is missing. Only files the
native tree lacks are packed; the build resolves the rest natively, so
the same archive serves every build of this client.

The export's pets ride the same archive (owner 2026-11-27: every new pet,
native or not). pet_import.py's own selection decides which kinds
qualify, so the packer and the importer never disagree; the packer then
keeps those kinds' original rows - character rows, summon scrolls, the
skill rows their DefaultSkillIDs name and their name texts - and leaves
the identities to the importer. A kind whose resource closure fails is
dropped and reported, like an avatar.

===========================================================================
"""
import argparse
import base64
import bisect
import json
from pathlib import Path
import subprocess
import sys
import tempfile

import alexandria_npcs as text_tables
import avatar_archive
import cap110_item_import as shared
import cap110_items as items
import import_vsro_alexandria as tables
import mall_avatar_import as importer
import pet_import as pets
import pet_rows
import skilldata_enc as skill_tables
import sro_pk2
import later_tables
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

SHARD_NAME = importer.AVATARS_SOURCE["shard"]
TEXT_TABLES = ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
# The top-level folders an avatar's closure can touch in the flattened
# export tree; effect programs (.efp) are indexed wherever they sit.
RESOURCE_FOLDERS = ("res", "prim", "icon", "icon64", "server_dep", "effect", "slot_effect")
# The pet shards, clear of the importer's own outputs (pet_import
# ARCHIVE_SOURCE writes the *_150000 shards) and named so each manifest
# reader accepts them (skilldata_enc.SHARD_NAME).
PET_CHAR_SHARD = "characterdata_150001.txt"
PET_ITEM_SHARD = "itemdata_150001.txt"
PET_SKILL_SHARD = "skilldata_150001.txt"
# The pets' posture for the resource audit: a pet model's authored sound
# tracks (Draco, the balloons) are walked as Data resources and its
# ambient particle sets (the Circus Bear, the Ghost) as Particles, as the
# Sky Temple monsters' are; without it the audit refuses the model.
PET_POSTURE = shared.WALK_EVERYTHING


# ================
# FileBlob
#
# The export tree as one addressable blob: files are indexed by their
# tree-relative path and read back by extent, so the pk2-shaped
# (data, index) archives the importer machinery expects work unchanged
# over plain files.
# ================
class FileBlob:
	def __init__(self):
		self.extents = []
		self.starts = []
		self.size = 0

	# ================
	# add
	# ================
	def add(self, path, size):
		self.extents.append((self.size, self.size + size, path))
		self.starts.append(self.size)
		self.size += size
		return self.extents[-1][0]

	# ================
	# __getitem__
	#
	# Only whole-entry slices are ever requested (sro_pk2.payload).
	# ================
	def __getitem__(self, extent):
		index = bisect.bisect_right(self.starts, extent.start) - 1
		start, _, path = self.extents[index]
		if extent.start != start:
			raise ValueError(f"unaligned blob read at {extent.start}")
		return path.read_bytes()


# ================
# export_archives
#
# (data, index) archives over the export tree. Data and Media see every
# indexed path (lookups are exact per archive); Particles sees only the
# effect programs, whose bare names the closure suffix-matches.
# ================
def export_archives(root):
	blob = FileBlob()
	index = {}
	files = [path for path in root.rglob("*") if path.is_file()
		and (path.relative_to(root).parts[0] in RESOURCE_FOLDERS or path.suffix.lower() == ".efp")]
	for path in sorted(files):
		key = shared.normalized_path(path.relative_to(root).as_posix())
		if key in index:
			raise ValueError(f"duplicate export path: {key}")
		index[key] = sro_pk2.Entry(key, blob.add(path, path.stat().st_size), path.stat().st_size)
	archives = {
		"Data": (blob, dict(index)),
		"Media": (blob, dict(index)),
		"Particles": (blob, {key: entry for key, entry in index.items() if key.endswith(".efp")}),
	}
	return archives


# ================
# native_files
#
# The native tree as {archive: {path: file}} for resource reads.
# ================
def native_files(native):
	return {name: {
		shared.normalized_path(path.relative_to(native / folder).as_posix()): path
		for path in (native / folder).rglob("*") if path.is_file()
	} for name, folder in shared.FOLDERS.items()}


# ================
# read_resource
#
# One resource's bytes, native first as the build resolves it.
# ================
def read_resource(archives, natives, archive, name):
	local = natives[archive].get(name)
	if local is not None:
		return local.read_bytes()
	data, index = archives[archive]
	entry = index.get(name)
	return sro_pk2.payload(data, entry) if entry is not None else None


# ================
# normalize_item_row
#
# An export item row at the native width: trailing empty cells go (some
# shards carry a few extra), a short row is padded, and a row with real
# cells past the width is refused - its columns would shift.
# ================
def normalize_item_row(row):
	cells = list(row)
	while len(cells) > items.ITEM_COLUMNS and cells[-1] == "":
		cells.pop()
	if len(cells) > items.ITEM_COLUMNS:
		return None
	return cells + [""] * (items.ITEM_COLUMNS - len(cells))


# ================
# export_text_row
#
# One export text row in the VSRO layout the importer reads: service,
# key, Korean, five empty cells, the English text at index 8, the rest
# empty. A row with no English text is dropped, so the importer's own
# name-text rule skips the avatar and reports it.
# ================
def export_text_row(row):
	if len(row) <= text_tables.TEXT_ENGLISH_COLUMN + 1:
		return None
	english = row[text_tables.TEXT_ENGLISH_COLUMN + 1]
	if english in ("", "0"):
		return None
	return [row[0], row[1], row[2]] + ["0"] * 5 + [english] + ["0"] * (importer.VSRO_TEXT_COLUMNS - 9)


# ================
# run_screen
# ================
def run_screen(mode, requests):
	result = subprocess.run(
		["node", str(REPO_ROOT / "scripts" / "analysis" / "avatar_model_screen.mjs"), mode],
		input=json.dumps(requests), capture_output=True, text=True, encoding="utf-8", check=False
	)
	if result.returncode != 0:
		raise ValueError(f"avatar model screen failed:\n{result.stderr}")
	return json.loads(result.stdout)


# ================
# screen_models
#
# The codenames whose worn model the item-model builder would refuse
# (avatar_model_screen runs the builder's own material rule), with the
# refusal reason. A missing material set refuses too: the closure would
# drop the row for it anyway.
# ================
def screen_models(rows, archives, natives):
	requests = []
	for row in rows:
		model = "res/" + shared.normalized_path(row[items.MODEL].split(",")[0].strip())
		raw = read_resource(archives, natives, "Data", model)
		if raw is None:
			continue
		requests.append({"code": row[items.CODENAME], "bsr": base64.b64encode(raw).decode("ascii")})
	refused = {}
	material_paths = {}
	for result in run_screen("modifiers", requests):
		if "error" in result:
			refused[result["code"]] = "model screen failed: " + result["error"]
		else:
			material_paths[result["code"]] = result
	verdicts = []
	for code, result in material_paths.items():
		materials = []
		for path in result["materialPaths"]:
			raw = read_resource(archives, natives, "Data", path)
			if raw is None:
				raw = read_resource(archives, natives, "Data", "res/" + path)
			if raw is None:
				refused[code] = f"material set missing: {path}"
				break
			materials.append({"path": path, "bytes": base64.b64encode(raw).decode("ascii")})
		else:
			verdicts.append({
				"code": code, "environmentModifiers": result["environmentModifiers"], "materials": materials,
			})
	for verdict in run_screen("verdicts", verdicts):
		if not verdict["ok"]:
			refused[verdict["code"]] = verdict["reason"]
	return refused


# ================
# closure_completeness
#
# The rows whose whole asset closure exists, and the rest with the file
# they miss. The walk mirrors copy_asset_closure (native files win, a
# bare particle name suffix-matches) but only reads: one dependency
# audit per frontier round serves every row at once, then each row's
# file set is propagated along the dependency edges, so one absent file
# drops only its own rows. The importer runs the real closure
# afterwards over the survivors, which cannot then fail.
# ================
def closure_completeness(rows, archives, natives):
	seeds = {}
	for row in rows:
		code = row[items.CODENAME]
		seeds[code] = {(archive, shared.normalized_path(name)) for archive, name in items.direct_assets([row])}
	worn = shared.resource_dependencies([{"weapon": {
		"resolvedModel": "res/" + shared.normalized_path(row[items.MODEL]),
		"tid": list(map(int, row[items.TYPE_FIRST:items.TYPE_FOURTH + 1])),
	}} for row in rows])
	for row, names in zip(rows, worn):
		for name in names:
			seeds[row[items.CODENAME]].add(("Data", shared.normalized_path(name)))

	# ================
	# resolve
	#
	# The (name, raw, origin) a file resolves to under its archive, or
	# None. A bare particle name takes its unique suffix match, as the
	# copy does.
	# ================
	def resolve(archive, name):
		local = natives[archive].get(name)
		if local is not None:
			return name, local.read_bytes(), "native"
		data, index = archives[archive]
		if archive == "Particles" and "/" not in name and name not in index:
			matches = {path for path in set(natives[archive]) | set(index) if path.endswith("/" + name)}
			if len(matches) != 1:
				return None
			name = matches.pop()
			local = natives[archive].get(name)
			if local is not None:
				return name, local.read_bytes(), "native"
		entry = index.get(name)
		if entry is None:
			return None
		return name, sro_pk2.payload(data, entry), "later"

	edges, absent = {}, {}
	seen = set()
	frontier = {key for files in seeds.values() for key in files}
	while frontier:
		requests, owners = [], []
		for archive, name in sorted(frontier):
			key = (archive, name)
			if key in seen:
				continue
			seen.add(key)
			found = resolve(archive, name)
			if found is None:
				absent[key] = f"missing {archive} resource: {name}"
				continue
			resolved, raw, origin = found
			if resolved.endswith(shared.PARSED_EXTENSIONS):
				requests.append({
					"path": resolved, "bytes": base64.b64encode(raw).decode("ascii"), "native": origin == "native",
				})
				owners.append(key)
		if not requests:
			break
		dependencies = []
		for key, result in zip(owners, shared.tolerant_dependencies(requests)):
			if isinstance(result, ValueError):
				lines = [line.strip() for line in str(result).splitlines() if "Error:" in line]
				absent[key] = "resource dependency audit failed: " + (lines[0] if lines else "unparseable")
				result = []
			dependencies.append(result)
		frontier = set()
		for key, names in zip(owners, dependencies):
			edges[key] = set()
			for name in names:
				archive, path = (name["archive"], name["path"]) if isinstance(name, dict) else ("Data", name)
				dep = (archive, shared.normalized_path(path))
				edges[key].add(dep)
				if dep not in seen:
					frontier.add(dep)
	owners_of = {}
	queue = []
	for code, files in seeds.items():
		for key in files:
			if code not in owners_of.setdefault(key, set()):
				owners_of[key].add(code)
				queue.append(key)
	while queue:
		key = queue.pop()
		for dep in edges.get(key, ()):
			before = len(owners_of.get(dep, ()))
			owners_of.setdefault(dep, set()).update(owners_of[key])
			if len(owners_of[dep]) != before:
				queue.append(dep)
	excluded = {}
	for key, reason in absent.items():
		for code in owners_of.get(key, ()):
			excluded.setdefault(code, reason)
	surviving = [row for row in rows if row[items.CODENAME] not in excluded]
	dropped = [{"code": code, "missing": [excluded[code]]} for code in sorted(excluded)]
	return surviving, dropped


# ================
# table_bytes
# ================
def table_bytes(rows):
	return b"\xff\xfe" + text_tables.table_text(rows).encode("utf-16-le")


# ================
# model_path
#
# A character row's model cell as a res/ path (the cells use backslashes).
# ================
def model_path(row):
	return row[text_tables.MODEL_COLUMN].replace("\\", "/").lower() if len(row) > text_tables.MODEL_COLUMN else ""


# ================
# pack_pets
#
# The export's pets, as pet_import selects them: (character rows, scroll
# rows, skill rows, resource entries, report). Selection runs on the
# export's own tables against the native tree; each chosen family's
# original rows are kept, and its model and scroll icons are closed over
# like an avatar's, one family at a time so one broken closure drops one
# family. closure is a seam for tests.
# ================
def pack_pets(archives, native, closure=None):
	closure = closure or shared.copy_resource_closure
	media, index = archives["Media"]
	_data, data_index = archives["Data"]
	if not later_tables.Archive.media(media, index).has("characterdata.txt"):
		return [], [], [], {}, {"families": [], "excluded": []}
	later_chars, later_items, _text = pets.later_tables_from_media(media, index, object_text_required=False)
	native_chars, native_items = pets.native_tables(native)

	def missing(family, rows):
		for row in rows:
			model = model_path(row)
			if not model or not ((Path(native) / "Data_extracted" / "res" / model).exists() or f"res/{model}" in data_index):
				return ["model"]
		return []

	_rows, _items, selection = pets.select_pets(native_chars, native_items, set(), later_chars, later_items, missing=missing)
	char_rows, item_rows, entries, families, excluded = [], [], {}, [], list(selection["skipped"])
	for added in selection["added"]:
		family = added["family"]
		rows = [row for row in later_chars if len(row) > text_tables.CODENAME_COLUMN and pet_rows.family_of(row[text_tables.CODENAME_COLUMN]) == family]
		codes = set(added["scrolls"])
		scrolls = [normalize_item_row(row) for row in later_items if row[items.CODENAME] in codes]
		if any(row is None for row in scrolls):
			excluded.append({"family": family, "missing": ["scroll row wider than the native layout"]})
			continue
		pending = {("Data", "res/" + model_path(row)) for row in rows}
		pending |= items.direct_assets(scrolls)
		try:
			with tempfile.TemporaryDirectory() as staging:
				records = closure(pending, native, Path(staging), archives, posture=PET_POSTURE)
				found = {
					f"{record['archive'].lower()}/{record['path']}":
						(Path(staging) / shared.FOLDERS[record["archive"]] / record["path"]).read_bytes()
					for record in records if record["source"] == "later"
				}
		except ValueError as error:
			excluded.append({"family": family, "missing": [str(error)]})
			continue
		entries.update(found)
		char_rows += rows
		item_rows += scrolls
		families.append(family)
	needed = {
		value for row in char_rows for value in row[text_tables.NATIVE_DEFAULT_SKILL_AT:text_tables.NATIVE_DEFAULT_SKILL_END]
		if value.isdigit() and int(value)
	}
	skill_rows = []
	if needed:
		read = later_tables.Archive.media(media, index)
		skill_rows = [row for row in skill_tables.client_skill_rows(read) if len(row) > 3 and row[1] in needed]
	return char_rows, item_rows, skill_rows, entries, {"families": families, "excluded": excluded}


# ================
# pack
#
# The whole pack: select, screen, close over the resources, then write
# the archive and its report. Screen and completeness are seams for
# tests; the copy itself is the importer's own closure.
# ================
def pack(export_root, native, output, screen=screen_models, completeness=closure_completeness):
	export_root, native = Path(export_root).resolve(), Path(native).resolve()
	archives = export_archives(export_root)
	media, index = archives["Media"]
	natives = native_files(native)
	base = native / importer.TEXT_ROOT
	native_rows = shared.read_item_tables(later_tables.Archive.tree(base))
	native_codes = {row[items.CODENAME] for row in native_rows}
	source = later_tables.Archive.media(media, index)
	later_rows = shared.read_item_tables(source)
	texts = {}
	for name in TEXT_TABLES:
		for row in source.rows(name, optional=True):
			converted = export_text_row(row)
			if converted is not None and converted[1] not in texts:
				texts[converted[1]] = (name, converted)
	missing = importer.resource_check(archives, {name: set(files) for name, files in natives.items()})
	selected, excluded = [], []
	seen_codes, seen_ids = set(), set()
	for row in later_rows:
		if not importer.candidate(row, any_prefix=True):
			continue
		code = row[items.CODENAME]
		if code in native_codes:
			continue
		normalized = normalize_item_row(row)
		if normalized is None:
			excluded.append({"code": code, "missing": ["row wider than the native layout"]})
			continue
		if code in seen_codes or int(normalized[items.IDENTITY]) in seen_ids:
			excluded.append({"code": code, "missing": ["duplicate codename or identity in the export"]})
			continue
		seen_codes.add(code)
		seen_ids.add(int(normalized[items.IDENTITY]))
		absent = missing(normalized)
		if normalized[items.NAME_KEY] not in texts:
			absent = absent + ["name text"]
		if absent:
			excluded.append({"code": code, "missing": absent})
			continue
		selected.append(normalized)
	refused = screen(selected, archives, natives)
	surviving = []
	for row in selected:
		if row[items.CODENAME] in refused:
			excluded.append({"code": row[items.CODENAME], "missing": [refused[row[items.CODENAME]]]})
		else:
			surviving.append(row)
	surviving, dropped = completeness(surviving, archives, natives)
	excluded += dropped
	final = surviving
	pet_chars, pet_items, pet_skills, pet_entries, pet_report = pack_pets(archives, native)
	with tempfile.TemporaryDirectory() as staging:
		records = shared.copy_asset_closure(final, native, Path(staging), archives)
		entries = {}
		for record in records:
			if record["source"] != "later":
				continue
			raw = (Path(staging) / shared.FOLDERS[record["archive"]] / record["path"]).read_bytes()
			entries[f"{record['archive'].lower()}/{record['path']}"] = raw
	entries.update(pet_entries)
	wanted = {row[column] for row in final + pet_items for column in (items.NAME_KEY, items.DESCRIPTION_KEY)}
	wanted |= {row[text_tables.NAME_KEY_COLUMN] for row in pet_chars if len(row) > text_tables.NAME_KEY_COLUMN}
	wanted -= {items.UNUSED, ""}
	per_table = {name: [] for name in TEXT_TABLES}
	for key in sorted(wanted):
		if key in texts:
			name, converted = texts[key]
			per_table[name].append(converted)
	entries["media/" + tables.TEXTDATA + "itemdata.txt"] = table_bytes([[SHARD_NAME]] + ([[PET_ITEM_SHARD]] if pet_items else []))
	entries["media/" + tables.TEXTDATA + SHARD_NAME] = table_bytes(final)
	if pet_chars:
		entries["media/" + tables.TEXTDATA + PET_ITEM_SHARD] = table_bytes(pet_items)
		entries["media/" + tables.TEXTDATA + "characterdata.txt"] = table_bytes([[PET_CHAR_SHARD]])
		entries["media/" + tables.TEXTDATA + PET_CHAR_SHARD] = table_bytes(pet_chars)
	if pet_skills:
		entries["media/" + tables.TEXTDATA + skill_tables.PLAIN_MANIFEST] = table_bytes([[PET_SKILL_SHARD]])
		entries["media/" + tables.TEXTDATA + PET_SKILL_SHARD] = table_bytes(pet_skills)
	for name in TEXT_TABLES:
		entries["media/" + tables.TEXTDATA + name] = table_bytes(per_table[name])
	output = Path(output)
	output.parent.mkdir(parents=True, exist_ok=True)
	avatar_archive.write_archive(output, entries)
	return {
		"format": "opensro-avatars-pack-v1", "portOnly": True,
		"rows": len(final), "excluded": sorted(excluded, key=lambda entry: entry["code"]),
		"pets": pet_report["families"], "petsExcluded": pet_report["excluded"],
		"files": len(entries), "bytes": output.stat().st_size,
		"sha256": sro_pk2.digest(output.read_bytes()),
	}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--export-root", type=Path, required=True, help="the extracted v2.355 export (data only, nothing in it is run)")
	parser.add_argument("--native-extracted", type=Path, default=EXTRACTED_ROOT)
	parser.add_argument("--output", type=Path, required=True, help="the archive to write, outside the repository")
	parser.add_argument("--report", type=Path, help="also write the pack report as JSON here")
	args = parser.parse_args()
	report = pack(args.export_root, args.native_extracted, args.output)
	# The report is written before the summary, and the summary never
	# fails on a console codepage: export reasons carry raw bytes.
	if args.report:
		args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
		print(f"Report: {args.report}")
	sys.stdout.reconfigure(errors="backslashreplace")
	print(f"Avatars packed: {report['rows']}; excluded: {len(report['excluded'])}")
	print(f"Pets packed: {len(report['pets'])}; excluded: {len(report['petsExcluded'])}")
	for entry in report["petsExcluded"]:
		print(f"  excluded pet {entry['family']}: {', '.join(entry['missing'])}")
	for entry in report["excluded"]:
		print(f"  excluded {entry['code']}: {', '.join(entry['missing'])}")
	print(f"Archive: {args.output} ({report['files']} entries, {report['bytes']} bytes)")
	print(f"sha256: {report['sha256']}")
	print(f"Pin it as scripts/avatars_archive_pins.json: {{\"format\": \"{importer.AVATARS_PIN_FORMAT}\", \"sha256\": \"{report['sha256']}\"}}")


if __name__ == "__main__":
	main()

"""
===========================================================================

pet_import.py - stage the later clients' pets (pickup, attack, transport and vehicle kinds)

Port-only, not native, where the source is not a later native client. The
v1.150 client already carries every pet the VSRO client has (the cap110
character import brought their COS rows with the monsters), so a pass over
an old source reports every kind as existing and adds nothing. What this
importer adds is the road for the kinds only a later source holds: the
v1.696 client's pets (a later native client, so no flag of its own) and
the v2.355 export's custom pets, read only from the pinned, optional
opensro zip and gated by SRO_PETS_ARCHIVE (unset: no zip, the pass skips).

Selection is by codename family (the codename minus its trailing level
number): COS_P_ kinds with leveled rows are attack (growth) pets, COS_P_
kinds with one level are pickup pets, COS_T_ are transports, COS_C_ are
vehicles and trade animals. COS_GUARD_ and COS_GUILD_ rows are mercenary
and guild systems, not pets, and never candidates. A kind with no server
lane yet (fellow pets and any other prefix) is deferred and reported,
never half imported: its rows wait for the lane that can run them.

A chosen kind brings its characterdata rows, the summon scrolls whose
itemdata column 119 names it, and the name text for both. The pet's model
(.bsr, characterdata column 52) must exist in the later archives or the
native tree, or the kind is skipped and reported; the mesh, material and
animation closure below the .bsr is the character model builder's, as it
is for every imported monster. What an import does not bring is the
server's rule for the kind: companion/petdesign.json is the owner's
single source for pet rules, so the report names, per added kind, the
design entry (and, for attack kinds, the pet skill rows) a follow-up
still owes. No retail table is committed to Git; rows land in the variant
staging tree beside the avatar shards.

Rows keep their later-client identity when it is free in the native
tables and the character data; the few that clash take a fresh one above
every known identity, remembered in the marker so a rebuild never moves
a pet. Each source owns one character shard, one item shard and one
marker, so a source's off path removes only its own rows.

===========================================================================
"""
import argparse
import collections
import json
import mmap
import os
from pathlib import Path
import re
import zipfile

import alexandria_npcs as characters
import cap110_item_import as shared
import cap110_items as items
import import_vsro_alexandria as tables
from prepare_client_resources import safe_target, write_atomically
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

FLAG = "SRO_PETS"
FLAG_OFF = ("0", "off", "false", "no")
ARCHIVE_ENV = "SRO_PETS_ARCHIVE"
V1696_ENV = "SRO_V1696_ROOT"
V1696_PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
FOLDERS = shared.FOLDERS
TEXT_ROOT = shared.TEXT_ROOT

# Character row columns (the layout scripts/cap110_monsters.py reads):
CHAR_ID = 1
CHAR_CODE = 2
CHAR_NAME_KEY = 5
CHAR_MODEL = 52
CHAR_LEVEL = 57
# Item row column 119 is the associated character codename (the server's
# itemrefs.go reads the same column): a summon scroll names its pet there.
ITEM_ASSOCIATED_CHAR = 119
SERVICE_ON = "1"

# One shard pair and one marker per source, in import order: a kind is
# imported once, from the earliest source that holds it.
VSRO_SOURCE = {"name": "vsro", "char_shard": "characterdata_130000.txt", "item_shard": "itemdata_130000.txt",
	"report": ".opensro-pets.json", "pins": None}
V1696_SOURCE = {"name": "v1696", "char_shard": "characterdata_140000.txt", "item_shard": "itemdata_140000.txt",
	"report": ".opensro-pets-v1696.json", "pins": V1696_PIN_FILE}
ARCHIVE_SOURCE = {"name": "archive", "char_shard": "characterdata_150000.txt", "item_shard": "itemdata_150000.txt",
	"report": ".opensro-pets-archive.json", "pins": None}
SOURCES = (VSRO_SOURCE, V1696_SOURCE, ARCHIVE_SOURCE)

EXCLUDED_PREFIXES = ("COS_GUARD_", "COS_GUILD_")
LEVEL_SUFFIX = re.compile(r"_\d+$")
# Kinds the server's COS lanes already run (the summoning, pickup, growth,
# transport and riding lanes); anything else defers until its lane exists.
LANE_KINDS = ("pickup", "attack", "transport", "vehicle")


# ================
# enabled
#
# The import as a whole defaults on like the mall avatars: an unset or
# empty value is on, only an explicit off value turns it off. An
# unreadable value is refused rather than guessed. The zip source has
# its own gate (ARCHIVE_ENV names the zip; unset imports nothing).
# ================
def enabled(env=os.environ):
	value = env.get(FLAG, "").strip().lower()
	if value in ("", "1", "on", "true", "yes"):
		return True
	if value in FLAG_OFF:
		return False
	raise ValueError(f"{FLAG} must be on or off, not {value!r}")


# ================
# family_of / kind_of
#
# A kind is its codename family: COS_P_WOLF_001 and COS_P_WOLF_140 are
# one wolf. The kind follows the prefix and the family's shape.
# ================
def family_of(codename):
	return LEVEL_SUFFIX.sub("", codename)


def kind_of(family, rows):
	if family.startswith(EXCLUDED_PREFIXES):
		return "excluded"
	if family.startswith("COS_P_"):
		levels = {row[CHAR_LEVEL] for row in rows if len(row) > CHAR_LEVEL}
		return "attack" if len(levels) > 1 else "pickup"
	if family.startswith("COS_T_"):
		return "transport"
	if family.startswith("COS_C_"):
		return "vehicle"
	return "fellow"


# ================
# previous_targets
#
# A marker's identity map, so a rebuilt import keeps the identities it
# already handed out (the avatar marker's rule).
# ================
def previous_targets(prior):
	result = {}
	for code, entry in prior.get("idMap", {}).items():
		result[code] = int(entry["target"]) if isinstance(entry, dict) else int(entry)
	return result


# ================
# select_pets
#
# later_chars / later_items are full row lists from one later source;
# earlier_codes are the row codenames an earlier source imported.
# missing(family, rows) names absent resources. Scrolls are the later
# item rows whose associated-character column names the family (or one
# of its rows); a kind without one cannot be obtained and is skipped.
# Returns (char_rows, item_rows, report) with identities applied.
# ================
def select_pets(native_chars, native_items, earlier_codes, later_chars, later_items, previous=None, missing=lambda family, rows: []):
	native_codes = {row[CHAR_CODE] for row in native_chars if len(row) > CHAR_CODE}
	taken_ids = {int(row[CHAR_ID]) for row in native_chars if len(row) > CHAR_ID and row[CHAR_ID].isdigit()}
	taken_ids |= {int(row[items.IDENTITY]) for row in native_items if len(row) > items.IDENTITY and row[items.IDENTITY].isdigit()}
	previous = previous or {}
	families = collections.defaultdict(list)
	for row in later_chars:
		if len(row) > CHAR_CODE and row[CHAR_CODE].startswith("COS_"):
			families[family_of(row[CHAR_CODE])].append(row)
	scrolls = collections.defaultdict(list)
	for row in later_items:
		if len(row) > ITEM_ASSOCIATED_CHAR and row[items.SERVICE] == SERVICE_ON and row[ITEM_ASSOCIATED_CHAR].strip():
			scrolls[row[ITEM_ASSOCIATED_CHAR].strip()].append(row)
	chosen, existing, deferred, skipped = [], [], [], []
	for family in sorted(families):
		rows = sorted(families[family], key=lambda row: int(row[CHAR_ID]) if row[CHAR_ID].isdigit() else 0)
		kind = kind_of(family, rows)
		if kind == "excluded":
			continue
		fresh_rows = [row for row in rows if row[CHAR_CODE] not in native_codes and row[CHAR_CODE] not in earlier_codes]
		if not fresh_rows:
			existing.append(family)
			continue
		if kind not in LANE_KINDS:
			deferred.append({"family": family, "kind": kind, "reason": "no server lane yet"})
			continue
		codes = {row[CHAR_CODE] for row in rows}
		linked = [row for code, linked_rows in scrolls.items() if code == family or code in codes for row in linked_rows]
		absent = list(missing(family, fresh_rows))
		if not linked:
			absent.append("summon scroll")
		if absent:
			skipped.append({"family": family, "kind": kind, "missing": absent})
			continue
		chosen.append((family, kind, fresh_rows, linked))
	# Identities: a retained one first, else the later one when free,
	# else a fresh one above every known identity. Character and item
	# rows share one identity space in the built data.
	targets = {}

	def claim(code, source_id):
		retained = previous.get(code)
		if retained is not None and retained not in taken_ids:
			targets[code] = retained
		elif source_id not in taken_ids:
			targets[code] = source_id
		else:
			fresh = max(taken_ids) + 1 if taken_ids else 1
			while fresh in taken_ids:
				fresh += 1
			targets[code] = fresh
		taken_ids.add(targets[code])

	char_rows, id_map, report_kinds = [], {}, []
	for family, kind, rows, linked in chosen:
		levels = sorted({int(row[CHAR_LEVEL]) for row in rows if len(row) > CHAR_LEVEL and row[CHAR_LEVEL].isdigit()})
		for row in rows:
			claim(row[CHAR_CODE], int(row[CHAR_ID]))
			converted = list(row)
			id_map[row[CHAR_CODE]] = {"source": int(row[CHAR_ID]), "target": targets[row[CHAR_CODE]]}
			converted[CHAR_ID] = str(targets[row[CHAR_CODE]])
			char_rows.append(converted)
		needs = ["petdesign entry"]
		if kind == "attack":
			needs.append("pet skill rows")
		report_kinds.append({
			"family": family, "kind": kind, "rows": len(rows),
			"levels": [levels[0], levels[-1]] if len(levels) > 1 else levels,
			"scrolls": sorted({row[items.CODENAME] for row in linked}), "needs": needs})
	item_rows = []
	for family, kind, rows, linked in chosen:
		for row in linked:
			claim(row[items.CODENAME], int(row[items.IDENTITY]))
			converted = list(row)
			id_map[row[items.CODENAME]] = {"source": int(row[items.IDENTITY]), "target": targets[row[items.CODENAME]]}
			converted[items.IDENTITY] = str(targets[row[items.CODENAME]])
			item_rows.append(converted)
	report = {"added": report_kinds, "existing": existing, "deferred": deferred, "skipped": skipped, "idMap": id_map}
	return char_rows, item_rows, report


# ================
# later table readers
#
# A pk2 source reads its Media archive; the zip source reads the same
# tables at their extracted-tree paths inside the pinned archive. Both
# return (character rows, item rows, object text table) with shards
# resolved through their manifests.
# ================
def _shard_rows(read, manifest_name, prefix):
	names = [row[0] for row in tables.table_rows(read(manifest_name)) if row and row[0]]
	rows = []
	for name in names:
		if Path(name).name.lower().startswith(prefix):
			rows.extend(tables.table_rows(read(name)))
	return rows


def later_tables_from_pk2(root):
	with open(Path(root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		def read(name):
			return tables.table_text(media, index, name.lower())

		return (_shard_rows(read, "characterdata.txt", "characterdata"),
			_shard_rows(read, "itemdata.txt", "itemdata"),
			read("textdata_object.txt"))


def later_tables_from_zip(zip_path):
	with zipfile.ZipFile(zip_path) as archive:
		names = {name.lower(): name for name in archive.namelist()}

		def read(name):
			key = (TEXT_ROOT.as_posix() + "/" + name).lower()
			if key not in names:
				return b""
			return archive.read(names[key])

		return (_shard_rows(read, "characterdata.txt", "characterdata"),
			_shard_rows(read, "itemdata.txt", "itemdata"),
			read("textdata_object.txt"))


def native_tables(native):
	text_root = Path(native) / TEXT_ROOT
	chars = [row for path in sorted(text_root.glob("characterdata_*.txt")) for row in tables.table_rows(path.read_bytes())]
	item_rows = [row for path in sorted(text_root.glob("itemdata_*.txt")) for row in tables.table_rows(path.read_bytes())]
	return chars, item_rows


def read_marker(output, source):
	path = Path(output) / source["report"]
	if not path.exists():
		return {}
	try:
		return json.loads(path.read_text(encoding="utf-8"))
	except (OSError, ValueError):
		return {}


def _encode(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# remove_import
#
# A source's off path: its shards leave the manifests and the tree, and
# its marker goes, leaving every other source's rows untouched.
# ================
def remove_import(output, source):
	text_root = Path(output) / TEXT_ROOT
	for manifest_name, shard in (("characterdata.txt", source["char_shard"]), ("itemdata.txt", source["item_shard"])):
		manifest = text_root / manifest_name
		if manifest.exists():
			kept = [row for row in tables.table_rows(manifest.read_bytes()) if not row or row[0].lower() != shard]
			write_atomically(manifest, _encode(kept))
		(text_root / shard).unlink(missing_ok=True)
	(Path(output) / source["report"]).unlink(missing_ok=True)


def _manifest_with(output, manifest_name, shard):
	manifest = safe_target(Path(output) / TEXT_ROOT, manifest_name)
	rows = tables.table_rows(manifest.read_bytes()) if manifest.exists() else []
	if not any(row and row[0].lower() == shard for row in rows):
		rows.append([shard])
	write_atomically(manifest, _encode(rows))


# ================
# _merge_name_text
#
# Name text for the imported kinds: rows the later object table carries
# under the kinds' name keys, appended to the staging name table where
# the native one lacks them. Missing keys are reported, not fatal (a
# kind whose name never arrived is still listed for the follow-up).
# ================
def _merge_name_text(output, char_rows, later_text, report):
	name_path = safe_target(Path(output) / TEXT_ROOT, "textdataname.txt")
	base = name_path.read_bytes() if name_path.exists() else b""
	known = {row[1] for row in characters.translation_rows(base) if len(row) > 1}
	wanted = {row[CHAR_NAME_KEY] for row in char_rows} - known
	texts = [[row[0], row[1], row[2], "", "", row[5], row[4], row[7], row[8], row[9], ""]
		for row in characters.translation_rows(later_text) if len(row) >= 10 and row[1] in wanted]
	if texts:
		write_atomically(name_path, characters.append_rows(base, texts))
	report["missingNameText"] = sorted(wanted - {row[1] for row in texts})


def _write_pass(output, source, char_rows, item_rows, report):
	text_root = Path(output) / TEXT_ROOT
	if char_rows:
		write_atomically(safe_target(text_root, source["char_shard"]), _encode(sorted(char_rows, key=lambda row: int(row[CHAR_ID]))))
		_manifest_with(output, "characterdata.txt", source["char_shard"])
	else:
		(text_root / source["char_shard"]).unlink(missing_ok=True)
	if item_rows:
		write_atomically(safe_target(text_root, source["item_shard"]), _encode(sorted(item_rows, key=lambda row: int(row[items.IDENTITY]))))
		_manifest_with(output, "itemdata.txt", source["item_shard"])
	else:
		(text_root / source["item_shard"]).unlink(missing_ok=True)
	report["enabled"] = True
	report["source"] = source["name"]
	write_atomically(Path(output) / source["report"], (json.dumps(report, indent=2) + "\n").encode("utf-8"))
	return report


def _disabled(source, note):
	return {"enabled": False, "added": [], "existing": [], "deferred": [], "skipped": [], "source": source["name"], "note": note}


# ================
# add_pets
#
# One source's pass over a client root (pk2 archives). root None, the
# master flag off, or the archives absent removes the source's previous
# import and reports disabled. A pinned source's root is verified
# against the pins file; without the pins file in the tree the pass
# refuses (the supply chain is pinned, never guessed).
# ================
def add_pets(root, native, output, source=VSRO_SOURCE, earlier_codes=(), env=os.environ):
	output = Path(output)
	if root is None or not enabled(env) or not all((Path(root) / f"{name}.pk2").is_file() for name in FOLDERS):
		remove_import(output, source)
		return _disabled(source, "no later client found")
	if source["pins"] is not None:
		if not Path(source["pins"]).is_file():
			remove_import(output, source)
			return _disabled(source, "archive pins not in tree")
		pins = json.loads(Path(source["pins"]).read_text(encoding="utf-8"))
		if pins.get("format") != PIN_FORMAT:
			raise ValueError(f"{source['pins']}: unknown pin format")
		tables.require_vsro_pin(root, tuple(FOLDERS), pins["archives"])
	native_chars, native_items = native_tables(native)
	later_chars, later_items, later_text = later_tables_from_pk2(root)
	res_root = Path(native) / "Data_extracted" / "res"
	native_models = {path.name.lower() for path in res_root.rglob("*.bsr")} if res_root.exists() else set()
	with open(Path(root) / "Data.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
		data_index = tables.archive_index(data)

		def missing(family, rows):
			for row in rows:
				model = row[CHAR_MODEL].replace("\\", "/").lower() if len(row) > CHAR_MODEL else ""
				if not model or (model.rsplit("/", 1)[-1] not in native_models and f"res/{model}" not in data_index):
					return ["model"]
			return []

		prior = read_marker(output, source)
		char_rows, item_rows, report = select_pets(
			native_chars, native_items, set(earlier_codes), later_chars, later_items,
			previous=previous_targets(prior), missing=missing)
	if char_rows:
		_merge_name_text(output, char_rows, later_text, report)
	return _write_pass(output, source, char_rows, item_rows, report)


# ================
# add_archive_pets
#
# The zip source: the pinned opensro pets archive (a zip holding the
# export's tables and resources at their extracted-tree paths). Unset
# ARCHIVE_ENV, a missing zip or the master flag off removes the pass's
# previous import. The zip is read, never the export folder.
# ================
def add_archive_pets(native, output, earlier_codes=(), env=os.environ):
	output = Path(output)
	zip_path = env.get(ARCHIVE_ENV, "").strip()
	if not zip_path or not enabled(env) or not Path(zip_path).is_file():
		remove_import(output, ARCHIVE_SOURCE)
		return _disabled(ARCHIVE_SOURCE, "no pets archive")
	native_chars, native_items = native_tables(native)
	later_chars, later_items, later_text = later_tables_from_zip(zip_path)
	with zipfile.ZipFile(zip_path) as archive:
		members = {name.lower() for name in archive.namelist()}

		def missing(family, rows):
			for row in rows:
				model = row[CHAR_MODEL].replace("\\", "/").lower() if len(row) > CHAR_MODEL else ""
				native_hit = bool(model) and (Path(native) / "Data_extracted" / "res" / model).exists()
				if not model or (not native_hit and f"data_extracted/res/{model}" not in members):
					return ["model"]
			return []

		prior = read_marker(output, ARCHIVE_SOURCE)
		char_rows, item_rows, report = select_pets(
			native_chars, native_items, set(earlier_codes), later_chars, later_items,
			previous=previous_targets(prior), missing=missing)
	if char_rows:
		_merge_name_text(output, char_rows, later_text, report)
		# The zip's resources join the staging tree at their archive
		# paths, so the model builder reads one tree for every source.
		with zipfile.ZipFile(zip_path) as archive:
			for name in archive.namelist():
				if name.lower().startswith("data_extracted/") and not name.endswith("/"):
					target = safe_target(output, name)
					if not target.exists():
						write_atomically(target, archive.read(name))
	return _write_pass(output, ARCHIVE_SOURCE, char_rows, item_rows, report)


# ================
# v1696_root / add_all_sources
#
# The three passes in order over the variant staging tree, each seeing
# the row codenames the earlier ones imported.
# ================
def v1696_root(env=os.environ):
	value = env.get(V1696_ENV, "").strip()
	return Path(value) if value else None


def add_all_sources(vsro_root, native, output, env=os.environ):
	vsro = add_pets(vsro_root, native, output, VSRO_SOURCE, env=env)
	codes = set(read_marker(output, VSRO_SOURCE).get("idMap", {}))
	v1696 = add_pets(v1696_root(env), native, output, V1696_SOURCE, earlier_codes=codes, env=env)
	codes |= set(read_marker(output, V1696_SOURCE).get("idMap", {}))
	archive = add_archive_pets(native, output, earlier_codes=codes, env=env)
	return {"pets": vsro, "petsV1696": v1696, "petsArchive": archive}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--v1696-root", type=Path, default=v1696_root(), help=f"the v1.696 client (else {V1696_ENV})")
	parser.add_argument("--native-extracted", type=Path, default=EXTRACTED_ROOT)
	parser.add_argument("--output-extracted", type=Path, default=REPO_ROOT / ".state" / "pets" / "extracted")
	args = parser.parse_args()
	summaries = add_all_sources(args.vsro_root, args.native_extracted, args.output_extracted)
	for name, summary in summaries.items():
		added = summary.get("added", [])
		print(f"[{name}] enabled: {summary.get('enabled')}; kinds added: {len(added)}; "
			f"existing: {len(summary.get('existing', []))}; deferred: {len(summary.get('deferred', []))}; skipped: {len(summary.get('skipped', []))}")
		for kind in added:
			print(f"  added {kind['family']} ({kind['kind']}, levels {kind['levels']}, scrolls {kind['scrolls']}, needs {kind['needs']})")


if __name__ == "__main__":
	main()

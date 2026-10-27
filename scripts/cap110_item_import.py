"""
===========================================================================

cap110_item_import.py - stage and verify optional degree 10/11 equipment

Port-only, not native. Off unless SRO_CAP110_ITEMS is 1/on/true. Called by
the Alexandria builder after its native links and NPC texts exist. The CLI
builds an isolated overlay for review; it never enables the running stack.
Native files win in resource lookup. Atomic replacement breaks hard links
instead of writing through them. No retail tables are committed to Git.

===========================================================================
"""
import argparse
import base64
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path
import re
import subprocess

import alexandria_npcs as text_tables
import build_alexandria_variant as variant
import cap110_items as items
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

FLAG = "SRO_CAP110_ITEMS"
REPORT_NAME = ".opensro-cap110-items.json"
FOLDERS = {"Data": "Data_extracted", "Media": "Media_extracted", "Particles": "Particles_extracted"}
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
PARSED_EXTENSIONS = (".bsr", ".bmt", ".efp")


# ================
# enabled
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# read_item_tables
#
# Manifest entries are validated basenames, never arbitrary paths.
# ================
def read_item_tables(read):
	names = [row[0].strip() for row in tables.table_rows(read("itemdata.txt"))]
	if not names or any(not re.fullmatch(r"itemdata_\d+\.txt", name, re.IGNORECASE) for name in names):
		raise ValueError("invalid item manifest")
	if len(set(map(sro_pk2.fold_ascii, names))) != len(names):
		raise ValueError("duplicate item manifest entry")
	return [row for name in names for row in tables.table_rows(read(sro_pk2.fold_ascii(name)))]


# ================
# normalized_path
# ================
def normalized_path(value):
	name = sro_pk2.fold_ascii(value.replace("\\", "/"))
	if not name or any(part in ("", ".", "..") or ":" in part for part in name.split("/")):
		raise ValueError(f"unsafe resource path: {value!r}")
	return name


# ================
# resource_dependencies
# ================
def resource_dependencies(requests):
	result = subprocess.run(
		["node", str(REPO_ROOT / "scripts" / "analysis" / "cap110_asset_dependencies.mjs")],
		input=json.dumps(requests), capture_output=True, text=True, encoding="utf-8", check=False
	)
	if result.returncode != 0:
		raise ValueError(f"resource dependency audit failed:\n{result.stderr}")
	dependencies = json.loads(result.stdout)
	if len(dependencies) != len(requests):
		raise ValueError("resource parser response count mismatch")
	return dependencies


# ================
# copy_asset_closure
#
# Parser subprocesses receive data through stdin with no shell. Unknown
# modifier dependencies fail rather than yielding an incomplete certificate.
# ================
def copy_asset_closure(equipment, native, output, archives, worn=True):
	native_paths = {name: {
		normalized_path(path.relative_to(native / folder).as_posix()): path
		for path in (native / folder).rglob("*") if path.is_file()
	} for name, folder in FOLDERS.items()}
	pending, seen, records = set(items.direct_assets(equipment)), set(), []
	# worn is false for rows that are carried, not worn (alchemy materials,
	# tokens): they have an icon and a drop model but no worn mesh.
	# A comma pair (set armour's plain and full-set model) was already cut
	# to its plain path (items.plain_worn_models); one cell is one path.
	worn_models = resource_dependencies([{"weapon": {
		"resolvedModel": "res/" + normalized_path(row[items.MODEL]),
		"tid": list(map(int, row[items.TYPE_FIRST:items.TYPE_FOURTH + 1]))
	}} for row in equipment if row[items.MODEL] not in ("", items.UNUSED)]) if worn else []
	for row in equipment if worn else ():
		# Authored circlet rows (armor head kind 1) and jewelry have no worn
		# mesh. Keep that absence, as the native equipment resolver does.
		model_optional = row[items.TYPE_THIRD] in items.ACCESSORY_TYPES or (
			row[items.TYPE_THIRD] in items.ARMOR_TYPES and row[items.TYPE_FOURTH] == "1"
		)
		if row[items.MODEL] in ("", items.UNUSED) and not model_optional:
			raise ValueError(f"equipment has no worn model: {row[items.CODENAME]}")
	for names in worn_models:
		pending.update(("Data", normalized_path(name)) for name in names)
	while pending:
		batch, requests = sorted(pending), []
		pending = set()
		for archive_name, source in batch:
			name = normalized_path(source)
			data, index = archives[archive_name]
			if archive_name == "Particles" and "/" not in name and name not in native_paths[archive_name] and name not in index:
				matches = {path for path in native_paths[archive_name].keys() | index.keys() if path.endswith("/" + name)}
				if len(matches) != 1:
					raise ValueError(f"ambiguous or missing particle resource: {name}")
				name = matches.pop()
			key = (archive_name, name)
			if key in seen:
				continue
			seen.add(key)
			local = native_paths[archive_name].get(name)
			entry = index.get(name)
			if local is not None:
				raw, origin = local.read_bytes(), "native"
			elif entry is not None:
				raw, origin = sro_pk2.payload(data, entry), "later"
			else:
				raise ValueError(f"missing {archive_name} resource: {name}")
			write_atomically(safe_target(output / FOLDERS[archive_name], name), raw)
			records.append({"archive": archive_name, "path": name, "source": origin, "sha256": sro_pk2.digest(raw)})
			if name.endswith(PARSED_EXTENSIONS):
				requests.append({"path": name, "bytes": base64.b64encode(raw).decode("ascii"), "native": origin == "native"})
		if requests:
			for names in resource_dependencies(requests):
				pending.update(
					(name["archive"], normalized_path(name["path"])) if isinstance(name, dict)
					else ("Data", normalized_path(name)) for name in names
				)
	return records


# ================
# add_items
#
# A successful marker owns exactly one generated shard. Off removes that
# shard and restores the native manifest, preventing stale imports on rebuild.
# ================
def add_items(vsro_root, native, output):
	native, output = native.resolve(), output.resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("item output must be separate from the native extraction")
	marker = output / REPORT_NAME
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(output / TEXT_ROOT, items.OUTPUT_SHARD)
	manifest = safe_target(output / TEXT_ROOT, "itemdata.txt")
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked item import shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed imported shard")
	if not enabled():
		if marker.exists():
			if shard.exists():
				shard.unlink()
			for name in (items.SET_GROUP_TABLE, items.MAGIC_GROUP_TABLE):
				catalogue = safe_target(output / TEXT_ROOT, name)
				if catalogue.exists():
					catalogue.unlink()
			write_atomically(manifest, (native / TEXT_ROOT / "itemdata.txt").read_bytes())
			marker.unlink()
		return {"enabled": False, "added": 0}
	with ExitStack() as stack:
		archives, identities = {}, {}
		for name in FOLDERS:
			handle = stack.enter_context(open(Path(vsro_root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			index = {normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}
			archives[name] = (data, index)
			identities[name] = tables.sha256_of(data)
		media, index = archives["Media"]
		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)
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
		native_rows = read_item_tables(read_native)
		later_rows = read_item_tables(read_later)
		reserved = {
			int(row[1]) for path in (output / TEXT_ROOT).glob("characterdata_*.txt")
			for row in tables.table_rows(path.read_bytes()) if len(row) > 1
		}
		equipment, summary = items.merge_equipment(native_rows, later_rows, reserved, prior.get("idMap"))
		summary["fullSetModelsDropped"] = items.plain_worn_models(equipment)
		set_groups, set_magic, catalogue = items.set_catalogue(
			tables.table_rows(read_later(items.SET_GROUP_TABLE)),
			tables.table_rows(read_later(items.MAGIC_GROUP_TABLE)),
			tables.table_rows(read_later(items.MAGIC_OPTION_TABLE)),
			equipment,
		)
		text_path = safe_target(output / TEXT_ROOT, "textdataname.txt")
		base_text = text_path.read_bytes() if text_path.exists() else read_native("textdataname.txt")
		later_texts = [
			row for name in ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
			for row in tables.table_rows(read_later(name))
		]
		merged_text, missing_descriptions = items.merge_texts(base_text, later_texts, equipment)
		shops = items.shop_dependencies({
			name: tables.table_rows(read_later(name))
			for name in ("refscrapofpackageitem.txt", "refshopgoods.txt", "refpricepolicyofitem.txt", "shopitemdata.txt")
		}, equipment, {row[items.CODENAME] for row in native_rows}, {
			identity["source"]: code for code, identity in summary["idMap"].items()
		})
		assets = copy_asset_closure(equipment, native, output, archives)
		raw_shard = b"\xff\xfe" + text_tables.table_text(equipment).encode("utf-16-le")
		base_manifest = tables.decode_table(read_native("itemdata.txt")).rstrip("\r\n")
		if items.OUTPUT_SHARD in sro_pk2.fold_ascii(base_manifest):
			raise ValueError("native item manifest already owns the import shard")
		write_atomically(shard, raw_shard)
		write_atomically(
			safe_target(output / TEXT_ROOT, items.SET_GROUP_TABLE),
			b"\xff\xfe" + text_tables.table_text(set_groups).encode("utf-16-le"),
		)
		write_atomically(
			safe_target(output / TEXT_ROOT, items.MAGIC_GROUP_TABLE),
			b"\xff\xfe" + text_tables.table_text(set_magic).encode("utf-16-le"),
		)
		write_atomically(text_path, merged_text)
		write_atomically(manifest, b"\xff\xfe" + (base_manifest + "\r\n" + items.OUTPUT_SHARD + "\r\n").encode("utf-16-le"))
		summary.update({
			"enabled": True, "format": "opensro-cap110-equipment-v2", "portOnly": True,
			"laterArchiveHashes": identities, "shardSha256": sro_pk2.digest(raw_shard),
			"nativeItemHashes": native_hashes,
			"absentAuthoredDescriptions": missing_descriptions,
			"assets": assets, "shopsImported": False,
			"shopDependencies": shops,
			"setCatalogue": catalogue,
			"limits": [
				"Set rows and their group catalogue are dormant: no set bonuses, shop stock or drops are added.",
				"Render resource closure is verified; unchanged native particle/audio modifiers retain their existing owners.",
				"Ambient equipment particles are imported through the shared EFP parser; new sound modifiers are refused.",
				"Rendered appearance and combat require playtesting."
			]
		})
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode("utf-8"))
		return summary


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--native-extracted", type=Path, default=EXTRACTED_ROOT)
	parser.add_argument("--output-extracted", type=Path, default=REPO_ROOT / ".state" / "cap110-weapons" / "extracted")
	args = parser.parse_args()
	if enabled():
		tables.require_vsro_pin(args.vsro_root, tuple(FOLDERS))
	summary = add_items(args.vsro_root, args.native_extracted, args.output_extracted)
	print(f"Equipment: {summary['added']}; enabled: {summary['enabled']}")
	if summary["enabled"]:
		print(f"Categories: {summary['categories']}")
		print(f"Set items: {summary['setCatalogue']['setItems']}; set groups: {summary['setCatalogue']['groups']}; verified resources: {len(summary['assets'])}")
		print(f"Report: {args.output_extracted / REPORT_NAME}")


if __name__ == "__main__":
	main()

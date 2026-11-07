"""
===========================================================================

mall_avatar_import.py - stage the later client's mall avatars (outfits, hats, attachments)

Port-only, not native. On unless SRO_MALL_AVATARS is 0/off/false/no: the
owner approved these cosmetics behind a flag that defaults on (2026-11-04).
Called by the Alexandria builder after the cap-110 item import. The v1.150
client lacks about 150 mall avatar rows the later (VSRO) client has, among
them the Ottoman ("Turkey") set.

Selection is by item type and codename: a service-enabled avatar row
(type 3/1/13) named ITEM_MALL_AVATAR_*, not in the native tables, not a
test row. Event copies (ITEM_EVENT_*) share the models but belong to events
this port does not have, so they stay out. A row whose model or icon is
absent from both the later archives and the native tree, or whose name text
is missing, is skipped and reported, never half imported (the Nasrun mounts
are the known case).

Rows keep their later-client identity when it is free in the native tables
and the character data; the few that clash take a fresh one above every
known identity, remembered in the marker so a rebuild never moves an item.
Native files win in resource lookup; resources the native tree lacks are
copied from the later archives, pinned by hash by the caller. Each avatar
the later client's Item Mall sold is listed in the matching mall tab at its
authored silk price; the rest are carried by GM grant only. Nothing here
adds a drop or an event reward. No retail table is committed to Git.

===========================================================================
"""
import argparse
import collections
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path
import re

import alexandria_npcs as text_tables
import cap110_item_import as shared
import cap110_items as items
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

FLAG = "SRO_MALL_AVATARS"
FLAG_OFF = ("0", "off", "false", "no")
REPORT_NAME = ".opensro-mall-avatars.json"
OUTPUT_SHARD = "itemdata_100000.txt"
FOLDERS = shared.FOLDERS
TEXT_ROOT = shared.TEXT_ROOT
CODE_PREFIX = "ITEM_MALL_AVATAR_"
EXCLUDED_WORDS = ("_TEST",)
AVATAR_TYPES = ["3", "1", "13"]
SERVICE_ON = "1"
FRESH_ID_BASE = 50000
HAT_SUFFIX = "_HAT"
# Rows whose model the v1.150 item-model builder refuses: their material has an
# animated or material-scoped environment map (characterMaterialState.mjs
# "needs runtime ownership"), and one such model aborts the whole asset
# build. Found by screening every imported avatar model's environment
# modifiers; revisit if that builder learns the modifier.
RENDER_UNSUPPORTED = {
	"ITEM_MALL_AVATAR_M_EARTHSPIRIT_HAT": "environment-mapped material",
	"ITEM_MALL_AVATAR_W_EARTHSPIRIT_HAT": "environment-mapped material",
}
# Package, scrap, price and goods tables with the column holding the package code.
MALL_TABLES = (
	("refpackageitem.txt", 3), ("refscrapofpackageitem.txt", 2),
	("refpricepolicyofitem.txt", 2), ("refshopgoods.txt", 3),
)
MALL_TAB_FOR = {"MALL_AVATAR_DRESS": "MALL_AVATAR_DRESS", "MALL_AVATAR_HAT": "MALL_AVATAR_HAT", "MALL_AVATAR_ATTACH": "MALL_AVATAR_HAT"}
NATIVE_SOURCE = "11"
# The silk price of every listed avatar is the v1.150 mall's own tier, not the
# later client's authored amount (owner, 2026-11-04: "same as the others"):
# the European wedding dress sells at 130 and its hat at 30, and the native
# Halloween attachment is priced 15 (mallextra.go carries the same figures).
SILK_CURRENCY = "2"
SILK_PRICE = {"dress": "130", "hat": "30", "attach": "15"}
HAT_CODE = re.compile(r"_HAT(_|$)")
ATTACH_CODE = re.compile(r"_ATTACH(_|$)")
PACKAGE_FLAG_COLUMN = 4
MALL_PRICE_AMOUNT = 5


# ================
# enabled
#
# Unlike the cap-110 switches this one defaults on: an unset or empty value
# is on, and only an explicit off value turns it off. An unreadable value is
# refused rather than guessed.
# ================
def enabled(env=os.environ):
	value = env.get(FLAG, "").strip().lower()
	if value in ("", "1", "on", "true", "yes"):
		return True
	if value in FLAG_OFF:
		return False
	raise ValueError(f"{FLAG} must be on or off, not {value!r}")


# ================
# candidate
# ================
def candidate(row):
	code = row[items.CODENAME]
	return (
		row[items.SERVICE] == SERVICE_ON and row[items.TYPE_FIRST:items.TYPE_THIRD + 1] == AVATAR_TYPES
		and code.startswith(CODE_PREFIX) and not any(word in code for word in EXCLUDED_WORDS)
	)


# ================
# previous_targets
#
# Reads a marker's identity map. The first (Turkey-only) marker stored a
# bare number per code, which was the later identity; both forms are read.
# ================
def previous_targets(prior):
	result = {}
	for code, entry in prior.get("idMap", {}).items():
		result[code] = int(entry["target"]) if isinstance(entry, dict) else int(entry)
	return result


# ================
# select_avatars
#
# missing(row) returns the names of a row's absent resources. Identities:
# a retained one first, else the later one when free, else a fresh one.
# ================
def select_avatars(native_rows, later_rows, reserved_ids=(), previous=None, missing=lambda row: [], texts=None):
	native, later = items.index_items(native_rows), items.index_items(later_rows)
	taken = {int(row[items.IDENTITY]) for row in native_rows} | set(reserved_ids)
	previous = previous or {}
	chosen, skipped, existing = [], [], []
	for code, row in sorted(later.items()):
		if not candidate(row):
			continue
		if code in native:
			existing.append(code)
			continue
		absent = missing(row)
		if code in RENDER_UNSUPPORTED:
			absent = absent + [RENDER_UNSUPPORTED[code]]
		if texts is not None and row[items.NAME_KEY] not in texts:
			absent = absent + ["name text"]
		if absent:
			skipped.append({"code": code, "missing": absent})
			continue
		chosen.append(row)
	if not chosen:
		raise ValueError("no mall avatar rows to import")
	targets = {}
	for row in chosen:
		code = row[items.CODENAME]
		retained = previous.get(code)
		if retained is not None and retained not in taken:
			targets[code] = retained
			taken.add(retained)
	for row in chosen:
		code, source = row[items.CODENAME], int(row[items.IDENTITY])
		if code not in targets and source not in taken:
			targets[code] = source
			taken.add(source)
	fresh = max(FRESH_ID_BASE, max(taken) + 1)
	for row in chosen:
		code = row[items.CODENAME]
		if code not in targets:
			while fresh in taken:
				fresh += 1
			targets[code] = fresh
			taken.add(fresh)
	added = []
	for row in chosen:
		converted = list(row)
		converted[items.IDENTITY] = str(targets[row[items.CODENAME]])
		converted[items.PLACEHOLDER_NAME] = items.UNUSED
		added.append(converted)
	if len({int(row[items.IDENTITY]) for row in added}) != len(added) or any(not 0 < int(row[items.IDENTITY]) <= items.MAX_ID for row in added):
		raise ValueError("avatar identities collide or exceed the wire width")
	return added, {
		"added": len(added), "existingNative": existing, "skipped": skipped,
		"hats": sum(row[items.CODENAME].endswith(HAT_SUFFIX) for row in added),
		"idMap": {
			row[items.CODENAME]: {"source": int(later[row[items.CODENAME]][items.IDENTITY]), "target": int(row[items.IDENTITY])}
			for row in added
		},
		"preservedNativeRows": len(native_rows),
	}


# ================
# strip_mall
#
# Drops the rows a previous run added to the four mall tables, by package
# code, leaving every other line (including comments) untouched.
# ================
def strip_mall(output, packages):
	if not packages:
		return
	for name, column in MALL_TABLES:
		path = safe_target(output / TEXT_ROOT, name)
		if not path.exists():
			continue
		lines = tables.decode_table(path.read_bytes()).splitlines()
		kept = [line for line in lines if not (len(line.split("\t")) > column and line.split("\t")[column] in packages)]
		if len(kept) != len(lines):
			write_atomically(path, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))


# ================
# avatar_kind
# ================
def avatar_kind(code):
	if ATTACH_CODE.search(code):
		return "attach"
	return "hat" if HAT_CODE.search(code) else "dress"


# ================
# mall_rows
#
# The rows that list the imported avatars in the Item Mall, for the avatars
# the later client's mall sold. The later price table carries one extra
# column before the amount, dropped to the native layout. The v1.150 mall
# has no attachment tab, so attachments take the hat tab (the owner's
# 2026-10-13 mall-extra rule). Silk amounts follow the native tier. Slots follow the later order, after the
# tab's present slots. Avatars with no package, no sale or no price stay
# unlisted and are reported.
# ================
def mall_rows(later, avatars, current, native_package_ids):
	scrap_of = {row[3]: row[2] for row in later["refscrapofpackageitem.txt"] if row[0] == SERVICE_ON and len(row) >= 20}
	package_row = {row[3]: row for row in later["refpackageitem.txt"] if row[0] == SERVICE_ON and len(row) >= 9}
	sale = {row[3]: (row[2], int(row[4])) for row in later["refshopgoods.txt"] if row[0] == SERVICE_ON and len(row) >= 5}
	prices = collections.defaultdict(list)
	for row in later["refpricepolicyofitem.txt"]:
		if row[0] == SERVICE_ON and len(row) > MALL_PRICE_AMOUNT:
			prices[row[2]].append(row)
	next_slot = collections.Counter()
	for row in current["refshopgoods.txt"]:
		if len(row) >= 5 and row[2] in MALL_TAB_FOR.values():
			next_slot[row[2]] = max(next_slot[row[2]], int(row[4]) + 1)
	rows = {name: [] for name, _ in MALL_TABLES}
	packages, unlisted = [], []
	order = sorted(avatars, key=lambda row: (sale.get(scrap_of.get(row[items.CODENAME]), ("", 0))[1], row[items.CODENAME]))
	for row in order:
		code = row[items.CODENAME]
		package = scrap_of.get(code)
		if package is None or package not in package_row:
			unlisted.append({"code": code, "reason": "no package"})
			continue
		if package not in sale:
			unlisted.append({"code": code, "reason": "not sold in the later mall"})
			continue
		tab = MALL_TAB_FOR.get(sale[package][0])
		if tab is None:
			unlisted.append({"code": code, "reason": "tab " + sale[package][0]})
			continue
		if not prices[package]:
			unlisted.append({"code": code, "reason": "no price"})
			continue
		if int(package_row[package][2]) in native_package_ids:
			raise ValueError(f"package identity clash: {package}")
		package_cells = list(package_row[package])
		package_cells[1], package_cells[PACKAGE_FLAG_COLUMN] = NATIVE_SOURCE, "0"
		rows["refpackageitem.txt"].append(package_cells)
		for scrap in later["refscrapofpackageitem.txt"]:
			if scrap[0] == SERVICE_ON and len(scrap) >= 20 and scrap[2] == package:
				rows["refscrapofpackageitem.txt"].append(scrap[:1] + [NATIVE_SOURCE] + scrap[2:])
		for price in prices[package]:
			converted = price[:1] + [NATIVE_SOURCE] + price[2:MALL_PRICE_AMOUNT - 1] + price[MALL_PRICE_AMOUNT:]
			if converted[3] == SILK_CURRENCY:
				converted[4] = SILK_PRICE[avatar_kind(code)]
			rows["refpricepolicyofitem.txt"].append(converted)
		rows["refshopgoods.txt"].append(["1", NATIVE_SOURCE, tab, package, str(next_slot[tab])] + ["-1", items.UNUSED] * 4)
		next_slot[tab] += 1
		packages.append(package)
	return rows, {"packages": packages, "listed": len(packages), "unlisted": unlisted}


# ================
# remove_import
#
# Off (or a rebuild) drops the shard, its manifest line and its mall rows,
# so a reused variant cannot keep a previous on build.
# ================
def remove_import(output, marker, shard, manifest):
	if marker.exists():
		strip_mall(output, set(json.loads(marker.read_text(encoding="utf-8")).get("mall", {}).get("packages", [])))
	if shard.exists():
		shard.unlink()
	if manifest.exists():
		names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
		kept = [name for name in names if sro_pk2.fold_ascii(name) != OUTPUT_SHARD]
		if len(kept) != len(names):
			write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(kept) + "\r\n").encode("utf-16-le"))
	if marker.exists():
		marker.unlink()


# ================
# resource_check
#
# The row's worn model, drop model and icon must exist in the later archives
# or the native tree. Deeper dependencies (meshes, textures) are verified by
# copy_asset_closure afterwards, which fails the build if one is absent.
# ================
def resource_check(archives, native_paths):
	def has(archive, path):
		return path in archives[archive][1] or path in native_paths[archive]
	def missing(row):
		absent = []
		model = row[items.MODEL].split(",")[0].strip()
		if model in ("", items.UNUSED) or not has("Data", "res/" + shared.normalized_path(model)):
			absent.append("model")
		if row[items.DROP_MODEL] in ("", items.UNUSED) or not has("Data", "res/" + shared.normalized_path(row[items.DROP_MODEL])):
			absent.append("drop model")
		if row[items.ICON] in ("", items.UNUSED) or not has("Media", "icon/" + shared.normalized_path(row[items.ICON])):
			absent.append("icon")
		return absent
	return missing


# ================
# add_avatars
#
# A successful marker owns exactly one generated shard. The manifest is read
# from the output when the cap-110 import already wrote it, else from the
# native tree, and gains one line.
# ================
def add_avatars(vsro_root, native, output):
	native, output = native.resolve(), output.resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("avatar output must be separate from the native extraction")
	marker = output / REPORT_NAME
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(output / TEXT_ROOT, OUTPUT_SHARD)
	manifest = safe_target(output / TEXT_ROOT, "itemdata.txt")
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked avatar import shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed imported shard")
	if not enabled():
		remove_import(output, marker, shard, manifest)
		return {"enabled": False, "added": 0}
	if not all((Path(vsro_root) / f"{name}.pk2").is_file() for name in FOLDERS):
		remove_import(output, marker, shard, manifest)
		return {"enabled": False, "added": 0, "skipped": "no later client found"}
	with ExitStack() as stack:
		archives, identities = {}, {}
		for name in FOLDERS:
			handle = stack.enter_context(open(Path(vsro_root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			archives[name] = (data, {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
			identities[name] = tables.sha256_of(data)
		media, index = archives["Media"]
		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)
		base = native / TEXT_ROOT
		base_paths = {sro_pk2.fold_ascii(path.name): path for path in base.iterdir() if path.is_file()}
		# ================
		# read_native
		# ================
		def read_native(name):
			return base_paths[name].read_bytes()
		native_rows = shared.read_item_tables(read_native)
		later_rows = shared.read_item_tables(read_later)
		reserved = {
			int(row[1]) for path in (output / TEXT_ROOT).glob("characterdata_*.txt")
			for row in tables.table_rows(path.read_bytes()) if len(row) > 1
		}
		# The cap-110 shard's identities sit above every later row and below
		# FRESH_ID_BASE; they are reserved anyway so a clash cannot happen.
		other_shard = output / TEXT_ROOT / items.OUTPUT_SHARD
		if other_shard.exists():
			reserved |= {int(row[items.IDENTITY]) for row in tables.table_rows(other_shard.read_bytes())}
		native_paths = {
			name: {
				shared.normalized_path(path.relative_to(native / folder).as_posix())
				for path in (native / folder).rglob("*") if path.is_file()
			} for name, folder in FOLDERS.items()
		}
		later_texts = [
			row for name in ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
			for row in tables.table_rows(read_later(name))
		]
		text_path = safe_target(output / TEXT_ROOT, "textdataname.txt")
		base_text = text_path.read_bytes() if text_path.exists() else read_native("textdataname.txt")
		known_text = {row[1] for row in tables.table_rows(base_text) if len(row) > 1} | {row[1] for row in later_texts if len(row) > 1}
		avatars, summary = select_avatars(
			native_rows, later_rows, reserved, previous_targets(prior),
			resource_check(archives, native_paths), known_text,
		)
		merged_text, missing_descriptions = items.merge_texts(base_text, later_texts, avatars)
		assets = shared.copy_asset_closure(avatars, native, output, archives)
		strip_mall(output, set(prior.get("mall", {}).get("packages", [])))
		mall_files = {name: safe_target(output / TEXT_ROOT, name) for name, _ in MALL_TABLES}
		mall_summary = {"packages": [], "listed": 0, "unlisted": [], "note": "no mall tables in the later client or native tree"}
		if all(sro_pk2.fold_ascii(name) in base_paths and tables.TEXTDATA + name in index for name, _ in MALL_TABLES):
			current = {
				name: tables.table_rows(mall_files[name].read_bytes() if mall_files[name].exists() else read_native(name))
				for name, _ in MALL_TABLES
			}
			later_mall = {name: tables.table_rows(read_later(name)) for name, _ in MALL_TABLES}
			mall, mall_summary = mall_rows(later_mall, avatars, current, {int(row[2]) for row in current["refpackageitem.txt"] if len(row) > 2})
			for name, _ in MALL_TABLES:
				raw = mall_files[name].read_bytes() if mall_files[name].exists() else read_native(name)
				write_atomically(mall_files[name], text_tables.append_rows(raw, mall[name]))
		raw_shard = b"\xff\xfe" + text_tables.table_text(avatars).encode("utf-16-le")
		current = manifest.read_bytes() if manifest.exists() else read_native("itemdata.txt")
		names = [row[0] for row in tables.table_rows(current) if row]
		names = [name for name in names if sro_pk2.fold_ascii(name) != OUTPUT_SHARD]
		write_atomically(shard, raw_shard)
		write_atomically(text_path, merged_text)
		write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(names + [OUTPUT_SHARD]) + "\r\n").encode("utf-16-le"))
		summary.update({
			"enabled": True, "format": "opensro-mall-avatars-v1", "portOnly": True,
			"laterArchiveHashes": identities, "shardSha256": sro_pk2.digest(raw_shard),
			"absentAuthoredDescriptions": missing_descriptions,
			"assets": assets, "mall": mall_summary,
			"limits": [
				"Listed in the Item Mall only where the later client's mall sold them; the rest are granted by GM. No drop or event reward is added.",
				"Event copies and rows with absent resources (the Nasrun mounts) are not imported.",
				"Rendered appearance in each slot requires playtesting."
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
	parser.add_argument("--output-extracted", type=Path, default=REPO_ROOT / ".state" / "mall-avatars" / "extracted")
	args = parser.parse_args()
	if enabled() and all((args.vsro_root / f"{name}.pk2").is_file() for name in FOLDERS):
		tables.require_vsro_pin(args.vsro_root, tuple(FOLDERS))
	summary = add_avatars(args.vsro_root, args.native_extracted, args.output_extracted)
	print(f"Avatars: {summary['added']}; enabled: {summary['enabled']}")
	if summary["enabled"]:
		print(f"Hats: {summary['hats']}; skipped: {len(summary['skipped'])}; verified resources: {len(summary['assets'])}")
		print(f"Item Mall: {summary['mall']['listed']} listed; {len(summary['mall']['unlisted'])} unlisted")
		for entry in summary["skipped"]:
			print(f"  skipped {entry['code']}: {', '.join(entry['missing'])}")
		print(f"Report: {args.output_extracted / REPORT_NAME}")
	elif "skipped" in summary:
		print(summary["skipped"])


if __name__ == "__main__":
	main()

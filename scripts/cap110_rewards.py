"""
===========================================================================

cap110_rewards.py - optional database-authored monster rewards

Port-only, not native. Rides SRO_CAP110_MONSTERS, because it describes the
monsters that flag (and SRO_PHARAOH_TOMB) install. The loot catalogue treats
the native v1.150 client as its content boundary, so every reward rule of a
monster the native client lacks was dropped. This carries the Alexandria and
tomb monsters' own rules instead: the fixed drops (_RefMonster_AssignedItemDrop),
the random groups (_RefMonster_AssignedItemRndDrop, _RefDropItemGroup), and the
few ETC items they name that the native item table does not have (alchemy
materials, tokens). The qinshi family's rules (#1800 step 2) ride the same
projection: its monsters join the rewarded population when its nest file
does, and a rule the server cannot represent refuses exactly as the field
family's does. The supplied exports are data, never executable input.

The rules are written as cap110-rewards.txt, which the server reads at startup
(loot/cap110.go). The imported items go in their own item shard.

===========================================================================
"""
import argparse
from contextlib import ExitStack
import json
import mmap
import os
from pathlib import Path

import alexandria_npcs as characters
import cap110_item_import as item_import
import cap110_items as items
import cap110_monsters as monsters
import import_vsro_alexandria as tables
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

MARKER = ".opensro-cap110-rewards.json"
REWARDS_FILE = "cap110-rewards.txt"
OUTPUT_SHARD = "itemdata_110001.txt"
TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA
REWARDS_VERSION = 1
# Source group ids are offset into a range the native groups never use, so an
# imported group can neither replace nor join a native one.
GROUP_BASE = 1000000
# The ETC item kinds the rewards name: alchemy materials and SD tokens. Any
# other kind is a different mechanic and refuses the import.
ITEM_KINDS = (("3", "3", "11", "4"), ("3", "3", "5", "1"))
MAGIC_COLUMNS = tuple("m" + str(index) for index in range(1, 10))


# ================
# checked_chance
# ================
def checked_chance(value, what):
	if not 0 < float(value) <= 1:
		raise ValueError(f"reward chance out of range for {what}: {value}")
	return float(value)


# ================
# checked_amounts
# ================
def checked_amounts(low, high, what):
	if not 0 <= int(low) <= int(high) <= 255:
		raise ValueError(f"reward amount out of range for {what}: {low}-{high}")
	return int(low), int(high)


# ================
# project_rewards
#
# The rules of the given monsters, in the shape the server reads. Anything
# the server cannot represent (modifiers, rent codes, magic groups, spare
# parameters) refuses instead of shipping a different reward than authored.
# ================
def project_rewards(fixed, random, groups, population):
	rules = {"Version": REWARDS_VERSION, "Fixed": [], "Random": [], "Groups": {}}
	for row in fixed:
		if row["monster"] not in population:
			continue
		what = row["monster"] + "/" + str(row["item"])
		if row["item"] is None or row["group_type"] != 0 or row["rent"] not in (None, "", "xxx") \
			or any(row[column] for column in MAGIC_COLUMNS):
			raise ValueError("unsupported fixed reward: " + what)
		low, high = checked_amounts(row["amount_min"], row["amount_max"], what)
		rules["Fixed"].append({"Monster": row["monster"], "Item": row["item"], "Plus": int(row["plus"]),
			"Min": low, "Max": high, "Probability": checked_chance(row["ratio"], what)})
	members = {}
	for row in groups:
		if row["service"] == 1:
			members.setdefault(row["item_group"], []).append(row)
	for row in random:
		if row["monster"] not in population or row["service"] != 1:
			continue
		what = row["monster"] + "/group " + str(row["item_group"])
		if row["param1"] or row["param2"] or row["item_group"] not in members:
			raise ValueError("unsupported random reward: " + what)
		low, high = checked_amounts(row["amount_min"], row["amount_max"], what)
		rules["Random"].append({"Monster": row["monster"], "Group": GROUP_BASE + row["item_group"],
			"Distinct": bool(row["distinct_items"]), "Min": low, "Max": high,
			"Probability": checked_chance(row["ratio"], what)})
	for group in sorted({rule["Group"] - GROUP_BASE for rule in rules["Random"]}):
		listed = []
		for row in members[group]:
			# The magic group column only shapes equipment; it carries the group's
			# own id here. Every reward item is checked to be an ETC kind instead
			# (select_imports), so no equipment can reach it.
			if row["item"] is None:
				raise ValueError(f"unsupported reward group member in group {group}")
			listed.append({"Codename": row["item"], "Probability": checked_chance(row["ratio"], f"group {group} member")})
		rules["Groups"][str(GROUP_BASE + group)] = listed
	rules["Fixed"].sort(key=lambda rule: (rule["Monster"], rule["Item"]))
	rules["Random"].sort(key=lambda rule: (rule["Monster"], rule["Group"]))
	return rules


# ================
# reward_items
# ================
def reward_items(rules):
	named = {rule["Item"] for rule in rules["Fixed"]}
	for listed in rules["Groups"].values():
		named.update(member["Codename"] for member in listed)
	return sorted(named)


# ================
# select_imports
#
# The later client's rows for the reward items the native table lacks. A
# native row wins. An item in neither client, or of a kind other than an
# alchemy material or token (native or not), refuses.
# ================
def select_imports(named, later, native):
	selected = []
	for code in named:
		row = native.get(code) or later.get(code)
		if row is None or row[items.SERVICE] != "1":
			raise ValueError("reward item is in neither client: " + code)
		if tuple(row[items.TYPE_FIRST:items.TYPE_FOURTH + 1]) not in ITEM_KINDS:
			raise ValueError("reward item is not an alchemy material or token: " + code)
		if code in native:
			continue
		# The parameter columns of a material hold its alchemy elements, a count
		# and a native element item each, beside text labels. A link to an item
		# the native table lacks would dangle, so it refuses.
		for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
			link = row[column + 1]
			if link.startswith("ITEM_") and link not in native:
				raise ValueError(f"reward item links an item the native table lacks: {code} -> {link}")
		selected.append(row)
	return selected


# ================
# convert_rows
#
# Fresh identities, retained across rebuilds through the marker. Source
# identities from the later client never become runtime identities.
# ================
def convert_rows(selected, occupied, previous):
	targets = monsters.allocate_identities(occupied, [row[items.CODENAME] for row in selected], previous)
	converted = []
	for row in selected:
		copy = list(row)
		copy[items.IDENTITY], copy[items.PLACEHOLDER_NAME] = str(targets[row[items.CODENAME]]), items.UNUSED
		converted.append(copy)
	return converted


# ================
# occupied_identities
#
# Items, characters and teleport buildings share one reference space; every
# table of the output and of the later client is reserved, except our shard.
# ================
def occupied_identities(text_root, later_rows):
	occupied = {int(row[items.IDENTITY]) for row in later_rows}
	for path in text_root.iterdir():
		name = path.name.lower()
		if name == OUTPUT_SHARD or not name.startswith(("itemdata_", "characterdata_", "teleportbuilding")):
			continue
		occupied.update(int(row[1]) for row in tables.table_rows(path.read_bytes()) if len(row) > 1 and row[1].isdigit())
	return occupied


# ================
# population_codes
#
# The monsters this build installed: the field and, when asked, the
# tomb and the qinshi family (#1800 step 2). A family's nest file is
# present exactly when its switch built it, so presence is the ask.
# ================
def population_codes(text_root):
	codes = set()
	for name in (monsters.POPULATION_FILE, monsters.TOMB_FILE, monsters.QINSHI_FILE):
		path = text_root / name
		if path.exists():
			codes.update(row["Codename"] for row in json.loads(path.read_text(encoding="utf-8"))["Rows"])
	return codes


# ================
# remove_owned
# ================
def remove_owned(text_root, prior):
	(text_root / REWARDS_FILE).unlink(missing_ok=True)
	(text_root / OUTPUT_SHARD).unlink(missing_ok=True)
	manifest = text_root / "itemdata.txt"
	if manifest.exists():
		lines = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row and row[0].lower() != OUTPUT_SHARD]
		write_atomically(manifest, b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
	text_path = text_root / "textdataname.txt"
	owned = set(prior.get("nameSymbols", []))
	if owned and text_path.exists():
		kept = [row for row in characters.translation_rows(text_path.read_bytes()) if len(row) < 2 or row[1] not in owned]
		write_atomically(text_path, monsters.encode_rows(kept))


# ================
# add_rewards
# ================
def add_rewards(vsro_root, native, output, source_root=None):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("reward overlay must be separate from native extraction")
	text_root, marker = output / TEXT_ROOT, output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	shard = safe_target(text_root, OUTPUT_SHARD)
	if shard.exists():
		if not marker.exists():
			raise ValueError("output already owns an unmarked reward item shard")
		if sro_pk2.digest(shard.read_bytes()) != prior["shardSha256"]:
			raise ValueError("refusing to replace a locally changed reward item shard")
	if os.environ.get(monsters.FLAG, "").lower() not in tables.FLAG_ON:
		if marker.exists():
			remove_owned(text_root, prior)
			marker.unlink()
		return {"enabled": False}
	source_root = Path(source_root or os.environ[monsters.SOURCE_ENV])
	monsters.require_pinned_export(source_root)
	# ================
	# read
	# ================
	def read_export(name):
		return json.loads((source_root / name).read_text(encoding="utf-8"))
	population = population_codes(text_root)
	if not population:
		raise ValueError("no installed Alexandria monster population to reward")
	rules = project_rewards(read_export("vsro-assigned-fixed.json"), read_export("vsro-assigned-random.json"),
		read_export("vsro-assigned-groups.json"), population)
	if not rules["Fixed"] and not rules["Random"]:
		raise ValueError("the installed monsters have no database rewards")
	with ExitStack() as stack:
		archives, identities = {}, {}
		for name in item_import.FOLDERS:
			handle = stack.enter_context(open(Path(vsro_root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			archives[name] = (data, {item_import.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
			identities[name] = tables.sha256_of(data)
		media, index = archives["Media"]
		# ================
		# read_later
		# ================
		def read_later(name):
			return tables.table_text(media, index, name)
		later_rows = item_import.read_item_tables(read_later)
		later = items.index_items(later_rows)
		base = native / TEXT_ROOT
		native_items = items.index_items(item_import.read_item_tables(lambda name: (base / name).read_bytes()))
		native_codes = set(native_items)
		selected = select_imports(reward_items(rules), later, native_items)
		converted = convert_rows(selected, occupied_identities(text_root, later_rows),
			{code: entry["target"] for code, entry in prior.get("idMap", {}).items()})
		text_path = safe_target(text_root, "textdataname.txt")
		later_texts = [row for name in ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")
			for row in tables.table_rows(read_later(name))]
		base_text = text_path.read_bytes()
		merged_text, missing_descriptions = items.merge_texts(base_text, later_texts, converted)
		known = {row[1] for row in characters.translation_rows(base_text) if len(row) > 1}
		added_names = sorted({row[1] for row in characters.translation_rows(merged_text) if len(row) > 1} - known)
		assets = item_import.copy_asset_closure(converted, native, output, archives, worn=False)
		raw_shard = b"\xff\xfe" + characters.table_text(converted).encode("utf-16-le")
		manifest_path = safe_target(text_root, "itemdata.txt")
		manifest_names = [row[0].strip() for row in tables.table_rows(manifest_path.read_bytes())]
		if OUTPUT_SHARD not in map(sro_pk2.fold_ascii, manifest_names):
			manifest_names.append(OUTPUT_SHARD)
		write_atomically(shard, raw_shard)
		write_atomically(text_path, merged_text)
		write_atomically(manifest_path, b"\xff\xfe" + ("\r\n".join(manifest_names) + "\r\n").encode("utf-16-le"))
		write_atomically(safe_target(text_root, REWARDS_FILE), (json.dumps(rules) + "\n").encode("utf-8"))
		summary = {"enabled": True, "format": "opensro-cap110-rewards-v1", "portOnly": True,
			"fixed": len(rules["Fixed"]), "random": len(rules["Random"]), "groups": len(rules["Groups"]),
			"monsters": sorted({rule["Monster"] for rule in rules["Fixed"] + rules["Random"]}),
			"imported": len(converted), "alreadyNative": sorted(set(reward_items(rules)) & native_codes),
			"idMap": {row[items.CODENAME]: {"source": int(later[row[items.CODENAME]][items.IDENTITY]),
				"target": int(row[items.IDENTITY])} for row in converted},
			"nameSymbols": sorted(set(prior.get("nameSymbols", [])) | set(added_names)),
			"absentAuthoredDescriptions": missing_descriptions, "assets": assets,
			"laterArchiveHashes": identities, "shardSha256": sro_pk2.digest(raw_shard),
			"exportHashes": {name: sro_pk2.digest((source_root / name).read_bytes()) for name in (
				"vsro-assigned-fixed.json", "vsro-assigned-random.json", "vsro-assigned-groups.json")}}
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
	parser.add_argument("--source-root", required=True)
	args = parser.parse_args()
	# Off, add_rewards only removes its owned outputs and reads no archive.
	if os.environ.get(monsters.FLAG, "").lower() in tables.FLAG_ON:
		tables.require_vsro_pin(args.vsro_root, tuple(item_import.FOLDERS))
	summary = add_rewards(args.vsro_root, args.native_extracted, args.output, args.source_root)
	print(json.dumps({key: value for key, value in summary.items() if key not in ("assets", "idMap")}, indent=2))


if __name__ == "__main__":
	main()

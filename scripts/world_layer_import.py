"""
===========================================================================

world_layer_import.py - the later client's seasonal world layers

The v1.696 client draws its seasonal town decoration (Halloween pumpkins
and lanterns in Jangan and the other towns, Christmas trees, the Lunar
New Year lamps, the festival and OBT arches) through world layers that
the v1.150 client does not have. Two Map.pk2 tables carry them:

	layerobjdef.txt       one row per layer: enabled, id, kind (0 MESH,
	                      1 DECO), name. The shipped client has only
	                      BASIC and the two guard-post layers on.
	layerobjectlist.ifo   JMXVOBJL1000: one row per member placement,
	                      keyed by (region << 16 | placement uid), the
	                      region's x and y, the position and yaw as float
	                      bits, and a flag.

A layer's members are ordinary .o2 placements: every DECO member joins a
placement of the same region and uid (3334 rows, all DECO rows joined;
measured on the pinned archives). This import copies the members of the
layers the later client ships switched off into the variant's own .o2
files, records which (region, uid) belongs to which layer in
Map_extracted/layerobject.json, and copies the resources they draw.

Inference (no v1.696 client executable is available to read): a layer
row's id is the event group the server switches with the entered tail and
0x3347 (event-environment.ts), so the client shows a layer's members while
its group is active. The shipped flags stay the default: a layer the later
client ships on is native world content and is not imported here.

MESH layers (4, 8, 10) change the walkable mesh; their rows mostly join
no .o2 placement (25 of 26 Halloween MESH rows, measured) and are left
out and reported.

The import runs inside the variant build, after the object catalog is
written, with the v1.696 archives (SRO_V1696_ROOT) checked against their
pins. Unset, a previous import is removed.

===========================================================================
"""
import json
import mmap
import re
import struct
from contextlib import ExitStack
from pathlib import Path

import build_alexandria_variant as variant
import import_vsro_alexandria as tables
import sro_pk2
from cap110_item_import import FOLDERS, copy_resource_closure, normalized_path
from prepare_client_resources import write_atomically

LAYER_DEFINITIONS = "layerobjdef.txt"
LAYER_LIST = "layerobjectlist.ifo"
LAYER_LIST_SIGNATURE = "JMXVOBJL1000"
OBJECT_CATALOG = "object.ifo"
OUTPUT_NAME = "layerobject.json"
OUTPUT_FORMAT = "opensro-world-layers-v1"
REPORT_NAME = ".opensro-world-layers.json"
LAYER_KIND_DECO = 1

O2_SIGNATURE = b"JMXVMAPO1001"
O2_BLOCKS = 36
O2_LOD_GROUPS = 4
O2_RECORD = struct.Struct("<IfffhfhhBBH")
# A placement uid is a signed 16-bit field; the import keeps to the
# non-negative half the native files use.
UID_LIMIT = 0x8000

DEFINITION_ROW = re.compile(r'^\s*(\d+)\s+(\d+)\s+(\d+)\s+"([^"]*)"')
CATALOG_PATH = re.compile(r'"([^"]+)"')


# ================
# parse_layer_definitions
# ================
def parse_layer_definitions(raw):
	"""[{id, enabled, kind, name}] of layerobjdef.txt, in file order."""
	layers = []
	for line in tables.decode_table(raw).splitlines():
		match = DEFINITION_ROW.match(line)
		if match:
			enabled, layer, kind, name = match.groups()
			layers.append({"id": int(layer), "enabled": int(enabled) != 0, "kind": int(kind), "name": name})
	if not layers or len({row["id"] for row in layers}) != len(layers):
		raise ValueError(f"{LAYER_DEFINITIONS}: no rows or a repeated layer id")
	return layers


# ================
# parse_layer_list
# ================
def parse_layer_list(raw):
	"""{(region, uid): layer} of layerobjectlist.ifo."""
	lines = tables.decode_table(raw).splitlines()
	if not lines or lines[0].strip() != LAYER_LIST_SIGNATURE:
		raise ValueError(f"{LAYER_LIST}: not {LAYER_LIST_SIGNATURE}")
	members = {}
	for line in lines[2:]:
		fields = line.split()
		if not fields:
			continue
		key, layer, region_x, region_y = int(fields[0], 16), int(fields[1]), int(fields[2]), int(fields[3])
		region = key >> 16
		# The key's high word is the region the row's own x and y name.
		if region != (region_y << 8 | region_x):
			raise ValueError(f"{LAYER_LIST}: row {fields[0]} names region {region_x},{region_y}")
		members[(region, key & 0xFFFF)] = layer
	return members


# ================
# read_o2
# ================
def read_o2(raw, source="<o2>"):
	"""The 36 x 4 record lists of a JMXVMAPO1001 file, each record a tuple."""
	if raw[:len(O2_SIGNATURE)] != O2_SIGNATURE:
		raise ValueError(f"{source}: not {O2_SIGNATURE.decode()}")
	offset, groups = len(O2_SIGNATURE), []
	for _ in range(O2_BLOCKS * O2_LOD_GROUPS):
		(count,) = struct.unpack_from("<H", raw, offset)
		offset += 2
		groups.append([O2_RECORD.unpack_from(raw, offset + i * O2_RECORD.size) for i in range(count)])
		offset += count * O2_RECORD.size
	if offset != len(raw):
		raise ValueError(f"{source}: {len(raw) - offset} trailing bytes")
	return groups


# ================
# write_o2
# ================
def write_o2(groups):
	parts = [O2_SIGNATURE]
	for records in groups:
		parts.append(struct.pack("<H", len(records)))
		parts.extend(O2_RECORD.pack(*record) for record in records)
	return b"".join(parts)


# ================
# record_key
#
# The (owning region, uid) a placement record answers to; uid is the
# 7th field and the owning region the last.
# ================
def record_key(record):
	return record[10], record[6] & 0xFFFF


# ================
# seasonal_layers
# ================
def seasonal_layers(layers):
	"""The DECO layers the later client ships switched off: the ones to import."""
	return {row["id"] for row in layers if not row["enabled"] and row["kind"] == LAYER_KIND_DECO}


# ================
# region_files
# ================
def region_files(index):
	"""{(y, x): entry} of every Map.pk2 region .o2."""
	files = {}
	for path, entry in index.items():
		parts = path.split("/")
		if len(parts) == 2 and parts[1].endswith(".o2") and parts[0].isdigit() and parts[1][:-3].isdigit():
			files[(int(parts[0]), int(parts[1][:-3]))] = entry
	return files


# ================
# catalog_paths
# ================
def catalog_paths(rows):
	"""{lower-case resource path: id} of object.ifo rows ({id: row})."""
	paths = {}
	for key, row in rows.items():
		match = CATALOG_PATH.search(row)
		if match:
			paths.setdefault(match.group(1).lower(), key)
	return paths


# ================
# strip_previous
# ================
def strip_previous(out, report):
	"""Remove a previous import's records from the variant's .o2 files."""
	for relative, keys in report.get("files", {}).items():
		target = out / "Map_extracted" / relative
		if not target.is_file():
			continue
		owned = {tuple(key) for key in keys}
		groups = read_o2(target.read_bytes(), relative)
		variant.replace_file(target, write_o2([[r for r in records if record_key(r) not in owned] for records in groups]))


# ================
# allocate_uid
# ================
def allocate_uid(taken, region, wanted):
	"""The later uid when the region has not used it, else the lowest free one."""
	used = taken.setdefault(region, set())
	uid = wanted if wanted not in used else next((u for u in range(UID_LIMIT) if u not in used), None)
	if uid is None:
		raise ValueError(f"region {region:#06x} has no free placement uid")
	used.add(uid)
	return uid


# ================
# import_layers
# ================
def import_layers(map_data, map_index, out):
	"""
	Copy the seasonal members into out's .o2 files and object catalog.
	Returns (summary, report files, resource paths to copy).
	"""
	layers = parse_layer_definitions(sro_pk2.payload(map_data, map_index[LAYER_DEFINITIONS]))
	members = parse_layer_list(sro_pk2.payload(map_data, map_index[LAYER_LIST]))
	wanted = seasonal_layers(layers)
	_, later_rows = variant.catalog_lines(sro_pk2.payload(map_data, map_index[OBJECT_CATALOG]))
	catalog = out / "Map_extracted" / OBJECT_CATALOG
	head, rows = variant.catalog_lines(catalog.read_bytes())
	paths = catalog_paths(rows)

	# Every placement uid the variant already uses, per owning region.
	targets, taken = {}, {}
	for later_key in sorted(region_files(map_index)):
		target = out / "Map_extracted" / str(later_key[0]) / f"{later_key[1]}.o2"
		if target.is_file():
			targets[later_key] = (target, read_o2(target.read_bytes(), str(target)))
			for records in targets[later_key][1]:
				for record in records:
					region, uid = record_key(record)
					taken.setdefault(region, set()).add(uid)

	object_ids, uids, resources = {}, {}, set()
	files, placements, absent, added_objects = {}, set(), set(), 0
	for later_key, entry in sorted(region_files(map_index).items()):
		inserted = []
		later_groups = read_o2(sro_pk2.payload(map_data, entry), entry.path)
		for group_index, records in enumerate(later_groups):
			for record in records:
				key = record_key(record)
				layer = members.get(key)
				if layer not in wanted:
					continue
				if later_key not in targets:
					absent.add(f"{later_key[0]}/{later_key[1]}")
					continue
				object_id = record[0]
				if object_id not in object_ids:
					path = CATALOG_PATH.search(later_rows[object_id]).group(1)
					known = paths.get(path.lower())
					if known is None:
						known = max(rows) + 1
						rows[known] = variant.with_id(later_rows[object_id], known)
						paths[path.lower()] = known
						added_objects += 1
					# Every member's resources, new row or not: a catalog row
					# another import already wrote does not prove its files
					# are in this tree. Native files win in the closure.
					resources.add(("Data", normalized_path(path)))
					object_ids[object_id] = known
				if key not in uids:
					uids[key] = allocate_uid(taken, key[0], key[1])
				# The record stores the uid signed; the keys use its 16 bits.
				signed = uids[key] - 0x10000 if uids[key] >= UID_LIMIT else uids[key]
				placed = (object_ids[object_id], *record[1:6], signed, *record[7:])
				targets[later_key][1][group_index].append(placed)
				inserted.append([key[0], uids[key]])
				placements.add((key[0], uids[key], layer))
		if inserted:
			relative = f"{later_key[0]}/{later_key[1]}.o2"
			variant.replace_file(targets[later_key][0], write_o2(targets[later_key][1]))
			files[relative] = inserted
	mesh_layers = {row["id"] for row in layers if not row["enabled"] and row["kind"] != LAYER_KIND_DECO}
	mesh_rows = sum(1 for layer in members.values() if layer in mesh_layers)
	variant.replace_file(catalog, variant.format_catalog(head[0], rows).encode("cp949"))
	table = {
		"format": OUTPUT_FORMAT,
		"layers": [{"id": row["id"], "name": row["name"], "kind": row["kind"], "nativeEnabled": row["enabled"]}
			for row in layers if row["id"] in wanted],
		"placements": sorted(placements)
	}
	write_atomically(out / "Map_extracted" / OUTPUT_NAME, (json.dumps(table, separators=(",", ":")) + "\n").encode("utf-8"))
	summary = {
		"enabled": True,
		"layers": sorted(wanted),
		"placements": len(placements),
		"records": sum(len(keys) for keys in files.values()),
		"regionFiles": len(files),
		"objectsAdded": added_objects,
		"objectsUsed": len(resources),
		"regionsAbsent": sorted(absent),
		"meshRowsLeftOut": mesh_rows
	}
	return summary, files, resources


# ================
# add_layers
#
# The variant build's step. later_root None (SRO_V1696_ROOT unset)
# removes a previous import; the object catalog is rewritten by every
# variant build, so only the .o2 records and the table need removing.
# ================
def add_layers(later_root, native, out, pins):
	marker = out / REPORT_NAME
	report = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	strip_previous(out, report)
	table = out / "Map_extracted" / OUTPUT_NAME
	if later_root is None:
		if table.exists():
			table.unlink()
		if marker.exists():
			marker.unlink()
		return {"enabled": False}
	tables.require_vsro_pin(later_root, ("Map",) + tuple(FOLDERS), pins)
	with ExitStack() as stack:
		archives = {}
		for name in ("Map",) + tuple(FOLDERS):
			handle = stack.enter_context(open(Path(later_root) / f"{name}.pk2", "rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			archives[name] = (data, {normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)})
		map_data, map_index = archives.pop("Map")
		summary, files, resources = import_layers(map_data, map_index, out)
		records = copy_resource_closure(resources, native, out, archives)
	summary["resourcesCopied"] = sum(1 for record in records if record["source"] == "later")
	write_atomically(marker, (json.dumps({"summary": summary, "files": files}, indent=1) + "\n").encode("utf-8"))
	variant.log(f"world layers: {summary['placements']} placements of layers {summary['layers']} "
		f"in {summary['regionFiles']} region files, {summary['objectsAdded']} objects")
	return summary

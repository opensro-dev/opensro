"""
===========================================================================

variant_files.py - the variant tree's file and catalog helpers

PORT-ONLY, NOT NATIVE. The writes every variant step shares: a file
replaced without writing through a hard link, a VSRO archive file read,
the files the native tree lacks added, the native folders linked in, the
map catalogs merged, and the on/off build switch.

A leaf: it imports the stage 1 table readers and nothing that builds a
step. Split out of build_alexandria_variant.py (#4251), whose sixteen
importers pulled the orchestrator, and through its imports nearly the
whole build, into every step's code key.

===========================================================================
"""
import os
import re
import shutil
import struct
from pathlib import Path

import import_vsro_alexandria as stage1
import sro_pk2
from prepare_client_resources import log, safe_target, write_atomically


TEXTDATA = stage1.TEXTDATA


# A build switch's values: the on values are stage 1's (FLAG_ON), the one
# list every importer's flag reads.
SWITCH_OFF_VALUES = ("", "0", "off", "false")
SWITCH_ON_VALUES = stage1.FLAG_ON


MFO_SIGNATURE = b"JMXVMFO 1000"


MFO_HEADER_BYTES = 24


TILE_ID_LIMIT = 0x0400


MAPM_TEXTURE_SCALE_MASK = 0xFC00


CATALOG_ROW = re.compile(r"^(\d+)(\s.*)$")


#============================================================================
# Catalogs
#============================================================================


# ================
# catalog_lines
# ================
def catalog_lines(raw):
	"""(header lines, {id: row line}) of a tile2d.ifo or object.ifo."""
	lines = stage1.decode_table(raw).splitlines()
	rows = {}
	for line in lines[2:]:
		match = CATALOG_ROW.match(line.strip())
		if match:
			rows[int(match.group(1))] = line.strip()
	return lines[:2], rows


# ================
# format_catalog
# ================
def format_catalog(signature, rows):
	"""A catalog file's text: signature, count, then rows in id order."""
	ordered = [rows[key] for key in sorted(rows)]
	return "\r\n".join([signature, str(len(ordered)), *ordered]) + "\r\n"


# ================
# with_id
# ================
def with_id(row, tile_id):
	"""A catalog row renumbered; ids are five digits in tile2d.ifo."""
	return f"{tile_id:05d}" + CATALOG_ROW.match(row).group(2)


# ================
# merge_tile_catalog
# ================
def merge_tile_catalog(native_raw, vsro_raw, used_ids):
	"""
	The merged tile2d.ifo and the remap of the used ids it could not keep.
	A VSRO row fills an id the native catalog lacks. A used id the native
	catalog also defines, as a placeholder or as a different texture, moves to
	a new id past every existing one; unused ones stay native.
	"""
	native_head, native_rows = catalog_lines(native_raw)
	_, vsro_rows = catalog_lines(vsro_raw)
	native_tiles = stage1.tile_catalog(native_raw)
	vsro_tiles = stage1.tile_catalog(vsro_raw)
	# Only ids the native catalog lacks are filled in place. A native
	# placeholder is not: native regions may paint it, and giving it a real
	# name would ask the build for a texture they never had.
	merged, remap = dict(native_rows), {}
	for key, row in vsro_rows.items():
		if key not in native_rows:
			merged[key] = row
	next_id = max(max(native_rows), max(vsro_rows)) + 1
	for key in sorted(used_ids):
		if key in native_rows and native_tiles[key] != vsro_tiles[key]:
			if next_id >= TILE_ID_LIMIT:
				raise SystemExit("tile2d.ifo has no free tile id left for the remap")
			merged[next_id] = with_id(vsro_rows[key], next_id)
			remap[key] = next_id
			next_id += 1
	return format_catalog(native_head[0], merged).encode("cp949"), remap


# ================
# merge_object_catalog
# ================
def merge_object_catalog(native_raw, vsro_raw):
	"""The merged object.ifo: native rows win, VSRO fills the rest."""
	native_head, native_rows = catalog_lines(native_raw)
	_, vsro_rows = catalog_lines(vsro_raw)
	native_objects = stage1.object_catalog(native_raw)
	merged = dict(native_rows)
	for key, row in vsro_rows.items():
		if key not in native_rows or stage1.object_placeholder(native_objects[key]):
			merged[key] = row
	return format_catalog(native_head[0], merged).encode("cp949")


# ================
# remap_terrain
# ================
def remap_terrain(terrain, remap):
	"""A .m terrain file with its ground tile ids remapped; scale bits kept."""
	stage1.terrain_tile_ids(terrain)
	if not remap:
		return terrain
	data = bytearray(terrain)
	for block in range(stage1.MAP_BLOCKS):
		first = stage1.MAPM_SIGNATURE_BYTES + block * stage1.MAPM_BLOCK_BYTES + stage1.MAPM_BLOCK_HEAD_BYTES
		for vertex in range(stage1.MAPM_VERTICES):
			at = first + vertex * stage1.MAPM_VERTEX_BYTES + stage1.MAPM_VERTEX_TEXTURE_OFFSET
			value = struct.unpack_from("<H", data, at)[0]
			tile = value & stage1.MAPM_TEXTURE_ID_MASK
			if tile in remap:
				struct.pack_into("<H", data, at, (value & MAPM_TEXTURE_SCALE_MASK) | remap[tile])
	return bytes(data)


# ================
# enable_regions
# ================
def enable_regions(mapinfo, regions):
	"""mapinfo.mfo with the (sectorX, sectorY) regions' availability bits set."""
	if mapinfo[:len(MFO_SIGNATURE)] != MFO_SIGNATURE:
		raise ValueError("not a JMXVMFO availability file")
	data = bytearray(mapinfo)
	for sector_x, sector_y in regions:
		region_id = (sector_y << 8) | sector_x
		data[MFO_HEADER_BYTES + (region_id >> 3)] |= 0x80 >> (region_id & 7)
	return bytes(data)


#============================================================================
# Files
#============================================================================


# ================
# link_tree
# ================
def link_tree(source, target):
	"""
	Hard-link every file of source under target (copy if links are refused);
	returns how many were laid down. A tree on another volume than its
	source cannot link at all: the r204 staging on D: copied all of the
	native H: tree's ~48k files (2.6 GB), every fresh build (#4251), so the
	copies are reported rather than taken silently.
	"""
	count = copied = 0
	for folder, _, names in os.walk(source):
		destination = target / Path(folder).relative_to(source)
		destination.mkdir(parents=True, exist_ok=True)
		for name in names:
			link = destination / name
			if link.exists():
				continue
			try:
				os.link(Path(folder) / name, link)
			except OSError:
				shutil.copy2(Path(folder) / name, link)
				copied += 1
			count += 1
	if copied:
		log(f"{target.name}: {copied} of {count} files copied, not linked (links refused, as across volumes)")
	return count


# ================
# link_file
# ================
def link_file(source, link):
	"""Hard-link one file (copy if links are refused)."""
	try:
		os.link(source, link)
	except OSError:
		shutil.copy2(source, link)


# ================
# lower_paths
# ================
def lower_paths(root):
	"""Lower-case relative paths of every file under root."""
	return {
		(Path(folder) / name).relative_to(root).as_posix().lower()
		for folder, _, names in os.walk(root)
		for name in names
	}


# ================
# replace_file
# ================
def replace_file(target, content):
	"""
	Write content to target without writing through a hard link: the old
	file (possibly a link to the native tree) is unlinked first.
	"""
	target.parent.mkdir(parents=True, exist_ok=True)
	if target.exists():
		target.unlink()
	write_atomically(target, content)


# ================
# read_vsro_file
# ================
def read_vsro_file(vsro_root, archive, name):
	path, handle, data = stage1.open_archive(vsro_root, archive)
	try:
		entry = stage1.archive_index(data).get(name.lower())
		if entry is None:
			raise SystemExit(f"{path} has no {name}")
		return sro_pk2.payload(data, entry)
	finally:
		data.close()
		handle.close()


# ================
# add_missing_files
# ================
def add_missing_files(vsro_root, archive, destination, present, skip_prefix=()):
	"""Extract the VSRO archive's files whose lower-case path is not in present."""
	path, handle, data = stage1.open_archive(vsro_root, archive)
	try:
		added = 0
		for entry in sro_pk2.read_directory(data):
			key = entry.path.replace("\\", "/").lower()
			if key in present or key.startswith(skip_prefix):
				continue
			write_atomically(safe_target(destination, entry.path), sro_pk2.payload(data, entry))
			added += 1
		log(f"{destination.name}: {added} VSRO files added")
		return added
	finally:
		data.close()
		handle.close()


# ================
# copy_vsro_file
#
# One VSRO archive file laid over the variant's: the environment step
# (environment.ifo is a superset of the native one, ENVIRONMENT_NOTE).
# ================
def copy_vsro_file(vsro_root, archive, name, target):
	replace_file(target, read_vsro_file(vsro_root, archive, name))
	return {"file": name}


# ================
# build_switch
#
# An on/off build flag: unset or an off value is False (native), an on
# value True. Anything else is refused, as the server refuses a value it
# does not know.
# ================
def build_switch(flag, environ=None):
	value = (os.environ if environ is None else environ).get(flag, "").strip().lower()
	if value in SWITCH_OFF_VALUES:
		return False
	if value in SWITCH_ON_VALUES:
		return True
	raise ValueError(f"{flag} must be off, on or unset, not {value!r}")


# ================
# log_omitted_links
#
# One line per keyed link variant_gates.merge_gate_tables left out: the gate it would
# have joined stays shut rather than open without its key.
# ================
def log_omitted_links(label, summary):
	for omitted in summary.get("omittedLinks", []):
		log(f"{label}: link {omitted['from']} -> {omitted['to']} left out; the variant carries no item "
			f"{', '.join(omitted['items'])} its condition names")

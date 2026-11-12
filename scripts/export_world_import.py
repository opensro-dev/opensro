"""
===========================================================================

export_world_import.py - optional export-world areas (port-only).

Port-only, not native. Off unless SRO_EXPORT_WORLD is "1", "on" or
"true" and SRO_CONTENT_ZIP names a content archive that verifies
against content_archive_pins.json beside this file. With the flag
off, the archive absent or no pin committed yet, this layer reaps
what a previous on build enrolled and writes nothing else: the
world is unchanged, which is the state every checkout sits in until
a real pack is pinned (#3263, #3317).

On, every area the pinned archive packs merges into the variant:
its sector files land in Map_extracted at the export's own layout,
its navmesh in Data_extracted/navmesh, its map tiles in
Media_extracted, its sectors' availability bits are set, and its
region rows join the tree's regioninfo.txt under a section of its
own. The marker enrols every file and row, so a switch off restores
the tree exactly (the variant root is reused across builds; see
later_world_import.py). A named archive that fails its pin fails
the build (content_archive.py), never imports silently.

What an area spawns, who stands in it and where its gates lead are
its own import PR's rows (#3329): this importer carries no
population and converts no character kinds. The archive's character
shard rides along unread for those PRs, and the character identity
band 190000 is reserved here for their conversions - no other layer
may take it. Dungeon rows (tables/dungeoninfo.txt) likewise wait
for their own slices.

===========================================================================
"""
import json
import os
from pathlib import Path

import content_archive
import import_vsro_alexandria as tables
from prepare_client_resources import write_atomically

FLAG = "SRO_EXPORT_WORLD"
ZIP_ENV = "SRO_CONTENT_ZIP"
PIN_FILE = Path(__file__).resolve().parent / content_archive.PIN_FILE
MARKER = ".opensro-export-world.json"
MAP_DIR = "Map_extracted"
DATA_DIR = "Data_extracted"
MEDIA_DIR = "Media_extracted"
REGIONINFO = Path(MEDIA_DIR) / "textdata" / "regioninfo.txt"


# ================
# flag_enabled
# ================
def flag_enabled(env=os.environ):
	"""True only when SRO_EXPORT_WORLD names an on value; unset is native."""
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# open_pinned_archive
#
# The layer's one read of the outside world: the content archive,
# verified against its pin before any entry is read. No archive
# named, or no pin committed yet, is the ordinary off state (None);
# a named archive that fails verification refuses the build inside
# content_archive, by name.
# ================
def open_pinned_archive(env=os.environ):
	named = env.get(ZIP_ENV, "").strip()
	if not named or not PIN_FILE.is_file():
		return None
	return content_archive.open_archive(named, PIN_FILE)


# ================
# region_id
# ================
def region_id(sector_x, sector_y):
	"""A sector's region id, the (y << 8) | x the tables name it by."""
	return (sector_y << 8) | sector_x


# ================
# destination
#
# Where one of an area's entries lands in the variant tree: sector
# files and navmesh keep the export's layout under their roots, map
# tiles under the media root. Any other shape is an archive this
# importer does not understand, refused by name.
# ================
def destination(relative):
	if relative.startswith("navmesh/"):
		return Path(DATA_DIR) / relative
	if relative.startswith(("map/", "minimap/")):
		return Path(MEDIA_DIR) / relative
	head, _, _ = relative.partition("/")
	if head.isdigit():
		return Path(MAP_DIR) / relative
	raise SystemExit(f"the content archive carries an entry this importer cannot place: {relative!r}")


# ================
# regioninfo_rows
#
# The region rows of one area out of the archive's regioninfo.txt
# (CP949, neither has a BOM): every line whose first two cells are
# an area sector key, verbatim, in file order. Section headers and
# other areas' rows are not ours to copy.
# ================
def regioninfo_rows(raw, keys):
	wanted = {tuple(key) for key in keys}
	rows = []
	for line in raw.decode("cp949").split("\r\n"):
		cells = line.split("\t")
		if len(cells) < 2:
			continue
		try:
			key = (int(cells[0]), int(cells[1]))
		except ValueError:
			continue
		if key in wanted:
			rows.append(line)
	return rows


# ================
# regioninfo_with
#
# The tree's regioninfo with one area's section appended: its own
# header, then its rows that the tree does not already carry (a key
# the native table already names keeps the native row). The added
# lines come back for the marker: the reap removes exactly them.
# ================
def regioninfo_with(raw, area, rows):
	text = raw.decode("cp949")
	lines = text.split("\r\n")
	present = set()
	for line in lines:
		cells = line.split("\t")
		if len(cells) < 2:
			continue
		try:
			present.add((int(cells[0]), int(cells[1])))
		except ValueError:
			continue
	added = []
	for line in rows:
		cells = line.split("\t")
		if (int(cells[0]), int(cells[1])) not in present:
			added.append(line)
	if not added:
		return raw, []
	header = f"#FIELD\t{area}\t{area}"
	# No separator line and no trailing byte of our own: the reap
	# removes exactly the enrolled lines, so what it added must be
	# all it takes away, byte for byte.
	return "\r\n".join([*lines, header, *added]).encode("cp949"), [header, *added]


# ================
# regioninfo_without
#
# The reap's half: the tree's regioninfo with the marker's enrolled
# lines removed, each exactly once. Nothing else is touched - a row
# the import did not add is not the import's to remove.
# ================
def regioninfo_without(raw, enrolled_lines):
	lines = raw.decode("cp949").split("\r\n")
	for enrolled in enrolled_lines:
		try:
			lines.remove(enrolled)
		except ValueError:
			continue
	return "\r\n".join(lines).encode("cp949")


# ================
# read_marker
# ================
def read_marker(out):
	marker = out / MARKER
	if not marker.exists():
		return {}
	return json.loads(marker.read_text(encoding="utf-8"))


# ================
# reap
#
# A switch off (or an archive gone): the enrolled files are
# unlinked (the next build's link stage re-links anything the
# native tree owns at those paths), the availability bits this
# import set are cleared unless the native tree sets them too, and
# the regioninfo lines it added are removed. The marker goes last.
# No archive is read.
# ================
def reap(native, out, prior):
	import build_alexandria_variant as variant

	if not prior:
		return {"enabled": False}
	for relative in prior.get("files", []):
		target = (out / relative).resolve()
		# The marker is the build's own file, but a path it names is
		# still checked: a reap never deletes outside the overlay.
		if out.resolve() not in target.parents:
			raise ValueError(f"export-world enrolled file {relative!r} is outside the overlay")
		target.unlink(missing_ok=True)
	sectors = [tuple(sector) for sector in prior.get("sectors", [])]
	if sectors:
		mapinfo = out / DATA_DIR / "navmesh" / "mapinfo.mfo"
		if mapinfo.exists():
			native_mapinfo = native / DATA_DIR / "navmesh" / "mapinfo.mfo"
			native_bits = variant_bytes(variant, native_mapinfo)
			data = bytearray(variant_bytes(variant, mapinfo))
			for sector_x, sector_y in sectors:
				rid = region_id(sector_x, sector_y)
				at, bit = variant.MFO_HEADER_BYTES + (rid >> 3), 0x80 >> (rid & 7)
				if native_bits and at < len(native_bits) and native_bits[at] & bit:
					continue
				data[at] &= ~bit
			write_atomically(mapinfo, bytes(data))
	enrolled_lines = prior.get("regioninfoLines", [])
	if enrolled_lines:
		path = out / REGIONINFO
		if path.exists():
			write_atomically(path, regioninfo_without(path.read_bytes(), enrolled_lines))
	(out / MARKER).unlink(missing_ok=True)
	return {"enabled": False}


# ================
# variant_bytes
#
# An availability file's bytes through the variant build's own
# signature check; b"" when the file is absent (a native tree
# without one enables nothing).
# ================
def variant_bytes(variant, path):
	if not path.exists():
		return b""
	raw = path.read_bytes()
	if raw[:len(variant.MFO_SIGNATURE)] != variant.MFO_SIGNATURE:
		raise ValueError(f"{path} is not a JMXVMFO availability file")
	return raw


# ================
# add_export_world
# ================
def add_export_world(native, out, env=os.environ):
	"""
	Merge every area the pinned content archive packs into the
	variant (or, off, reap a previous on build). Port-only, not
	native; off is byte-for-byte the tree the other layers left.
	"""
	import build_alexandria_variant as variant

	native, out = Path(native).resolve(), Path(out).resolve()
	if native == out or native in out.parents or out in native.parents:
		raise ValueError("export-world overlay must be separate from native extraction")
	prior = read_marker(out)
	if not flag_enabled(env):
		return reap(native, out, prior)
	archive = open_pinned_archive(env)
	if archive is None:
		summary = reap(native, out, prior)
		summary["note"] = "SRO_EXPORT_WORLD is on but no pinned content archive is available; nothing imported"
		return summary
	try:
		return merge_archive(archive, variant, native, out, prior)
	finally:
		archive.close()


# ================
# merge_archive
#
# The on path: every packed area, whole. The packer already refused
# incomplete areas; a sector whose mesh, placements or navmesh the
# archive nonetheless lacks is refused here by name rather than
# merged half. The marker is written last, complete, or the merge
# has raised and the next build reaps by the old marker.
# ================
def merge_archive(archive, variant, native, out, prior):
	written = {relative for relative in prior.get("files", [])}
	sectors = {tuple(sector) for sector in prior.get("sectors", [])}
	enrolled_lines = list(prior.get("regioninfoLines", []))
	regioninfo_path = out / REGIONINFO
	if not regioninfo_path.exists():
		raise SystemExit(f"{regioninfo_path} is absent; the variant stage builds it before this layer runs")
	regioninfo = regioninfo_path.read_bytes()
	shared = archive.read("tables/regioninfo.txt")
	areas = {}
	for area in archive.areas():
		keys = archive.regions(area)
		names = archive.names(area)
		prefix = f"areas/{area}/"
		files, navmesh, tiles = 0, 0, 0
		sector_files = {f"{y}/{x}{ext}" for x, y in keys for ext in (".t", ".m", ".o", ".o2")}
		nav_files = {f"navmesh/nv_{region_id(x, y):04x}.nvm" for x, y in keys}
		for name in names:
			relative = name[len(prefix):]
			if relative in sector_files or relative in nav_files or relative.startswith(("map/", "minimap/")):
				continue
			raise SystemExit(f"the content archive's area {area!r} carries an unplaced entry {relative!r}")
		required = {f"{y}/{x}{ext}" for x, y in keys for ext in (".m", ".o")} | nav_files
		missing = sorted(relative for relative in required if prefix + relative not in names)
		if missing:
			raise SystemExit(f"the content archive's area {area!r} lacks {missing[0]}; the packer packs areas whole")
		for name in names:
			relative = name[len(prefix):]
			target = out / destination(relative)
			variant.replace_file(target, archive.read(name))
			written.add(target.relative_to(out).as_posix().lower())
			files += 1
			navmesh += relative.startswith("navmesh/")
			tiles += relative.startswith(("map/", "minimap/"))
		rows = regioninfo_rows(shared, keys)
		if len(rows) != len(keys):
			raise SystemExit(f"the content archive's regioninfo names {len(rows)} of area {area!r}'s {len(keys)} regions")
		regioninfo, added = regioninfo_with(regioninfo, area, rows)
		enrolled_lines += [line for line in added if line not in enrolled_lines]
		sectors |= {tuple(key) for key in keys}
		areas[area] = {"regions": len(keys), "files": files, "navmesh": navmesh, "tiles": tiles, "regionRows": len(added)}
	if areas:
		write_atomically(regioninfo_path, regioninfo)
		mapinfo = out / DATA_DIR / "navmesh" / "mapinfo.mfo"
		variant.replace_file(mapinfo, variant.enable_regions(mapinfo.read_bytes(), sorted(sectors)))
		# The server's admission index (internal/game/world/exportworld):
		# which regions and character kinds each area admits while
		# SRO_EXPORT_WORLD is on. Kinds start empty - an area's own
		# import PR names the kinds its spawns and standings use, and
		# the server refuses any population row outside this index.
		manifest = {"areas": {
			area: {
				"regions": sorted({region_id(x, y) for x, y in archive.regions(area)}),
				"kinds": [],
			} for area in sorted(areas)
		}}
		manifest_path = out / MEDIA_DIR / "textdata" / "export-world.json"
		write_atomically(manifest_path, (json.dumps(manifest, indent=2) + "\n").encode())
		written.add(manifest_path.relative_to(out).as_posix().lower())
	summary = {
		"enabled": True,
		"areas": areas,
		"files": sorted(written),
		"sectors": sorted(sectors),
		"regioninfoLines": enrolled_lines,
	}
	write_atomically(out / MARKER, (json.dumps(summary, indent=2) + "\n").encode())
	return {"enabled": True, "areas": areas}


# ================
# main
# ================
def main():
	import argparse
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	args = parser.parse_args()
	print(json.dumps(add_export_world(args.native_extracted, args.output), indent=2))


if __name__ == "__main__":
	main()

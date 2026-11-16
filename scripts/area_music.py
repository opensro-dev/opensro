"""
===========================================================================

area_music.py - which music areas a variant game root needs, from its regions

PORT-ONLY, NOT NATIVE (the variant root is). The client plays an area's
music and ambience by looking its region up in regioninfo.txt (region ->
area) and the area up in effectenvsnd.txt (area -> track and ambience). A
region with no regioninfo row is silent (#3546: the Storm and Cloud Desert
at region 73,84 had terrain from stage 1 but no area, because the area list
was a hand list naming Alexandria and the Delta only).

This module derives the list from what the variant actually holds: every
outdoor region with a terrain file and every dungeon region dungeoninfo.txt
names. An area of a source table is wanted when any of its regions is one
of those. Two sources, in this order:

	the VSRO client      Media textdata regioninfo.txt / effectenvsnd.txt,
	                     in the v1.150 layout (a "#TOWN"/"#FIELD" header,
	                     then one row per region); appended verbatim.
	the v1.696 client    Data regioninfo.txt (one row per region, the area
	                     on every row) and Media effectenvsnd.txt (one row
	                     per sound: area, kind, file, range); converted to
	                     the v1.150 layout. It names the later layers'
	                     areas (Baghdad, Jupiter, Muhan and the rest) that
	                     the VSRO tables do not have.

A later area only takes regions no earlier area claims, so a region never
resolves to two areas. The tracks come from the VSRO Music.pk2 (pinned); a
track only the v1.696 client ships is reported absent, since its Music.pk2
is not one of the pinned v1.696 archives.

===========================================================================
"""
import re
import subprocess
import tempfile
from pathlib import Path

import import_vsro_alexandria as stage1
import prepare_client_resources as prepare
import sro_pk2
from prepare_client_resources import write_atomically

# regioninfo.txt: the area header kinds and the row coverage words.
AREA_KINDS = ("#TOWN", "#FIELD")
COVERAGE_ALL = "ALL"
COVERAGE_RECT = "RECT"
# A dungeon's regioninfo rows name it as (dungeon id, DUNGEON_SECTOR_Y): the
# region id 0x8000 | id read as sector y (high byte) and x (low byte).
DUNGEON_SECTOR_Y = 128
# The v1.696 Data regioninfo.txt row: kind, area, x, y, coverage or the
# dungeon's page name, then the four RECT numbers.
LATER_REGION_COLUMNS = 9
# The v1.696 effectenvsnd.txt sound kinds: the area's track, then the day
# and night ambience, which the v1.150 layout writes as "<2>" periods.
LATER_SOUND_MUSIC = "0"
LATER_SOUND_PERIODS = (("1", "낮"), ("2", "밤"))
TAB = chr(9)
# The v1.150 regioninfo.txt and effectenvsnd.txt row widths.
REGIONINFO_COLUMNS = 7
EFFECTENVSND_COLUMNS = 7


# ================
# area_regions
# ================
def area_regions(lines):
	"""
	{area: [(x, y), ...]} of a regioninfo.txt in the v1.150 layout, in file
	order: each header's region rows up to the next header.
	"""
	areas, current = {}, None
	for line in lines:
		cells = line.split("\t")
		if line.startswith(AREA_KINDS) and len(cells) > 1:
			current = areas.setdefault(cells[1], [])
		elif current is not None and len(cells) > 1 and cells[0].isdigit() and cells[1].isdigit():
			current.append((int(cells[0]), int(cells[1])))
	return areas


# ================
# wanted_areas
# ================
def wanted_areas(lines, regions, known=()):
	"""
	The areas of a v1.150-layout regioninfo.txt, in file order, that cover any
	of regions and are not already known.
	"""
	return [
		name for name, rows in area_regions(lines).items()
		if name not in known and any(region in regions for region in rows)
	]


# ================
# later_areas
# ================
def later_areas(lines):
	"""
	The v1.696 per-region regioninfo.txt grouped by area, in file order:
	{area: {"kind": "#TOWN"/"#FIELD", "page": dungeon page or "", "rows": [cells]}}.
	"""
	areas = {}
	for line in lines:
		cells = line.split("\t")
		if not line.startswith(AREA_KINDS) or len(cells) < LATER_REGION_COLUMNS - 4:
			continue
		if not (cells[2].isdigit() and cells[3].isdigit()):
			continue
		area = areas.setdefault(cells[1], {"kind": cells[0], "page": "", "rows": []})
		if cells[4] not in (COVERAGE_ALL, COVERAGE_RECT):
			area["page"] = cells[4]
		area["rows"].append(cells)
	return areas


# ================
# later_region_section
# ================
def later_region_section(name, area, regions, claimed):
	"""
	One later area in the v1.150 regioninfo layout, keeping only its rows on
	regions the variant holds and no earlier area claims. Empty when none is
	left.
	"""
	rows = []
	for cells in area["rows"]:
		region = (int(cells[2]), int(cells[3]))
		if region not in regions or region in claimed:
			continue
		if cells[4] == COVERAGE_RECT:
			row = cells[2:4] + [COVERAGE_RECT] + cells[5:LATER_REGION_COLUMNS]
		else:
			row = cells[2:4] + [COVERAGE_ALL]
		rows.append(pad(row, REGIONINFO_COLUMNS))
	if not rows:
		return []
	return [pad([area["kind"], name, area["page"]], REGIONINFO_COLUMNS)] + rows


# ================
# later_sound_section
# ================
def later_sound_section(lines, name, columns):
	"""
	One area of the v1.696 effectenvsnd.txt (area, kind, "file", range per row)
	in the v1.150 layout: the "<1>" header, the track, then a "<2>" period per
	ambience kind with its "<3>" sounds. Empty when the table lacks the area.
	"""
	rows = [line.split("\t") for line in lines]
	rows = [cells for cells in rows if len(cells) >= 4 and cells[0] == name]
	if not rows:
		return []
	section = [pad(["<1>", name], columns)]
	section += [pad(["", cells[2]], columns) for cells in rows if cells[1] == LATER_SOUND_MUSIC]
	for kind, label in LATER_SOUND_PERIODS:
		sounds = [cells for cells in rows if cells[1] == kind]
		if not sounds:
			continue
		section.append(pad(["", "<2>", label], columns))
		section += [pad(["", "", "", "<3>", cells[2], cells[3]], columns) for cells in sounds]
		section.append(pad([], columns))
	return section


# ================
# later_music_rows
# ================
def later_music_rows(region_lines, later_regions, later_sounds, regions, sound_columns):
	"""
	The later areas the variant still needs once the native and VSRO areas
	are in: (regioninfo rows, effectenvsnd rows, area names). region_lines is
	the merged regioninfo.txt so far, whose areas and regions win.
	"""
	present = area_regions(region_lines)
	claimed = {region for rows in present.values() for region in rows}
	region_rows, sound_rows, names = [], [], []
	for name, area in later_areas(later_regions).items():
		if name in present:
			continue
		section = later_region_section(name, area, regions, claimed)
		sounds = later_sound_section(later_sounds, name, sound_columns)
		if not section or not sounds:
			continue
		claimed |= {(int(row.split("\t")[0]), int(row.split("\t")[1])) for row in section[1:]}
		region_rows += section
		sound_rows += sounds
		names.append(name)
	return region_rows, sound_rows, names


# ================
# variant_regions
# ================
def variant_regions(out, dungeon_ids):
	"""
	The regions a variant tree holds, as regioninfo names them: (x, y) of every
	Map_extracted/<y>/<x>.m terrain file, and (id, DUNGEON_SECTOR_Y) of every
	dungeon id.
	"""
	regions = set()
	map_root = Path(out) / "Map_extracted"
	for folder in map_root.iterdir():
		if not (folder.is_dir() and folder.name.isdigit()):
			continue
		for terrain in folder.glob("*.m"):
			if terrain.stem.isdigit():
				regions.add((int(terrain.stem), int(folder.name)))
	regions |= {(dungeon_id, DUNGEON_SECTOR_Y) for dungeon_id in dungeon_ids}
	return regions


# ================
# dungeon_ids
# ================
def dungeon_ids(raw):
	"""The region ids dungeoninfo.txt (CP949, id <tab> "path") names."""
	ids = []
	for line in raw.decode("cp949").splitlines():
		cells = line.split("\t")
		if cells and cells[0].strip().isdigit():
			ids.append(int(cells[0]))
	return ids


# ================
# silent_regions
# ================
def silent_regions(region_lines, regions):
	"""The outdoor regions of the variant that no regioninfo area covers."""
	covered = {region for rows in area_regions(region_lines).values() for region in rows}
	return sorted(region for region in regions if region[1] < DUNGEON_SECTOR_Y and region not in covered)


# ================
# area_names
# ================
def area_names(lines, marker):
	"""The second cell of every line whose first cell is marker ("<1>" headers)."""
	return [cells[1] for cells in (line.split(TAB) for line in lines) if len(cells) > 1 and cells[0] == marker]


# ================
# pad
# ================
def pad(cells, columns):
	"""A tab-joined row of at least columns cells."""
	return "\t".join(list(cells) + [""] * (columns - len(cells)))


#============================================================================
# Tables and tracks
#============================================================================


# ================
# table_lines
# ================
def table_lines(raw):
	"""The CP949 lines of regioninfo.txt / effectenvsnd.txt (neither has a BOM)."""
	return raw.decode("cp949").split("\r\n")


# ================
# regioninfo_section
# ================
def regioninfo_section(lines, name):
	"""
	One area's block of regioninfo.txt: its #TOWN / #FIELD header line and the
	region rows below it, up to the next header.
	"""
	for start, line in enumerate(lines):
		cells = line.split("\t")
		if line.startswith(("#TOWN", "#FIELD")) and len(cells) > 1 and cells[1] == name:
			end = start + 1
			while end < len(lines) and not lines[end].startswith("#"):
				end += 1
			return [row for row in lines[start:end] if row.strip("\t")]
	raise SystemExit(f"regioninfo.txt has no area {name!r}")


# ================
# effectenvsnd_section
# ================
def effectenvsnd_section(lines, name):
	"""
	One area's block of effectenvsnd.txt: its "<1>" line and everything below
	it up to the next "<1>", padded to the v1.150 file's seven columns.
	"""
	for start, line in enumerate(lines):
		cells = line.split("\t")
		if cells[0] == "<1>" and len(cells) > 1 and cells[1] == name:
			end = start + 1
			while end < len(lines) and not lines[end].startswith("<1>"):
				end += 1
			return [
				row + "\t" * (EFFECTENVSND_COLUMNS - len(row.split("\t")))
				for row in lines[start:end] if row.strip("\t")
			]
	raise SystemExit(f"effectenvsnd.txt has no area {name!r}")


# ================
# music_tracks
# ================
def music_tracks(section):
	"""The music files (.ogg) an effectenvsnd section names."""
	return sorted({
		match.group(1) for row in section for match in [re.search(r'"([^"]+\.ogg)"', row)] if match
	})


# ================
# append_cp949
# ================
def append_cp949(native_raw, rows):
	"""The native CP949 table with rows appended (unchanged when there are none)."""
	if not rows:
		return native_raw
	text = native_raw.decode("cp949")
	if not text.endswith("\n"):
		text += "\r\n"
	return (text + "\r\n".join(rows) + "\r\n").encode("cp949")


# ================
# merge_music_tables
# ================
def merge_music_tables(native, vsro, areas):
	"""
	The native regioninfo.txt and effectenvsnd.txt with the areas added (their
	region lists and their music and ambience), and the music files they name.
	Areas the native tables already have are left alone.
	"""
	region_lines = table_lines(native["regioninfo.txt"])
	sound_lines = table_lines(native["effectenvsnd.txt"])
	vsro_regions = table_lines(vsro["regioninfo.txt"])
	vsro_sounds = table_lines(vsro["effectenvsnd.txt"])
	known = {line.split("\t")[1] for line in region_lines if line.startswith(("#TOWN", "#FIELD")) and "\t" in line}
	known_sounds = {line.split("\t")[1] for line in sound_lines if line.startswith("<1>") and "\t" in line}
	tracks, region_rows, sound_rows = [], [], []
	for name in areas:
		sounds = effectenvsnd_section(vsro_sounds, name)
		if name not in known:
			region_rows += regioninfo_section(vsro_regions, name)
		if name not in known_sounds:
			sound_rows += sounds
			tracks += music_tracks(sounds)
	return {
		"regioninfo.txt": append_cp949(native["regioninfo.txt"], region_rows),
		"effectenvsnd.txt": append_cp949(native["effectenvsnd.txt"], sound_rows)
	}, sorted(set(tracks))


# ================
# add_music
# ================
def add_music(vsro_root, native, out, later_root=None):
	"""
	Add the music and ambience tables of every area that covers a region the
	variant holds (see the header): the VSRO areas verbatim, then, with the
	v1.696 archives, the later areas the VSRO tables lack. Convert the music
	files they name from the VSRO Music.pk2 to MP3, exactly as
	prepare_client_resources does for the native tracks. Returns a summary.
	"""
	import build_alexandria_variant as variant
	textdata = Path("Media_extracted") / stage1.TEXTDATA
	names = ("regioninfo.txt", "effectenvsnd.txt")
	tables = {name: (native / textdata / name).read_bytes() for name in names}
	vsro = {name: variant.read_vsro_file(vsro_root, "Media", stage1.TEXTDATA + name) for name in names}
	dungeons = dungeon_ids((out / "Data_extracted" / "dungeon" / "dungeoninfo.txt").read_bytes())
	regions = variant_regions(out, dungeons)
	native_regions = variant_regions(native, ())
	known = area_regions(table_lines(tables["regioninfo.txt"]))
	sounded = set(area_names(table_lines(vsro["effectenvsnd.txt"]), "<1>"))
	areas = [
		name for name in wanted_areas(table_lines(vsro["regioninfo.txt"]), regions, known)
		if name in sounded
	]
	merged, tracks = merge_music_tables(tables, vsro, areas)
	later_areas, later_tracks = [], []
	if later_root is not None:
		merged, later_tracks, later_areas = add_later_music(later_root, merged, regions)
		tracks = sorted(set(tracks) | set(later_tracks))
	for name, content in merged.items():
		variant.replace_file(out / textdata / name, content)
	silent = silent_regions(table_lines(merged["regioninfo.txt"]), regions - native_regions)
	converted, absent = convert_tracks(vsro_root, tracks, out)
	later_converted, absent = source_later_tracks(absent, later_tracks, out, later_root)
	converted += later_converted
	return {
		"areas": areas, "laterAreas": later_areas, "tracks": tracks, "converted": converted, "absent": absent,
		"silentRegions": [list(region) for region in silent]
	}


# ================
# source_later_tracks
# ================
def source_later_tracks(absent, later_tracks, out, later_root, convert=None, pins=None):
	"""
	Convert the later areas' tracks the VSRO Music.pk2 lacks (Arabia's town,
	field, desert and dungeon) from the v1.696 client's own Music.pk2, pinned
	in v1696_archive_pins.json beside the other v1.696 archives; returns
	(converted, still absent). A later area's track that no source holds
	stops the build: the region table already names it, and the client would
	ask for a file that was never published. convert and pins are injectable
	for tests.
	"""
	convert = convert or convert_tracks
	wanted = [track for track in absent if track in set(later_tracks)]
	if not wanted:
		return [], absent
	if pins is None:
		from mall_avatar_import import V1696_SOURCE, source_pins
		pins = source_pins(V1696_SOURCE)
	stage1.require_vsro_pin(later_root, ("Music",), pins)
	converted, missing = convert(later_root, wanted, out)
	if missing:
		raise SystemExit(f"no music source holds {', '.join(missing)}, which later areas name")
	return converted, [track for track in absent if track not in wanted]


# ================
# add_later_music
# ================
def add_later_music(later_root, merged, regions):
	"""
	The merged tables with the v1.696 areas appended that cover the variant's
	regions and that no native or VSRO area already holds; returns (tables,
	tracks, area names). The v1.696 Data and Media archives are pin-checked.
	"""
	import build_alexandria_variant as variant
	from mall_avatar_import import V1696_SOURCE, source_pins
	stage1.require_vsro_pin(later_root, ("Data", "Media"), source_pins(V1696_SOURCE))
	later_regions = table_lines(variant.read_vsro_file(later_root, "Data", "regioninfo.txt"))
	later_sounds = table_lines(variant.read_vsro_file(later_root, "Media", stage1.TEXTDATA + "effectenvsnd.txt"))
	region_rows, sound_rows, names = later_music_rows(
		table_lines(merged["regioninfo.txt"]), later_regions, later_sounds, regions, EFFECTENVSND_COLUMNS
	)
	return {
		"regioninfo.txt": append_cp949(merged["regioninfo.txt"], region_rows),
		"effectenvsnd.txt": append_cp949(merged["effectenvsnd.txt"], sound_rows)
	}, music_tracks(sound_rows), names


# ================
# convert_tracks
# ================
def convert_tracks(vsro_root, tracks, out):
	"""
	Convert the named tracks from the VSRO Music.pk2 into out/Music_mp3 (a
	track already there is kept); returns (converted, absent).
	"""
	converted, absent = [], []
	ffmpeg = prepare.find_ffmpeg() if tracks else None
	path, handle, data = stage1.open_archive(vsro_root, "Music")
	try:
		index = stage1.archive_index(data)
		with tempfile.TemporaryDirectory(prefix="opensro-music-") as scratch:
			for track in tracks:
				entry = index.get(track.lower())
				if entry is None:
					absent.append(track)
					continue
				target = out / "Music_mp3" / (Path(track).stem + ".mp3")
				if target.exists():
					continue
				source = Path(scratch) / Path(track).name
				source.write_bytes(sro_pk2.payload(data, entry))
				partial = Path(scratch) / target.name
				subprocess.run(
					[ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", str(source), *prepare.MUSIC_ENCODER_ARGS, str(partial)],
					check=True
				)
				write_atomically(target, partial.read_bytes())
				converted.append(target.name)
	finally:
		data.close()
		handle.close()
	return converted, absent

r"""
===========================================================================

later_fortress_import.py - a later client's fortress, staged into the variant

PORT-ONLY, NOT NATIVE to v1.150 (each fortress is native to the later
client it comes from). One engine for every fortress the shipped
siegefortress.txt lacks; each caller names its fortress in a
FortressSpec and owns one switch, one marker, one character shard and
one placement file, so two fortresses never reap each other's rows:

	constantinople_import.py   FORTRESS_CONSTANTINOPLE (v1.696)
	vsro_fortress_import.py    FORTRESS_BIJEOKDAN, FORTRESS_HOTAN (v1.188)

Off, add_fortress removes exactly what its marker records and reads no
archive; the flag-off build is byte-identical to the build without it.

What a source authors, and what the engine does with it:

	regions   the spec's regioncode sectors. Terrain, navmesh, tile and
	          object catalogues merge over the variant's current ones
	          exactly as the Sky Temple's do; regioncode and
	          textzonename rows join where the current row is absent
	          or a placeholder. A sector the native tree already holds
	          (the Bandit Fortress's: v1.150 ships the ground the later
	          client built the fortress on) is replaced, and the off
	          path puts the native bytes back.
	fortress  the world's enabled event zones join eventzonedata.txt,
	          the fortress row joins siegefortress.txt, and the
	          structure characters the shipped tables lack join as the
	          spec's character shard. The zones' placements
	          (navmesh\objectstring.ifo) are staged as the spec's
	          objectstring.*portonly.ifo beside the native file; the
	          server bundle's structure-zones projection merges every
	          such file. The fortress then stands up through the port's
	          existing fortress line (the authority catalog reads
	          siegefortress.txt, Authority.ForWorld binds the world).
	staff     the later npcpos standings of the spec's fortress staff
	          (official, manager, battle aide, smith, trainer) that the
	          native world lacks; v1.150 ships every one of those
	          characters but places only Jangan's. Hotan's official
	          stands in Hotan town, outside the fortress regions. The
	          marker names the rows it appended, as Arabia's standings.

===========================================================================
"""
import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

sys.path.insert(0, str(Path(__file__).resolve().parent))
import alexandria_npcs as characters  # noqa: E402
import build_alexandria_variant as variant  # noqa: E402
import cap110_monsters as monsters  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402
import skytemple_import as sky  # noqa: E402
import sro_pk2  # noqa: E402
from alexandria_map_tables import later_zone_rows, native_text_row  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from sro_paths import trees_overlap  # noqa: E402

TEXT_ROOT = sky.TEXT_ROOT
NAVMESH = Path("Data_extracted") / "navmesh"


# ================
# FortressSpec
#
# One fortress and the source it comes from. The archive functions are
# called through the spec at run time, so a test that patches the
# source module (its pin file) reaches the engine too.
# ================
@dataclass(frozen=True)
class FortressSpec:
	label: str
	source_label: str
	flag: str
	marker: str
	character_shard: str
	portonly_ifo: str
	region_code: str
	fortress_code: str
	name_symbol: str
	world: str
	expected_regions: int
	expected_zones: int
	open_archive: Callable
	verify_archives: Callable
	default_root: Callable
	staff_codes: tuple = ()
	# The later name rows in later_object_texts' shape; None reads the
	# v1.696 shard manifest (skytemple_import.later_object_texts).
	object_texts: Callable = None
	# {authored model path: shipped model path} for structures whose
	# authored model no source ships (an inference, documented by the
	# spec). A fallback the source does ship is refused as stale.
	model_fallbacks: tuple = ()


# ================
# enabled
# ================
def enabled(spec, env=os.environ):
	return env.get(spec.flag, "").strip().lower() in tables.FLAG_ON


# ================
# fortress_regions
#
# The region ids regioncode.txt names the spec's region code. Any other
# count is a different client than the one the spec was written against.
# ================
def fortress_regions(spec, regioncode_rows):
	regions = {int(row[1]) for row in regioncode_rows
		if len(row) >= 3 and row[1].isdigit() and row[2].strip() == spec.region_code}
	if len(regions) != spec.expected_regions:
		raise ValueError(f"{spec.source_label} regioncode names {len(regions)} {spec.region_code} regions, "
			f"expected {spec.expected_regions}")
	return regions


# ================
# fortress_zones
#
# The enabled eventzonedata rows of the fortress world, in file order.
# ================
def fortress_zones(spec, eventzone_rows):
	zones = [row for row in eventzone_rows
		if len(row) > 10 and row[0] == "1" and row[10].strip() == spec.world]
	if len(zones) != spec.expected_zones:
		raise ValueError(f"{spec.source_label} eventzonedata names {len(zones)} {spec.world} zones, "
			f"expected {spec.expected_zones}")
	return zones


# ================
# needed_characters
#
# Every character the area stands on: the zones' default structures,
# plus whatever npcpos places inside the regions (the fortress staff
# and the gate pulleys). A needed codename with no later character row
# is a data-contract break, refused by name.
# ================
def needed_characters(later, zones, npcpos_rows, codename_by_id, regions):
	needed = {row[9].strip() for row in zones}
	for row in npcpos_rows:
		if len(row) < 2 or row[0] not in codename_by_id:
			continue
		try:
			region = int(row[1]) & 0xFFFF
		except ValueError:
			continue
		if region in regions:
			needed.add(codename_by_id[row[0]])
	for code in sorted(needed):
		if code not in later:
			raise ValueError("fortress character has no later character row: " + code)
	return sorted(needed)


# ================
# objectstring_lines
#
# The later objectstring.ifo lines that place this fortress's zones,
# kept verbatim: the server bundle parses them with the same reader as
# the native file. A zone the file never places would stand its
# structure nowhere, so every zone name must appear exactly once.
# ================
def objectstring_lines(spec, text, zone_names):
	lines = [line for line in text.splitlines() if line.strip()]
	if len(lines) < 2 or lines[0] != "JMXVOBJI1000":
		raise ValueError(f"{spec.source_label} objectstring.ifo has no JMXVOBJI1000 header")
	found, seen = [], set()
	for line in lines[2:]:
		parts = line.rsplit('"', 2)
		if len(parts) < 3:
			continue
		name = parts[1]
		if name in zone_names:
			if name in seen:
				raise ValueError(f"{spec.source_label} objectstring.ifo places {name} twice")
			seen.add(name)
			found.append(line)
	if seen != set(zone_names):
		raise ValueError(f"{spec.source_label} objectstring.ifo places "
			f"{len(seen)} of the {len(zone_names)} {spec.world} zones")
	return found


# ================
# staff_standings
#
# The later npcpos rows of the spec's fortress staff, in the native ids
# the shipped character tables give those codenames. A staff member the
# native tables do not carry is refused: the shipped client must draw it.
# ================
def staff_standings(spec, later_npcpos, later_id_by_code, native_id_by_code):
	rows = []
	for code in spec.staff_codes:
		if code not in native_id_by_code or code not in later_id_by_code:
			raise ValueError(f"{spec.label} staff {code} is missing from the native or later character tables")
		later_id = later_id_by_code[code]
		standings = [row for row in later_npcpos if len(row) >= 5 and row[0] == later_id]
		if not standings:
			raise ValueError(f"{spec.source_label} npcpos never places {code}")
		rows += [[native_id_by_code[code]] + row[1:5] for row in standings]
	return rows


# ================
# vsro_object_texts
#
# The v1.188 client's name rows. Its textdata_object.txt and
# textdataname.txt are the tables themselves (v1.696 splits them into
# shards behind a manifest); English sits in column 8. The rows come out
# in later_object_texts' shape, so native_text_row reads either source.
# ================
VSRO_ENGLISH_COLUMN = 8
VSRO_NAME_TABLES = ("textdata_object.txt", "textdataname.txt")


def vsro_object_texts(read):
	rows = []
	for table in VSRO_NAME_TABLES:
		for row in characters.translation_rows(read(table)):
			if len(row) <= VSRO_ENGLISH_COLUMN or not row[1]:
				continue
			rows.append([row[0], row[1], row[2], "", "", "", "", "", row[VSRO_ENGLISH_COLUMN], ""])
	return rows


# ================
# resource_key
#
# A character row's model path as the Data archive index names it.
# ================
def resource_key(model_path):
	return "res/" + model_path.replace("\\", "/").lower()


# ================
# shipped_closure
#
# The resource closure for a source whose placed objects reach files the
# dependency reader cannot parse but the variant already ships: v1.150's
# own tre_bamboo04.bsr, and the v1.188 palms stage 1 copies into every
# variant (tre_palm02_copy.bsr and its two twins). Unlike Arabia's
# tolerant walk it admits no gap: a missing file, or an unparsable one
# only this walk brought in, stops the build by name.
# ================
def shipped_closure(seeds, archives, native, output):
	result = sky.copy_resource_closure(seeds, archives, native, output, tolerate_gaps=True)
	if result.get("missingFiles"):
		raise ValueError("fortress resources missing from every source: " + ", ".join(result["missingFiles"]))
	brought = set(result.get("files", []))
	foreign = [name for name in result.get("unparsedFiles", [])
		if not (Path(native) / "Data_extracted" / name).is_file()
		and ("Data_extracted/" + name in brought or not (Path(output) / "Data_extracted" / name).is_file())]
	if foreign:
		raise ValueError("fortress resources the dependency reader cannot parse: " + ", ".join(foreign))
	return result


# ================
# restore_or_unlink
#
# An owned file leaves the variant; where the native tree holds the same
# path (a replaced sector), its native bytes come back instead.
# ================
def restore_or_unlink(native, output, relative):
	target = safe_target(output, relative)
	source = Path(native) / relative
	if source.is_file():
		variant.replace_file(target, source.read_bytes())
	else:
		target.unlink(missing_ok=True)


# ================
# remove_owned
#
# The off path: everything the marker owns leaves - the character
# shard, the placement file, the copied files, and the owned event-zone,
# siege-fortress, region, name and npcpos rows. The catalogues and
# mapinfo need no reaping because the variant build rewrites them from
# the native tables on every run.
# ================
def remove_owned(spec, native, output, prior):
	text_root = output / TEXT_ROOT
	zone_ids = {str(zone) for zone in prior.get("zoneIds", [])}
	if zone_ids:
		zone_path = text_root / "eventzonedata.txt"
		if zone_path.exists():
			write_atomically(zone_path, monsters.encode_rows(
				[row for row in tables.table_rows(zone_path.read_bytes())
					if len(row) <= 1 or row[1] not in zone_ids]))
	siege_path = text_root / "siegefortress.txt"
	if prior.get("fortressRow") and siege_path.exists():
		write_atomically(siege_path, monsters.encode_rows(
			[row for row in tables.table_rows(siege_path.read_bytes())
				if len(row) <= 2 or row[2] != spec.fortress_code]))
	region_ids = {str(region) for region in prior.get("regions", [])}
	if region_ids:
		region_path = text_root / "regioncode.txt"
		if region_path.exists():
			write_atomically(region_path, monsters.encode_rows(
				[row for row in tables.table_rows(region_path.read_bytes())
					if len(row) <= 2 or row[1] not in region_ids or row[2].strip() != spec.region_code]))
	symbols = set(prior.get("nameSymbols", []))
	if symbols:
		text_path = text_root / "textdataname.txt"
		if text_path.exists():
			write_atomically(text_path, monsters.encode_rows(
				monsters.drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), symbols)))
	standings = {tuple(row) for row in prior.get("npcStandings", [])}
	if standings:
		npcpos_path = text_root / "npcpos.txt"
		if npcpos_path.exists():
			write_atomically(npcpos_path, monsters.encode_rows(
				[row for row in tables.table_rows(npcpos_path.read_bytes()) if tuple(row[:5]) not in standings]))
	(text_root / spec.character_shard).unlink(missing_ok=True)
	(output / NAVMESH / spec.portonly_ifo).unlink(missing_ok=True)
	for relative in prior.get("files", []):
		restore_or_unlink(native, output, relative)
	(output / spec.marker).unlink(missing_ok=True)
	return {"enabled": False}


# ================
# add_fortress
# ================
def add_fortress(spec, native, output, source=None, closure=sky.copy_resource_closure):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError(f"{spec.label} output must be separate from the native extraction")
	marker = output / spec.marker
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if not enabled(spec):
		return remove_owned(spec, native, output, prior)
	root = Path(source) if source else spec.default_root()
	spec.verify_archives(root)
	text_root = output / TEXT_ROOT

	media_path, media_handle, media = spec.open_archive(root, "Media")
	try:
		media_index = tables.archive_index(media)
		archive_hashes = {"Media": tables.sha256_of(media)}

		# ================
		# read
		# ================
		def read(name):
			return tables.table_text(media, media_index, name.lower())

		later = {row[2]: row for shard in tables.table_rows(read("characterdata.txt"))
			for row in tables.table_rows(read(shard[0].lower())) if len(row) > 2}
		codename_by_id = {row[1]: row[2] for row in later.values()}
		regions = fortress_regions(spec, tables.table_rows(read("regioncode.txt")))
		zones = fortress_zones(spec, tables.table_rows(read("eventzonedata.txt")))
		zone_names = {row[2].strip() for row in zones}
		later_npcpos = tables.table_rows(read("npcpos.txt"))
		needed = needed_characters(later, zones, later_npcpos, codename_by_id, regions)
		fortress_row = next((row for row in tables.table_rows(read("siegefortress.txt"))
			if len(row) > 2 and row[2] == spec.fortress_code), None)
		if fortress_row is None:
			raise ValueError(f"{spec.source_label} siegefortress.txt names no {spec.fortress_code}")

		# Before the first write, a provisional marker owns what this run
		# is about to change: a run that stops part way (a missing model,
		# an interrupted build) still leaves the off path everything it
		# needs, including the native sectors a later fortress replaces.
		sectors = sorted({(region & 0xFF, (region >> 8) & 0xFF) for region in regions})
		planned = {f"Map_extracted/{y}/{x}{extension}" for x, y in sectors for extension in tables.MAP_EXTENSIONS}
		write_atomically(marker, (json.dumps({
			"enabled": True,
			"provisional": True,
			"characters": prior.get("characters", {}),
			"zoneIds": sorted(int(row[1]) for row in zones),
			"fortressRow": True,
			"regions": sorted(regions),
			"nameSymbols": prior.get("nameSymbols", []),
			"npcStandings": prior.get("npcStandings", []),
			"files": sorted(set(prior.get("files", [])) | planned),
		}, indent=2) + "\n").encode())

		# The characters the shipped tables lack, converted to the
		# v1.150 layout (the conversion gives a structure the skills
		# its shipped twin carries, so no skill shard joins). The
		# shipped rows carry the same codenames and identities as the
		# later ones, so the codename test is the identity test.
		shipped_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != spec.character_shard
			for r in tables.table_rows(p.read_bytes())]
		shipped_codes = {r[2] for r in shipped_chars if len(r) > 2}
		refs = {code: characters.convert_character(later[code])
			for code in needed if code not in shipped_codes}
		occupied = {int(r[1]) for r in shipped_chars if len(r) > 1 and r[1].isdigit()}
		allocated = monsters.allocate_characters(occupied, sorted(refs), prior.get("characters", {}))
		fallbacks = dict(spec.model_fallbacks)
		for code, row in refs.items():
			row[1] = str(allocated[code])
			row[characters.MODEL_COLUMN] = fallbacks.get(row[characters.MODEL_COLUMN], row[characters.MODEL_COLUMN])
		if refs:
			write_atomically(text_root / spec.character_shard,
				monsters.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		else:
			(text_root / spec.character_shard).unlink(missing_ok=True)

		# The event zones and the fortress row join their tables by
		# identity; rows already present (a reused variant root) stand.
		zone_path = text_root / "eventzonedata.txt"
		current_zones = tables.table_rows(zone_path.read_bytes())
		known_zone_ids = {row[1] for row in current_zones if len(row) > 1}
		write_atomically(zone_path, monsters.encode_rows(
			current_zones + [row for row in zones if row[1] not in known_zone_ids]))
		siege_path = text_root / "siegefortress.txt"
		current_siege = tables.table_rows(siege_path.read_bytes())
		if not any(len(row) > 2 and row[2] == spec.fortress_code for row in current_siege):
			write_atomically(siege_path, monsters.encode_rows(current_siege + [fortress_row]))

		# The staff's standings, by the shipped ids. A standing the
		# world already has stays the world's; one the prior marker
		# named stays owned through a rebuild.
		npcpos_path = text_root / "npcpos.txt"
		owned_standings = []
		if spec.staff_codes:
			native_id_by_code = {r[2]: r[1] for r in shipped_chars if len(r) > 2}
			later_id_by_code = {code: row[1] for code, row in later.items()}
			wanted = staff_standings(spec, later_npcpos, later_id_by_code, native_id_by_code)
			current = tables.table_rows(npcpos_path.read_bytes())
			standing = {tuple(row[:5]) for row in current if len(row) >= 5}
			prior_standings = {tuple(row) for row in prior.get("npcStandings", [])}
			appended = [row for row in wanted if tuple(row) not in standing]
			if appended:
				write_atomically(npcpos_path, characters.append_rows(npcpos_path.read_bytes(), appended))
			owned_standings = [row for row in wanted
				if tuple(row) not in standing or tuple(row) in prior_standings]

		# Region codes and zone names, on the rules the Alexandria map
		# merge uses: a code row joins where the current row is absent
		# or a placeholder, a zone name where its key is new.
		region_path = text_root / "regioncode.txt"
		current_regions = tables.table_rows(region_path.read_bytes())
		current_by_id = {row[1]: row for row in current_regions if len(row) >= 3}
		code_rows = [row for row in tables.table_rows(read("regioncode.txt"))
			if len(row) >= 4 and row[1].isdigit() and int(row[1]) in regions
			and current_by_id.get(row[1], [0, 0, "xxx"])[2] == "xxx"]
		replaced = {row[1] for row in code_rows}
		kept = [row for row in current_regions if len(row) < 2 or row[1] not in replaced]
		write_atomically(region_path, monsters.encode_rows(kept + code_rows))
		zone_name_path = text_root / "textzonename.txt"
		zone_keys = {row[1] for row in tables.table_rows(zone_name_path.read_bytes()) if len(row) > 1}
		zone_rows = [native_text_row(row) for row in later_zone_rows(read)
			if len(row) >= 10 and row[1] in {str(region) for region in regions}
			and row[1] not in zone_keys]
		write_atomically(zone_name_path, characters.append_rows(zone_name_path.read_bytes(), zone_rows))

		# The fortress's and the new structures' name texts join the
		# name table; a wanted symbol the later client never wrote is
		# a data-contract break. Only the rows this import writes need
		# names - the shipped characters' names ship with them.
		object_texts = (spec.object_texts or sky.later_object_texts)(read)
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted_symbols = {spec.name_symbol} | {row[5] for row in refs.values()}
		wanted = wanted_symbols - known
		texts = [native_text_row(r) for r in object_texts if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError(f"missing {spec.label} name texts: "
				+ str(sorted(wanted - {r[1] for r in texts})))
		stale_names = set(prior.get("nameSymbols", [])) - wanted_symbols
		if stale_names:
			base_text = monsters.encode_rows(monsters.drop_symboled_rows(
				characters.translation_rows(base_text), stale_names))
		write_atomically(text_root / "textdataname.txt", characters.append_rows(base_text, texts))

		# Map: catalogues over the variant's current ones, terrain with
		# its tile ids remapped, placements with their object ids.
		map_path, map_handle, map_data = spec.open_archive(root, "Map")
		try:
			map_index = tables.archive_index(map_data)
			archive_hashes["Map"] = tables.sha256_of(map_data)
			terrain, used_tiles, used_objects = {}, set(), set()
			for sector_x, sector_y in sectors:
				for extension in tables.MAP_EXTENSIONS:
					archive_path = f"{sector_y}/{sector_x}{extension}"
					entry = map_index.get(archive_path)
					if entry is None:
						raise ValueError(f"{spec.source_label} Map.pk2 has no {archive_path}")
					terrain[archive_path] = sro_pk2.payload(map_data, entry)
				used_tiles |= tables.terrain_tile_ids(terrain[f"{sector_y}/{sector_x}.m"])
				used_objects |= sky.o2_object_ids(terrain[f"{sector_y}/{sector_x}.o2"])
			if "tile2d.ifo" not in map_index or "object.ifo" not in map_index:
				raise ValueError(f"{spec.source_label} Map.pk2 lacks tile2d.ifo or object.ifo")
			later_tiles = tables.tile_catalog(sro_pk2.payload(map_data, map_index["tile2d.ifo"]))
			tiles_merged, tile_remap = variant.merge_tile_catalog(
				(output / "Map_extracted" / "tile2d.ifo").read_bytes(),
				sro_pk2.payload(map_data, map_index["tile2d.ifo"]), used_tiles)
			variant.replace_file(output / "Map_extracted" / "tile2d.ifo", tiles_merged)
			objects_merged, object_remap = sky.merge_object_catalog_remap(
				(output / "Map_extracted" / "object.ifo").read_bytes(),
				sro_pk2.payload(map_data, map_index["object.ifo"]), used_objects)
			variant.replace_file(output / "Map_extracted" / "object.ifo", objects_merged)

			copied = []
			current_tiles = tables.tile_catalog((output / "Map_extracted" / "tile2d.ifo").read_bytes())
			for tile_id in sorted(used_tiles):
				if tile_id not in later_tiles:
					raise ValueError(f"{spec.label} terrain paints tile id {tile_id} missing from tile2d.ifo")
				if current_tiles.get(tile_remap.get(tile_id, tile_id)) != later_tiles[tile_id]:
					continue
				name = later_tiles[tile_id][1].replace("\\", "/")
				entry = map_index.get("tile2d/" + name.lower())
				if entry is None:
					entry = map_index.get("tile2d/" + later_tiles[tile_id][0].replace("\\", "/").lower() + "/" + name.lower())
				if entry is None:
					raise ValueError(f"{spec.source_label} Map.pk2 has no tile texture {name}")
				target = output / "Map_extracted" / "tile2d" / name
				stored = sro_pk2.payload(map_data, entry)
				if target.exists() and target.read_bytes() == stored:
					continue
				if target.exists():
					# The variant already holds a texture of this name
					# from an earlier import; it stands, as in the
					# Alexandria build, and the clash is reported.
					copied.append("clash:Map_extracted/tile2d/" + name)
					continue
				variant.replace_file(target, stored)
				copied.append("Map_extracted/tile2d/" + name)
			for sector_x, sector_y in sectors:
				for extension in tables.MAP_EXTENSIONS:
					archive_path = f"{sector_y}/{sector_x}{extension}"
					stored = terrain[archive_path]
					if extension == ".m":
						stored = variant.remap_terrain(stored, tile_remap)
					elif extension == ".o2":
						stored = sky.patch_o2_objects(stored, object_remap)
					variant.replace_file(output / "Map_extracted" / archive_path, stored)
					copied.append("Map_extracted/" + archive_path)
		finally:
			map_data.close()
			map_handle.close()

		# Data: the sectors' navmesh, the zones' placements, and the
		# model closures of the placed objects and new characters.
		data_path, data_handle, data = spec.open_archive(root, "Data")
		particles_path, particles_handle, particles = spec.open_archive(root, "Particles")
		try:
			data_index = tables.archive_index(data)
			archive_hashes["Data"] = tables.sha256_of(data)
			archive_hashes["Particles"] = tables.sha256_of(particles)
			archives = {"Data": (data, data_index),
				"Particles": (particles, tables.archive_index(particles))}
			for authored in fallbacks:
				if resource_key(authored) in data_index:
					raise ValueError(f"{spec.source_label} Data.pk2 ships {authored}; drop its model fallback")
			copied += sky.copy_sector_navmesh(data, data_index, sectors, output, object_remap)
			ifo_entry = data_index.get("navmesh/objectstring.ifo")
			if ifo_entry is None:
				raise ValueError(f"{spec.source_label} Data.pk2 has no navmesh/objectstring.ifo")
			placements = objectstring_lines(spec,
				sro_pk2.payload(data, ifo_entry).decode("latin1"), zone_names)
			ifo_text = "JMXVOBJI1000\n" + str(len(placements)) + "\n" + "\n".join(placements) + "\n"
			variant.replace_file(output / NAVMESH / spec.portonly_ifo, ifo_text.encode("latin1"))
			copied.append((NAVMESH / spec.portonly_ifo).as_posix())
			merged_objects = tables.object_catalog((output / "Map_extracted" / "object.ifo").read_bytes())
			final_ids = {object_remap.get(object_id, object_id) for object_id in used_objects}
			if not final_ids <= set(merged_objects):
				raise ValueError(f"{spec.label} placements use object ids the merged catalogue lacks")
			seeds = {merged_objects[object_id][1] for object_id in final_ids}
			seeds |= {"res/" + row[characters.MODEL_COLUMN].replace("\\", "/")
				for row in refs.values() if row[characters.MODEL_COLUMN] not in ("", "xxx")}
			closure_result = closure(seeds, archives, native, output)
			copied += closure_result["files"]
		finally:
			data.close()
			data_handle.close()
			particles.close()
			particles_handle.close()

		mapinfo = output / NAVMESH / "mapinfo.mfo"
		variant.replace_file(mapinfo, variant.enable_regions(mapinfo.read_bytes(), sectors))

		files = sorted({entry for entry in set(prior.get("files", [])) | set(copied) if not entry.startswith("clash:")})
		summary = {
			"enabled": True,
			"characters": {code: int(row[1]) for code, row in refs.items()},
			"zones": len(zones),
			"zoneIds": sorted(int(row[1]) for row in zones),
			"fortressRow": True,
			"regions": sorted(regions),
			"sectors": [[x, y] for x, y in sectors],
			"tileRemap": {str(k): v for k, v in sorted(tile_remap.items())},
			"objectRemap": {str(k): v for k, v in sorted(object_remap.items())},
			"tileTextureClashes": sorted(e[len("clash:"):] for e in copied if e.startswith("clash:")),
			"missingSounds": closure_result["missingSounds"],
			"missingEffects": closure_result["missingEffects"],
			# The marker owns the rows this run appended and the ones an
			# earlier run owned, never a native row that already held a
			# wanted symbol: the off run reaps exactly these.
			"nameSymbols": sorted(((set(prior.get("nameSymbols", [])) & wanted_symbols)
				| {r[1] for r in texts})),
			"files": files,
			"archives": archive_hashes,
		}
		if spec.staff_codes:
			summary["npcStandings"] = owned_standings
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary
	finally:
		media.close()
		media_handle.close()

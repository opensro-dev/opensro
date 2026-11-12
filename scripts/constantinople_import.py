r"""
===========================================================================

constantinople_import.py - the Constantinople fortress area (v1.696)

PORT-ONLY, NOT NATIVE. Off unless SRO_FORT_CONSTANTINOPLE is "1", "on"
or "true". Called by build_alexandria_variant.py after the Sky Temple
import; the server reads the same switch. Build and server must agree,
and the flag-off build is byte-identical to the build without this
import: everything below joins only while the switch is on, and turning
it off reaps the shard, the copied files and the owned table rows.

The source is the operator's own v1.696 client (SRO_V1696_ROOT, default
inputs/sro_1696 beside the game root); its archives are never committed
and are verified against the pin file before anything reads them (the
Sky Temple import's verify_archives).

What the source authors, and what this import does with it:

	regions   the 36 RN_FORT_CONSTANTINOPLE sectors (a 6 x 6 block,
	          sectors x 90-95, z 59-64). Terrain, navmesh, tile and
	          object catalogues merge over the variant's current ones
	          exactly as the Sky Temple's do; regioncode and
	          textzonename rows join where the current row is absent
	          or a placeholder.
	fortress  the 49 INS_FORT_CT event zones join eventzonedata.txt,
	          the FORTRESS_CONSTANTINOPLE row joins siegefortress.txt,
	          and the seven structure characters the shipped tables
	          lack (the fort stone, the two gates and the four gate
	          pulleys) join as characterdata_210000.txt. The zones'
	          placements (navmesh\objectstring.ifo) are staged as
	          objectstring.portonly.ifo beside the native file; the
	          server bundle's structure-zones projection merges it
	          when it exists. The fortress then stands up through the
	          port's existing fortress line: the authority catalog
	          reads siegefortress.txt, appendFortressStructures joins
	          zones, placements and structure records, and
	          Authority.ForWorld binds world 5 (INS_FORT_CT, already in
	          the shipped gameworld catalogue) to fortress id 4. The
	          three fortress staff NPCs npcpos places in the yard are
	          shipped characters already; staging their spawns is a
	          population change and stays a follow-up, not this import.

The 210000 identity band: 120000 is the cap-110 family's, 160000 the
Sky Temple's, 170000 Jupiter's, 180000 the second hunting grounds',
190000 GOD's temples and the gears stones', 200000 Arabia's and
Muhan's. 210000 is this fortress's.

===========================================================================
"""
import argparse
import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import alexandria_npcs as characters  # noqa: E402
import build_alexandria_variant as variant  # noqa: E402
import cap110_monsters as monsters  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402
import skytemple_import as sky  # noqa: E402
import sro_pk2  # noqa: E402
from alexandria_map_tables import native_text_row  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402

FLAG = "SRO_FORT_CONSTANTINOPLE"
MARKER = ".opensro-constantinople.json"
CHARACTER_SHARD = "characterdata_210000.txt"
PORTONLY_IFO = "objectstring.portonly.ifo"
TEXT_ROOT = sky.TEXT_ROOT

REGION_CODE = "RN_FORT_CONSTANTINOPLE"
FORTRESS_CODE = "FORTRESS_CONSTANTINOPLE"
FORTRESS_NAME_SYMBOL = "SN_FORTRESS_CONSTANTINOPLE"
FORTRESS_WORLD = "INS_FORT_CT"
EXPECTED_REGIONS = 36
EXPECTED_ZONES = 49


# ================
# enabled
# ================
def enabled(env=os.environ):
	return env.get(FLAG, "").strip().lower() in tables.FLAG_ON


# ================
# fortress_regions
#
# The region ids regioncode.txt names RN_FORT_CONSTANTINOPLE. The
# fortress yard is one authored 6 x 6 sector block; any other count is
# a different client than the one this import was written against.
# ================
def fortress_regions(regioncode_rows):
	regions = {int(row[1]) for row in regioncode_rows
		if len(row) >= 3 and row[1].isdigit() and row[2].strip() == REGION_CODE}
	if len(regions) != EXPECTED_REGIONS:
		raise ValueError(f"v1.696 regioncode names {len(regions)} {REGION_CODE} regions, "
			f"expected {EXPECTED_REGIONS}")
	return regions


# ================
# fortress_zones
#
# The enabled eventzonedata rows of the fortress world, in file order.
# ================
def fortress_zones(eventzone_rows):
	zones = [row for row in eventzone_rows
		if len(row) > 10 and row[0] == "1" and row[10].strip() == FORTRESS_WORLD]
	if len(zones) != EXPECTED_ZONES:
		raise ValueError(f"v1.696 eventzonedata names {len(zones)} {FORTRESS_WORLD} zones, "
			f"expected {EXPECTED_ZONES}")
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
def objectstring_lines(text, zone_names):
	lines = [line for line in text.splitlines() if line.strip()]
	if len(lines) < 2 or lines[0] != "JMXVOBJI1000":
		raise ValueError("v1.696 objectstring.ifo has no JMXVOBJI1000 header")
	found, seen = [], set()
	for line in lines[2:]:
		parts = line.rsplit('"', 2)
		if len(parts) < 3:
			continue
		name = parts[1]
		if name in zone_names:
			if name in seen:
				raise ValueError(f"v1.696 objectstring.ifo places {name} twice")
			seen.add(name)
			found.append(line)
	if seen != set(zone_names):
		raise ValueError("v1.696 objectstring.ifo places "
			f"{len(seen)} of the {len(zone_names)} Constantinople zones")
	return found


# ================
# remove_owned
#
# The off path: everything the marker owns leaves - the character
# shard, the port-only placement file, the copied files, and the owned
# event-zone, siege-fortress, region and name rows. The catalogues and
# mapinfo need no reaping because the variant build rewrites them from
# the native tables on every run.
# ================
def remove_owned(output, prior):
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
				if len(row) <= 2 or row[2] != FORTRESS_CODE]))
	region_ids = {str(region) for region in prior.get("regions", [])}
	if region_ids:
		region_path = text_root / "regioncode.txt"
		if region_path.exists():
			write_atomically(region_path, monsters.encode_rows(
				[row for row in tables.table_rows(region_path.read_bytes())
					if len(row) <= 2 or row[1] not in region_ids or row[2].strip() != REGION_CODE]))
	symbols = set(prior.get("nameSymbols", []))
	if symbols:
		text_path = text_root / "textdataname.txt"
		if text_path.exists():
			write_atomically(text_path, monsters.encode_rows(
				monsters.drop_symboled_rows(characters.translation_rows(text_path.read_bytes()), symbols)))
	(text_root / CHARACTER_SHARD).unlink(missing_ok=True)
	(output / "Data_extracted" / "navmesh" / PORTONLY_IFO).unlink(missing_ok=True)
	for relative in prior.get("files", []):
		safe_target(output, relative).unlink(missing_ok=True)
	(output / MARKER).unlink(missing_ok=True)
	return {"enabled": False}


# ================
# add_constantinople
# ================
def add_constantinople(native, output, source=None, closure=sky.copy_resource_closure):
	native, output = Path(native).resolve(), Path(output).resolve()
	if native == output or native in output.parents or output in native.parents:
		raise ValueError("constantinople output must be separate from the native extraction")
	marker = output / MARKER
	prior = json.loads(marker.read_text(encoding="utf-8")) if marker.exists() else {}
	if not enabled():
		return remove_owned(output, prior)
	root = Path(source) if source else sky.source_root()
	sky.verify_archives(root)
	text_root = output / TEXT_ROOT

	media_path, media_handle, media = sky.open_archive(root, "Media")
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
		regions = fortress_regions(tables.table_rows(read("regioncode.txt")))
		zones = fortress_zones(tables.table_rows(read("eventzonedata.txt")))
		zone_names = {row[2].strip() for row in zones}
		needed = needed_characters(later, zones,
			tables.table_rows(read("npcpos.txt")), codename_by_id, regions)
		fortress_row = next((row for row in tables.table_rows(read("siegefortress.txt"))
			if len(row) > 2 and row[2] == FORTRESS_CODE), None)
		if fortress_row is None:
			raise ValueError("v1.696 siegefortress.txt names no " + FORTRESS_CODE)

		# The characters the shipped tables lack, converted to the
		# v1.150 layout (the conversion gives a structure the skills
		# its shipped twin carries, so no skill shard joins). The
		# shipped rows carry the same codenames and identities as the
		# later ones, so the codename test is the identity test.
		shipped_chars = [r for p in text_root.glob("characterdata_*.txt") if p.name != CHARACTER_SHARD
			for r in tables.table_rows(p.read_bytes())]
		shipped_codes = {r[2] for r in shipped_chars if len(r) > 2}
		refs = {code: characters.convert_character(later[code])
			for code in needed if code not in shipped_codes}
		occupied = {int(r[1]) for r in shipped_chars if len(r) > 1 and r[1].isdigit()}
		allocated = monsters.allocate_identities(occupied, sorted(refs), prior.get("characters", {}))
		for code, row in refs.items():
			row[1] = str(allocated[code])
		if refs:
			write_atomically(text_root / CHARACTER_SHARD,
				monsters.encode_rows(sorted(refs.values(), key=lambda row: int(row[1]))))
		else:
			(text_root / CHARACTER_SHARD).unlink(missing_ok=True)

		# The event zones and the fortress row join their tables by
		# identity; rows already present (a reused variant root) stand.
		zone_path = text_root / "eventzonedata.txt"
		current_zones = tables.table_rows(zone_path.read_bytes())
		known_zone_ids = {row[1] for row in current_zones if len(row) > 1}
		write_atomically(zone_path, monsters.encode_rows(
			current_zones + [row for row in zones if row[1] not in known_zone_ids]))
		siege_path = text_root / "siegefortress.txt"
		current_siege = tables.table_rows(siege_path.read_bytes())
		if not any(len(row) > 2 and row[2] == FORTRESS_CODE for row in current_siege):
			write_atomically(siege_path, monsters.encode_rows(current_siege + [fortress_row]))

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
		zone_rows = [native_text_row(row) for row in tables.table_rows(read("textzonename.txt"))
			if len(row) >= 10 and row[1] in {str(region) for region in regions}
			and row[1] not in zone_keys]
		write_atomically(zone_name_path, characters.append_rows(zone_name_path.read_bytes(), zone_rows))

		# The fortress's and the new structures' name texts join the
		# name table; a wanted symbol the later client never wrote is
		# a data-contract break. Only the rows this import writes need
		# names - the shipped characters' names ship with them.
		object_texts = sky.later_object_texts(read)
		base_text = (text_root / "textdataname.txt").read_bytes()
		known = {r[1] for r in characters.translation_rows(base_text) if len(r) > 1}
		wanted_symbols = {FORTRESS_NAME_SYMBOL} | {row[5] for row in refs.values()}
		wanted = wanted_symbols - known
		texts = [native_text_row(r) for r in object_texts if len(r) >= 10 and r[1] in wanted]
		if wanted - {r[1] for r in texts}:
			raise ValueError("missing constantinople name texts: "
				+ str(sorted(wanted - {r[1] for r in texts})))
		stale_names = set(prior.get("nameSymbols", [])) - wanted_symbols
		if stale_names:
			base_text = monsters.encode_rows(monsters.drop_symboled_rows(
				characters.translation_rows(base_text), stale_names))
		write_atomically(text_root / "textdataname.txt", characters.append_rows(base_text, texts))

		# Map: catalogues over the variant's current ones, terrain with
		# its tile ids remapped, placements with their object ids.
		map_path, map_handle, map_data = sky.open_archive(root, "Map")
		try:
			map_index = tables.archive_index(map_data)
			archive_hashes["Map"] = tables.sha256_of(map_data)
			sectors = sorted({(region & 0xFF, (region >> 8) & 0xFF) for region in regions})
			terrain, used_tiles, used_objects = {}, set(), set()
			for sector_x, sector_y in sectors:
				for extension in tables.MAP_EXTENSIONS:
					archive_path = f"{sector_y}/{sector_x}{extension}"
					entry = map_index.get(archive_path)
					if entry is None:
						raise ValueError(f"v1.696 Map.pk2 has no {archive_path}")
					terrain[archive_path] = sro_pk2.payload(map_data, entry)
				used_tiles |= tables.terrain_tile_ids(terrain[f"{sector_y}/{sector_x}.m"])
				used_objects |= sky.o2_object_ids(terrain[f"{sector_y}/{sector_x}.o2"])
			if "tile2d.ifo" not in map_index or "object.ifo" not in map_index:
				raise ValueError("v1.696 Map.pk2 lacks tile2d.ifo or object.ifo")
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
					raise ValueError(f"constantinople terrain paints tile id {tile_id} missing from tile2d.ifo")
				if current_tiles.get(tile_remap.get(tile_id, tile_id)) != later_tiles[tile_id]:
					continue
				name = later_tiles[tile_id][1].replace("\\", "/")
				entry = map_index.get("tile2d/" + name.lower())
				if entry is None:
					entry = map_index.get("tile2d/" + later_tiles[tile_id][0].replace("\\", "/").lower() + "/" + name.lower())
				if entry is None:
					raise ValueError(f"v1.696 Map.pk2 has no tile texture {name}")
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
		data_path, data_handle, data = sky.open_archive(root, "Data")
		particles_path, particles_handle, particles = sky.open_archive(root, "Particles")
		try:
			data_index = tables.archive_index(data)
			archive_hashes["Data"] = tables.sha256_of(data)
			archive_hashes["Particles"] = tables.sha256_of(particles)
			archives = {"Data": (data, data_index),
				"Particles": (particles, tables.archive_index(particles))}
			for sector_x, sector_y in sectors:
				nav = tables.navmesh_path(sector_x, sector_y)
				entry = data_index.get(nav)
				if entry is None:
					raise ValueError(f"v1.696 Data.pk2 has no {nav}")
				target = output / "Data_extracted" / "navmesh" / Path(nav).name
				stored = sro_pk2.payload(data, entry)
				if target.exists() and target.read_bytes() == stored:
					continue
				variant.replace_file(target, stored)
				copied.append("Data_extracted/" + nav)
			ifo_entry = data_index.get("navmesh/objectstring.ifo")
			if ifo_entry is None:
				raise ValueError("v1.696 Data.pk2 has no navmesh/objectstring.ifo")
			placements = objectstring_lines(
				sro_pk2.payload(data, ifo_entry).decode("latin1"), zone_names)
			ifo_text = "JMXVOBJI1000\n" + str(len(placements)) + "\n" + "\n".join(placements) + "\n"
			variant.replace_file(output / "Data_extracted" / "navmesh" / PORTONLY_IFO,
				ifo_text.encode("latin1"))
			copied.append("Data_extracted/navmesh/" + PORTONLY_IFO)
			merged_objects = tables.object_catalog((output / "Map_extracted" / "object.ifo").read_bytes())
			final_ids = {object_remap.get(object_id, object_id) for object_id in used_objects}
			if not final_ids <= set(merged_objects):
				raise ValueError("constantinople placements use object ids the merged catalogue lacks")
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

		mapinfo = output / "Data_extracted" / "navmesh" / "mapinfo.mfo"
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
		write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
		return summary
	finally:
		media.close()
		media_handle.close()


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--native-extracted", required=True)
	parser.add_argument("--output", required=True)
	parser.add_argument("--source-root")
	args = parser.parse_args()
	print(json.dumps(add_constantinople(args.native_extracted, args.output, args.source_root), indent=2))


if __name__ == "__main__":
	main()

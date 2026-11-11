"""
===========================================================================

import_survival_colosseum.py - stage the owned Survival Colosseum for the world build

Port-only, not native. Requires SRO_SURVIVAL_COLOSSEUM=on. Makes a separate
game root using the Alexandria pipeline's safe linking/catalog operations.
The default source is the checked-in Colosseum inputs, not a newer PK2.
Archives and the native extraction remain read-only. Model dependencies are
walked with the same BSR/BMT/BMS parsers the world publisher uses.

===========================================================================
"""
import argparse
import base64
import hashlib
import json
import mmap
import os
import subprocess
import struct
from contextlib import ExitStack
from pathlib import Path

import build_alexandria_variant as variant
import import_vsro_alexandria as source
import sro_pk2
from prepare_client_resources import safe_target, write_atomically

PIN_FILE = Path(__file__).with_name("survival_archive_pins.json")
MANIFEST = ".opensro-survival-colosseum.json"
SAFEZONE_PREFIX = "STRUCTURE_POS_COLOSSEUM_SAFEZONE_"
SCRIPT_ROOT = Path(__file__).resolve().parent
ASSET_ROOT = SCRIPT_ROOT.parent / "docs/features/assets/survival/colosseum"
ASSET_MANIFEST = "manifest.json"
ARENA_REF_BASE = 5100000
DEPENDENCIES = """
import fs from 'node:fs';
import {parseJmxResourceBsr, parseJmxBmtMaterialSet, parseJmxBmsStaticMesh, resolveBmtTexturePath}
from './scripts/build/world/objects/formats.mjs';
import {parseJmxMapObjectPlacementO2} from './scripts/build/world/jmx/JMXVMAPO1001.mjs';
import {ENV_TRACKS_READ_ORDER} from './scripts/build/world/assets/environment/environmentTracks.mjs';
import {readEnvironmentIfo} from './scripts/build/world/assets/environment/readEnvironmentIfo.mjs';
import {parseJmxVisualEffect,collectJmxEffectTexturePaths} from './scripts/build/effects/parseJmxVisualEffect.mjs';
import {collectMeshPaths} from './scripts/build/effects/buildEffectPrograms.mjs';
const {root, files, placements, environment, particles, effects} = JSON.parse(fs.readFileSync(0, 'utf8'));
if (environment) {
	const split = file => {
		const parsed = readEnvironmentIfo(file), bytes = fs.readFileSync(file);
		let at = 14;
		const string = () => { const n = bytes.readUInt32LE(at); at += 4 + n; if(at > bytes.length) throw Error('Truncated environment string'); };
		string();
		const header = bytes.subarray(0,at), entries = new Map();
		for(const entry of parsed.entries){
			const start = at;
			if(bytes.readUInt16LE(at) !== entry.key) throw Error('Environment parser mismatch');
			at += 2; string(); string(); string();
			for(const track of ENV_TRACKS_READ_ORDER){ const n=bytes.readUInt32LE(at); at += 4+n*(track.kind==='vec3'?16:8); if(at>bytes.length) throw Error('Truncated environment track'); }
			entries.set(entry.key,bytes.subarray(start,at));
		}
		return {header, entries, tail:bytes.subarray(at)};
	};
	const native=split(environment.native), source=split(environment.source);
	for(const key of environment.keys){
		const entry=source.entries.get(key);
		if(!entry) throw Error('Missing environment '+key);
		if(native.entries.has(key) && !native.entries.get(key).equals(entry)) throw Error('Conflicting native environment '+key);
		native.entries.set(key,entry);
	}
	const header=Buffer.from(native.header); header.writeUInt16LE(native.entries.size,12);
	console.log(JSON.stringify(Buffer.concat([header,...native.entries.values(),native.tail]).toString('base64')));
} else if (placements) {
	console.log(JSON.stringify([...new Set(parseJmxMapObjectPlacementO2(fs.readFileSync(placements)).placements.map(p => p.objectId))]));
} else {
	const paths = new Set();
	for (const file of files) {
		const bytes = fs.readFileSync(root + '/' + file);
		if (file.endsWith('.bsr')) {
			const value = parseJmxResourceBsr(bytes, file);
			if(effects) {
				for(const modifier of value.modifiers?.particleModifiers ?? []) for(const entry of modifier.entries ?? []) paths.add(entry.effectPath);
			} else for (const path of [...value.materialPaths, ...value.meshPaths]) paths.add(path);
		} else if (particles && file.endsWith('.efp')) {
			const value=parseJmxVisualEffect(bytes,file);
			for(const path of [...collectMeshPaths(value),...collectJmxEffectTexturePaths(value)]) paths.add(path);
		} else if (file.endsWith('.bmt')) {
			for (const m of parseJmxBmtMaterialSet(bytes, file).materials) {
				const texture = resolveBmtTexturePath(file, m.textureName);
				if (texture) paths.add(texture);
			}
		} else if (file.endsWith('.bms')) {
			const lightmap = parseJmxBmsStaticMesh(bytes, file).lightmap;
			if (lightmap) paths.add(lightmap.sourcePath);
		}
	}
	console.log(JSON.stringify([...paths]));
}
"""


# ================
# node_dependencies
# ================
def node_dependencies(**request):
	result = subprocess.run(["node", "--input-type=module", "-e", DEPENDENCIES],
		input=json.dumps(request), text=True, capture_output=True, cwd=SCRIPT_ROOT.parent, check=True)
	return json.loads(result.stdout)


# ================
# colosseum_region
# ================
def colosseum_region(raw):
	rows = [row for row in source.table_rows(raw) if len(row) >= 13 and row[0] == "1" and row[2].startswith(SAFEZONE_PREFIX)]
	regions = {int(row[5]) for row in rows}
	if len(regions) != 1 or len(rows) < 8:
		raise ValueError("Colosseum safezone table must identify one region and at least eight starts")
	return regions.pop()


# ================
# merge_objects
# ================
def merge_objects(native, imported, used):
	head, rows = variant.catalog_lines(native)
	_, extra = variant.catalog_lines(imported)
	old, new = source.object_catalog(native), source.object_catalog(imported)
	for key in used:
		if key not in new:
			raise ValueError(f"Colosseum object {key} has no source row")
		if key in old and not source.object_placeholder(old[key]) and old[key] != new[key]:
			raise ValueError(f"Colosseum object {key} conflicts with a native object")
		rows[key] = extra[key]
	return variant.format_catalog(head[0], rows).encode("cp949")


# ================
# arena_character_rows
# ================
def arena_character_rows(rows):
	base = next((row for row in rows if len(row) > 98 and row[0] == "1" and row[2] == "MOB_RM_ROCKY"), None)
	if base is None:
		raise ValueError("Missing native Rocky combat/model template")
	used = {int(row[1]) for row in rows if len(row) > 2 and row[1].isdigit()}
	result = []
	for level in range(89, 98):
		ref = ARENA_REF_BASE + level
		if ref in used:
			raise ValueError(f"Survival monster reference collision: {ref}")
		row = list(base)
		row[1], row[2], row[4], row[57] = str(ref), f"MOB_PORT_SOOK_ROCKY_{level}", base[2], str(level)
		# Port-only balance inference: quadratic level growth; native mesh, skills and timings stay authored.
		growth = (level / int(base[57])) ** 2
		for column in (59, 77, 78, 79, 80, 81, 83, 85):
			row[column] = str(round(int(base[column]) * growth))
		result.append(row)
	return result


# ================
# read_asset_input
# ================
def read_asset_input(root, manifest, archive, name):
	"""Read a pinned repository input through the same importer as a PK2 entry."""
	key = archive + "/" + sro_pk2.fold_ascii(name.replace("\\", "/"))
	target = safe_target(root, key)
	expected = manifest["files"].get(key)
	if expected is None:
		raise ValueError(f"Missing Colosseum dependency: {key}")
	content = target.read_bytes()
	if hashlib.sha256(content).hexdigest() != expected:
		raise ValueError(f"Colosseum input differs from its source pin: {key}")
	return content


# ================
# build_variant
# ================
def build_variant(client, native_root, output):
	client, native_root, output = client.resolve(), native_root.resolve(), output.resolve()
	if output == native_root or native_root in output.parents or output in native_root.parents or output == client or client in output.parents or output in client.parents:
		raise ValueError("Variant must be separate from the native game and source archives")
	if output.exists() and any(output.iterdir()) and not (output / MANIFEST).is_file():
		raise ValueError("Refuse reusing a nonempty folder without a Colosseum import manifest")
	pins = json.loads(PIN_FILE.read_text(encoding="utf8"))
	identity = {"format": "opensro-survival-colosseum-v1", "archives": pins, "nativeRoot": str(native_root)}
	if (output / MANIFEST).exists():
		previous = json.loads((output / MANIFEST).read_text(encoding="utf8"))
		if previous.get("format") != identity["format"] or previous.get("nativeRoot") != identity["nativeRoot"] or any(pins.get(key) != value for key, value in previous.get("archives", {}).items()):
			raise ValueError("Variant manifest belongs to different input assets")
	inputs = None
	if (client / ASSET_MANIFEST).is_file():
		inputs = json.loads((client / ASSET_MANIFEST).read_text(encoding="utf8"))
		if inputs.get("format") != "opensro-survival-colosseum-inputs-v1" or inputs.get("archives") != pins or not isinstance(inputs.get("files"), dict):
			raise ValueError("Colosseum asset manifest does not match the inspected source")
	with ExitStack() as stack:
		archives, indexes = {}, {}
		for name, expected in (() if inputs is not None else pins.items()):
			handle = stack.enter_context((client / f"{name}.pk2").open("rb"))
			data = stack.enter_context(mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ))
			if source.sha256_of(data) != expected:
				raise ValueError(f"{name}.pk2 differs from the inspected Colosseum source pin")
			archives[name] = data
			indexes[name] = source.archive_index(data)

		# ================
		# read
		# ================
		def read(archive, name):
			if inputs is not None:
				return read_asset_input(client, inputs, archive, name)
			key = sro_pk2.fold_ascii(name.replace("\\", "/"))
			entry = indexes[archive].get(key)
			if entry is None:
				raise ValueError(f"Missing Colosseum dependency: {archive}/{key}")
			return sro_pk2.payload(archives[archive], entry)

		teleport = read("Media", source.TEXTDATA + "teleportdata.txt")
		region = colosseum_region(teleport)
		x, y = source.region_xy(region)
		native = native_root / "extracted"
		out = output / "extracted"
		write_atomically(output / MANIFEST, (json.dumps({**identity, "complete": False}, indent=2) + "\n").encode("utf8"))
		for folder in ("Media_extracted", "Map_extracted", "Data_extracted", "Particles_extracted", "Music_mp3"):
			variant.link_tree(native / folder, out / folder)
		for name in ("SRO_Client.exe", "Particles.pk2"):
			if not (output / name).exists():
				variant.link_file(native_root / name, output / name)
		map_root = out / "Map_extracted"
		textdata = native / "Media_extracted" / source.TEXTDATA
		character_rows = [row for file in textdata.glob("characterdata*.txt") for row in source.table_rows(file.read_bytes())]
		arena_rows = arena_character_rows(character_rows)
		arena_table = "\r\n".join("\t".join(row) for row in arena_rows) + "\r\n"
		variant.replace_file(out / "Media_extracted" / source.TEXTDATA / "characterdata_sook_survival.txt", b"\xff\xfe" + arena_table.encode("utf-16-le"))
		minimap = f"minimap/{x}x{y}.ddj"
		variant.replace_file(safe_target(out / "Media_extracted", minimap), read("Media", minimap))
		terrain = read("Map", f"{y}/{x}.m")
		tiles, remap = variant.merge_tile_catalog((native / "Map_extracted/tile2d.ifo").read_bytes(),
			read("Map", "tile2d.ifo"), source.terrain_tile_ids(terrain))
		variant.replace_file(map_root / "tile2d.ifo", tiles)
		for name in source.region_files(x, y):
			content = read("Map", name)
			if name.endswith(".m"):
				content = variant.remap_terrain(content, remap)
			variant.replace_file(safe_target(map_root, name), content)
		modern_environment = output / "colosseum-environment-source.ifo"
		variant.replace_file(modern_environment, read("Map", "environment.ifo"))
		environment_keys = sorted({struct.unpack_from("<H", terrain, 12 + i * source.MAPM_BLOCK_BYTES + 4)[0] for i in range(source.MAP_BLOCKS)})
		merged_environment = node_dependencies(environment={"native": str(native / "Map_extracted/environment.ifo"),
			"source": str(modern_environment), "keys": environment_keys})
		variant.replace_file(map_root / "environment.ifo", base64.b64decode(merged_environment))
		used = node_dependencies(placements=str(map_root / f"{y}/{x}.o2"))
		objects = read("Map", "object.ifo")
		variant.replace_file(map_root / "object.ifo", merge_objects(
			(native / "Map_extracted/object.ifo").read_bytes(), objects, used))
		# A different texture at a native path would silently retexture the old world.
		for tile in source.terrain_tile_ids(terrain):
			texture = source.tile_catalog(read("Map", "tile2d.ifo"))[tile][1].replace("\\", "/").lower()
			name = "tile2d/" + texture
			target = safe_target(map_root, name)
			content = read("Map", name)
			if target.exists() and target.read_bytes() != content:
				raise ValueError(f"Terrain texture path conflicts with native data: {name}")
			variant.replace_file(target, content)
		data_root = out / "Data_extracted"
		variant.replace_file(data_root / source.navmesh_path(x, y), read("Data", source.navmesh_path(x, y)))
		variant.replace_file(data_root / "navmesh/mapinfo.mfo", variant.enable_regions(
			(native / "Data_extracted/navmesh/mapinfo.mfo").read_bytes(), [(x, y)]))
		queue = [source.object_catalog(objects)[key][1].replace("\\", "/").lower() for key in used]
		imported = {}
		while queue:
			batch = []
			for name in queue:
				name = name.replace("\\", "/").lower()
				if name in imported:
					continue
				content = read("Data", name)
				target = safe_target(data_root, name)
				if target.exists() and target.read_bytes() != content:
					raise ValueError(f"Model dependency conflicts with native data: {name}")
				variant.replace_file(target, content)
				imported[name] = hashlib.sha256(content).hexdigest()
				batch.append(name)
			queue = node_dependencies(root=str(data_root), files=batch) if batch else []
		particle_root = out / "Particles_extracted"
		queue = node_dependencies(root=str(data_root), files=[name for name in imported if name.endswith(".bsr")], effects=True)
		particle_files = {}
		while queue:
			batch = []
			for name in queue:
				name = name.replace("\\", "/").lower()
				if name in particle_files:
					continue
				content = read("Particles", name)
				target = safe_target(particle_root, name)
				if target.exists() and target.read_bytes() != content:
					raise ValueError(f"Particle dependency conflicts with native data: {name}")
				variant.replace_file(target, content)
				particle_files[name] = hashlib.sha256(content).hexdigest()
				batch.append(name)
			queue = node_dependencies(root=str(particle_root), files=batch, particles=True) if batch else []
		manifest = {**identity, "complete": True, "portOnly": True,
			"region": region, "sector": [x, y], "archives": pins, "nativeRoot": str(native_root),
			"objectIds": sorted(used), "tileRemap": remap, "files": imported}
		manifest["environmentKeys"] = environment_keys
		manifest["particleFiles"] = particle_files
		manifest["arenaReferences"] = [{"refObjId": int(row[1]), "codename": row[2], "level": int(row[57]), "baseCodename": row[4]} for row in arena_rows]
		write_atomically(output / MANIFEST, (json.dumps(manifest, indent=2) + "\n").encode("utf8"))
		return manifest


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--client-root", type=Path, default=ASSET_ROOT, help="Colosseum inputs (default: repository assets); original pinned archives remain supported")
	parser.add_argument("--native-root", type=Path, required=True)
	parser.add_argument("--variant-root", type=Path, required=True)
	args = parser.parse_args()
	if os.environ.get("SRO_SURVIVAL_COLOSSEUM", "").lower() not in ("1", "on", "true"):
		parser.error("Port-only import requires SRO_SURVIVAL_COLOSSEUM=on")
	print(json.dumps(build_variant(args.client_root, args.native_root, args.variant_root), indent=2))


if __name__ == "__main__":
	main()

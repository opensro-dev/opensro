"""
===========================================================================

variant_build.py - stage an opt-in Alexandria variant candidate

The variant (v1.150 plus Alexandria, port-only, not native) ships as a
separate opt-in candidate; the native candidate stays byte-identical
(#1745). This tool runs the four builds into one staging folder:

	1. the stage 1 VSRO import, isolated under the staging folder and
	   pinned before it reads an archive (import_vsro_alexandria.py)
	2. the variant game root (build_alexandria_variant.py)
	3. the variant client data (the assets build, against the variant)
	4. the variant server bundle (buildServerGameDataBundle.mjs)

It then refuses the staging unless the variant bundle holds the
Alexandria overlay and the variant textdata's cap110 overlays, and the
native server archive and client manifest it was shown hash exactly as
they did before the build. Nothing here publishes: stage the printed
server archive with data_release.py --server-data, and the variant
client data beside it, when the staging rehearsal is scheduled.

	py variant_build.py --vsro-root <folder> --staging <folder> \\
		--data-version <version> --native-server-archive <server.srogz> \\
		--native-client-manifest <manifest.json> \\
		[--monster-database <folder>] [--native-mapinfo <mapinfo.mfo>]

The cap110 equipment and skill tiers are shard settings (#1745):
SRO_CAP110_ITEMS and SRO_CAP110_SKILLS, registered in the shard
settings table with "off" as the deployment default, are consumed here
at build time. This tool reads them from its environment, hands them
to the variant step normalized, and refuses a bundle whose equipment
or skill shard disagrees with them.

SRO_CAP110_MONSTERS is read the same way. On, it needs the monster
database export (--monster-database), which both the import and the
variant step read, and the bundle must hold the cap110 overlays the
variant textdata wrote. Off, the variant carries no field monsters
and no overlay is required.

The stage 1 import compares the VSRO map against the native v1.150
mapinfo.mfo. The isolated import root holds no native extraction, so
this tool resolves the native file in its own (native) environment,
hands the path over, and adds it to the native identity it checks.

===========================================================================
"""

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

REPO_ROOT = Path(__file__).resolve().parents[4]
SCRIPTS = REPO_ROOT / "scripts"
OVERLAY_NAME = "alexandria-overlay.json"
OVERLAY_FORMAT = "sro-server-alexandria-overlay"
OVERLAY_PATH = Path("textdata") / OVERLAY_NAME
TEXTDATA_DIR = Path("extracted") / "Media_extracted" / "server_dep" / "silkroad" / "textdata"
STAGE1_OUTPUT_FOLDER = "Alexandria_extracted"
HASH_CHUNK_BYTES = 1 << 20
# The build-time shard settings (#1745): the shard settings table's
# names, the import tools' on values (import_vsro_alexandria.FLAG_ON),
# and the textdata shard each tier adds when on.
ITEMS_FLAG = "SRO_CAP110_ITEMS"
SKILLS_FLAG = "SRO_CAP110_SKILLS"
FLAG_ON = ("1", "on", "true")
SHARD_FILES = {ITEMS_FLAG: "itemdata_110000.txt", SKILLS_FLAG: "skilldata_110000.txt"}
# The monster setting adds no shard; it adds the cap110-*.txt overlays and
# needs the database export (cap110_monsters.SOURCE_ENV).
MONSTERS_FLAG = "SRO_CAP110_MONSTERS"
MONSTER_DATABASE_ENV = "SRO_CAP110_MONSTER_DATABASE"
NATIVE_MAPINFO = Path("Data_extracted") / "navmesh" / "mapinfo.mfo"


# ================
# VariantInputs
#
# What one variant build reads. native_files are the artifacts whose
# bytes must not move; build adds the native mapinfo to them.
# ================
@dataclass
class VariantInputs:
	vsro_root: Path
	staging: Path
	data_version: str
	native_files: list
	native_mapinfo: Path
	monster_database: Path = None
	native_server_data: Path = None


# ================
# sha256_file
#
# Streamed, so a server archive is never held in memory.
# ================
def sha256_file(path):
	digest = hashlib.sha256()
	with open(path, "rb") as source:
		for chunk in iter(lambda: source.read(HASH_CHUNK_BYTES), b""):
			digest.update(chunk)
	return digest.hexdigest()


# ================
# file_hashes
#
# The native identity the build must not move. A missing file stops the
# build before any step runs: there is nothing to prove identity against.
# ================
def file_hashes(paths):
	hashes = {}
	for path in paths:
		if not path.is_file():
			raise ValueError(f"native artifact {path} not found; build the native candidate first")
		hashes[path] = sha256_file(path)
	return hashes


# ================
# require_same_hashes
# ================
def require_same_hashes(before, after):
	for path, expected in before.items():
		if after[path] != expected:
			raise ValueError(f"native artifact changed during the variant build: {path}")


# ================
# run_process
#
# The one step door. Arguments are passed directly; candidate text never
# becomes shell code.
# ================
def run_process(argv, env):
	subprocess.run(argv, cwd=REPO_ROOT, env=env, check=True)


# ================
# shard_settings
#
# The three build-time settings, normalized to the "on"/"off" the shard
# settings table deploys, with the import tools' own on values.
# ================
def shard_settings(env):
	return {
		flag: "on" if str(env.get(flag, "")).strip().lower() in FLAG_ON else "off"
		for flag in (*SHARD_FILES, MONSTERS_FLAG)
	}


# ================
# monster_overrides
#
# The monster setting and its database, for the two steps that read
# them. Off passes an empty database, so a stale one in the caller's
# environment cannot reach a step.
# ================
def monster_overrides(inputs, settings):
	database = str(inputs.monster_database) if settings[MONSTERS_FLAG] == "on" else ""
	return {MONSTERS_FLAG: settings[MONSTERS_FLAG], MONSTER_DATABASE_ENV: database}


# ================
# step_commands
#
# The four builds, in order, as (argv, environment overrides). The stage 1
# import runs against an isolated game root so its extraction can never
# land in the native tree; it reads the native mapinfo by the path this
# tool resolved. The variant root then reads that import. The variant
# step receives the build-time settings explicitly: its add_items,
# add_skills and add_monsters steps read them, and their off paths
# remove what an earlier on build left behind.
# ================
def step_commands(inputs, settings):
	staging = inputs.staging
	variant = staging / "variant"
	variant_env = {"SRO_GAME_ROOT": str(variant), "SRO_GENERATED_ROOT": str(variant / ".generated")}
	monsters = monster_overrides(inputs, settings)
	return [
		([sys.executable, str(SCRIPTS / "import_vsro_alexandria.py"), "--vsro-root", str(inputs.vsro_root),
			"--native-mapinfo", str(inputs.native_mapinfo)],
			{"SRO_GAME_ROOT": str(staging / "stage1"), "SRO_ALEXANDRIA": "1", **monsters}),
		([sys.executable, str(SCRIPTS / "build_alexandria_variant.py"), "--vsro-root", str(inputs.vsro_root),
			"--variant-root", str(variant),
			"--imported-root", str(staging / "stage1" / "extracted" / STAGE1_OUTPUT_FOLDER)],
			{"SRO_ALEXANDRIA": "1", **settings, **monsters}),
		(["node", "scripts/task.mjs", "assets", "build", "full"], variant_env),
		(["node", "scripts/build/server/buildServerGameDataBundle.mjs",
			"--output", str(staging / "server-data"), "--data-version", inputs.data_version], variant_env),
	]


# ================
# verify_variant
#
# The staged bundle must be the variant, not a silent native rebuild:
# the overlay the server gates on SRO_ALEXANDRIA, and every cap110
# overlay of the variant textdata, projected into the bundle. Monsters
# on must have written at least one overlay. The equipment and skill
# shards must match the build-time settings: a bundle built against
# other settings than the declared ones is not the candidate the
# operator asked for.
# ================
def verify_variant(staging, shards):
	bundle_textdata = staging / "server-data" / "textdata"
	overlay = bundle_textdata / OVERLAY_NAME
	if not overlay.is_file():
		raise ValueError(f"variant bundle holds no {OVERLAY_PATH}; the build read a native game root")
	if json.loads(overlay.read_text(encoding="utf-8")).get("format") != OVERLAY_FORMAT:
		raise ValueError(f"{overlay} is not a {OVERLAY_FORMAT} document")
	variant_textdata = staging / "variant" / TEXTDATA_DIR
	overlays = sorted(path.name.lower() for path in variant_textdata.glob("cap110-*.txt"))
	if not overlays and shards[MONSTERS_FLAG] == "on":
		raise ValueError(f"variant textdata {variant_textdata} holds no cap110-*.txt overlay with {MONSTERS_FLAG}=on")
	missing = [name for name in overlays if not (bundle_textdata / name).is_file()]
	if missing:
		raise ValueError(f"variant bundle is missing cap110 overlays: {', '.join(missing)}")
	for flag, shard in SHARD_FILES.items():
		if (bundle_textdata / shard).is_file() != (shards[flag] == "on"):
			raise ValueError(f"variant bundle's {shard} disagrees with {flag}={shards[flag]}")
	if not (staging / "server-data.srogz").is_file():
		raise ValueError("variant bundle wrote no server-data.srogz archive")


# ================
# verify_native
#
# The counterpart of verify_variant, when the operator names the native
# bundle folder: it must hold no overlay, or the native candidate the
# byte-identity check protects is not native at all.
# ================
def verify_native(native_server_data):
	if native_server_data is not None and (native_server_data / OVERLAY_PATH).exists():
		raise ValueError(f"native bundle {native_server_data} holds {OVERLAY_NAME}; it is not native")


# ================
# build
#
# Native identity (with the native mapinfo the import reads) before the
# first step and after the last; any drift refuses the whole staging.
# Monsters on without a database export stop before any step: the
# import would otherwise fail halfway with the staging half written.
# ================
def build(inputs, run=run_process, env=os.environ):
	staging = inputs.staging
	if staging.exists() and any(staging.iterdir()):
		raise ValueError(f"staging folder {staging} is not empty; a variant build needs a fresh one")
	settings = shard_settings(env)
	if settings[MONSTERS_FLAG] == "on" and inputs.monster_database is None:
		raise ValueError(f"{MONSTERS_FLAG}=on needs --monster-database, the monster database export")
	native_files = [*inputs.native_files, inputs.native_mapinfo]
	before = file_hashes(native_files)
	verify_native(inputs.native_server_data)
	staging.mkdir(parents=True, exist_ok=True)
	for argv, overrides in step_commands(inputs, settings):
		print(" ".join(argv), flush=True)
		run(argv, {**env, **overrides})
	verify_variant(staging, settings)
	require_same_hashes(before, file_hashes(native_files))
	return staging / "server-data.srogz"


# ================
# native_mapinfo_path
#
# The native mapinfo.mfo, resolved by the repository's path owner in this
# tool's own environment, which is the native game root.
# ================
def native_mapinfo_path():
	sys.path.insert(0, str(SCRIPTS))
	from sro_paths import EXTRACTED_ROOT
	return EXTRACTED_ROOT / NATIVE_MAPINFO


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--vsro-root", type=Path, required=True, help="the pinned VSRO client folder")
	parser.add_argument("--staging", type=Path, required=True, help="a fresh folder for the variant candidate")
	parser.add_argument("--data-version", required=True, help="the variant bundle's data version label")
	parser.add_argument("--native-server-archive", type=Path, required=True,
		help="the native server.srogz whose bytes must not move")
	parser.add_argument("--native-client-manifest", type=Path, required=True,
		help="the native client manifest whose bytes must not move")
	parser.add_argument("--native-server-data", type=Path,
		help="the native bundle folder, checked for a stray overlay")
	parser.add_argument("--monster-database", type=Path,
		help=f"the monster database export; required with {MONSTERS_FLAG}=on")
	parser.add_argument("--native-mapinfo", type=Path,
		help="the native v1.150 mapinfo.mfo (default: the native extraction's)")
	arguments = parser.parse_args()
	archive = build(VariantInputs(arguments.vsro_root, arguments.staging, arguments.data_version,
		[arguments.native_server_archive, arguments.native_client_manifest],
		arguments.native_mapinfo or native_mapinfo_path(), arguments.monster_database,
		arguments.native_server_data))
	print(f"variant server archive: {archive}")
	print("stage it with data_release.py --server-data; nothing was published")


if __name__ == "__main__":
	main()

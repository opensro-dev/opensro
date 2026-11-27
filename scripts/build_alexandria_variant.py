"""
===========================================================================

build_alexandria_variant.py - stage 2 of Alexandria: a v1.150 + Alexandria game root

PORT-ONLY, NOT NATIVE. Off unless SRO_ALEXANDRIA is "1", "on" or "true".

The asset pipeline binds to one game root (paths.mjs, sro_paths.py), so the
safe way to build the city without touching the native extraction is a second
game root. This tool makes it, from three inputs:

	the native v1.150 extraction     SRO_GAME_ROOT/extracted
	the stage 1 import               extracted/Alexandria_extracted
	the VSRO client                  --vsro-root (Data.pk2 for the models)

The VSRO archives are hash-pinned (vsro_archive_pins.json, checked
before anything is read; a missing or changed archive fails the build,
#1745). The archives are the operator's own and are never committed.

into <variant root>/extracted/:

	Media_extracted, Particles_extracted, Music_mp3
		hard links to the native files (copies where links are refused);
		Particles_extracted also gains the VSRO effect files the native lacks.
	Map_extracted
		native files, plus the city's 36 regions with their ground-tile ids
		remapped where the VSRO catalog disagrees with the native one,
		plus tile2d.ifo / object.ifo merged (the native row wins; a VSRO row
		fills a native placeholder or a new id), the new tile textures, and
		the VSRO environment.ifo (a superset; see ENVIRONMENT_NOTE).
	Data_extracted
		native files, plus every VSRO Data.pk2 file the native tree lacks
		(the city's buildings and their materials and textures), the city's
		navmesh, and navmesh/mapinfo.mfo with the city's bits set.

No native file is ever overwritten or changed: a link is never written
through, and a VSRO file is added only where no native file of that name
exists, except source-authored overview tiles covering imported regions.
A rerun with the same code, flags and sources over an untouched tree is
skipped (variant_stamp.py); --force rebuilds anyway. Below that, each
importer is a step of its own (variant_steps.py, #3993): it reruns only
when its own code, flags, sources or the tree the steps before it left
changed, and replays its recorded effect byte for byte otherwise.

Build the variant with its own generated tree:

	SRO_GAME_ROOT=<variant root> SRO_GENERATED_ROOT=<variant root>/.generated pnpm assets build full

===========================================================================
"""
import argparse
import json
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
# The steps are functions of their own modules, called by the runner: no
# step runs code of this file, so its imports key none of them (#4251).
import alexandria_map_tables  # noqa: E402
import alexandria_npcs  # noqa: E402
import area_music  # noqa: E402
import import_vsro_alexandria as stage1  # noqa: E402
import pharaoh_temple as temple  # noqa: E402
import published_ids  # noqa: E402
import variant_base  # noqa: E402
import variant_files  # noqa: E402
import variant_gates  # noqa: E402
import variant_settle  # noqa: E402
import variant_stamp  # noqa: E402
import variant_steps  # noqa: E402
from build_parallelism import build_jobs  # noqa: E402
from prepare_client_resources import write_atomically  # noqa: E402
from sro_paths import EXTRACTED_ROOT  # noqa: E402
from variant_files import build_switch, link_file, log  # noqa: E402

VARIANT_MANIFEST = ".opensro-alexandria-variant.json"
# Game-root files the build reads besides extracted/: the doctor and the
# builders want the client and Particles.pk2 beside it.
LINKED_FILES = ("SRO_Client.exe", "Particles.pk2")
# environment.ifo: 45 native entries against the VSRO's 60. Keys 6 and 11
# differ between the clients, so the variant's regions that use them sky
# slightly differently from native v1.150; a binary merge would avoid that.
ENVIRONMENT_NOTE = "environment keys 6 and 11 differ from native in the variant"

# The shops' stock choice (#2846): unset, the Alexandria variant stocks
# each store with the goods one official source had active; on, the
# shop passes stock the port's own degree goods instead.
PORT_STOCK_FLAG = "SRO_ALEXANDRIA_PORT_STOCK"


#============================================================================
# Build
#============================================================================


# ================
# clear_content
#
# A build lays the variant's content down from nothing. The steps below
# only add (link_tree skips a target that exists, add_missing_files fills
# what the native tree lacks), so over last release's tree a re-extracted
# native file would keep its old bytes and a file the sources dropped
# would never leave (ops #368 review, 2026-11-16). Removing a hard link
# leaves the native file it names alone. The build state beside the tree
# (.state: the settle ledger, the asset caches) and the generated tree
# stay: settle gives each file whose bytes come back the same its old time
# (variant_settle.py), so the caches downstream still hit.
# ================
def clear_content(variant, native):
	out = variant / "extracted"
	if out.resolve() == Path(native).resolve() or Path(native).resolve() in out.resolve().parents:
		raise SystemExit(f"{out} is inside the native extraction; refusing to clear it")
	if out.exists():
		shutil.rmtree(out)
	for name in (VARIANT_MANIFEST,) + LINKED_FILES:
		(variant / name).unlink(missing_ok=True)
	return out


# ================
# build_variant
# ================
def build_variant(vsro_root, native, variant, imported_root=None, force=False, explain=False):
	"""
	Make the variant's extracted/ tree; returns its manifest. explain only
	prints which steps a build would rerun and why, and changes nothing.
	"""
	imported = Path(imported_root) if imported_root else native / stage1.OUTPUT_FOLDER
	# Stage 1 of this build hashed these archives minutes ago (#3590); an
	# unchanged file with today's pinned digest is not hashed again.
	stage1.admit_verified_archives(imported)
	stage1.require_vsro_pin(vsro_root, stage1.ARCHIVE_NAMES)
	stage_manifest = json.loads((imported / stage1.MANIFEST_NAME).read_text(encoding="utf-8"))
	regions = [(r["sectorX"], r["sectorY"]) for r in stage_manifest["regions"]]

	# The v1.696 client's folders, shared by the avatar pass, the world
	# layers and the music: a root without its archives has no layers.
	from mall_avatar_import import add_avatars, add_source, avatars_zip, source_pins, v1696_root, V1696_SOURCE, AVATARS_SOURCE
	v1696 = v1696_root()
	later = v1696
	if later is not None and not all((later / f"{name}.pk2").is_file() for name in ("Map", "Data", "Media", "Particles")):
		log(f"world layers: {later} lacks the v1.696 archives; no seasonal layers")
		later = None
	avatars = avatars_zip()

	# Every importer is a step (variant_steps.py, #3993): it reruns only
	# when its own code, flags, sources or the tree the steps before it
	# left changed, and replays its recorded effect otherwise. The order
	# is load-bearing exactly as this sequence always was.
	from cap110_item_import import add_items
	from nasrun_import import add_nasrun, add_nasrun_skills
	from pet_import import add_all_sources
	from cap110_skill_import import add_skills, add_skill_effects
	from cap110_monsters import add_monsters
	from cap110_rewards import add_rewards
	import variant_later_layers
	from title_import import add_titles
	from roc_import import add_roc
	import bandit_fortress_import
	import hotan_fortress_import
	import honor_items
	from cap110_quest_layers import add_quest_layers
	from alexandria_npc_services import add_services
	from world_layer_import import add_layers
	import later_mall_goods

	# The tree every step writes; a fresh build clears it below, once the
	# journal has been read.
	out = variant / "extracted"
	runner = variant_steps.Runner(
		variant, variant_stamp.source_roots(native, imported, vsro_root, variant, os.environ)
	)
	got: dict = {}
	runner.register(
		"base", variant_base.build_base, (vsro_root, native, imported, out, regions, stage_manifest),
		roots=(native, imported, vsro_root)
	)
	runner.register("gates", variant_gates.add_gates, (vsro_root, native, out,))
	runner.register("temple", temple.add_temple, (vsro_root, native, out,))
	# The map step adds to textdataname.txt on top of the gates' texts.
	runner.register(
		"map", alexandria_map_tables.add_map, (vsro_root, native, out, regions), later=lambda: (got["gates"][1]["text"],)
	)
	runner.register("npcs", alexandria_npcs.add_npcs, (vsro_root, native, out, regions))
	# Imported equipment is independently optional; its off path also removes
	# a shard from a previous on build so a reused variant cannot retain it.
	runner.register("items", add_items, (vsro_root, native, out,))
	# The mall avatars append their own shard to the manifest the item
	# import just wrote, so they must follow it.
	runner.register("avatars", add_avatars, (vsro_root, native, out,))
	# The v1.696 client's avatars the VSRO client lacks (SRO_V1696_ROOT,
	# pinned by v1696_archive_pins.json). Unset removes a previous import.
	runner.register("avatars-v1696", add_source, (v1696, native, out, V1696_SOURCE,), roots=(v1696,) if v1696 else ())
	# The v2.355 export's avatars from the pinned archive (SRO_AVATARS_ZIP,
	# #3254): without it the build is byte-identical and a previous
	# import is removed.
	runner.register("avatars-archive", add_source, (avatars, native, out, AVATARS_SOURCE,), roots=(avatars,) if avatars else ())
	# The Devil's Spirit (Nasrun) rows, inert (SRO_NASRUN, #3348): after the
	# avatars, whose shards and identities they keep clear of.
	runner.register("nasrun", add_nasrun, (vsro_root, native, out,))
	# The later clients' pets follow the avatars: their COS rows and
	# summon scrolls append their own shards to the same manifests. The
	# v1.696 pass and the pinned-archive pass read their sources from the
	# environment and skip cleanly while those are not on the machine.
	runner.register("pets", add_all_sources, (vsro_root, native, out,))
	runner.register("skills", add_skills, (vsro_root, native, out,))
	runner.register("skill-effects", add_skill_effects, (vsro_root, native, out,))
	# The skills a worn Devil's Spirit lends (SRO_NASRUN, #3456): after
	# the skill tier, which rewrites the skill manifest from the native
	# one, and before the monsters, whose skill ids keep clear of them.
	runner.register("nasrun-skills", add_nasrun_skills, (vsro_root, native, out,))
	runner.register("monsters", add_monsters, (vsro_root, native, out,))
	# The monsters' own database rewards follow the population they describe.
	runner.register("rewards", add_rewards, (vsro_root, native, out,))
	# The later sources' optional layers, in their load-bearing order
	# (variant_later_layers.py owns the order and the manifest keys).
	# Each layer is its own step, so a change to one reruns that layer
	# alone: as one step, any layer's change reran all of them, 380-445 s
	# in r204j/r204l (#4337 item 11).
	for key, add in variant_later_layers.LATER_LAYERS:
		runner.register(variant_later_layers.step_name(key), add, (native, out,))
	# The later client's character title table (#3502): later-native,
	# no switch, so it is a stage here rather than a layer - titles.txt
	# joins the variant's server textdata after the layers merged
	# theirs, and the server bundle's projection copies it with the
	# root's other tables. A tree naming no later client prints one
	# skip line; a tree naming one that cannot produce the table
	# fails inside title_import.
	runner.register("titles", add_titles, (native, out,))
	# The Roc range's kinds (SRO_ROC, #3399): after every later layer,
	# so their identities allocate above all of the layers' rows.
	runner.register("roc", add_roc, (vsro_root, native, out,))
	# The two siege fortresses the v1.188 client authored and v1.150 never
	# placed (#3886): after Roc, in a fixed order, so their structures
	# allocate above every row before. The Bandit fortress is shipped
	# content, imported unconditionally (#3586); Hotan keeps its switch
	# (SRO_FORT_HOTAN).
	runner.register("fortress-bandit", bandit_fortress_import.add, (vsro_root, native, out,))
	runner.register("fortress-hotan", hotan_fortress_import.add, (vsro_root, native, out,))
	# The honor line the client authored and never shipped (SRO_HONOR_GOODS,
	# #3548): after every other item import, whose identities it keeps
	# clear of, and before the shops that sell it.
	runner.register("honor-goods", honor_items.add_honor_items, (native, out,))
	# The Blue Berserk imports follow their own switches (cap110_quest_layers.py owns the order).
	runner.register("quest-layers", add_quest_layers, (vsro_root, native, out,))
	runner.register(
		"services", add_services, (vsro_root, native, out, port_stock_enabled()),
		later=lambda: (got["honor-goods"]["enabled"],)
	)
	# The v1.696 mall goods whose items the tree now holds (later-native,
	# no switch): after every item, pet and layer import, so it sees all
	# of their rows. Without the v1.696 client a previous listing is removed.
	runner.register("later-mall", later_mall_goods.add_goods, (v1696, native, out,), roots=(v1696,) if v1696 else ())
	# The later client's seasonal town decoration (Halloween and the other
	# event layers), into the object catalog and region files written above.
	# Without the v1.696 archives a previous import is removed.
	runner.register(
		"world-layers", add_layers, (later, native, out, source_pins(V1696_SOURCE) if later else None),
		roots=(later,) if later else ()
	)
	# Music last: its areas are those of every region the steps above left
	# in the tree, stage 1's, the temple's and each later layer's (#3546).
	runner.register("music", area_music.add_music, (vsro_root, native, out, later,), roots=(later,) if later else ())
	runner.register(
		"environment", variant_files.copy_vsro_file,
		(vsro_root, "Map", "environment.ifo", out / "Map_extracted" / "environment.ifo")
	)

	runner.load({
		"native": str(Path(native).resolve()),
		"imported": str(Path(imported).resolve()),
		"vsro": str(Path(vsro_root).resolve()),
		"variant": str(Path(variant).resolve()),
	})
	if explain:
		for name, verdict in runner.explain():
			log(f"step {name}: {verdict}")
		return None
	runner.begin(force)
	if runner.fresh:
		# A build with no usable journal lays the variant down from
		# nothing, as it always did.
		clear_content(variant, native)
	out.mkdir(parents=True, exist_ok=True)
	for name in LINKED_FILES:
		source = native.parent / name
		if not source.is_file():
			raise SystemExit(f"{source} is missing; the variant needs the v1.150 client files")
		if not (variant / name).exists():
			link_file(source, variant / name)

	def take(name):
		return runner.result(runner.index(name))

	got["base"] = take("base")
	got["gates"] = take("gates")
	gate_tables = got["gates"][1]
	stranded = stage1.gates_without_terrain(gate_tables["teleportdata.txt"], variant_gates.GATE_CODES,
		set(regions) | stage1.mfo_regions((native / "Data_extracted" / "navmesh" / "mapinfo.mfo").read_bytes()))
	if stranded:
		raise SystemExit(f"gates {', '.join(stranded)} stand on regions stage 1 did not import; rerun stage 1")
	got["temple"] = take("temple")
	got["map"] = take("map")
	got["npcs"] = take("npcs")
	got["items"] = take("items")
	got["avatars"] = take("avatars")
	got["avatars-v1696"] = take("avatars-v1696")
	got["avatars-archive"] = take("avatars-archive")
	got["nasrun"] = take("nasrun")
	got["pets"] = take("pets")
	got["skills"] = take("skills")
	got["skill-effects"] = take("skill-effects")
	got["nasrun-skills"] = take("nasrun-skills")
	got["monsters"] = take("monsters")
	got["rewards"] = take("rewards")
	got["later-layers"] = {key: take(variant_later_layers.step_name(key)) for key, _ in variant_later_layers.LATER_LAYERS}
	got["titles"] = take("titles")
	got["roc"] = take("roc")
	got["fortress-bandit"] = take("fortress-bandit")
	got["fortress-hotan"] = take("fortress-hotan")
	got["honor-goods"] = take("honor-goods")
	got["quest-layers"] = take("quest-layers")
	got["services"] = take("services")
	got["later-mall"] = take("later-mall")
	got["world-layers"] = take("world-layers")
	got["music"] = take("music")
	got["environment"] = take("environment")

	base = got["base"]
	manifest = {
		"fieldMonsters": got["monsters"],
		"monsterRewards": got["rewards"],
		**got["later-layers"],
		"worldLayers": got["world-layers"],
		**got["quest-layers"],
		"format": "opensro-alexandria-variant-v1",
		"portOnly": True,
		"regions": stage_manifest["regions"],
		"tileRemap": base["tileRemap"],
		"tileTextureClashes": base["tileTextureClashes"],
		"mapFilesAdded": base["mapFilesAdded"],
		"dataFilesAdded": base["dataFilesAdded"],
		"particleFilesAdded": base["particleFilesAdded"],
		"gates": got["gates"][0],
		"map": got["map"],
		"music": got["music"],
		"npcs": got["npcs"],
		"npcServices": got["services"],
		"honorGoods": got["honor-goods"],
		"weapons": got["items"],
		"mallAvatars": got["avatars"],
		"mallAvatarsV1696": got["avatars-v1696"],
		"mallAvatarsArchive": got["avatars-archive"],
		"laterMallGoods": {key: value for key, value in got["later-mall"].items() if key != "packages"},
		"nasrun": got["nasrun"],
		"nasrunSkills": got["nasrun-skills"],
		"roc": got["roc"],
		"titles": got["titles"],
		"pets": got["pets"]["pets"],
		"petsV1696": got["pets"]["petsV1696"],
		"petsArchive": got["pets"]["petsArchive"],
		"skills": got["skills"],
		"skillEffects": got["skill-effects"],
		"linked": base["linked"],
		"note": ENVIRONMENT_NOTE
	}
	pharaoh_temple = got["temple"]
	if pharaoh_temple["enabled"]:
		# Absent while off, so an off manifest is what it was before the temple.
		manifest["pharaohTemple"] = pharaoh_temple
	# Absent while off, as the temple: an off manifest stays byte-for-byte.
	bandit_fortress = got["fortress-bandit"]
	if bandit_fortress["enabled"]:
		manifest["banditFortress"] = bandit_fortress
	hotan_fortress = got["fortress-hotan"]
	if hotan_fortress["enabled"]:
		manifest["hotanFortress"] = hotan_fortress
	# Items, characters and teleport buildings share one id space and the
	# client keeps one object per id: a fox pet whose id a gears stone also
	# held spawned as a ground item and disconnected players (#3994).
	shared = published_ids.shared_objects(out / "Media_extracted" / stage1.TEXTDATA)
	if shared:
		sample = ", ".join(f"{identity} {'/'.join(codes)}" for identity, codes in sorted(shared.items())[:5])
		raise ValueError(f"{len(shared)} object ids are held by two rows: {sample}")
	# A released object keeps its id whichever importer numbered it: a
	# player's records name it by id (#4019).
	moved = published_ids.moved_objects(out / "Media_extracted" / stage1.TEXTDATA)
	if moved:
		sample = ", ".join(f"{code} {pinned}->{built}" for code, (pinned, built) in sorted(moved.items())[:5])
		raise ValueError(f"{len(moved)} released object ids moved: {sample}")
	write_atomically(variant / VARIANT_MANIFEST, (json.dumps(manifest, indent=2) + "\n").encode("utf-8"))
	# The journal lands only with the manifest it describes: a build that
	# died anywhere above leaves the previous journal (and its tree) the
	# record, so the next build replays or reruns from it.
	runner.finish()
	return manifest


# ================
# port_stock_enabled
#
# The shops' stock choice (#2846), from SRO_ALEXANDRIA_PORT_STOCK: off -
# the default, and the value every official build has - asks the shop
# passes for each store's official stock; on asks for the port's own
# degree goods.
# ================
def port_stock_enabled(environ=None):
	return build_switch(PORT_STOCK_FLAG, environ)


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description="Build the v1.150 + Alexandria game root (port-only, not native).")
	parser.add_argument("--vsro-root", default=os.environ.get(stage1.ROOT_ENV), help="VSRO client folder")
	parser.add_argument("--variant-root", required=True, help="the new game root to create")
	parser.add_argument("--imported-root", type=Path, help="isolated stage-1 import instead of the native extraction's import folder")
	parser.add_argument("--force", action="store_true", help="rebuild even when the stamp says nothing changed")
	parser.add_argument(
		"--explain", action="store_true",
		help="print which steps a build would rerun and which key part moved; builds nothing (seconds)"
	)
	args = parser.parse_args()
	if not stage1.flag_enabled():
		raise SystemExit(f"{stage1.FLAG_ENV} is not on. Alexandria is port-only, not native.")
	if not args.vsro_root:
		raise SystemExit(f"Pass --vsro-root or set {stage1.ROOT_ENV}.")
	variant = Path(args.variant_root).resolve()
	if variant == EXTRACTED_ROOT.parent.resolve() or EXTRACTED_ROOT.resolve() in variant.parents:
		raise SystemExit("The variant root must be a different folder from the native game root.")
	vsro_root = Path(args.vsro_root)
	imported = Path(args.imported_root) if args.imported_root else EXTRACTED_ROOT / stage1.OUTPUT_FOLDER
	if args.explain:
		build_variant(vsro_root, EXTRACTED_ROOT, variant, args.imported_root, explain=True)
		return
	# Same code, flags and sources as the last successful build of this
	# tree, and the tree untouched since: nothing to do (variant_stamp.py).
	inputs = variant_stamp.read_inputs(EXTRACTED_ROOT, imported, vsro_root, variant)
	stale = ["--force"] if args.force else variant_stamp.stale_parts(variant, inputs, (VARIANT_MANIFEST,) + LINKED_FILES)
	if not stale:
		log(f"unchanged since the last build of {variant}; skipped (pass --force to rebuild)")
		return
	log(f"rebuilding: {', '.join(stale[:8])}{' ...' if len(stale) > 8 else ''}")
	variant_stamp.clear_stamp(variant)
	manifest = build_variant(vsro_root, EXTRACTED_ROOT, variant, args.imported_root, force=args.force)
	# Files the rebuild left byte-identical get their old mtime back, so the
	# asset build's (size, mtime) caches still hit (variant_settle.py).
	settled = variant_settle.settle(variant, build_jobs())
	log(f"settle: {settled['files']} files, {settled['restored']} unchanged kept their time, "
		f"{settled['changed']} changed, {settled['unread']} matched unread")
	# Stamped with what the build read before it started; a source that
	# moved under it leaves no stamp, so the next run rebuilds.
	moved = variant_stamp.differences(inputs, variant_stamp.read_inputs(EXTRACTED_ROOT, imported, vsro_root, variant))
	if moved:
		log(f"not stamped: {', '.join(moved)} changed during the build")
	else:
		variant_stamp.write_stamp(variant, inputs)
	log(f"tile remap {manifest['tileRemap']}, {manifest['mapFilesAdded']} map files, {manifest['dataFilesAdded']} data files")
	log(f"Next: SRO_GAME_ROOT={variant} SRO_GENERATED_ROOT={variant / '.generated'} pnpm assets build full")


if __name__ == "__main__":
	main()

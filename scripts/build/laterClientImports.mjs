/*
===========================================================================

laterClientImports.mjs - the later client's imported assets

The M window's dungeon pages and their chrome exist in the later
client only (issues #2661, #3210), so the build imports them from the
pinned VSRO archive when SRO_VSRO_ROOT names it and never commits the
art: the Python importers verify the archive against its pin (#2956),
publish into the loose public tree, and these steps claim what they
published for the publication ledger. Without the archive each step
prints its one skip line and the build ships the plain world map.

===========================================================================
*/
import { readFile, stat } from "node:fs/promises";
import path from "node:path";

import { runPython } from "./shared/pythonRun.mjs";
import { claimPublicPaths } from "./shared/publicationLedger.mjs";
import { publicRoot, rebuildRoot } from "./world/paths.mjs";

// The later client's dungeon floor pages (#2661) live under this catalogue;
// the client asks for exactly it and the images it names.
const LATER_DUNGEON_MAP_CATALOG = "/assets/data/later-dungeon-map.json";

/*
================
laterDungeonMapImportPlan

What the dungeon-map step does with one environment: run the importer
against SRO_VSRO_ROOT's folder (the later-client importers' own
convention), or skip with the one line the build prints. archiveThere
says the folder holds a Media.pk2; the importer verifies the archive
against its pin before reading a byte, so a wrong archive fails the
build rather than slipping in (#2956).
================
*/
export function laterDungeonMapImportPlan( env = process.env, archiveThere = false ) {
	const root = (env.SRO_VSRO_ROOT ?? "").trim();
	if ( !root ) {
		return {
			run: false,
			line: "Later dungeon maps: skipped (SRO_VSRO_ROOT is not set; the M window keeps the plain world map)"
		};
	}
	if ( !archiveThere ) {
		return { run: false, line: `Later dungeon maps: skipped (no Media.pk2 under SRO_VSRO_ROOT ${root})` };
	}
	return { run: true, root, args: [ "scripts/import_vsro_dungeon_maps.py", "--vsro-root", root ] };
}

/*
================
laterDungeonMapPublicPaths

The public paths one import publishes, read back from its own catalogue
so the publication ledger claims exactly what the client will ask for:
the catalogue, one stitched page per floor, and the gate sign icon.
================
*/
export function laterDungeonMapPublicPaths( catalog ) {
	const paths = [ LATER_DUNGEON_MAP_CATALOG ];
	if ( catalog && typeof catalog.imageRoot === "string" ) {
		for ( const map of catalog.maps ?? [] ) {
			if ( typeof map?.image === "string" && map.image ) paths.push( catalog.imageRoot + map.image + ".png" );
		}
		if ( typeof catalog.signIcon === "string" && catalog.signIcon ) {
			paths.push( catalog.imageRoot + catalog.signIcon + ".png" );
		}
	}
	return paths;
}

/*
================
importLaterDungeonMapsStep

The M window's dungeon floor pages are native in the later client only
(#2661), so the build imports them from the pinned archive when one is
configured and never commits the art: the catalogue and its stitched
pages land in the loose public tree after the preload sweep and the
texture variants, in time for the final pack tail, and the step claims
them for the publication ledger. Without the archive the build says so
in one line and ships the plain world map.
================
*/
export async function importLaterDungeonMapsStep() {
	const root = (process.env.SRO_VSRO_ROOT ?? "").trim();
	const archiveThere = root !== "" &&
		await stat( path.join( root, "Media.pk2" ) ).then( ( entry ) => entry.isFile(), () => false );
	const plan = laterDungeonMapImportPlan( process.env, archiveThere );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[dungeon-maps] later-client dungeon map import",
		cwd: rebuildRoot,
		context: [
			"The dungeon floor pages are native to the later client (#2661); the build imports them " +
			"from the pinned VSRO archive (SRO_VSRO_ROOT) and publishes them with the other loose assets."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	const catalog = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "data", "later-dungeon-map.json" ), "utf8" )
	);
	claimPublicPaths( laterDungeonMapPublicPaths( catalog ) );
	return { maps: Array.isArray( catalog.maps ) ? catalog.maps.length : 0 };
}

// The later client's M window chrome (issue #3210, the v1.188 layout):
// the big globe, the follow pin's two mode faces, the floor selector,
// the window's foot, corner and map edges, the guide tabs and the reverse
// return point's three faces (#3391), under the interface folders the
// v1.150 extraction already publishes.
const LATER_WORLDMAP_BUTTON_ROOT = "/assets/images/Media_extracted/interface/";

/*
================
laterWorldMapButtonsImportPlan

What the world-map button step does with one environment: the same
contract as the dungeon maps beside it (SRO_VSRO_ROOT folder, else the
one skip line), because one archive carries both.
================
*/
export function laterWorldMapButtonsImportPlan( env = process.env, archiveThere = false ) {
	const root = (env.SRO_VSRO_ROOT ?? "").trim();
	if ( !root ) {
		return {
			run: false,
			line: "Later world map buttons: skipped (SRO_VSRO_ROOT is not set; the dungeon pages keep the plain strip)"
		};
	}
	if ( !archiveThere ) {
		return { run: false, line: `Later world map buttons: skipped (no Media.pk2 under SRO_VSRO_ROOT ${root})` };
	}
	return { run: true, root, args: [ "scripts/import_vsro_worldmap_buttons.py", "--vsro-root", root ] };
}

/*
================
laterWorldMapButtonPublicPaths

The public paths one button import publishes: a fixed set, named here
once so the step claims exactly what the map tools will ask for.
================
*/
export function laterWorldMapButtonPublicPaths() {
	return [
		"worldmap/wmap_big_button_world",
		"worldmap/wmap_button_automatic",
		"worldmap/wmap_button_passivity",
		"worldmap/wmap_up_button",
		"worldmap/wmap_up_button_disable",
		"worldmap/wmap_down_button",
		"worldmap/wmap_down_button_disable",
		"worldmap/wmap_floor_box",
		"worldmap/wmap_floor_choice_box",
		"worldmap/wmap_floor_big_box",
		"worldmap/wmap_wnd_bottom_frame",
		"worldmap/wmap_wnd_corner_02",
		"worldmap/wmap_window_edge_1",
		"worldmap/wmap_window_edge_2",
		"worldmap/wmap_window_edge_3",
		"worldmap/wmap_window_edge_4",
		"guide/gd_area_tab_on",
		"guide/gd_area_tab_off",
		"guide/gd_monster_tab_on",
		"guide/gd_monster_tab_off",
		"guide/gd_long_index_open",
		"guide/gd_long_contents_event",
		"worldmap/map/xy_returnicon",
		"worldmap/map/xy_returnicon_focus",
		"worldmap/map/xy_returnicon_press"
	].map( ( name ) => LATER_WORLDMAP_BUTTON_ROOT + name + ".png" );
}

/*
================
importLaterWorldMapButtonsStep

The globe, the follow pin and the floor selector of the later client's
dungeon pages (issue #3210) come from the same pinned archive and the
same convention as the floor pages: imported loose beside the v1.150
worldmap images after the preload sweep, claimed for the publication
ledger, and skipped with one line without the archive.
================
*/
export async function importLaterWorldMapButtonsStep() {
	const root = (process.env.SRO_VSRO_ROOT ?? "").trim();
	const archiveThere = root !== "" &&
		await stat( path.join( root, "Media.pk2" ) ).then( ( entry ) => entry.isFile(), () => false );
	const plan = laterWorldMapButtonsImportPlan( process.env, archiveThere );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[dungeon-maps] later-client world map button import",
		cwd: rebuildRoot,
		context: [
			"The dungeon pages' globe, follow pin and floor selector are native to the later client " +
			"(issue #3210); the build imports them from the pinned VSRO archive (SRO_VSRO_ROOT) and " +
			"publishes them with the other loose assets."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	const paths = laterWorldMapButtonPublicPaths();
	for ( const publicPath of paths ) {
		const there = await stat( path.join( publicRoot, publicPath ) )
			.then( ( entry ) => entry.isFile(), () => false );
		if ( !there ) throw Error( `The world map button import published no ${publicPath}` );
	}
	claimPublicPaths( paths );
	return { buttons: paths.length };
}

// The renewal M map's guide (issue #3387): its tables, the hunting
// ground masks and the guide's own art, from the operator's pinned
// v1.696 client (SRO_V1696_ROOT), the only client that ships them.
const LATER_WORLDMAP_GUIDE_CATALOG = "/assets/data/worldmap-guide.json";

/*
================
laterWorldMapGuideImportPlan

What the guide step does with one environment: run the importer
against SRO_V1696_ROOT's folder, or skip with the one line the build
prints. The importer verifies the archive against its pin first.
================
*/
export function laterWorldMapGuideImportPlan( env = process.env, archiveThere = false ) {
	const root = (env.SRO_V1696_ROOT ?? "").trim();
	if ( !root ) {
		return {
			run: false,
			line: "Later world map guide: skipped (SRO_V1696_ROOT is not set; the M map guide stays empty)"
		};
	}
	if ( !archiveThere ) {
		return { run: false, line: `Later world map guide: skipped (no Media.pk2 under SRO_V1696_ROOT ${root})` };
	}
	return { run: true, root, args: [ "scripts/import_v1696_worldmap_guide.py", "--v1696-root", root ] };
}

/*
================
laterWorldMapGuidePublicPaths

The public paths one guide import publishes, read back from its own
catalogue: the catalogue, its art and every monster's tile masks.
================
*/
export function laterWorldMapGuidePublicPaths( catalog ) {
	const paths = [ LATER_WORLDMAP_GUIDE_CATALOG ];
	if ( catalog && typeof catalog.imageRoot === "string" ) {
		for ( const name of catalog.art ?? [] ) {
			if ( typeof name === "string" && name ) paths.push( catalog.imageRoot + name + ".png" );
		}
		for ( const monster of catalog.monsters ?? [] ) {
			for ( const tile of monster?.tiles ?? [] ) {
				if ( typeof tile?.[2] === "string" && tile[2] ) paths.push( catalog.imageRoot + tile[2] + ".png" );
			}
		}
	}
	return paths;
}

/*
================
importLaterWorldMapGuideStep

The guide's data and art are imported loose beside the v1.150 interface
images after the preload sweep, claimed for the publication ledger, and
skipped with one line without the archive, as the steps above.
================
*/
export async function importLaterWorldMapGuideStep() {
	const root = (process.env.SRO_V1696_ROOT ?? "").trim();
	const archiveThere = root !== "" &&
		await stat( path.join( root, "Media.pk2" ) ).then( ( entry ) => entry.isFile(), () => false );
	const plan = laterWorldMapGuideImportPlan( process.env, archiveThere );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[worldmap-guide] later-client world map guide import",
		cwd: rebuildRoot,
		context: [
			"The M map guide's tables and art are native to the v1.696 client (#3387); the build imports " +
			"them from the pinned archive (SRO_V1696_ROOT) and publishes them with the other loose assets."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	const catalog = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "data", "worldmap-guide.json" ), "utf8" )
	);
	claimPublicPaths( laterWorldMapGuidePublicPaths( catalog ) );
	return { monsters: Array.isArray( catalog.monsters ) ? catalog.monsters.length : 0 };
}

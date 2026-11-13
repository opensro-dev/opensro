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

// The later client's dungeon-page map chrome (issue #3210): the big
// globe, the follow pin's two mode faces and the floor selector, under
// the same worldmap folder the v1.150 extraction already publishes.
const LATER_WORLDMAP_BUTTON_ROOT = "/assets/images/Media_extracted/interface/worldmap/";

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
		"wmap_big_button_world",
		"wmap_button_automatic",
		"wmap_button_passivity",
		"wmap_up_button",
		"wmap_up_button_disable",
		"wmap_down_button",
		"wmap_down_button_disable",
		"wmap_floor_box",
		"wmap_floor_choice_box",
		"wmap_floor_big_box",
		"wmap_button_location",
		"wmap_sign_location"
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

/*
===========================================================================

laterClientImports.mjs - the later client's imported assets

The M window's dungeon pages and their chrome exist in the later
client only (issues #2661, #3210), so the build imports them from the
pinned VSRO archive and never commits the art: the Python importers
verify the archive against its pin (#2956), publish into the loose public
tree, and these steps claim what they published for the publication
ledger. Which archive is shared/laterClientRoot.mjs's call: the shell's
SRO_VSRO_ROOT, else the one the game tree's variant was built from. Only
a tree with neither prints the one skip line per step and ships the plain
world map; a named root without its archive fails the build.

===========================================================================
*/
import { readFile, stat } from "node:fs/promises";
import path from "node:path";

import { resolveVsroRoot, stampedVsroRoot } from "./shared/laterClientRoot.mjs";
import { runPython } from "./shared/pythonRun.mjs";
import { claimPublicPaths } from "./shared/publicationLedger.mjs";
import { publicRoot, rebuildRoot } from "./world/paths.mjs";

// The later client's dungeon floor pages (#2661) live under this catalogue;
// the client asks for exactly it and the images it names.
const LATER_DUNGEON_MAP_CATALOG = "/assets/data/later-dungeon-map.json";

/*
================
vsroImportPlan

The plan every VSRO step makes for one environment: run its importer
against the resolved later client, or skip with its one line when
neither the shell nor the variant stamp names one. archiveThere says
the folder holds a Media.pk2. A named folder without it throws: the tree
was built to carry the later art, and the quiet skip shipped a release
variant without it (11-17).
================
*/
function vsroImportPlan( step, env, archiveThere, stamped ) {
	const { root, source } = resolveVsroRoot( env, stamped );
	if ( !root ) {
		return {
			run: false,
			line: `${step.label}: skipped (SRO_VSRO_ROOT is not set and no variant stamp names a later client; ` +
				`${step.without})`
		};
	}
	if ( !archiveThere ) {
		throw Error(
			`${step.label}: ${source} names the later client ${root}, but it holds no Media.pk2. Restore that ` +
				`folder or set SRO_VSRO_ROOT to the pinned VSRO client; without it ${step.without}.`
		);
	}
	return { run: true, root, args: [ step.script, "--vsro-root", root ] };
}

/*
================
vsroStepPlan

A step's plan against the real environment: resolves the root once,
looks for its archive, then asks plan.
================
*/
async function vsroStepPlan( plan, stamped ) {
	const { root } = resolveVsroRoot( process.env, stamped );
	const archiveThere = root !== "" &&
		await stat( path.join( root, "Media.pk2" ) ).then( ( entry ) => entry.isFile(), () => false );
	return plan( process.env, archiveThere, stamped );
}

/*
================
laterDungeonMapImportPlan

What the dungeon-map step does with one environment (vsroImportPlan):
run the importer against the resolved later client, or skip with the one
line the build prints. The importer verifies the archive against its pin
before reading a byte, so a wrong archive fails the build rather than
slipping in (#2956).
================
*/
export function laterDungeonMapImportPlan( env = process.env, archiveThere = false, stamped = stampedVsroRoot() ) {
	return vsroImportPlan(
		{
			label: "Later dungeon maps",
			without: "the M window keeps the plain world map",
			script: "scripts/import_vsro_dungeon_maps.py"
		},
		env,
		archiveThere,
		stamped
	);
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
export async function importLaterDungeonMapsStep( stamped = stampedVsroRoot() ) {
	const plan = await vsroStepPlan( laterDungeonMapImportPlan, stamped );
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
export function laterWorldMapButtonsImportPlan( env = process.env, archiveThere = false, stamped = stampedVsroRoot() ) {
	return vsroImportPlan(
		{
			label: "Later world map buttons",
			without: "the dungeon pages keep the plain strip",
			script: "scripts/import_vsro_worldmap_buttons.py"
		},
		env,
		archiveThere,
		stamped
	);
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
export async function importLaterWorldMapButtonsStep( stamped = stampedVsroRoot() ) {
	const plan = await vsroStepPlan( laterWorldMapButtonsImportPlan, stamped );
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

// CIFMacroSystemGuide's under-minimap icon (#3907): icon/etc/mecro_0.ddj
// from the pinned v1.696 Media.pk2 (SRO_V1696_ROOT).
const LATER_MACRO_GUIDE_ICON = "/assets/images/Media_extracted/icon/etc/mecro_0.png";

/*
================
laterMacroGuideImportPlan
================
*/
export function laterMacroGuideImportPlan( env = process.env, archiveThere = false ) {
	const root = (env.SRO_V1696_ROOT ?? "").trim();
	if ( !root ) {
		return {
			run: false,
			line: "Later macro guide: skipped (SRO_V1696_ROOT is not set; the under-minimap macro icon stays missing)"
		};
	}
	if ( !archiveThere ) {
		return { run: false, line: `Later macro guide: skipped (no Media.pk2 under SRO_V1696_ROOT ${root})` };
	}
	return { run: true, root, args: [ "scripts/import_v1696_macro_guide.py", "--v1696-root", root ] };
}

/*
================
laterMacroGuidePublicPaths
================
*/
export function laterMacroGuidePublicPaths() {
	return [ LATER_MACRO_GUIDE_ICON ];
}

/*
================
importLaterMacroGuideStep

Publishes mecro_0.png for CIFMacroSystemGuide. Without the archive the
build says so in one line; the HUD hides the button when the image is
missing.
================
*/
export async function importLaterMacroGuideStep() {
	const root = (process.env.SRO_V1696_ROOT ?? "").trim();
	const archiveThere = root !== "" &&
		await stat( path.join( root, "Media.pk2" ) ).then( ( entry ) => entry.isFile(), () => false );
	const plan = laterMacroGuideImportPlan( process.env, archiveThere );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[macro-guide] later-client SRO Macro guide icon import",
		cwd: rebuildRoot,
		context: [
			"CIFMacroSystemGuide's mecro_0 icon is native to the v1.696 client (#3907); the build imports " +
			"it from the pinned archive (SRO_V1696_ROOT) beside the other Media_extracted icons."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	const paths = laterMacroGuidePublicPaths();
	for ( const publicPath of paths ) {
		const there = await stat( path.join( publicRoot, publicPath ) )
			.then( ( entry ) => entry.isFile(), () => false );
		if ( !there ) throw Error( `The macro guide import published no ${publicPath}` );
	}
	claimPublicPaths( paths );
	return { icons: paths.length };
}

// The reverse return map's destination table (SRO_REVERSE_RETURN_MAP):
// the later client's refoptionalteleport.txt, written beside the v1.150
// extraction (extracted/VsroOptionalTeleport_extracted) for the server
// game-data build to project. Without it the server infers its points
// from the recall gates and every unique nest, which is not the table
// the later client offers.

/*
================
laterOptionalTeleportImportPlan

What the reverse return table step does with one environment: the same
contract as the map buttons (SRO_VSRO_ROOT folder, else the one skip
line), because one archive carries both. The importer verifies the
archive against its pin before reading a byte.
================
*/
export function laterOptionalTeleportImportPlan(
	env = process.env,
	archiveThere = false,
	stamped = stampedVsroRoot()
) {
	return vsroImportPlan(
		{
			label: "Later reverse return table",
			without: "the server infers its map points",
			script: "scripts/import_vsro_optional_teleport.py"
		},
		env,
		archiveThere,
		stamped
	);
}

/*
================
importLaterOptionalTeleportStep

Writes the import into the game root, not the public tree: the client
receives the points from the server's world references, so nothing is
published or claimed here. Skipped with one line without the archive.
================
*/
export async function importLaterOptionalTeleportStep( stamped = stampedVsroRoot() ) {
	const plan = await vsroStepPlan( laterOptionalTeleportImportPlan, stamped );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[reverse-return] later-client optional teleport import",
		cwd: rebuildRoot,
		context: [
			"The reverse return map's destinations are the later client's refoptionalteleport.txt; the build " +
			"imports them from the pinned VSRO archive (SRO_VSRO_ROOT) for the server game-data build."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	return { imported: true };
}

// The Ch'in Tomb's floor minimap (the "jangan cave" radar background):
// v1.150 ships floor art for Donwhang Stone Cave alone, the pinned later
// client also has minimap_d/jinsi on the same grid. Its own catalogue sits
// beside the retail one, which never changes; the client merges it into
// the tomb's empty floors (runtime/ui/hud/minimap.ts).
const LATER_TOMB_MINIMAP_CATALOG = "/assets/data/later-dungeon-minimap.json";

/*
================
laterTombMinimapImportPlan

What the tomb minimap step does with one environment: the same contract
as the dungeon maps (SRO_VSRO_ROOT folder, else the one skip line),
because one archive carries both. The importer verifies the archive
against its pin before reading a byte.
================
*/
export function laterTombMinimapImportPlan( env = process.env, archiveThere = false, stamped = stampedVsroRoot() ) {
	return vsroImportPlan(
		{
			label: "Later tomb minimap",
			without: "the tomb's floors keep an empty minimap",
			script: "scripts/import_vsro_tomb_minimap.py"
		},
		env,
		archiveThere,
		stamped
	);
}

/*
================
laterTombMinimapPublicPaths

The public paths one import publishes, read back from its own catalogue
so the publication ledger claims exactly what the client will ask for:
the catalogue and every tile it names.
================
*/
export function laterTombMinimapPublicPaths( catalog ) {
	const paths = [ LATER_TOMB_MINIMAP_CATALOG ];
	for ( const tile of catalog?.tilePaths ?? [] ) {
		if ( typeof tile === "string" && tile ) paths.push( tile );
	}
	return paths;
}

/*
================
importLaterTombMinimapStep

Before this step the import ran by hand only, so a full build retired a
tree's tomb tiles and the radar lost its background in the tomb. Now the
tiles land in the loose public tree with the other later-client imports,
in time for the final pack tail, and the step claims them. Without the
archive the build says so in one line.
================
*/
export async function importLaterTombMinimapStep( stamped = stampedVsroRoot() ) {
	const plan = await vsroStepPlan( laterTombMinimapImportPlan, stamped );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[tomb-minimap] later-client tomb minimap import",
		cwd: rebuildRoot,
		context: [
			"The Ch'in Tomb's floor minimap is native to the later client; the build imports it from the " +
			"pinned VSRO archive (SRO_VSRO_ROOT) and publishes it with the other loose assets."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	const catalog = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "data", "later-dungeon-minimap.json" ), "utf8" )
	);
	claimPublicPaths( laterTombMinimapPublicPaths( catalog ) );
	return { tiles: Array.isArray( catalog.tilePaths ) ? catalog.tilePaths.length : 0 };
}

// The later client's renewal Item Mall window, the client's desktop Item
// Mall whenever this import is published (owner 2026-11-20): its layouts,
// caption strings and art. The art lands under Media_extracted/later/ so no v1.150 picture
// of the same name is replaced; the client keeps the classic window when
// the catalogue is absent.
const LATER_RENEWAL_MALL_CATALOG = "/assets/data/renewal-mall.json";

/*
================
laterRenewalMallImportPlan

What the renewal mall step does with one environment: the same contract
as the dungeon maps (SRO_VSRO_ROOT folder, else the one skip line),
because one archive carries both. The importer verifies the archive
against its pin before reading a byte.
================
*/
export function laterRenewalMallImportPlan( env = process.env, archiveThere = false, stamped = stampedVsroRoot() ) {
	return vsroImportPlan(
		{
			label: "Later renewal mall",
			without: "the Item Mall keeps the classic window",
			script: "scripts/import_vsro_renewal_mall.py"
		},
		env,
		archiveThere,
		stamped
	);
}

/*
================
laterRenewalMallPublicPaths

The public paths one import publishes, read back from its own catalogue
so the publication ledger claims exactly what the client will ask for:
the catalogue and every picture it lists.
================
*/
export function laterRenewalMallPublicPaths( catalog ) {
	const paths = [ LATER_RENEWAL_MALL_CATALOG ];
	for ( const image of catalog?.images ?? [] ) {
		if ( typeof image?.path === "string" && image.path ) paths.push( image.path );
	}
	return paths;
}

/*
================
importLaterRenewalMallStep

The renewal mall's layouts and art land in the loose public tree with
the other later-client imports, in time for the final pack tail, and
the step claims them. Without the archive the build says so in one line.
================
*/
export async function importLaterRenewalMallStep( stamped = stampedVsroRoot() ) {
	const plan = await vsroStepPlan( laterRenewalMallImportPlan, stamped );
	if ( !plan.run ) {
		console.log( plan.line );
		return { skipped: true };
	}
	const result = await runPython( plan.args, {
		task: "[renewal-mall] later-client renewal Item Mall import",
		cwd: rebuildRoot,
		context: [
			"The renewal Item Mall window is native to the later client; the build imports its layouts and art " +
			"from the pinned VSRO archive (SRO_VSRO_ROOT); the client draws it as its desktop Item Mall."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	const catalog = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "data", "renewal-mall.json" ), "utf8" )
	);
	claimPublicPaths( laterRenewalMallPublicPaths( catalog ) );
	return { images: Array.isArray( catalog.images ) ? catalog.images.length : 0 };
}

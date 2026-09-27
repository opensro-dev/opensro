/*
===========================================================================

assetPackGroupParity.test.mjs - asset-pack group membership and packing

Checks the shared pack-group collector and the two build entry points that
use it: the canonical group set and sweep exclusions, where native name
filters and mip textures land, that a skipped outdoor lane leaks nothing,
that both entry points refresh JSON sidecars before packing, and that the
packed mission boot catalog matches its loose gzip authority byte for byte.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdtemp, mkdir, open, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";
import {
	collectAssetPackGroups,
	OUTDOOR_WORLD_PACK_TARGET_BYTES,
	VAT_PACK_TARGET_BYTES
} from "../../build/assetPackGroups.mjs";

/*
================
groupFiles

Returns the published files of the named pack group, failing with the group's
name (not a TypeError) when the group is missing.
================
*/
function groupFiles( groups, name ) {
	const group = groups.find( ( candidate ) => candidate.name === name );
	assert.ok( group, `asset pack group ${name} is missing` );
	return group.files;
}

// Parity guard for the two asset-pack entry points:
//   scripts/build_sro_resources.mjs           (full pipeline tail)
//   scripts/rebuild_asset_packs_from_public.mjs (standalone pack rebuild)
//
// Both once carried their own inline copy of the pack-group list and sweep
// exclusions, and they drifted: the mission-npc-vat group (plus its json-sweep
// exclusions) was added to the pipeline only, so standalone rebuilds silently
// produced packs missing that group. The fix was to extract the whole definition
// into build/assetPackGroups.mjs; this file pins both halves of that fix:
//
// 1. BEHAVIOR: collectAssetPackGroups() is run against a fixture tree covering
//    every group and every exclusion rule, and its effective group array is
//    deep-compared against the expected shape. Editing the shared module cannot
//    silently drop a group or an exclusion.
// 2. ROUTING: both entry scripts are statically parsed (neither exports anything --
//    both are top-level programs that take the generated-assets lock and run a
//    build on import, so importing them here is not an option). Each must import
//    collectAssetPackGroups, must not call listPublicAssetFiles itself, must not
//    pass an inline `groups: [...]` literal to buildAssetPacks, and must keep the
//    buildWebAssetManifest -> optimizePublicJsonAssets sidecar-refresh tail.
//
// Legitimate per-caller differences (do NOT assert them away):
// - the pipeline passes includeOutdoorWorld: Boolean(outdoorWorldRegion) because
//   its world lane may legitimately produce no outdoor region group; the
//   standalone rebuild always packs what is on disk and uses the default (true);
// - the ui-preload image list and minimap tile list are runtime inputs produced
//   by each caller, not part of the shared definition.
//
// scripts/refresh_outdoor_asset_packs.mjs is deliberately OUTSIDE this contract:
// it is an incremental refresh that rebuilds only game-data/game-images/
// outdoor-world, takes their membership and load modes from the PREVIOUS pack
// manifest rather than a fresh sweep, and merges into the manifest the full
// builds wrote -- it never defines the canonical group set.

const testDir = path.dirname( fileURLToPath( import.meta.url ) );
const scriptsDir = path.resolve( testDir, "..", ".." );
const entryScriptPaths = {
	pipeline: path.join( scriptsDir, "build_sro_resources.mjs" ),
	standalone: path.join( scriptsDir, "rebuild_asset_packs_from_public.mjs" )
};

test("native name filtering ships through the canonical game-data pack group", async () => {
	const fixture = await mkdtemp( path.join( os.tmpdir(), "sro-name-filter-pack-" ) );
	try {
		for ( const relative of FIXTURE_FILES ) {
			const target = path.join( fixture, relative );
			await mkdir( path.dirname( target ), { recursive: true } );
			await writeFile( target, "fixture" );
		}
		await mkdir( path.join( fixture, "assets/textdata" ), { recursive: true } );
		await writeFile( path.join( fixture, "assets/textdata/abusefilter.txt" ), "native filter fixture" );
		await writeFile(
			path.join( fixture, "assets/textdata/unrelated.txt" ),
			"must not expand the publication surface"
		);
		const groups = await collectAssetPackGroups( {
			publicRoot: fixture,
			uiImagePreloadPaths: [],
			missionMinimapTilePaths: []
		} );
		const files = groupFiles( groups.groups, "game-data" );
		assert.ok( files.includes( "/assets/textdata/abusefilter.txt" ) );
		assert.ok( !files.includes( "/assets/textdata/unrelated.txt" ) );
	} finally {
		await rm( fixture, { recursive: true, force: true } );
	}
});

const FIXTURE_FILES = [
	"assets/textdata/unrelated.txt",
	"assets/ui/preload.png",
	"assets/images/minimap/tile0.png",
	"assets/images/loose.png",
	"assets/char/vat/crowd.bin",
	"assets/char/vat/crowd.json",
	"assets/npc/vat/npc.bin",
	"assets/npc/vat/npc.json",
	"assets/npc/animation-catalog.json",
	"assets/npc/animation-catalog.json.gz",
	"assets/world/outdoor/region.json",
	"assets/world/outdoor/region.json.gz",
	"assets/world/outdoor/tex.dds",
	"assets/data/table.json",
	"assets/data/table.json.gz",
	"assets/data/loose.json",
	"assets/manifest.json",
	"assets/manifest.json.gz",
	"assets/anim/clip.ban",
	"assets/models/model.glb",
	"assets/audio/theme.mp3",
	// Pack artifacts must be invisible to every sweep.
	"assets/packs/pack-001.bin",
	"assets/packs/manifest.json"
];

const UI_PRELOAD_PATHS = [ "/assets/ui/preload.png" ];
const MINIMAP_TILE_PATHS = [ "/assets/images/minimap/tile0.png" ];

let fixtureRoot;

after( async () => {
	if ( fixtureRoot ) {
		await rm( fixtureRoot, { recursive: true, force: true } );
	}
} );

async function makeFixtureTree() {
	if ( !fixtureRoot ) {
		fixtureRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-group-parity-" ) );
		for ( const relativePath of FIXTURE_FILES ) {
			const absolutePath = path.join( fixtureRoot, relativePath );
			await mkdir( path.dirname( absolutePath ), { recursive: true } );
			await writeFile( absolutePath, `fixture:${relativePath}` );
		}
	}
	return fixtureRoot;
}

function expectedGroups( { outdoorFiles } ) {
	return [
		{ name: "native-ui", load: "startup", files: UI_PRELOAD_PATHS },
		{ name: "game-images", load: "startup", files: [ "/assets/images/loose.png" ] },
		{
			name: "game-data",
			load: "startup",
			files: [ "/assets/data/table.json.gz", "/assets/data/loose.json", "/assets/anim/clip.ban" ]
		},
		{
			name: "developer-labs",
			load: "manual",
			files: [ "/assets/npc/animation-catalog.json.gz" ]
		},
		{ name: "game-audio", load: "manual", files: [ "/assets/audio/theme.mp3" ] },
		{
			name: "title-crowd-vat",
			load: "lazy",
			targetBytes: VAT_PACK_TARGET_BYTES,
			files: [ "/assets/char/vat/crowd.bin", "/assets/char/vat/crowd.json" ]
		},
		{
			name: "mission-npc-vat",
			load: "lazy",
			targetBytes: VAT_PACK_TARGET_BYTES,
			files: [ "/assets/npc/vat/npc.bin", "/assets/npc/vat/npc.json" ]
		},
		{ name: "mission-minimap", load: "lazy", files: MINIMAP_TILE_PATHS },
		{ name: "game-models", load: "lazy", files: [ "/assets/models/model.glb" ] },
		{
			name: "outdoor-world",
			load: "manual",
			targetBytes: OUTDOOR_WORLD_PACK_TARGET_BYTES,
			files: outdoorFiles
		}
	];
}

test("the shared collector resolves the canonical group set and sweep exclusions", async () => {
	const publicRoot = await makeFixtureTree();
	const collected = await collectAssetPackGroups( {
		publicRoot,
		uiImagePreloadPaths: UI_PRELOAD_PATHS,
		missionMinimapTilePaths: MINIMAP_TILE_PATHS
	} );

	// Pins the group names, load modes, per-group target bytes and every exclusion:
	// VAT/lab json out of game-data, .json.gz sources out of the raw sweep, the
	// loose web manifest and its sidecars out of everything, outdoor out of the
	// generic groups, pack artifacts invisible everywhere.
	assert.deepEqual(
		collected.groups,
		expectedGroups( { outdoorFiles: [ "/assets/world/outdoor/region.json.gz", "/assets/world/outdoor/tex.dds" ] } )
	);
});

test("a skipped outdoor lane empties the outdoor group without leaking outdoor files", async () => {
	const publicRoot = await makeFixtureTree();
	const collected = await collectAssetPackGroups( {
		publicRoot,
		uiImagePreloadPaths: UI_PRELOAD_PATHS,
		missionMinimapTilePaths: MINIMAP_TILE_PATHS,
		includeOutdoorWorld: false
	} );

	// The pipeline's legitimate per-caller difference: with the outdoor lane skipped,
	// the outdoor-world group must go empty while the outdoor files on disk STILL stay
	// out of game-images/game-data (the generated-asset membership contract).
	assert.deepEqual( collected.groups, expectedGroups( { outdoorFiles: [] } ) );
});

test("both entry points route their pack groups through the shared collector", async () => {
	for ( const [label, scriptPath] of Object.entries( entryScriptPaths ) ) {
		const source = await readFile( scriptPath, "utf8" );

		assert.match(
			source,
			/import\s*\{\s*collectAssetPackGroups\s*\}\s*from\s*"\.\/build\/assetPackGroups\.mjs"/,
			`${label} must import collectAssetPackGroups from ./build/assetPackGroups.mjs`
		);
		assert.match(
			source,
			/uiImagePreloadPaths\s*:/,
			`${label} must feed its ui-preload image list into the shared collector`
		);
		assert.match(
			source,
			/missionMinimapTilePaths\s*:/,
			`${label} must feed its minimap tile list into the shared collector`
		);
		assert.match(
			source,
			/groups\s*:\s*\w+\.groups/,
			`${label} must pass the collector's groups array to buildAssetPacks`
		);
		assert.doesNotMatch(
			source,
			/groups\s*:\s*\[/,
			`${label} reintroduced an inline pack-group literal; the group list must live only in build/assetPackGroups.mjs`
		);
		assert.doesNotMatch(
			source,
			/listPublicAssetFiles/,
			`${label} lists public assets itself; per-script sweeps are how the mission-npc-vat drift happened`
		);
	}
});

test("both entry points keep the manifest-then-sidecar-refresh tail", async () => {
	for ( const [label, scriptPath] of Object.entries( entryScriptPaths ) ) {
		const source = await readFile( scriptPath, "utf8" );
		const manifestAt = source.indexOf( "buildWebAssetManifest()" );
		const finalRefreshAt = source.lastIndexOf( "optimizePublicJsonAssets()" );

		assert.ok( manifestAt >= 0, `${label} must rebuild the web asset manifest` );
		assert.ok(
			finalRefreshAt > manifestAt,
			`${label} must refresh precompressed sidecars (optimizePublicJsonAssets) AFTER regenerating the manifests`
		);
	}
});

test("both entry points refresh JSON sidecars before packing them", async () => {
	for ( const [label, scriptPath] of Object.entries( entryScriptPaths ) ) {
		const source = await readFile( scriptPath, "utf8" );
		const firstRefreshAt = source.indexOf( "optimizePublicJsonAssets()" );
		// Layout-independent: dprint pads call parentheses.
		const packAt = source.search( /buildAssetPacks\s*\(\s*\{/ );

		assert.ok( firstRefreshAt >= 0, `${label} must precompress generated JSON` );
		assert.ok( packAt >= 0, `${label} must build asset packs` );
		assert.ok(
			firstRefreshAt < packAt,
			`${label} must refresh .json.gz before buildAssetPacks; otherwise the pack can hide a newer loose manifest behind stale bytes`
		);
	}
});

test("the packed mission boot catalog is byte-identical to its current loose gzip authority", async ( t ) => {
	const publicRoot = path.resolve( testDir, "../../../.generated/client-public" );
	const assetPath = "/assets/data/ginterface-sections.json.gz";
	const index = JSON.parse(
		await readFile( path.join( publicRoot, "assets/packs/manifest.json" ), "utf8" )
	);
	const asset = index.assets.find( ( candidate ) => candidate.path === assetPath );
	assert.ok( asset, `${assetPath} must be packed in the startup game-data group` );

	const looseBytes = await readFile( path.join( publicRoot, assetPath.slice( 1 ) ) ).catch( ( error ) => {
		if ( error?.code === "ENOENT" ) return undefined;
		throw error;
	} );
	if ( !looseBytes ) {
		t.skip( "compact profile intentionally removes the loose generated authority" );
		return;
	}
	const packPath = path.join( publicRoot, asset.packPath.slice( 1 ) );
	const pack = await open( packPath, "r" );
	try {
		const prefix = Buffer.alloc( 12 );
		await pack.read( prefix, 0, prefix.length, 0 );
		assert.equal( prefix.subarray( 0, 8 ).toString( "ascii" ), "SROPACK1" );
		const dataStart = 12 + prefix.readUInt32LE( 8 );
		const packedBytes = Buffer.alloc( asset.length );
		await pack.read( packedBytes, 0, packedBytes.length, dataStart + asset.offset );

		assert.deepEqual(
			packedBytes,
			looseBytes,
			"the pack must be rebuilt after the generated catalog or it hides current loose bytes"
		);
		const catalog = JSON.parse( gunzipSync( packedBytes ).toString( "utf8" ) );
		assert.ok(
			catalog.resinfoSections?.["resinfo\\ifnotify.txt"]?.Create?.length > 0,
			"the packed mission authority must contain CIFNotify's synchronous Create section"
		);
	} finally {
		await pack.close();
	}
});

test("native mip texture containers survive canonical image and outdoor pack collection", async () => {
	const fixture = await mkdtemp( path.join( os.tmpdir(), "sro-native-texture-pack-" ) );
	try {
		for (
			const relative of [
				...FIXTURE_FILES,
				"assets/images/Map_extracted/sun/lens1.texture",
				"assets/world/outdoor/native.texture"
			]
		) {
			const target = path.join( fixture, relative );
			await mkdir( path.dirname( target ), { recursive: true } );
			await writeFile( target, "fixture" );
		}
		const { groups } = await collectAssetPackGroups( {
			publicRoot: fixture,
			uiImagePreloadPaths: [],
			missionMinimapTilePaths: []
		} );
		assert.ok( groupFiles( groups, "game-images" ).includes( "/assets/images/Map_extracted/sun/lens1.texture" ) );
		assert.ok( groupFiles( groups, "outdoor-world" ).includes( "/assets/world/outdoor/native.texture" ) );
		assert.ok( !groupFiles( groups, "game-images" ).includes( "/assets/world/outdoor/native.texture" ) );
	} finally {
		await rm( fixture, { recursive: true, force: true } );
	}
});

/*
===========================================================================

imagePackOwnership.test.mjs - full and focused image publication agree

New images must enter the same groups during a focused refresh as a full
build. Explicit preload and minimap owners take precedence over image paths.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { collectAssetPackGroups } from "../../build/assetPackGroups.mjs";
import { LOOSE_FAMILIES } from "../../build/families/looseFamilies.mjs";
import { publicIndex } from "../../../apps/client-next/tools/beta/policy.mjs";
import { ASSET_PACK_VERSION } from "../../build/shared/packFormat.mjs";
import { effectProgramFile } from "../../../apps/client-next/src/engine/foundation/assets/effect-programs.ts";

test("focused publishers give new images their full-build group", () => {
	const particle = "/assets/images/Particles_extracted/textures/new.texture";
	for ( const family of [ "effect", "entity-bsr" ] ) {
		assert.equal( LOOSE_FAMILIES[family].defaultGroup( particle, {} ), "particle-textures", family );
		assert.equal( LOOSE_FAMILIES[family].defaultGroup( "/assets/skill/effectRecords.json.gz", {} ), "game-data" );
		assert.equal(
			LOOSE_FAMILIES[family].defaultGroup( effectProgramFile( "battle/hit.efp" ), {} ),
			"game-data"
		);
	}
	assert.equal( LOOSE_FAMILIES["entity-bsr"].defaultGroup( "/assets/skillfx/new.glb", {} ), "game-models" );
	assert.equal(
		LOOSE_FAMILIES["slot-effect"].defaultGroup( "/assets/images/Media_extracted/icon/new.png", {} ),
		"ui-icons"
	);
});

test("new dungeon textures do not inherit the provider's data group", () => {
	const provider = "/assets/world/dungeon/dungeon-resources.json";
	const index = { assets: [ { path: provider, group: "game-data" } ] };
	const group = LOOSE_FAMILIES["dungeon-worlds"].defaultGroup;
	assert.equal( group( "/assets/world/dungeon/new.texture", index ), "world-textures" );
	assert.equal( group( "/assets/world/dungeon/world.json.gz", index ), "game-data" );
	assert.equal( group( provider, index ), "game-data" );
});

test("image collection folds family paths and preserves explicit owners", async () => {
	const fixture = await mkdtemp( path.join( os.tmpdir(), "sro-image-ownership-" ) );
	const expected = new Map( [
		[ "/assets/world/Title/wall.TEXTURE", "world-textures" ],
		[ "/assets/images/map_extracted/TILE2D/page.png", "map-tiles" ],
		[ "/assets/images/media_extracted/ICON/skill.png", "ui-icons" ],
		[ "/assets/images/particles_extracted/TEXTURES/spark.png", "particle-textures" ],
		[ "/assets/images/media_extracted/ICON/preload.png", "native-ui" ],
		[ "/assets/images/map_extracted/TILE2D/minimap.png", "mission-minimap" ],
		[ "/assets/images/media_extracted/interface/chrome.png", "game-images" ],
		[ "/assets/world/outdoor/ground.texture", "outdoor-world" ]
	] );
	try {
		for ( const folder of [ "char/vat", "npc/vat", "anim", "textdata", "audio" ] ) {
			await mkdir( path.join( fixture, "assets", folder ), { recursive: true } );
		}
		for ( const file of expected.keys() ) {
			const target = path.join( fixture, file.slice( 1 ) );
			await mkdir( path.dirname( target ), { recursive: true } );
			await writeFile( target, "fixture" );
		}
		const { groups } = await collectAssetPackGroups( {
			publicRoot: fixture,
			uiImagePreloadPaths: [ "/assets/images/media_extracted/ICON/preload.png" ],
			missionMinimapTilePaths: [ "/assets/images/map_extracted/TILE2D/minimap.png" ]
		} );
		const entries = groups.flatMap( group => group.files.map( file => [ file, group.name ] ) );
		assert.equal( entries.length, expected.size, "each file has exactly one owner" );
		assert.deepEqual( new Map( entries ), expected );
		const published = publicIndex( {
			format: "sro-asset-pack-index",
			version: ASSET_PACK_VERSION,
			groups,
			assets: entries.map( ( [file, group] ) => ({ path: file, group }) )
		} );
		assert.deepEqual( new Map( published.assets.map( row => [ row.path, row.group ] ) ), expected );
		for ( const name of [ "world-textures", "map-tiles", "ui-icons", "particle-textures" ] ) {
			assert.equal( published.groups.find( group => group.name === name )?.load, "startup", name );
		}
	} finally {
		await rm( fixture, { recursive: true, force: true } );
	}
});

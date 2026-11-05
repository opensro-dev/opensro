/*
===========================================================================

tomb-minimap-art.test.mjs - the tomb draws the later client's floor art

The later catalogue fills the tomb, a v1.150 floor is never replaced, and a
build without the catalogue keeps the tomb's empty floors without an error.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createMinimapResources } = await import( "../../src/engine/runtime/ui/hud/minimap.ts" );
const { minimapTiles, minimapWithLaterFloors } = await import( "../../src/engine/foundation/ui/minimap-tiles.ts" );

const TOMB = 0x8007, DONWHANG = 0x8001;
const FORMAT = "sro-mission-dungeon-minimap-manifest";
const RETAIL_TILE = "/assets/images/Media_extracted/minimap_d/donwhang/dh_a01_floor01_128x128.texture";
const LATER_TILE = "/assets/images/Media_extracted/minimap_d/jinsi/qt_a01_floor01_128x128.png";
/** @param {string} directory @param {string} prefix */
const floor = ( directory, prefix ) => ({
	floorIndex: 0,
	floorLabel: "",
	directory,
	prefix,
	tiles: [ "128x128" ]
});
const RETAIL = {
	format: FORMAT,
	version: 1,
	dungeons: [ { sectorId: DONWHANG, floors: [ floor( "donwhang", "dh_a01_floor01" ) ] }, {
		sectorId: TOMB,
		floors: []
	} ],
	tilePaths: [ RETAIL_TILE ]
};
const LATER = {
	format: FORMAT,
	version: 1,
	dungeons: [ { sectorId: TOMB, floors: [ floor( "jinsi", "qt_a01_floor01" ) ] }, {
		sectorId: DONWHANG,
		floors: [ floor( "jinsi", "other" ) ]
	} ],
	tilePaths: [ LATER_TILE ]
};

/*
================
assetsServing

An asset owner answering each catalogue at once, recording what was asked.
A missing path answers as an error, as a build without later tiles does.
================
*/
/** @param {Record<string, object>} files */
function assetsServing( files ) {
	/** @type {string[]} */
	const requested = [];
	return {
		requested,
		available: () => 1,
		/** @param {string} url */
		request( url ) {
			requested.push( new URL( url ).pathname );
			return requested.length;
		},
		/** @param {number} id */
		take( id ) {
			const file = files[requested[id - 1]];
			if ( !file ) return { kind: "error", error: "missing" };
			return { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( file ) ).buffer };
		},
		cancel() {}
	};
}

/** @param {Record<string, object>} [files] */
function resources( files ) {
	const assets = assetsServing(
		files ?? {
			"/assets/data/mission-dungeon-minimap.json": RETAIL,
			"/assets/data/later-dungeon-minimap.json": LATER
		}
	);
	const minimap = createMinimapResources( assets, "http://fixture.invalid" );
	minimap.step( false, true );
	return { assets, minimap };
}

test("the tomb draws its later floor", () => {
	const { minimap } = resources();
	const pose = { regionId: TOMB, x: 10, y: 0, z: 10, angle: 0 };
	const tiles = minimapTiles( pose, 256, minimap.dungeons()?.get( TOMB )?.[0], minimap.art() );
	assert.deepEqual( tiles.map( tile => tile.path ), [ LATER_TILE ] );
});

test("a later floor never replaces retail art", () => {
	const { minimap } = resources();
	assert.equal( minimap.dungeons()?.get( DONWHANG )?.[0]?.directory, "donwhang" );
	const merged = minimapWithLaterFloors(
		new Map( [ [ DONWHANG, [ floor( "donwhang", "a" ) ] ] ] ),
		new Map( [ [ DONWHANG, [ floor( "jinsi", "b" ) ] ] ] )
	);
	assert.equal( merged.get( DONWHANG )?.[0]?.directory, "donwhang" );
});

test("a build without later tiles stays native and reports no error", () => {
	const { minimap } = resources( { "/assets/data/mission-dungeon-minimap.json": RETAIL } );
	assert.deepEqual( minimap.dungeons()?.get( TOMB ), [] );
	assert.equal( minimap.error(), null );
});

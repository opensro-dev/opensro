/*
===========================================================================

world-map-unique-nests.test.mjs - the M map's unique nest signs

Port-only, not native, and two switches: the Experimental row (on by
default) only offers a "Unique spawns" box at the M window's foot, and
the box (off by default) draws the signs. With both on the world map
loads the published nest table and draws the minimap's unique sign on
every nest, all candidates of a unique together. Row off is the native
map: no box, no request and no sign.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );
const { decodeUniqueNests, UNIQUE_NEST_SIGN, UNIQUE_NESTS_PATH } = await import(
	"../../src/engine/foundation/ui/unique-nests.ts"
);

// Jangan town (page 1): sector 168 x 97 sits inside its authored bounds.
const JANGAN_REGION = 97 * 256 + 168;
const JANGAN_POSE = { regionId: JANGAN_REGION, x: 960, y: 0, z: 960, angle: 0 };
// One unique with two candidate nests beside the player, and a dungeon
// nest the world map cannot place.
const TABLE = {
	format: "sro-unique-nests",
	version: 1,
	nests: [
		{ refObjId: 1954, regionId: JANGAN_REGION, x: 900, z: 900 },
		{ refObjId: 1954, regionId: JANGAN_REGION, x: 1100, z: 1000 },
		{ refObjId: 1955, regionId: 0x8007, x: 960, z: 960 }
	]
};

/*
================
fixture

A map HUD over a fake asset owner that answers the nest table, stepped
and presented frames times; the foot's box is clicked before each frame
listed in clicks, and panelAt picks a frame's panel. Returns the nest
table requests, the painted textures, demanded paths and control ids of
the last frame.
================
*/
/**
 * @param {{ enabled: boolean, panel?: string, frames?: number, clicks?: number[],
 *   panelAt?: (frame: number) => string }} setup
 */
function present( setup ) {
	/** @type {string[]} */
	const requests = [];
	const bytes = new TextEncoder().encode( JSON.stringify( TABLE ) );
	const nestsUrl = new URL( UNIQUE_NESTS_PATH, "http://fixture.invalid" ).href;
	const hud = createWorldMapHud( {
		assets: {
			available: () => 1,
			// The map's other loads (the region names) stay pending: the
			// fixture answers the nest table only.
			request: ( /** @type {string} */ url ) => requests.push( url ),
			take: ( /** @type {number} */ id ) =>
				requests[id - 1] === nestsUrl ?
					{ kind: /** @type {const} */ ("bytes"), id, buffer: bytes.buffer } :
					null,
			cancel: () => {}
		},
		base: "http://fixture.invalid",
		experimental: {
			state: () => ({ saved: experimentalOptions( setup.enabled ? {} : { uniqueSpawnPoints: false } ) }),
			restore: () => {}
		}
	} );
	/** @type {any[]} */
	let quads = [];
	/** @type {string[]} */
	let paths = [];
	/** @type {any[]} */
	let controls = [];
	for ( let frame = 0; frame < (setup.frames ?? 2); frame++ ) {
		const panel = setup.panelAt?.( frame ) ?? setup.panel ?? "Map";
		if ( setup.clicks?.includes( frame ) ) {
			assert.ok( controls.some( c => c.id === "map-unique-spawns" ), "the box is on screen to click" );
			hud.activate( "map-unique-spawns", JANGAN_POSE );
		}
		hud.step( /** @type {any} */ ({ session: null, gameplay: { pose: JANGAN_POSE } }), panel );
		quads = [];
		paths = [];
		controls = [];
		/** @type {any} */
		const input = {
			next: { entities: [] },
			game: { pose: JANGAN_POSE, social: { self: 1, localName: "Me", members: [] } },
			pose: JANGAN_POSE,
			panel,
			w: 1200,
			h: 900,
			full: [ 0, 0, 1200, 900 ],
			hover: null,
			hudData: { map: {} },
			trackedQuest: 0,
			minimapResources: { positions: () => undefined },
			mapTeleport: { view: () => {}, hide: () => {} },
			reverseScrollHud: { mapping: () => false },
			text: {
				path: () => null,
				run: ( /** @type {string} */ value ) => ({ width: value.length * 6 }),
				extentHeight: () => 12,
				quads: () => []
			},
			resources: { has: () => true, size: () => undefined },
			paths,
			quads,
			controls,
			hudCopy: ( /** @type {string} */ k ) => k,
			beginWindow: () => [ 0, 0, 0, 0 ],
			endWindow: () => {},
			windowBox: () => {},
			authoredButton: () => {},
			authoredLabeledButton: () => {},
			closeButton: () => {},
			image: () => {}
		};
		hud.present( input );
	}
	hud.dispose();
	return {
		requests: requests.filter( url => url === nestsUrl ),
		textures: quads.map( q => q.texture ),
		paths,
		controls: controls.map( c => c.id )
	};
}

const signs = ( /** @type {string[]} */ textures ) => textures.filter( texture => texture === UNIQUE_NEST_SIGN ).length;

test("row and box on: the open map draws every world nest of the table with the unique sign", () => {
	const { requests, textures } = present( { enabled: true, frames: 3, clicks: [ 1 ] } );
	assert.deepEqual( requests, [ new URL( UNIQUE_NESTS_PATH, "http://fixture.invalid" ).href ] );
	// Both Jangan candidates; the dungeon nest has no place on the map.
	assert.equal( signs( textures ), 2 );
});

test("row on, box off (its default): the box shows, unticked, with no request and no sign", () => {
	const { requests, textures, paths, controls } = present( { enabled: true } );
	assert.ok( controls.includes( "map-unique-spawns" ) );
	assert.deepEqual( requests, [] );
	assert.equal( signs( textures ), 0 );
	assert.ok( !paths.includes( UNIQUE_NEST_SIGN ) );
});

test("the native map (row off) shows no box, requests no table and draws no nest sign", () => {
	const { requests, textures, paths, controls } = present( { enabled: false } );
	assert.ok( !controls.includes( "map-unique-spawns" ) );
	assert.deepEqual( requests, [] );
	assert.ok( textures.length > 0 );
	assert.equal( signs( textures ), 0 );
	assert.ok( !paths.includes( UNIQUE_NEST_SIGN ) );
});

test("unticking the box mid-session drops the signs at once, without a reload", () => {
	const { requests, textures, paths } = present( { enabled: true, frames: 4, clicks: [ 1, 3 ] } );
	assert.equal( requests.length, 1 );
	assert.equal( signs( textures ), 0 );
	assert.ok( !paths.includes( UNIQUE_NEST_SIGN ) );
});

test("the ticked box survives closing and reopening M, as the level filter does", () => {
	const { requests, textures } = present( {
		enabled: true,
		frames: 5,
		clicks: [ 1 ],
		panelAt: ( frame ) => frame === 2 || frame === 3 ? "" : "Map"
	} );
	assert.equal( requests.length, 1 );
	assert.equal( signs( textures ), 2 );
});

test("a closed map with the box ticked requests nothing new and demands the sign image once", () => {
	const { requests, textures, paths } = present( {
		enabled: true,
		frames: 3,
		clicks: [ 1 ],
		panelAt: ( frame ) => frame === 2 ? "" : "Map"
	} );
	assert.equal( requests.length, 1 );
	assert.deepEqual( textures, [] );
	assert.equal( paths.filter( path => path === UNIQUE_NEST_SIGN ).length, 1 );
});

test("the nest table decodes to markers and rejects a malformed table whole", () => {
	assert.deepEqual( decodeUniqueNests( TABLE )[0], {
		regionId: JANGAN_REGION,
		x: 900,
		z: 900,
		rotation: 0,
		path: UNIQUE_NEST_SIGN
	} );
	for (
		const raw of [
			null,
			{ ...TABLE, format: "other" },
			{ ...TABLE, version: 2 },
			{ ...TABLE, nests: [ { regionId: -1, x: 0, z: 0 } ] },
			{ ...TABLE, nests: [ { regionId: 1, x: Number.NaN, z: 0 } ] },
			{ ...TABLE, nests: [ null ] }
		]
	) {
		assert.throws( () => decodeUniqueNests( raw ) );
	}
});

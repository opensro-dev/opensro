/*
===========================================================================

environment-integration.test.mjs - tests for weather.ts, gameplay.ts,
world.ts, world.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { decodeWeather, initialWeatherAmount, advanceWeatherAmount } = await import(
	"../../src/engine/foundation/gameplay/weather.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createWorldDecoder } = await import( "../../src/engine/runtime/assets/worker/world/world.ts" );
const { createWorldRenderer } = await import( "../../src/engine/runtime/renderer/world/world.ts" );

test("native weather admission preserves byte amount, rejects malformed input, and resets on world replacement", () => {
	for ( const mode of [ 0, 1, 2, 3, 4, 255 ] ) {
		assert.deepEqual( decodeWeather( Uint8Array.of( mode, 255 ) ), {
			mode: mode >= 1 && mode <= 3 ? mode : 1,
			amount: 255
		} );
	}
	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	const initialWeather = defined( game.take() ).weather;
	assert.equal( game.receive( { opcode: 0x3bde, payload: Uint8Array.of( 3, 20 ) }, 10 ), true );
	assert.deepEqual( defined( game.take() ).weather, { mode: 3, amount: 20 } );
	for ( const payload of [ new Uint8Array(), Uint8Array.of( 2 ), Uint8Array.of( 2, 20, 1 ) ] ) {
		assert.throws( () => game.receive( { opcode: 0x3bde, payload }, 11 ) );
		assert.equal( game.take(), null );
	}
	game.resetWorld();
	assert.deepEqual( defined( game.take() ).weather, initialWeather );
	game.dispose();
});
test("weather amount retargets from its current integer and reaches the five-second target", () => {
	let state = advanceWeatherAmount( initialWeatherAmount(), 100, 1 );
	assert.equal( state.value, 20 );
	state = advanceWeatherAmount( state, 100, 1 );
	assert.equal( state.value, 40 );
	state = advanceWeatherAmount( state, 0, 1 );
	assert.equal( state.start, 40 );
	assert.equal( state.value, 31 );
	state = advanceWeatherAmount( state, 0, 4 );
	assert.equal( state.value, 0 );
	assert.equal( state.progress, 1 );
	assert.throws( () => advanceWeatherAmount( state, 256, 1 ) );
	assert.throws( () => advanceWeatherAmount( state, 1, NaN ) );
});
function bundle() {
	return {
		source: { sectorX: 1, sectorY: 128 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: { placements: [], resources: { bsr: [], meshes: [], materialSets: [] } },
		dungeonBlocks: [
			{ index: 0, visibleBlocks: [], ambientColor: 0xff674961 },
			{ index: 1, visibleBlocks: [], ambientColor: 0xff476fa5 }
		],
		water: { normalFramePublicPaths: Array.from( { length: 30 }, ( _, i ) => "/assets/water/" + i ) },
		dungeonWater: [ {
			blockIndex: 0,
			vertices: [ -100, 0, 0, -100, 0, 100, 100, 0, 100, 100, 0, 0, 999, 999, 999 ],
			color: 0xb4112233,
			fog: { color: 0x112233, nearPlane: 50, farPlane: 500, intensity: .01 }
		} ]
	};
}
test("stationary retained selection still publishes Cerberus palette entry and exit", () => {
	const world = createWorldRenderer( undefined, undefined, { range: ( a, b ) => Math.trunc( (a + b) / 2 ) } );
	const geometry = {
		upload( g ) {
			return { g };
		},
		release() {},
		updateIndices() {},
		updatePositions() {}
	};
	const images = {
		upload() {
			return {};
		},
		release() {}
	};
	world.camera( { eye: [ 0, 100, -100 ], target: [ 0, 0, 50 ], fov: 1, near: 1, far: 3500 } );
	world.scene( {
		id: "event-transition",
		originRegion: 25256,
		warnings: [],
		groups: [],
		environment: { startTimeOfDay: .5, ratePerSecond: 0, tracks: {} }
	} );
	try {
		const clear = world.prepare( geometry, images, 1, 0 );
		world.weather( { mode: 1, amount: 0, eventRain: true } );
		const entering = world.prepare( geometry, images, 1, 1 );
		assert.equal( entering.draws, clear.draws, "stationary selection is reused" );
		assert.equal( entering.environment[31], -250, "fog transitions rather than snapping" );
		const event = world.prepare( geometry, images, 1, 3 ).environment;
		const close = ( offset, values ) =>
			values.forEach( ( value, i ) =>
				assert.ok( Math.abs( event[offset + i] - value ) < 1e-6, `environment ${offset + i}` )
			);
		close( 0, [ 46, 75, 156 ].map( v => v / 255 ) );
		close( 4, [ 105, 76, 138 ].map( v => v / 255 ) );
		close( 8, [ 128, 49, 62 ].map( v => v / 255 * .6 ) );
		close( 12, [ 117, 101, 120 ].map( v => v / 255 ) );
		// Float32 interpolation precedes byte truncation; convergence can cross a byte boundary.
		[ 11, 9, 12 ].forEach( ( v, i ) => assert.ok( Math.abs( event[28 + i] - v / 255 ) <= 1 / 255 + 1e-6 ) );
		assert.equal( event[31], -500 );
		assert.equal( event[32], 2500 );
		assert.equal( event[50], 1 );
		assert.equal( event[51], -1 );
		world.weather( { mode: 1, amount: 0, eventRain: false } );
		assert.equal( world.prepare( geometry, images, 1, 4 ).environment[31], -250 );
		const restored = world.prepare( geometry, images, 1, 6 ).environment;
		for ( const offset of [ 0, 4, 8, 12, 28 ] ) {
			for ( let i = 0; i < 3; i++ ) {
				assert.ok(
					Math.abs( restored[offset + i] - clear.environment[offset + i] ) <=
						(offset === 28 ? 1 / 255 : 0) + 1e-6
				);
			}
		}
		assert.equal( restored[31], clear.environment[31] );
	} finally {
		world.dispose( geometry, images );
	}
});
test("dungeon water executes through scene admission and block selection, with persistent texture arrays", () => {
	const decoder = createWorldDecoder(), scene = decoder.decode( bundle() ), group = scene.groups[0];
	assert.equal( group.id, "dungeon-water:0" );
	assert.deepEqual( [ ...group.geometry.uvs ], [ 0, 0, 0, 2, 4, 2, 4, 0 ] );
	assert.equal( group.geometry.positions.length, 12 );
	assert.deepEqual( [ ...group.geometry.indices ], [ 0, 1, 2, 0, 2, 3 ] );
	assert.equal( group.material.color[3], 180 / 255 );
	assert.equal( group.material.textureAlpha, false );
	const world = createWorldRenderer(), uploads = [], releases = [];
	let arrays = 0;
	const geometry = {
		upload( g ) {
			const draw = { g };
			uploads.push( draw );
			return draw;
		},
		release( d ) {
			releases.push( d );
		},
		updateIndices() {},
		updatePositions() {}
	};
	const images = {
		upload( first, frames ) {
			assert.equal( frames.length, 30 );
			arrays++;
			return {};
		},
		release() {}
	};
	const camera = {
		eye: [ 0, 100, -100 ],
		target: [ 0, 0, 50 ],
		fov: Math.PI / 2,
		near: 1,
		far: 1000,
		dungeonBlock: 0
	};
	world.camera( camera );
	world.scene( scene );
	defined( group.material.fog ).nearPlane = 1;
	for ( let i = 0; i < 29; i++ ) world.texture( "/assets/water/" + i, { width: 1, height: 1, close() {} } );
	assert.equal( world.prepare( geometry, images, 1, 0 ).draws.length, 0 );
	world.texture( "/assets/water/29", { width: 1, height: 1, close() {} } );
	const room0 = world.prepare( geometry, images, 1, .1 );
	assert.equal( room0.draws.length, 1 );
	assert.deepEqual( [ ...room0.environment.slice( 8, 12 ) ], [ 0, 0, 0, 0 ] );
	assert.deepEqual(
		[ ...room0.environment.slice( 12, 16 ) ],
		[ Math.fround( 0x67 / 255 ), Math.fround( 0x49 / 255 ), Math.fround( 0x61 / 255 ), 0 ]
	);
	assert.equal( uploads[0].g.material.fog.nearPlane, 50 );
	world.camera( { ...camera, dungeonBlock: 1 } );
	const room1 = world.prepare( geometry, images, 1, .2 );
	assert.equal( room1.draws.length, 0 );
	assert.deepEqual( [ ...room1.environment.slice( 8, 12 ) ], [ 0, 0, 0, 0 ] );
	assert.deepEqual(
		[ ...room1.environment.slice( 12, 16 ) ],
		[ Math.fround( 0x47 / 255 ), Math.fround( 0x6f / 255 ), Math.fround( 0xa5 / 255 ), 0 ]
	);
	world.camera( camera );
	assert.equal( world.prepare( geometry, images, 1, .3 ).draws.length, 1 );
	assert.equal( arrays, 1 );
	world.invalidate();
	assert.equal( world.prepare( geometry, images, 1, .4 ).draws.length, 1 );
	assert.equal( arrays, 2 );
	world.scene( null );
	assert.equal( world.prepare( geometry, images, 1, .5 ).draws.length, 0 );
	world.dispose( geometry, images );
});
test("dungeon water rejects missing frames and invalid block ownership before publication", () => {
	const decoder = createWorldDecoder(), b = bundle();
	b.water.normalFramePublicPaths.pop();
	assert.throws( () => decoder.decode( b ) );
	const bad = bundle();
	bad.dungeonWater[0].blockIndex = 2;
	assert.throws( () => decoder.decode( bad ) );
	const fog = bundle();
	fog.dungeonWater[0].fog.farPlane = 1;
	assert.throws( () => decoder.decode( fog ) );
	const order = bundle();
	order.dungeonBlocks.reverse();
	assert.throws( () => decoder.decode( order ) );
});

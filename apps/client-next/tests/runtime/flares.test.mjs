/*
===========================================================================

flares.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { OBJECT_CULL_COMMANDS } from "../helpers/cull-commands.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { flareUniforms, flareEntries } = await load( "src/engine/foundation/rendering/flares.ts" );
const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
test("flare projection handles horizon, behind-camera and viewport gates without nonfinite uniforms", () => {
	const camera = { eye: [ 500, 0, 700 ], target: [ 501, 1, 700 ], fov: Math.PI / 2, near: 1, far: 3500 };
	const data = flareUniforms( camera, .375, 1024, 768, .016 );
	assert.equal( data[25], 1 );
	assert.ok( Math.abs( data[20] - 512 ) < .001 );
	assert.ok( Math.abs( data[21] - 384 ) < .001 );
	assert.equal( flareUniforms( { ...camera, target: [ 499, -1, 700 ] }, .375, 1024, 768, .016 )[25], 0 );
	assert.equal( flareUniforms( camera, 0, 1024, 768, .016 )[25], 0 );
	const perpendicular = flareUniforms( { ...camera, eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ] }, .375, 1024, 768, .016 );
	assert.ok( perpendicular.every( Number.isFinite ) );
	assert.equal( perpendicular[25], 0 );
	const table = flareEntries();
	assert.equal( table.length, 30 );
	assert.equal( table.filter( e => e.size ).length, 23 );
	assert.deepEqual( table[2], { texture: 2, size: 150, alpha: 255, fan: true, over: true } );
	assert.equal( table[22].texture, 7 );
	assert.equal( table[28].alpha, 2 );
});
test("flare resources participate in atomic scene admission, recovery and retirement", () => {
	const world = createWorldRenderer(), closed = [], released = [];
	const geometry = { ...OBJECT_CULL_COMMANDS, upload: () => ({}), release() {} },
		textures = { upload: () => ({}), release: draw => released.push( draw ) };
	const paths = Array.from( { length: 8 }, ( _, i ) => "/assets/flare/" + i ),
		scene = {
			id: "flare",
			originRegion: 1,
			warnings: [],
			groups: [],
			flareTextures: paths,
			environment: { startTimeOfDay: .375, ratePerSecond: 0, tracks: {} }
		};
	world.camera( { eye: [ 0, 0, 0 ], target: [ 1, 1, 0 ], fov: Math.PI / 2, near: 1, far: 3500 } );
	world.scene( scene );
	paths[0] = "/assets/mutated";
	assert.equal( world.neededTextures()[0], "/assets/flare/0" );
	assert.equal( world.stats().pendingTextures, 8 );
	assert.equal( world.prepare( geometry, textures, 1, 0 ).flares, undefined );
	for ( let i = 0; i < 7; i++ ) {
		world.texture( "/assets/flare/" + i, {
			width: 1,
			height: 1,
			close() {
				closed.push( i );
			}
		} );
	}
	assert.equal( world.prepare( geometry, textures, 1, .1 ).flares, undefined );
	assert.equal( world.stats().pendingTextures, 1 );
	world.texture( "/assets/flare/7", {
		width: 1,
		height: 1,
		close() {
			closed.push( 7 );
		}
	} );
	const frame = world.prepare( geometry, textures, 1, .2 );
	assert.equal( frame.flares.textures.length, 8 );
	assert.equal( world.stats().pendingTextures, 0 );
	const reused = world.prepare( geometry, textures, 1, .3 );
	assert.deepEqual( reused.flares.textures, frame.flares.textures );
	world.invalidate();
	const rebuilt = world.prepare( geometry, textures, 1, .4 );
	assert.equal( rebuilt.flares.textures.length, 8 );
	assert.notEqual( rebuilt.flares.textures[0], frame.flares.textures[0] );
	world.scene( null );
	assert.equal( world.prepare( geometry, textures, 1, .5 ).flares, undefined );
	assert.equal( closed.length, 8 );
	assert.equal( released.length, 8 );
	world.dispose( geometry, textures );
});

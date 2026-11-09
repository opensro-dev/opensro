/*
===========================================================================

dungeon-lights.test.mjs - tests for dungeon-lights.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { dungeonPointLight } = await import(
	"@/engine/foundation/rendering/dungeon-lights"
);

/*
================
entry

One authored block light at the given position with readable channel
values: ambient (3.34, 1.67, 0.835) divides by the native 1.67 scale to
(2, 1, 0.5).
================
*/
function entry( x, y, z ) {
	return {
		name: `light@(${x},${y},${z})`,
		position: [ x, y, z ],
		diffuse: [ 0.6, 0.5, 0.4 ],
		ambient: [ 3.34, 1.67, 0.835 ],
		specular: [ 0.9, 0.9, 0.9 ],
		range: 120,
		attenuation: [ 1.5, 0.02 ]
	};
}

test("no authored block lights means no bound descriptor", () => {
	const blocks = [ [ entry( 0, 0, 0 ) ], [] ];
	assert.equal( dungeonPointLight( undefined, 0, 32773, 0, 0, 0 ), undefined );
	assert.equal( dungeonPointLight( blocks, undefined, 32773, 0, 0, 0 ), undefined );
	assert.equal( dungeonPointLight( blocks, 5, 32773, 0, 0, 0 ), undefined );
	assert.equal( dungeonPointLight( blocks, 1, 32773, 0, 0, 0 ), undefined );
});

test("the nearest authored entry is bound, first entry wins ties", () => {
	const far = entry( 500, 0, 0 );
	const near = entry( 30, 40, 0 );
	const tie = entry( -50, 0, 0 );
	const blocks = [ [ far, near ] ];
	assert.equal( dungeonPointLight( blocks, 0, 32773, 10, 0, 0 ).pose.x, 30 );
	// Two entries at distance 50: the authored order decides, not the
	// coordinate sign.
	assert.equal( dungeonPointLight( [ [ tie, entry( 50, 0, 0 ) ] ], 0, 32773, 0, 0, 0 ).pose.x, -50 );
	assert.equal( dungeonPointLight( [ [ entry( 50, 0, 0 ), tie ] ], 0, 32773, 0, 0, 0 ).pose.x, 50 );
});

test("channels follow RenderLight_ApplyLight1: ambient over 1.67, the rest raw", () => {
	const light = dungeonPointLight( [ [ entry( 10, 20, 30 ) ] ], 0, 32773, 0, 0, 0 );
	assert.deepEqual( light.pose, { regionId: 32773, x: 10, y: 20, z: 30 } );
	assert.deepEqual( light.ambient, [ 2, 1, 0.5 ] );
	assert.deepEqual( light.diffuse, [ 0.6, 0.5, 0.4 ] );
	// The authored pair is (constant, linear): the constant term fills the
	// descriptor's constant slot, the linear term the linear slot.
	assert.equal( light.attenuationConstant, 1.5 );
	assert.equal( light.attenuation, 0.02 );
	assert.equal( light.range, 120 );
});

/*
===========================================================================

cloth.test.mjs - fixed-step dynamics, anchored skinning and option transitions

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { clothData, createCloth } = await import( "../../src/engine/foundation/animation/cloth.ts" );
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

/*
================
fixture

The long edge keeps gravity tests independent of constraint relaxation.
================
*/
/** @param {Partial<import("../../src/engine/foundation/animation/cloth.ts").ClothData>} overrides
 * @returns {import("../../src/engine/foundation/animation/cloth.ts").ClothData} */
function fixture( overrides = {} ) {
	return {
		mobility: [ 0, 1 ],
		pins: [ 1, 0 ],
		constraints: [ [ 0, 1, 100 ] ],
		order: [ 0 ],
		force: null,
		gravity: 1,
		gravityMobility: 0,
		windMobility: 0,
		damping: 1,
		windPeriod: 2,
		...overrides
	};
}
const rest = new Float32Array( [ 0, 0, 0, 1, 0, 0 ] );
/*
================
advance
================
*/
function advance( sim, deltaMs, options = {} ) {
	return sim.advance( {
		anchors: rest,
		deltaMs,
		enabled: true,
		direction: [ 0, 0, 1 ],
		speed: 0,
		random: () => 1,
		...options
	} );
}

test("cloth drains 50ms steps, caps stalled frames, and leaves pins on their anchors", () => {
	const sim = createCloth( fixture(), rest );
	assert.deepEqual( advance( sim, 49 ), rest );
	const first = advance( sim, 1 ).slice();
	assert.equal( first[1], 0 );
	assert.ok( Math.abs( first[4] + .002 ) < 1e-8 );
	assert.deepEqual( advance( sim, 0 ), first );
	const stalled = createCloth( fixture(), rest ), four = createCloth( fixture(), rest );
	assert.deepEqual( advance( stalled, 10000 ), advance( four, 200 ) );
	const stepped = createCloth( fixture(), rest );
	for ( let i = 0; i < 4; i++ ) advance( stepped, 50 );
	assert.deepEqual( advance( four, 0 ), advance( stepped, 0 ) );
});
test("cloth constraints shorten extension without expanding compressed edges", () => {
	const data = fixture( { constraints: [ [ 0, 1, 1 ] ], gravity: 0 } );
	const extended = new Float32Array( [ 0, 0, 0, 3, 0, 0 ] );
	assert.deepEqual( advance( createCloth( data, extended ), 50, { anchors: extended } ), rest );
	const compressed = new Float32Array( [ 0, 0, 0, .5, 0, 0 ] );
	assert.deepEqual( advance( createCloth( data, compressed ), 50, { anchors: compressed } ), compressed );
});
test("gust gate, authored force override, and positive-motion clamp match native rules", () => {
	const data = fixture( { gravity: 0, force: [ 1, 0, 0 ] } );
	const gust = advance( createCloth( data, rest ), 50, { random: () => 0, direction: [ 0, 0, 1 ] } ).slice();
	assert.ok( Math.abs( gust[3] - 1.04 ) < 1e-6 );
	assert.equal( gust[5], 0 );
	assert.deepEqual( advance( createCloth( data, rest ), 50, { random: () => 1 } ), rest );
	const low = advance( createCloth( data, rest ), 50, { random: () => 0, speed: .01 } );
	assert.deepEqual( low, advance( createCloth( data, rest ), 50, { random: () => 0, speed: .2 } ) );
	assert.deepEqual(
		advance( createCloth( data, rest ), 50, { random: () => 0, speed: 8 } ),
		advance( createCloth( data, rest ), 50, { random: () => 0, speed: 1 } )
	);
});
test("disabling restores the current skeleton; re-enabling starts without stale velocity", () => {
	const sim = createCloth( fixture(), rest );
	advance( sim, 200 );
	const moved = new Float32Array( [ 0, 2, 0, 1, 2, 0 ] );
	assert.deepEqual( advance( sim, 200, { anchors: moved, enabled: false } ), moved );
	assert.deepEqual( advance( sim, 0, { anchors: moved } ), moved );
	assert.equal( advance( sim, 50, { anchors: moved } )[1], 2 );
});
test("cloth instances keep separate state and CPU skinning supplies the disabled pose", () => {
	const geometry = {
		positions: rest,
		indices: new Uint32Array( [ 0, 1, 0 ] ),
		transform: identity(),
		joints: new Uint32Array( 8 ),
		weights: new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0 ] )
	};
	const primitive = { geometry, cloth: fixture() },
		a = createClothVertices( primitive, () => 1 ),
		b = createClothVertices( primitive, () => 1 );
	const palette = identity(), motion = { direction: [ 0, 0, 1 ], speed: 0 };
	palette[13] = 3;
	a.update( palette, 0, true, motion );
	const animated = a.update( palette, .1, true, motion ).slice();
	assert.equal( animated[1], 3 );
	assert.ok( animated[15] < 3 );
	const ordinary = b.update( palette, .1, false, motion );
	assert.equal( ordinary[15], 3 );
	assert.equal( a.update( palette, .1, false, motion )[15], 3 );
});
test("cloth admission rejects invalid indices, duplicate order and nonfinite forces", () => {
	assert.equal( clothData( undefined, 2 ), undefined );
	for (
		const data of [
			fixture( { constraints: [ [ 0, 2, 1 ] ] } ),
			fixture( { order: [ 1 ] } ),
			fixture( { force: [ NaN, 0, 0 ] } ),
			fixture( { windPeriod: 0 } )
		]
	) {
		assert.throws( () => clothData( data, 2 ), /Invalid native cloth/ );
	}
	assert.deepEqual( clothData( fixture(), 2 ), fixture() );
});

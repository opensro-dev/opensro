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

test("animated cloth matches fully skinned anchors through motion, stalls and option changes", () => {
	for ( const pin of [ 1, 2 ] ) {
		const data = fixture( { pins: [ pin, 0 ], constraints: [ [ 0, 1, 1 ] ] } );
		const geometry = {
			positions: rest,
			indices: new Uint32Array( [ 0, 1, 0 ] ),
			transform: identity(),
			joints: new Uint32Array( 8 ),
			weights: new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0 ] )
		};
		let actualRandom = 0, expectedRandom = 0, seconds = 0;
		const primitive = { geometry, cloth: data };
		const skin = createClothVertices( primitive, () => 0 );
		const actual = createClothVertices( primitive, () => actualRandom++ );
		const expected = createCloth( data, rest );
		const anchors = new Float32Array( rest.length );
		const motion = { direction: [ 1, 0, 1 ], speed: .6 };
		for ( let frame = 0; frame < 60; frame++ ) {
			const before = seconds;
			seconds += [ 0, .016, .05, .1, .3, 1 ][frame % 6];
			const enabled = frame % 11 < 8, palette = identity();
			palette[12] = Math.sin( frame );
			palette[13] = Math.cos( frame );
			palette[0] = palette[5] = frame % 2 ? 1.2 : 1;
			const skinned = skin.update( palette, seconds, false, motion );
			for ( let vertex = 0; vertex < 2; vertex++ ) {
				anchors.set( skinned.subarray( vertex * 14, vertex * 14 + 3 ), vertex * 3 );
			}
			const positions = expected.advance( {
				anchors,
				deltaMs: frame === 0 ? 0 : Math.trunc( seconds * 1000 ) - Math.trunc( before * 1000 ),
				enabled,
				...motion,
				random: () => expectedRandom++
			} );
			const result = actual.update( palette, seconds, enabled, motion );
			for ( let vertex = 0; vertex < 2; vertex++ ) {
				assert.deepEqual(
					result.subarray( vertex * 14, vertex * 14 + 3 ),
					positions.subarray( vertex * 3, vertex * 3 + 3 )
				);
				if ( !enabled || vertex === 0 || frame === 0 ) {
					assert.deepEqual(
						result.subarray( vertex * 14 + 3, vertex * 14 + 6 ),
						skinned.subarray( vertex * 14 + 3, vertex * 14 + 6 )
					);
				}
			}
			assert.equal( actualRandom, expectedRandom );
		}
	}
});

test("unskinned cloth restores immutable anchors after animated motion", () => {
	const geometry = {
		positions: rest,
		indices: new Uint32Array( [ 0, 1, 0 ] ),
		transform: identity()
	};
	const cloth = createClothVertices( { geometry, cloth: fixture() }, () => 1 );
	const palette = new Float32Array( 0 ), motion = { direction: [ 0, 0, 1 ], speed: 0 };
	cloth.update( palette, 0, true, motion );
	assert.ok( cloth.update( palette, .1, true, motion )[15] < 0 );
	const restored = cloth.update( palette, .2, false, motion );
	assert.deepEqual( restored.subarray( 0, 3 ), rest.subarray( 0, 3 ) );
	assert.deepEqual( restored.subarray( 14, 17 ), rest.subarray( 3, 6 ) );
	assert.deepEqual( restored.subarray( 17, 20 ), Float32Array.of( 0, 1, 0 ) );
	assert.equal( cloth.update( palette, .2, true, motion )[15], 0 );
	assert.ok( cloth.update( palette, .3, true, motion )[15] < 0 );
});

/*
===========================================================================

hypot.test.mjs - allocation-free lengths return Math.hypot's exact bits

hypot2, hypot3 and hypot4 replace Math.hypot on per-frame paths; any bit they
differ by would change a bit-exact particle or pose result, so they are
held to Math.hypot over random magnitudes from subnormal to overflow and
over every special value.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { hypot2, hypot3, hypot4 } = await import( "../../src/engine/foundation/math/hypot.ts" );

/*
================
sampler

Values across every magnitude, with exact zeros of both signs.
================
*/
function sampler( seed ) {
	const random = () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32;
	return () => {
		const kind = random();
		if ( kind < .08 ) return 0;
		if ( kind < .16 ) return -0;
		if ( kind < .5 ) return (random() - .5) * 2;
		if ( kind < .75 ) return (random() - .5) * 1e4;
		return (random() - .5) * 10 ** (Math.floor( random() * 640 ) - 320);
	};
}

test("hypot4, hypot3 and hypot2 return Math.hypot's bits for random magnitudes", () => {
	const value = sampler( 7 );
	for ( let i = 0; i < 400000; i++ ) {
		const x = value(), y = value(), z = value(), w = value();
		assert.ok( Object.is( hypot4( x, y, z, w ), Math.hypot( x, y, z, w ) ), `${x} ${y} ${z} ${w}` );
		assert.ok( Object.is( hypot3( x, y, z ), Math.hypot( x, y, z ) ), `${x} ${y} ${z}` );
		assert.ok( Object.is( hypot2( x, y ), Math.hypot( x, y ) ), `${x} ${y}` );
	}
});

test("hypot4, hypot3 and hypot2 return Math.hypot's bits for special values", () => {
	const special = [ 0, -0, 1, -1, 3, 4, Infinity, -Infinity, NaN, Number.MAX_VALUE, Number.MIN_VALUE, 1e308, 5e-324 ];
	for ( const x of special ) {
		for ( const y of special ) {
			assert.ok( Object.is( hypot2( x, y ), Math.hypot( x, y ) ), `${x} ${y}` );
			for ( const z of special ) {
				assert.ok( Object.is( hypot3( x, y, z ), Math.hypot( x, y, z ) ), `${x} ${y} ${z}` );
				assert.ok( Object.is( hypot4( x, y, z, x ), Math.hypot( x, y, z, x ) ), `${x} ${y} ${z} ${x}` );
			}
		}
	}
});

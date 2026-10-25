/*
===========================================================================

combat-spacing.test.mjs - the server's combat spacing, mirrored

The values below are the Go spacing tests' own vectors
(apps/server/internal/game/world/simulation/combatspacing_test.go and
.../world/monster/gradescale_test.go): if the port and the server ever
disagree about where a cast becomes legal, these fail.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { combatAdmissionRadius, combatStopPoint, monsterBodyRadius, refBodyRadius } = await import(
	"../../src/engine/foundation/gameplay/combat-spacing.ts"
);

test("the admission radius is both bodies plus the reach", () => {
	assert.equal( combatAdmissionRadius( 4, 6, 6 ), 16 );
	// The Go boundary table's outer column, bodies 4 + 6.
	for (
		const [reach, admission] of [ [ 0, 10 ], [ 4, 14 ], [ 5, 15 ], [ 6, 16 ], [ 10, 20 ], [ 11, 21 ], [ 180, 190 ] ]
	) {
		assert.equal( combatAdmissionRadius( 4, 6, reach ), admission, `reach ${reach}` );
	}
});

test("an admission radius exists only for whole spacing data", () => {
	// The server holds the cast (never walks, never casts) on spacing it
	// cannot validate; the mirror answers undefined so its caller keeps
	// the unpinned aim instead of inventing a stop.
	for (
		const [actor, target, reach] of [
			[ 0, 6, 6 ],
			[ 4, 0, 6 ],
			[ -1, 6, 6 ],
			[ 4, 6, -1 ],
			[ NaN, 6, 6 ],
			[
				4,
				NaN,
				6
			],
			[ 4, 6, NaN ],
			[ 4, 6, Infinity ]
		]
	) {
		assert.equal( combatAdmissionRadius( actor, target, reach ), undefined, `${actor}/${target}/${reach}` );
	}
});

test("a monster's body is its graded, truncated radius", () => {
	// The Go grade table on a base of 7: champion 1.5, giant 3, elite 1.7,
	// the low nibble decides (0x13 keeps its body).
	for ( const [rarity, radius] of [ [ 0, 7 ], [ 1, 10 ], [ 4, 21 ], [ 6, 11 ], [ 0x13, 7 ] ] ) {
		assert.equal( monsterBodyRadius( 7, rarity ), radius, `rarity ${rarity}` );
	}
});

test("the grade multiplies the radius at float32 precision, as the server stores it", () => {
	// The server grades a float32 body: float32(4.6666667) * 3 lands just
	// under 14, so the giant's body truncates to 13. At double precision
	// the same product crosses 14.
	assert.equal( monsterBodyRadius( 4.6666667, 4 ), 13 );
});

test("a ref row's body grades only for monsters", () => {
	assert.equal( refBodyRadius( "monster", 2, 4 ), 6 );
	assert.equal( refBodyRadius( "monster", 2, 0 ), 2 );
	assert.equal( refBodyRadius( "player", 4, 0 ), 4 );
	assert.equal( refBodyRadius( "player", undefined, 0 ), undefined );
	assert.equal( refBodyRadius( "monster", undefined, 4 ), undefined );
});

test("the stop point stands at the admission radius, whole units", () => {
	assert.deepEqual( combatStopPoint( 100, 100, 400, 100, 64 ), { x: 336, z: 100 } );
	assert.deepEqual( combatStopPoint( 100, 100, 400, 100, 70 ), { x: 330, z: 100 } );
	// The server's walk goal rounds whole units, half away from zero (Go
	// math.Round): a raw -2.5 lands at -3, where Math.round alone gives -2.
	assert.deepEqual( combatStopPoint( 0, 0, -30, 0, 27.5 ), { x: -3, z: 0 } );
	assert.deepEqual( combatStopPoint( 0, 0, 0, -30, 27.5 ), { x: 0, z: -3 } );
});

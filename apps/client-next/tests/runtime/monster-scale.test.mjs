/*
===========================================================================

monster-scale.test.mjs - spawn records size monsters and companions

A companion takes its record percent with no grade factor (8554E0); the
monster rule and the champion material slot are unchanged by the rewrite.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { monsterScale, cosScale, monsterMaterialSlot } = await import(
	"../../src/engine/foundation/rendering/monster-scale.ts"
);

test("a companion is its record percent, whatever its grade", () => {
	// The glider (COS_P_GGLIDER) is authored at 55, the cat at 65.
	assert.equal( cosScale( 55 ), Math.fround( 0.55 ) );
	assert.equal( cosScale( 65 ), Math.fround( 0.65 ) );
	assert.equal( cosScale( 100 ), 1 );
	assert.equal( cosScale(), 1 );
});

test("the monster rule matches the formula it replaces", () => {
	// The previous one-line rule, kept here as the reference.
	const before = ( rarity, tidWord, percent ) => {
		const human = (tidWord & 0x7fe) === 0xc6 && [ 2, 3 ].includes( (tidWord >>> 11) & 31 );
		const factor = human ? 1 : rarity === 1 ? 1.5 : rarity === 4 ? 3 : rarity === 6 ? Math.fround( 1.7 ) : 1;
		return Math.fround( factor * percent / 100 );
	};
	for ( const rarity of [ 0, 1, 3, 4, 5, 6, 8 ] ) {
		for ( const tidWord of [ 0, 0xc6 | 2 << 11, 0xc6 | 3 << 11, 0xc6 | 4 << 11, 0x1c6 ] ) {
			for ( const percent of [ 55, 100, 130 ] ) {
				assert.equal( monsterScale( rarity, tidWord, percent ), before( rarity, tidWord, percent ) );
			}
			assert.equal(
				monsterMaterialSlot( rarity, tidWord, 1 ),
				(tidWord & 0x7fe) === 0xc6 && [ 2, 3 ].includes( (tidWord >>> 11) & 31 ) ? 0 : rarity === 1 ? 2 : 1
			);
		}
	}
});

/*
===========================================================================

weapon-sound-label.test.mjs - the native weapon swing/damage sound label

CItemData_GetSoundLabel (8ED490) maps a weapon class to the object label of
its effectsound.txt rows. The European classes use their own labels (two
of them share WAND); an unknown class has no label.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { weaponSoundLabel } = await import( "../../src/engine/foundation/animation/sound-selectors.ts" );

test("weapon classes take 8ED490's labels", () => {
	const label = weaponClass => weaponSoundLabel( weaponClass << 11 );
	assert.deepEqual(
		[ 2, 3, 4, 5, 6, 7, 8 ].map( label ),
		[ "SWORD", "BLADE", "SPEAR", "TBLADE", "BOW", "SWORD", "TSWORD" ]
	);
	assert.deepEqual(
		[ 9, 10, 11, 12, 13, 14, 15, 16 ].map( label ),
		[ "DUELAXE", "WAND", "STAFF", "CROSSBOW", "DAGGER", "HARP", "WAND", "HAMMER" ]
	);
	assert.equal( label( 1 ), "", "an unlisted class matches no row" );
	assert.equal( label( 17 ), "" );
});

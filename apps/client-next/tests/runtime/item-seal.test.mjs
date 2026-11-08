/*
===========================================================================

item-seal.test.mjs - tests for item-seal.ts and its readers

Which items are sealed (rarity 2 and 6, never 0 or 3), which seal tier they
carry, and that degree 11 names the first tier Seal of Nova. Also checks the
slot shine test and the notice colour read the same rule.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { itemRarityIsSealed, sealTier, sealTextSymbol } = await import(
	"../../src/engine/foundation/gameplay/item-seal.ts"
);
const { itemIsRare } = await import( "../../src/engine/foundation/ui/item-slot-effects.ts" );
const { sealNoticeColorArgb, SEAL_NOTICE_COLOR_STAR } = await import(
	"../../src/engine/foundation/gameplay/system-notices.ts"
);

test("only rarity 2 and 6 are sealed", () => {
	assert.equal( itemRarityIsSealed( 0 ), false );
	assert.equal( itemRarityIsSealed( 3 ), false );
	assert.equal( itemRarityIsSealed( undefined ), false );
	assert.equal( itemRarityIsSealed( 2 ), true );
	assert.equal( itemRarityIsSealed( 6 ), true );
});

test("seal symbols: Star, Moon, Sun up to degree 10, Nova from degree 11", () => {
	assert.equal( sealTextSymbol( 2, 28 ), "PARAM_RARE_FIRST" );
	assert.equal( sealTextSymbol( 2, 29 ), "PARAM_RARE_SECOND" );
	assert.equal( sealTextSymbol( 2, 30 ), "PARAM_RARE_THIRD" );
	assert.equal( sealTextSymbol( 2, 31 ), "PARAM_RARE_FIRST2" );
	assert.equal( sealTextSymbol( 6, 31 ), "PARAM_RARE_FIRST2" );
	assert.equal( sealTextSymbol( 6, 32 ), "PARAM_RARE_SECOND" );
	assert.equal( sealTextSymbol( 3, 31 ), undefined );
	assert.equal( sealTextSymbol( 0, 31 ), undefined );
	assert.equal( sealTextSymbol( 2, 0 ), undefined );
	assert.equal( sealTier( 6, 33 ), 2 );
});

test("the slot shine and the notice colour follow the same rule", () => {
	const item = rarity => ({ tooltip: { fields: { rarity } } });
	assert.equal( itemIsRare( item( 6 ) ), true );
	assert.equal( itemIsRare( item( 3 ) ), false );
	assert.equal( sealNoticeColorArgb( 6, 31 ), SEAL_NOTICE_COLOR_STAR );
	assert.equal( sealNoticeColorArgb( 3, 31 ), undefined );
});

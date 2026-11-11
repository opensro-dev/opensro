/*
===========================================================================

item-seal.test.mjs - tests for item-seal.ts and its readers

Which items are sealed (rarity 2 natively; rarity 6 only while the shard publishes
the cap-110 expansion; never 0 or 3), which seal tier they carry, and that degree 11
names the first tier Seal of Nova in the expansion. Also checks the slot shine and
the tooltip read the same rule.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { itemRarityIsSealed, sealTier, sealTextSymbol } = await import(
	"../../src/engine/foundation/gameplay/item-seal.ts"
);
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { itemIsRare } = await import( "../../src/engine/foundation/ui/item-slot-effects.ts" );
const { sealNoticeColorArgb, SEAL_NOTICE_COLOR_STAR } = await import(
	"../../src/engine/foundation/gameplay/system-notices.ts"
);

test("rarity 2 is sealed natively; rarity 6 is sealed only in the cap-110 expansion", () => {
	assert.equal( itemRarityIsSealed( 0, true ), false );
	assert.equal( itemRarityIsSealed( 3, true ), false );
	assert.equal( itemRarityIsSealed( undefined, true ), false );
	assert.equal( itemRarityIsSealed( 2 ), true );
	assert.equal( itemRarityIsSealed( 2, true ), true );
	assert.equal( itemRarityIsSealed( 6, true ), true );
	assert.equal( itemRarityIsSealed( 6 ), false, "native cap: no sealed set pieces" );
});

test("seal symbols: Star, Moon, Sun up to degree 10, Nova from degree 11 in the expansion", () => {
	for ( const expansion of [ false, true ] ) {
		assert.equal( sealTextSymbol( 2, 28, expansion ), "PARAM_RARE_FIRST" );
		assert.equal( sealTextSymbol( 2, 29, expansion ), "PARAM_RARE_SECOND" );
		assert.equal( sealTextSymbol( 2, 30, expansion ), "PARAM_RARE_THIRD" );
		assert.equal( sealTextSymbol( 3, 31, expansion ), undefined );
		assert.equal( sealTextSymbol( 0, 31, expansion ), undefined );
		assert.equal( sealTextSymbol( 2, 0, expansion ), undefined );
	}
	assert.equal( sealTextSymbol( 2, 31, true ), "PARAM_RARE_FIRST2" );
	assert.equal( sealTextSymbol( 6, 31, true ), "PARAM_RARE_FIRST2" );
	assert.equal( sealTextSymbol( 6, 32, true ), "PARAM_RARE_SECOND" );
	assert.equal( sealTextSymbol( 2, 31 ), "PARAM_RARE_FIRST", "native cap: the first tier stays Star" );
	assert.equal( sealTextSymbol( 6, 31 ), undefined );
	assert.equal( sealTier( 6, 33, true ), 2 );
	assert.equal( sealTier( 6, 33 ), undefined );
});

test("the slot shine follows the same rule; the native notice colour never takes rarity 6", () => {
	const item = rarity => ({ tooltip: { fields: { rarity } } });
	assert.equal( itemIsRare( item( 6 ), true ), true );
	assert.equal( itemIsRare( item( 6 ) ), false );
	assert.equal( itemIsRare( item( 2 ) ), true );
	assert.equal( itemIsRare( item( 3 ), true ), false );
	assert.equal( sealNoticeColorArgb( 6, 31 ), undefined );
	assert.equal( sealNoticeColorArgb( 2, 31 ), SEAL_NOTICE_COLOR_STAR );
});

/*
================
sealedPiece

An armor piece as the tooltip reads it; every stat column reads as 1 so the rows build.
================
*/
/** @returns {any} */
function sealedPiece( rarity, itemClass ) {
	const fields = new Proxy( { rarity, itemClass, setId: 0 }, {
		get: ( o, k ) => (k in o ? o[k] : 1),
		has: () => true
	} );
	return {
		name: "Piece",
		slot: 1,
		typeFlags: 0x2c | (3 << 7) | (1 << 11),
		quantity: 1,
		plus: 0,
		variance: 0,
		magic: [],
		tooltip: { fields }
	};
}

test("the tooltip names the seal of a sealed set piece and colours its name", () => {
	const text = symbol => (symbol === "PARAM_RARE_FIRST2" ? "Seal of Nova" : symbol);
	const rows = ( rarity, itemClass, expansion = true ) =>
		itemTooltip( sealedPiece( rarity, itemClass ), { masteries: [] }, text, { expansion } );
	assert.equal( rows( 6, 31 ).some( row => row.value === "Seal of Nova" ), true );
	assert.equal( rows( 6, 31 )[0].color, 0xffffd953 );
	assert.equal( rows( 2, 30 ).some( row => row.value === "PARAM_RARE_THIRD" ), true );
	assert.equal( rows( 3, 31 ).some( row => row.value === "Seal of Nova" ), false );
	assert.equal( rows( 3, 31 )[0].color, 0xffffffff );
	// Without the expansion cap a rarity 6 piece is plain, and a rarity 2 degree 11 piece is a Seal of Star.
	assert.equal( rows( 6, 31, false )[0].color, 0xffffffff );
	assert.equal( rows( 2, 31, false ).some( row => row.value === "PARAM_RARE_FIRST" ), true );
});

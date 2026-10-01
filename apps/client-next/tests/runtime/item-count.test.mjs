/*
===========================================================================

item-count.test.mjs - tests for item-count.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { itemCountQuads } = await import( sourceFileUrl( "src/engine/foundation/ui/item-count.ts" ).href );
test("native item count uses digit sprites including the final unit, and never equipment durability", () => {
	const r = [ 100, 200, 32, 32 ], clip = [ 0, 0, 1200, 900 ];
	for ( const n of [ 1, 9, 10, 50, 999, 1000, 65535 ] ) {
		const q = itemCountQuads( { typeFlags: 0x9ec, quantity: n }, r, clip ), digits = String( n );
		assert.equal( q.length, digits.length );
		q.forEach( ( q, i ) => {
			assert.ok( q.texture.endsWith( "item_number_" + digits.at( -1 - i ) + ".png" ) );
			assert.deepEqual( q.rect, [ 100 + digits.length * 4 - 2 - i * 5, 202, 8, 8 ] );
			assert.deepEqual( q.clip, clip );
		} );
	}
	for ( const n of [ 0, -1, 1.5, 65536, NaN ] ) {
		assert.deepEqual( itemCountQuads( { typeFlags: 0x9ec, quantity: n }, r, clip ), [] );
	}
	assert.deepEqual( itemCountQuads( { typeFlags: 0x132c, quantity: 50 }, r, clip ), [] );
});

test("timed-service quantity suppression consumes the reference flag only for its native family", () => {
	for ( const typeFlags of [ 0x7eec, 0x9ec ] ) {
		for ( const flag of [ 0, 1, 2, 3 ] ) {
			const q = itemCountQuads( { typeFlags, quantity: 1, tooltip: { fields: { itemParam6_2b0: flag } } }, [
				0,
				0,
				32,
				32
			], [ 0, 0, 32, 32 ] );
			assert.equal( q.length, typeFlags === 0x7eec && (flag & 2) ? 0 : 1 );
		}
	}
});

// A return scroll: expendable type 3/3 (0x6c family).
const scroll = { quantity: 1, typeFlags: 0x6c };
const slot = [ 100, 200, 32, 32 ];

test("a single expendable unit draws its digit sprite", () => {
	const quads = itemCountQuads( scroll, slot, slot );
	assert.deepEqual( quads.map( q => q.texture ), [
		"/assets/images/Media_extracted/interface/item_number/item_number_1.png"
	] );
});

test("an unlimited item draws the infinity sign instead of its stack", () => {
	const quads = itemCountQuads( scroll, slot, slot, true );
	assert.ok( quads.length > 1 );
	assert.ok( quads.every( q => q.texture === "" ), "drawn with solid quads, no asset" );
	const [backing, ...glyph] = quads;
	assert.deepEqual( backing.rect, [ 101, 202, 8, 5 ] );
	assert.deepEqual( backing.color, [ 0, 0, 0, 1 ] );
	// Every white run stays inside the backing.
	for ( const q of glyph ) {
		assert.deepEqual( q.color, [ 1, 1, 1, 1 ] );
		assert.ok( q.rect[0] >= 101 && q.rect[0] + q.rect[2] <= 109 && q.rect[1] >= 202 && q.rect[1] < 207 );
	}
});

test("items that show no count stay blank even when unlimited", () => {
	assert.deepEqual( itemCountQuads( { quantity: 1, typeFlags: 0x332c }, slot, slot, true ), [] );
});

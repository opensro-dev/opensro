/*
===========================================================================

renderer-row.test.mjs - the one renderer-row comparison (#3759)

Pins the rules tests/helpers/renderer-row.mjs shares between the two
presentation-due tests: key order does not matter, an undefined-valued
key reads as an absent one, -0 is not 0, a Float32Array compares by its
values, and a value the crowd capture could not encode - a function, a
symbol, a bigint, a non-finite number, or a non-plain object - is
refused instead of compared by identity.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { sameRow } from "../helpers/renderer-row.mjs";

test("key order and nesting do not matter, values do", () => {
	assert.equal( sameRow( { a: 1, b: [ 2, { c: "x" } ] }, { b: [ 2, { c: "x" } ], a: 1 } ), true );
	assert.equal( sameRow( { a: 1, b: [ 2, { c: "x" } ] }, { b: [ 2, { c: "y" } ], a: 1 } ), false );
	assert.equal( sameRow( [ 1, 2 ], [ 1, 2, 3 ] ), false );
	assert.equal( sameRow( "row", "row" ), true );
	assert.equal( sameRow( null, null ), true );
	assert.equal( sameRow( null, {} ), false );
	assert.equal( sameRow( true, 1 ), false );
});
test("an undefined-valued key reads as an absent one", () => {
	assert.equal( sameRow( { a: 1, b: undefined }, { a: 1 } ), true );
	assert.equal( sameRow( { a: undefined }, {} ), true );
	assert.equal( sameRow( { a: undefined }, { a: 0 } ), false );
	assert.equal( sameRow( { a: 1 }, { a: 1, b: undefined } ), true );
});
test("-0 is not 0 and a Float32Array compares by its values", () => {
	assert.equal( sameRow( { x: -0 }, { x: 0 } ), false );
	assert.equal( sameRow( { x: -0 }, { x: -0 } ), true );
	assert.equal( sameRow( new Float32Array( [ 1, 2 ] ), new Float32Array( [ 1, 2 ] ) ), true );
	assert.equal( sameRow( new Float32Array( [ 1, 2 ] ), new Float32Array( [ 1, 3 ] ) ), false );
	assert.equal( sameRow( new Float32Array( [ 1 ] ), [ 1 ] ), false );
});
test("a value the crowd capture could not encode is refused", () => {
	for ( const bad of [ () => {}, Symbol( "s" ), 10n ] ) {
		assert.throws( () => sameRow( bad, bad ), /Unsupported renderer row value/ );
		assert.throws( () => sameRow( { a: [ bad ] }, { a: [ bad ] } ), /Unsupported renderer row value/ );
	}
	for ( const bad of [ NaN, Infinity, -Infinity ] ) {
		assert.throws( () => sameRow( { a: bad }, { a: bad } ), /Non-finite renderer row value/ );
	}
	class Row {}
	assert.throws( () => sameRow( new Row(), new Row() ), /Unsupported renderer row object/ );
	assert.throws( () => sameRow( new Map(), new Map() ), /Unsupported renderer row object/ );
});
test("nesting past the limit is refused", () => {
	let deep = /** @type {any} */ (0);
	for ( let i = 0; i < 70; i++ ) deep = [ deep ];
	assert.throws( () => sameRow( deep, deep ), /Renderer row nesting limit/ );
});

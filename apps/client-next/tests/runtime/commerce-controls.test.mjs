/*
===========================================================================

commerce-controls.test.mjs - the world session admits every commerce frame

The Item Mall catalogue (15) was missing from the session's admission
list, so opening the mall (F10) ended the session.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const commerce = await import( "../../src/engine/foundation/gameplay/commerce-controls.ts" );

test("every commerce control the server sends is admitted, and nothing else", () => {
	for ( const opcode of [ 11, 12, 13, 14, 15, 16 ] ) assert.ok( commerce.isWorldControl( opcode ), String( opcode ) );
	for ( const opcode of [ 4, 10, 17, 255 ] ) assert.ok( !commerce.isWorldControl( opcode ), String( opcode ) );
	assert.equal( commerce.MALL_CATALOG_CONTROL, 15, "the server's opMallCatalog (itemmall.go)" );
});

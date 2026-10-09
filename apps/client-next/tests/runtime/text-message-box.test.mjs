/*
===========================================================================

text-message-box.test.mjs - tests for text-message-box.ts

The simple message box wraps its body at 600 px (52DB20) and grows to the
wrapped lines (52BCF0), never past the wrap width plus its margins.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { messageBoxLines, textMessageBoxLayout } = await import(
	sourceFileUrl( "src/engine/foundation/ui/text-message-box.ts" ).href
);

const GLYPH = 10;
const measure = value => value.length * GLYPH;

test("a body line wider than 600 px wraps, and the box grows only to the wrap width", () => {
	const words = Array.from( { length: 30 }, ( _, i ) => `word${String( i ).padStart( 2, "0" )}` ).join( " " );
	const lines = messageBoxLines( [ words ], measure );
	assert.ok( lines.length > 1, "the line was not wrapped" );
	assert.ok( lines.every( line => measure( line ) <= 600 ), "a wrapped line exceeds 600 px" );
	assert.equal( lines.join( " " ), words, "wrapping lost or reordered text" );
	const layout = textMessageBoxLayout( 1920, 1080, lines.map( measure ) );
	assert.ok( layout.frame[2] <= 600 + 60, `the box grew to ${layout.frame[2]}` );
	assert.equal( layout.lines.length, lines.length );
});

test("short lines keep the native 360x151 box", () => {
	const lines = messageBoxLines( [ "Leave the party?" ], measure );
	assert.deepEqual( lines, [ "Leave the party?" ] );
	const layout = textMessageBoxLayout( 1920, 1080, lines.map( measure ) );
	assert.deepEqual( [ layout.frame[2], layout.frame[3] ], [ 360, 151 ] );
});

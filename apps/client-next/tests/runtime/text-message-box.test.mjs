/*
===========================================================================

text-message-box.test.mjs - tests for text-message-box.ts

The simple message box wraps its body at 600 px (52DB20) and grows to the
wrapped lines (52BCF0), never past the wrap width plus its margins. An
SML2 body is split at its breaks and never shows its tags (#3002).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { markupLines, messageBoxLines, textMessageBoxLayout } = await import(
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

// UIIT_MSG_MSGBOX_ASK_REBIRTH_MUTATION as the shipped English text holds it.
const REBIRTH_MUTATION = "<sml2><left>Someone is trying to resurrect you in a beast form.<br><br>" +
	"This form has a side effect of weak<br>basic attacks and great defense increase<br>" +
	"and great reduction of HP and MP.<br><br>Resurrect and restart adventure?</left></sml2>";

test("an SML2 body is split at its breaks, an empty line for each doubled break, with no tags", () => {
	assert.deepEqual( markupLines( REBIRTH_MUTATION ), [
		"Someone is trying to resurrect you in a beast form.",
		"",
		"This form has a side effect of weak",
		"basic attacks and great defense increase",
		"and great reduction of HP and MP.",
		"",
		"Resurrect and restart adventure?"
	] );
	const lines = messageBoxLines( markupLines( REBIRTH_MUTATION ), measure );
	assert.ok( lines.every( line => !line.includes( "<" ) ), "a tag reached a drawn line" );
	const layout = textMessageBoxLayout( 1920, 1080, lines.map( measure ) );
	assert.equal( layout.lines.length, 7 );
	assert.ok( layout.frame[2] <= 600 + 60, `the box grew to ${layout.frame[2]}` );
});

test("plain text is one line, and unreadable markup falls back to the raw text", () => {
	assert.deepEqual( markupLines( "Leave the party?" ), [ "Leave the party?" ] );
	const broken = "<sml2><left>unbalanced</right></sml2>";
	assert.deepEqual( markupLines( broken ), [ broken ] );
});

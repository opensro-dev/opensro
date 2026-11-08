/*
===========================================================================

event-interface.test.mjs - the event window's published layouts (#323)

CIFEventInterface (547750) shows a window only when its layout file
loads and opens (546B50): the native gate publishes closed on v1.150,
the later PC-room layout publishes open. The decode must refuse a
malformed publication outright so the HUD load fails instead of
opening a wrong window, and the window is centered at 540x232.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	EVENT_INTERFACE_WIDTH,
	EVENT_INTERFACE_HEIGHT,
	decodeEventInterface,
	decodeEventInterfaceLayout,
	eventInterfaceOrigin,
	eventInterfaceShown
} = await import( "../../src/engine/foundation/ui/event-interface.ts" );

function layout( overrides = {} ) {
	return {
		open: true,
		titleFont: 4,
		titleColor: [ 0.66, 0.6, 0.08, 1 ],
		titlePosition: [ 31, 24 ],
		title: "",
		titleCode: "SN_TEST_TITLE",
		textFont: 0,
		textColor: [ 0.38, 0.35, 0.08, 1 ],
		textPosition: [ 32, 55 ],
		textMargin: 5,
		text: "Hello",
		textCode: "",
		...overrides
	};
}

test("the native gate and the later layouts decode side by side", () => {
	const data = decodeEventInterface(
		{ layout: layout( { open: false } ) },
		{ layouts: { pcroom: layout(), winter: layout( { textPosition: [ 32, 40 ] } ) } }
	);
	assert.ok( data.gate );
	assert.ok( data.pcroom );
	assert.ok( data.winter );
	assert.equal( data.gate.open, false );
	assert.equal( eventInterfaceShown( data.gate ), false );
	assert.equal( eventInterfaceShown( data.pcroom ), true );
	assert.equal( data.winter.textPosition[1], 40 );
	assert.equal( data.pcroom.picBg, undefined );
});

test("an absent later publication leaves both later windows unshown", () => {
	const data = decodeEventInterface( { layout: layout() }, null );
	assert.equal( data.pcroom, null );
	assert.equal( data.winter, null );
	assert.equal( eventInterfaceShown( data.pcroom ), false );
	assert.equal( eventInterfaceShown( data.gate ), true );
});

test("a malformed publication is refused, not repaired", () => {
	assert.throws( () => decodeEventInterfaceLayout( layout( { titleColor: [ 2, 0, 0, 1 ] } ) ) );
	assert.throws( () => decodeEventInterfaceLayout( layout( { titlePosition: [ 31 ] } ) ) );
	assert.throws( () => decodeEventInterfaceLayout( layout( { open: "yes" } ) ) );
	assert.throws( () => decodeEventInterface( {}, null ) );
});

test("the window is the native 540x232, centered and kept on screen", () => {
	assert.equal( EVENT_INTERFACE_WIDTH, 540 );
	assert.equal( EVENT_INTERFACE_HEIGHT, 232 );
	assert.deepEqual( eventInterfaceOrigin( 1024, 768 ), [ 242, 268 ] );
	assert.deepEqual( eventInterfaceOrigin( 320, 200 ), [ 0, 0 ] );
});

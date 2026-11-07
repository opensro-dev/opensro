/*
===========================================================================

ui-frame.test.mjs - the frame builder's window admission

uiBeginWindow/uiEndWindow (frame/frame.ts) are one window's resource
admission: while any of the window's own art is still loading, the
window's quads, controls and blocks are withheld (its image demand
stays), and a window admitted before is replayed in their place. These
tests drive the module directly, with no ui.ts step around it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { uiBeginWindow, uiEndWindow, uiCloseButton, uiImage } = await import(
	"../../src/engine/runtime/ui/frame/frame.ts"
);

/*
================
frame

A frame whose loaded set the test controls. The font and the common
window art are loaded unless the test takes them out.
================
*/
function frame( loaded ) {
	const has = new Set( loaded );
	/** @type {any} */
	const made = {
		quads: [],
		controls: [],
		blocks: [],
		paths: [],
		full: [ 0, 0, 800, 600 ],
		width: 800,
		height: 600,
		text: { quads: () => [], path: () => "font.png" },
		resources: { has: ( /** @type {string} */ path ) => has.has( path ), size: () => [ 16, 16 ] },
		placement: { frame: ( /** @type {string} */ _id, /** @type {any} */ initial ) => initial },
		contentImages: new Set(),
		windowMissing: [],
		admittedWindows: new Map(),
		windowPaths: [ "common.png" ],
		fontPath: "font.png",
		phase: "world",
		compact: null,
		chrome: {
			frame: "frame/",
			frameParts: [],
			button: "button.png",
			buttonFocus: "button_focus.png",
			buttonPress: "button_press.png",
			buttonDisable: "button_disable.png",
			close: "close.png",
			closePress: "close_press.png",
			closeFocus: "close_focus.png"
		},
		pressed: null,
		hover: null,
		focus: null
	};
	return made;
}

/*
================
drawWindow

One window's worth of draws between the admission pair: an image and
a close button.
================
*/
function drawWindow( made, art ) {
	const mark = uiBeginWindow( made );
	uiImage( made, [ 10, 10, 32, 32 ], art );
	uiCloseButton( made, 40, 10, "win-close" );
	uiEndWindow( made, mark );
	return mark;
}

test("a window whose art is still loading is withheld, its demand kept", () => {
	const made = frame( [ "font.png", "common.png" ] );
	drawWindow( made, "art.png" );
	assert.deepEqual( made.quads, [] );
	assert.deepEqual( made.controls, [] );
	assert.deepEqual( made.blocks, [] );
	assert.deepEqual( made.paths, [ "common.png", "art.png" ] );
	assert.deepEqual( made.windowMissing, [ "art.png" ] );
	assert.equal( made.admittedWindows.size, 0 );
});

test("a missing font withholds the window even with its art loaded", () => {
	const made = frame( [ "font.png", "common.png", "art.png" ] );
	made.fontPath = null;
	drawWindow( made, "art.png" );
	assert.deepEqual( made.quads, [] );
	assert.deepEqual( made.controls, [] );
	assert.equal( made.admittedWindows.size, 0 );
});

test("a loaded window is admitted and cached under its owner", () => {
	const made = frame( [ "font.png", "common.png", "art.png", "close.png", "close_press.png", "close_focus.png" ] );
	drawWindow( made, "art.png" );
	// The image quad and the close quad, the close control and its block.
	assert.equal( made.quads.length, 2 );
	assert.equal( made.controls.length, 1 );
	assert.equal( made.blocks.length, 1 );
	const admitted = made.admittedWindows.get( "primary" );
	assert.ok( admitted );
	assert.equal( admitted.key, "world:800:600" );
	assert.equal( admitted.quads.length, 2 );
	assert.equal( admitted.controls.length, 1 );
	assert.deepEqual( admitted.paths, [ "common.png", "art.png" ] );
});

test("an admitted window is replayed, controls disabled, while its replacement loads", () => {
	const loaded = [ "font.png", "common.png", "art.png", "close.png", "close_press.png", "close_focus.png" ];
	const made = frame( loaded );
	drawWindow( made, "art.png" );
	made.quads = [];
	made.controls = [];
	made.blocks = [];
	made.paths = [];
	drawWindow( made, "late.png" );
	assert.equal( made.quads.length, 2 );
	assert.equal( made.controls.length, 1 );
	assert.equal( made.controls[0].id, "win-close" );
	assert.equal( made.controls[0].disabled, true );
	// The replay adds the admitted demand to the demand already made.
	assert.deepEqual( made.paths, [ "common.png", "late.png", "common.png", "art.png" ] );
});

test("an item image still loading does not hold its window's admission", () => {
	const loaded = [ "font.png", "common.png", "art.png", "close.png", "close_press.png", "close_focus.png" ];
	const made = frame( loaded );
	made.contentImages.add( "item.png" );
	const mark = uiBeginWindow( made );
	uiImage( made, [ 10, 10, 32, 32 ], "art.png" );
	made.paths.push( "item.png" );
	uiCloseButton( made, 40, 10, "win-close" );
	uiEndWindow( made, mark );
	assert.equal( made.controls.length, 1 );
	assert.deepEqual( made.windowMissing, [ "item.png" ] );
	assert.equal( made.admittedWindows.size, 1 );
});

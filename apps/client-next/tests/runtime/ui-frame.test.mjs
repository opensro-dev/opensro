/*
===========================================================================

ui-frame.test.mjs - frame primitives and whole-window resource admission

Runs the frame module without a browser or retail assets. Missing chrome
must withhold hit targets as well as pixels while keeping resource demand.
An admitted window may bridge a cold replacement only in its own viewport.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { beginWindow, endWindow, image, rect, button, closeButton } = await import(
	"../../src/engine/runtime/ui/frame/frame.ts"
);
const { compactHudLayout, compactWindowDrag, compactCloseControl } = await import(
	"../../src/engine/runtime/ui/hud/compact-hud.ts"
);

const FONT = "font", CHROME = "chrome", CONTENT = "content", MISSING = "missing";
const VIEWPORT = /** @type {const} */ ([ 0, 0, 800, 600 ]);
const WINDOW = /** @type {const} */ ([ 100, 100, 200, 150 ]);
const WHITE = /** @type {const} */ ([ 1, 1, 1, 1 ]);

/*
================
emptyText
================
*/
function emptyText() {
	return [];
}

/*
================
imageSize
================
*/
function imageSize() {
	return /** @type {const} */ ([ 16, 16 ]);
}

/*
================
initialWindow
================
*/
function initialWindow( _id, initial ) {
	return initial;
}

/*
================
fixture
================
*/
/** @param {Partial<import("../../src/engine/runtime/ui/frame/frame-contract.ts").UiFrame>} [options] */
function fixture( options = {} ) {
	const loaded = new Set( [ FONT, CHROME, CONTENT ] );
	/** @type {import("../../src/engine/runtime/ui/frame/frame-contract.ts").UiFrame} */
	const frame = {
		quads: [],
		controls: [],
		blocks: [],
		paths: [ FONT ],
		w: VIEWPORT[2],
		h: VIEWPORT[3],
		full: VIEWPORT,
		hud: null,
		text: { quads: emptyText },
		resources: { has: loaded.has.bind( loaded ), size: imageSize },
		windowPlacement: { frame: initialWindow },
		compact: null,
		compactPolicy: { windowDrag: compactWindowDrag, closeControl: compactCloseControl },
		chrome: {
			frame: CHROME,
			parts: [],
			button: CHROME,
			buttonFocus: "focus",
			buttonPress: "press",
			buttonDisable: "disable",
			close: CHROME,
			closePress: "press",
			closeFocus: "focus"
		},
		white: WHITE,
		commonWindowPaths: [ CHROME ],
		admittedWindows: new Map(),
		windowMissing: [],
		contentImages: new Set(),
		phase: "world",
		fontPath: FONT,
		pressed: null,
		hover: null,
		focus: null,
		...options
	};
	return { frame, loaded };
}

/*
================
paintWindow
================
*/
function paintWindow( frame, path = CONTENT ) {
	const mark = beginWindow( frame );
	image( frame, { r: WINDOW, path } );
	frame.controls.push( { id: "window-action", label: "Action", kind: "button", rect: WINDOW } );
	frame.blocks.push( WINDOW );
	return mark;
}

test("missing window chrome rolls back only its output and keeps demand", () => {
	const { frame, loaded } = fixture();
	loaded.delete( CHROME );
	rect( frame, { r: [ 1, 2, 3, 4 ], color: WHITE } );
	frame.controls.push( { id: "earlier", label: "Earlier", kind: "button", rect: WINDOW } );
	frame.blocks.push( VIEWPORT );
	const earlier = { quads: frame.quads.slice(), controls: frame.controls.slice(), blocks: frame.blocks.slice() };
	endWindow( frame, paintWindow( frame ) );
	assert.deepEqual( frame.quads, earlier.quads );
	assert.deepEqual( frame.controls, earlier.controls );
	assert.deepEqual( frame.blocks, earlier.blocks );
	assert.deepEqual( frame.paths, [ FONT, CHROME, CONTENT ] );
	assert.deepEqual( frame.windowMissing, [ CHROME ] );
	assert.equal( frame.admittedWindows.size, 0 );
});

for ( const fontPath of [ null, MISSING ] ) {
	test(`a window waits for its font (${fontPath})`, () => {
		const { frame } = fixture( { fontPath } );
		endWindow( frame, paintWindow( frame ) );
		assert.deepEqual( [ frame.quads, frame.controls, frame.blocks ], [ [], [], [] ] );
		assert.ok( frame.paths.includes( CONTENT ), "a failed admission still requests its artwork" );
		assert.equal( frame.admittedWindows.size, 0 );
	});
}

test("missing content is reported but does not withhold complete chrome", () => {
	const { frame } = fixture();
	const mark = paintWindow( frame );
	frame.contentImages.add( MISSING );
	image( frame, { r: WINDOW, path: MISSING } );
	endWindow( frame, mark );
	assert.equal( frame.quads.length, 1 );
	assert.equal( frame.controls.length, 1 );
	assert.equal( frame.blocks.length, 1 );
	assert.deepEqual( frame.windowMissing, [ MISSING ] );
	assert.ok( frame.paths.includes( MISSING ) );
	assert.deepEqual( defined( frame.admittedWindows.get( "primary" ) ).paths, [ CHROME, CONTENT, MISSING ] );
});

test("retained admission disables old content but preserves close and tab controls", () => {
	const { frame: before } = fixture();
	const mark = paintWindow( before );
	before.controls.push(
		{ id: "close-window:Inventory", label: "Close", kind: "button", rect: WINDOW, disabled: false },
		{ id: "select-window:Character", label: "Character", kind: "button", rect: WINDOW },
		{ id: "select-window:Skills", label: "Skills", kind: "button", rect: WINDOW, disabled: true }
	);
	endWindow( before, mark );
	const cached = defined( before.admittedWindows.get( "primary" ) );
	const { frame } = fixture( { admittedWindows: before.admittedWindows } );
	endWindow( frame, paintWindow( frame, MISSING ) );
	assert.deepEqual( frame.quads, cached.quads );
	assert.deepEqual( frame.blocks, cached.blocks );
	assert.deepEqual( frame.controls.map( control => control.disabled ), [ true, false, undefined, true ] );
	assert.equal( cached.controls[0].disabled, undefined, "replay must not mutate the saved controls" );
	assert.ok(
		frame.paths.includes( MISSING ) && frame.paths.includes( CONTENT ),
		"both replacement and replay stay demanded"
	);
	assert.equal( frame.admittedWindows.get( "primary" ), cached, "a partial frame cannot replace the cache" );
});

for ( const change of [ { phase: "signed-out" }, { w: 1024 }, { h: 768 } ] ) {
	test(`retained admission does not cross ${Object.keys( change )[0]}`, () => {
		const { frame: before } = fixture();
		endWindow( before, paintWindow( before ) );
		const { frame } = fixture( { ...change, admittedWindows: before.admittedWindows } );
		endWindow( frame, paintWindow( frame, MISSING ) );
		assert.deepEqual( [ frame.quads, frame.controls, frame.blocks ], [ [], [], [] ] );
		assert.ok( frame.paths.includes( MISSING ) );
	});
}

test("window owners retain independent snapshots and accept a completed replacement", () => {
	const { frame: before } = fixture();
	endWindow( before, paintWindow( before ), "inventory" );
	const { frame, loaded } = fixture( { admittedWindows: before.admittedWindows } );
	endWindow( frame, paintWindow( frame, MISSING ), "shop" );
	assert.equal( frame.quads.length, 0, "shop cannot replay inventory" );
	loaded.add( MISSING );
	endWindow( frame, paintWindow( frame, MISSING ), "shop" );
	assert.equal( defined( frame.admittedWindows.get( "inventory" ) ).quads[0].texture, CONTENT );
	assert.equal( defined( frame.admittedWindows.get( "shop" ) ).quads[0].texture, MISSING );
});

test("compact admission fits the window and enlarges its exit before caching", () => {
	const width = 390, height = 844;
	const { frame } = fixture( {
		w: width,
		h: height,
		full: [ 0, 0, width, height ],
		compact: compactHudLayout( width, height, true )
	} );
	const mark = beginWindow( frame );
	const source = /** @type {const} */ ([ 600, 300, 200, 150 ]);
	rect( frame, { r: source, color: WHITE } );
	frame.blocks.push( source );
	frame.controls.push( {
		id: "window-drag:Inventory",
		label: "Move",
		kind: "region",
		draggable: true,
		rect: source
	} );
	closeButton( frame, 780, 300, "close-window:Inventory" );
	endWindow( frame, mark, "primary", source );
	assert.deepEqual( frame.quads[0].rect, [ 190, 300, 200, 150 ] );
	assert.equal( frame.controls[0].draggable, false );
	assert.deepEqual( frame.controls[1].rect, [ 350, 288, 40, 40 ] );
	assert.deepEqual( defined( frame.admittedWindows.get( "primary" ) ).controls, frame.controls );
});

test("primitives follow replaced arrays and current input in the frame record", () => {
	const { frame, loaded } = fixture();
	for ( const path of [ "focus", "press", "disable" ] ) loaded.add( path );
	const previous = { quads: frame.quads, controls: frame.controls, blocks: frame.blocks, paths: frame.paths };
	frame.quads = [];
	frame.controls = [];
	frame.blocks = [];
	frame.paths = [];
	frame.pressed = frame.hover = "test";
	button( frame, { id: "test", value: "Test", x: 10, y: 20 } );
	image( frame, { r: WINDOW, path: MISSING } );
	assert.equal( frame.quads[0].texture, "press" );
	assert.equal( frame.quads[0].clip, frame.full );
	assert.deepEqual( frame.controls[0].rect, [ 10, 20, 120, 24 ] );
	assert.deepEqual( frame.paths, [ MISSING ] );
	assert.deepEqual( previous, { quads: [], controls: [], blocks: [], paths: [ FONT ] } );
	const count = frame.quads.length;
	rect( frame, { r: [ 0, 0, 0, 20 ], color: WHITE } );
	rect( frame, { r: [ 0, 0, 20, -1 ], color: WHITE } );
	assert.equal( frame.quads.length, count, "non-positive rectangles remain absent" );
});

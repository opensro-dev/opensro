/*
===========================================================================

experimental-ui.test.mjs - numeric rows through the production Experimental UI

Numeric choices (render scale, UI size) share the existing draft lifecycle.
Controls publish their selection, Confirm persists it, and every
cancellation path discards edits.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { panelCloseId } = await import( "../../src/engine/foundation/ui/panels.ts" );

const SCALE_PREFIX = "experimental-render-scale:";
const { EXPERIMENTAL_TABS, experimentalRows } = await import( "../../src/engine/runtime/ui/hud/experimental-hud.ts" );
const ROW_IDS = new Set( EXPERIMENTAL_TABS.flatMap( experimentalRows ).map( row => row.id ) );
const SETTLE_STEPS = 12;
const STEP_MS = 100;

/*
================
fixture
================
*/
function fixture( initial = experimentalOptions() ) {
	const saved = [];
	const f = uiFixture( undefined, undefined, undefined, undefined, undefined, undefined, undefined, {
		saveExperimental: value => saved.push( value )
	} );
	let now = 0;
	let latest;
	f.ui.event( { kind: "experimental-preferences", value: initial } );
	for ( let step = 0; step < SETTLE_STEPS; step++ ) {
		latest = f.ui.step( f.state, now += STEP_MS ) ?? latest;
	}
	/*
	================
	activate
	================
	*/
	function activate( id ) {
		f.ui.event( { kind: "activate", id } );
		latest = f.ui.step( f.state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	/*
	================
	edit
	================
	*/
	function edit( id, value ) {
		f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
		latest = f.ui.step( f.state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	/*
	================
	wheel

	One wheel notch over the window; positive scrolls the list down.
	================
	*/
	function wheel( delta ) {
		f.ui.event( { kind: "scroll", x: 800, y: 450, delta } );
		latest = f.ui.step( f.state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	/*
	================
	reveal

	Wheel down until the control is painted, at most limit notches.
	================
	*/
	function reveal( id, limit = 30 ) {
		let view = defined( latest );
		for ( let notch = 0; notch < limit && !view.controls.some( control => control.id === id ); notch++ ) {
			view = wheel( 1 );
		}
		return view;
	}
	/*
	================
	drag
	================
	*/
	function drag( id, dy ) {
		f.ui.event( { kind: "drag", id, dx: 0, dy } );
		f.ui.event( { kind: "drag-end", id, x: 800, y: 450 } );
		latest = f.ui.step( f.state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	return { ...f, saved, activate, edit, wheel, reveal, drag };
}

/*
================
assertScale
================
*/
function assertScale( view, selected ) {
	const choices = view.controls.filter( control => control.id.startsWith( SCALE_PREFIX ) );
	assert.deepEqual( choices.map( control => control.label ), [ "100%", "75%", "50%" ] );
	assert.deepEqual( choices.filter( control => control.selected ).map( control => control.id ), [
		SCALE_PREFIX + selected
	] );
	for ( const choice of choices ) assert.equal( choice.disabled, false );
}

/*
================
Selection and persistence
================
*/
test("Experimental renders all numeric choices and only Confirm persists selection", () => {
	const f = fixture();
	try {
		assertScale( f.activate( "open-window:Experimental" ), 100 );
		assert.ok( f.hasText( "Render scale" ) );
		for ( const scale of [ 75, 50, 100 ] ) {
			assertScale( f.activate( SCALE_PREFIX + scale ), scale );
			assert.equal( f.saved.length, 0 );
		}
		assertScale( f.activate( SCALE_PREFIX + 75 ), 75 );
		f.activate( "experimental-anisotropic-filtering" );
		f.activate( "experimental-confirm" );
		assert.deepEqual( f.saved, [ experimentalOptions( { renderScale: 75, anisotropicFiltering: true } ) ] );
		assertScale( f.activate( "open-window:Experimental" ), 75 );
		const restored = fixture( experimentalOptions( JSON.parse( JSON.stringify( f.saved[0] ) ) ) );
		try {
			assertScale( restored.activate( "open-window:Experimental" ), 75 );
		} finally {
			restored.dispose();
		}
	} finally {
		f.dispose();
	}
});

test("the Safe loading screen checkbox defaults off, cancels and persists on Confirm", () => {
	const selected = view =>
		defined( view.controls.find( control => control.id === "experimental-safe-loading-screen" ) )
			.selected;
	const f = fixture();
	// The row lives at the end of Interface (section Other), below the
	// fold; opening the window returns to Graphics at the top.
	const INTERFACE_TAB = "experimental-tab:1";
	const open = () => {
		f.activate( "open-window:Experimental" );
		f.activate( INTERFACE_TAB );
		return f.reveal( "experimental-safe-loading-screen" );
	};
	try {
		assert.equal( selected( open() ), false );
		assert.ok( f.hasText( "Safe loading screen" ) );
		assert.equal( selected( f.activate( "experimental-safe-loading-screen" ) ), true );
		// Review Focus 1: scrolling away and back keeps the draft.
		for ( let notch = 0; notch < 30; notch++ ) f.wheel( -1 );
		assert.equal( selected( f.reveal( "experimental-safe-loading-screen" ) ), true );
		assert.equal( f.saved.length, 0 );
		f.activate( "experimental-cancel" );
		assert.equal( selected( open() ), false );
		f.activate( "experimental-safe-loading-screen" );
		f.activate( "experimental-confirm" );
		assert.deepEqual( f.saved, [ experimentalOptions( { safeLoadingScreen: true } ) ] );
		assert.equal( selected( open() ), true );
		f.activate( "experimental-safe-loading-screen" );
		f.activate( "experimental-confirm" );
		assert.equal( f.saved[1].safeLoadingScreen, false );
	} finally {
		f.dispose();
	}
});

/*
================
UI size slider
================
*/
test("the UI size slider drafts until Confirm, and 100 is its native stop", () => {
	const slider = view => view.controls.find( control => control.id === "experimental-ui-scale" );
	const f = fixture();
	try {
		const opened = slider( f.activate( "open-window:Experimental" ) );
		assert.ok( opened );
		assert.equal( opened.kind, "range" );
		assert.equal( opened.min, 50 );
		assert.equal( opened.max, 200 );
		assert.equal( opened.value, "100" );
		assert.ok( f.hasText( "UI size" ) );
		// A dragged size is only a draft; off-slider values leave it intact.
		assert.equal( slider( f.edit( "experimental-ui-scale", "150" ) ).value, "150" );
		assert.equal( slider( f.edit( "experimental-ui-scale", "137" ) ).value, "135" );
		assert.equal( slider( f.edit( "experimental-ui-scale", "999" ) ).value, "135" );
		assert.equal( f.saved.length, 0 );
		f.activate( "experimental-confirm" );
		assert.deepEqual( f.saved, [ experimentalOptions( { uiScale: 135 } ) ] );
		assert.equal( slider( f.activate( "open-window:Experimental" ) ).value, "135" );
		// Cancel discards a new draft; Default drafts the native 100.
		f.edit( "experimental-ui-scale", "75" );
		f.activate( "experimental-cancel" );
		assert.equal( f.saved.length, 1 );
		assert.equal( slider( f.activate( "open-window:Experimental" ) ).value, "135" );
		f.activate( "experimental-default" );
		assert.equal( f.saved.length, 1 );
		f.activate( "experimental-confirm" );
		assert.deepEqual( f.saved[1], experimentalOptions() );
	} finally {
		f.dispose();
	}
});

/*
================
Cancellation paths
================
*/
test("Cancel, close and Escape discard render scale edits", () => {
	for ( const close of [ "experimental-cancel", panelCloseId( "Experimental" ), "Escape" ] ) {
		const f = fixture( experimentalOptions( { renderScale: 75 } ) );
		try {
			assertScale( f.activate( "open-window:Experimental" ), 75 );
			assertScale( f.activate( SCALE_PREFIX + 50 ), 50 );
			if ( close === "Escape" ) f.ui.event( { kind: "key", code: "Escape" } );
			else f.activate( close );
			assertScale( f.activate( "open-window:Experimental" ), 75 );
			assert.equal( f.saved.length, 0 );
		} finally {
			f.dispose();
		}
	}
});

/*
================
Default and native Video separation
================
*/
test("Default remains a draft until Confirm and native Video has no render scale row", () => {
	const f = fixture( experimentalOptions( { renderScale: 50, anisotropicFiltering: true } ) );
	try {
		f.activate( "open-window:Experimental" );
		assertScale( f.activate( "experimental-default" ), 100 );
		assert.equal( f.saved.length, 0 );
		f.activate( "experimental-cancel" );
		assertScale( f.activate( "open-window:Experimental" ), 50 );
		f.activate( "experimental-default" );
		f.activate( "experimental-confirm" );
		assert.deepEqual( f.saved, [ experimentalOptions() ] );
		f.activate( "open-window:Option" );
		const video = f.activate( "option-tab:0" );
		assert.equal( video.controls.some( control => control.label === "Render scale" ), false );
		const VIDEO_SCROLL_STEPS = 20;
		for ( let step = 0; step < VIDEO_SCROLL_STEPS; step++ ) {
			const shown = f.activate( "option-video-down" );
			assert.equal( shown.controls.some( control => control.label === "Render scale" ), false );
			assert.equal( shown.controls.some( control => control.id.startsWith( SCALE_PREFIX ) ), false );
		}
	} finally {
		f.dispose();
	}
});

/*
================
Layout
================
*/
test("the window keeps one height on every tab", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Experimental" );
		const heights = [ 0, 1, 2, 3, 4, 5 ].map( tab => {
			const view = f.activate( "experimental-tab:" + tab );
			const close = defined( view.controls.find( control => control.id === panelCloseId( "Experimental" ) ) );
			const confirm = defined( view.controls.find( control => control.id === "experimental-confirm" ) );
			return confirm.rect[1] - close.rect[1];
		} );
		// Buttons centred in the footer band (height - 37), close at 10: 488 - 47.
		assert.deepEqual( heights, [ 441, 441, 441, 441, 441, 441 ] );
	} finally {
		f.dispose();
	}
});

test("a whole row is the click target and scrolling back to another tab starts at the top", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Experimental" );
		const row = defined(
			f.activate( "experimental-tab:4" ).controls.find( control =>
				control.id === "experimental-job-kill-notices"
			)
		);
		assert.ok( row.rect[2] >= 300 && row.rect[3] >= 40, "label, description and box" );
		// Review Focus 2: a long tab scrolled to its end, then another tab.
		f.activate( "experimental-tab:0" );
		for ( let notch = 0; notch < 30; notch++ ) f.wheel( 1 );
		assert.ok( !f.wheel( 1 ).controls.some( control => control.id === "experimental-render-scale:100" ) );
		const notices = f.activate( "experimental-tab:4" );
		assert.ok( notices.controls.some( control => control.id === "experimental-job-kill-notices" ) );
		assert.ok(
			f.activate( "experimental-tab:0" ).controls.some( control =>
				control.id === "experimental-render-scale:100"
			)
		);
	} finally {
		f.dispose();
	}
});

test("the five filter kinds are dimmed while the filter is off and still toggle", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Experimental" );
		const box = view => {
			const row = defined( view.controls.find( control => control.id === "experimental-pet-filter-gold" ) );
			return defined(
				f.scenes.at( -1 ).quads.find( quad =>
					quad.texture.includes( "com_checkbutton_" ) && quad.rect[1] >= row.rect[1] &&
					quad.rect[1] < row.rect[1] + row.rect[3]
				)
			);
		};
		const pets = f.activate( "experimental-tab:5" );
		assert.ok( box( pets ).color[3] < 1, "dimmed while the filter is off" );
		const toggled = f.activate( "experimental-pet-filter-gold" );
		assert.equal(
			defined( toggled.controls.find( control => control.id === "experimental-pet-filter-gold" ) ).selected,
			false
		);
		const on = f.activate( "experimental-pet-pickup-filter" );
		assert.equal( box( on ).color[3], 1, "full once the filter is on" );
	} finally {
		f.dispose();
	}
});

/*
================
Scrolling
================
*/

/*
================
lastRow

The last Experimental row the view paints.
================
*/
function lastRow( view ) {
	return view.controls.filter( control => ROW_IDS.has( control.id ) ).at( -1 )?.id;
}

test("every wheel notch moves the open list by one row", () => {
	const f = fixture();
	try {
		const firstRow = view =>
			view.controls.find( control => ROW_IDS.has( control.id ) || control.id.startsWith( SCALE_PREFIX ) )?.id;
		let view = f.activate( "open-window:Experimental" );
		const seen = [ firstRow( view ) ];
		for (
			let notch = 0;
			notch < 40 && !view.controls.some( c => c.id === "experimental-dungeon-character-pvs" );
			notch++
		) {
			view = f.wheel( 1 );
			seen.push( firstRow( view ) );
		}
		// No notch leaves the first row where it was (no step at a header).
		for ( let i = 1; i < seen.length; i++ ) assert.notEqual( seen[i], seen[i - 1], "notch " + i );
		// A notch is one row whatever the wheel's delta.
		for ( let notch = 0; notch < seen.length; notch++ ) f.wheel( -40 );
		assert.ok( f.wheel( -1 ).controls.some( control => control.id === SCALE_PREFIX + "100" ) );
	} finally {
		f.dispose();
	}
});

test("the arrows scroll and stop at the ends", () => {
	const f = fixture();
	try {
		const top = f.activate( "open-window:Experimental" );
		assert.ok( top.controls.some( control => control.id === "experimental-scroll-up" ) );
		assert.ok(
			f.activate( "experimental-scroll-up" ).controls.some( control => control.id === SCALE_PREFIX + "100" )
		);
		let view = top;
		for ( let press = 0; press < 30; press++ ) view = f.activate( "experimental-scroll-down" );
		// The list ends on its last row, with nothing past it.
		assert.equal( lastRow( view ), "experimental-dungeon-character-pvs" );
	} finally {
		f.dispose();
	}
});

test("dragging the thumb to the bottom shows the last row", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Experimental" );
		assert.equal( lastRow( f.drag( "experimental-scroll-thumb", 1000 ) ), "experimental-dungeon-character-pvs" );
		assert.ok(
			f.drag( "experimental-scroll-thumb", -1000 ).controls.some( control => control.id === SCALE_PREFIX + "100" )
		);
	} finally {
		f.dispose();
	}
});

test("a drag that starts on a row scrolls and does not toggle it", () => {
	const f = fixture();
	try {
		const open = f.activate( "open-window:Experimental" );
		const before = defined( open.controls.find( control => control.id === "experimental-anisotropic-filtering" ) );
		// Review Focus 3: a finger moving up drags the list down.
		const after = f.drag( "experimental-anisotropic-filtering", -60 );
		assert.ok( !after.controls.some( control => control.id === SCALE_PREFIX + "100" ) );
		const row = defined( after.controls.find( control => control.id === "experimental-anisotropic-filtering" ) );
		assert.equal( row.selected, before.selected );
		assert.ok( row.rect[1] < before.rect[1] );
		assert.equal( f.saved.length, 0 );
	} finally {
		f.dispose();
	}
});

test("two rows share each line and a section header spans both columns", () => {
	const f = fixture();
	try {
		const view = f.activate( "open-window:Experimental" );
		const control = id => defined( view.controls.find( c => c.id === id ) );
		// Image's third line: MSAA 4x, then Anisotropic filtering.
		const left = control( "experimental-msaa" ), right = control( "experimental-anisotropic-filtering" );
		assert.equal( left.rect[1], right.rect[1] );
		assert.equal( right.rect[0] - left.rect[0], 312 + 8 );
		assert.equal( left.rect[2], 312 );
		// The header art in three slices: its 12-pixel chamfered ends at
		// their own size, the middle stretched across both columns.
		const slices = f.scenes.at( -1 ).quads.filter( quad => quad.texture.endsWith( "opt_video_quality_tab.png" ) )
			.slice( 0, 3 );
		assert.equal( slices.length, 3 );
		const [first, middle, last] = slices.map( quad => defined( quad ) );
		assert.deepEqual( [ first.rect[2], last.rect[2] ], [ 12, 12 ] );
		assert.equal( first.rect[0], left.rect[0] );
		assert.equal( last.rect[0] + last.rect[2] - first.rect[0], 312 * 2 + 8 );
		assert.equal( middle.rect[0], first.rect[0] + 12 );
		assert.ok( first.color[0] < 1, "darker than the art" );
		// The native 28-pixel bar, 6 pixels outside it before the first line.
		assert.equal( first.rect[3], 28 );
		const scale = control( "experimental-render-scale:100" );
		assert.equal( scale.rect[1] - (first.rect[1] + first.rect[3]), 6 );
	} finally {
		f.dispose();
	}
});

test("the window is wide enough for native tabs", () => {
	const f = fixture();
	try {
		const view = f.activate( "open-window:Experimental" );
		const tab = defined( view.controls.find( c => c.id === "experimental-tab:0" ) );
		const close = defined( view.controls.find( c => c.id === panelCloseId( "Experimental" ) ) );
		assert.equal( tab.rect[2], 76 );
		// 710 wide: tabs centred (122 in), close button 26 from the right.
		assert.equal( close.rect[0] - tab.rect[0], 710 - 26 - 122 );
		// Default, OK and Cancel centred on the window.
		const windowLeft = close.rect[0] + 26 - 710;
		const first = defined( view.controls.find( c => c.id === "experimental-default" ) );
		const last = defined( view.controls.find( c => c.id === "experimental-cancel" ) );
		assert.ok(
			Math.abs( (first.rect[0] - windowLeft) - (windowLeft + 710 - (last.rect[0] + last.rect[2])) ) <= 1
		);
	} finally {
		f.dispose();
	}
});

test("a cancelled drag on a row does not swallow the next row click", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Experimental" );
		const before = f.wheel( 1 );
		const id = "experimental-anisotropic-filtering";
		const row = defined( before.controls.find( control => control.id === id ) );
		// A touch drag that the browser cancels (pointercancel) ends in
		// drag-cancel, never drag-end; its travel must not carry over.
		f.ui.event( { kind: "drag", id, dx: 0, dy: 6 } );
		f.ui.event( { kind: "drag-cancel", id } );
		const after = f.drag( id, 3 );
		const moved = defined( after.controls.find( control => control.id === id ) );
		assert.equal( moved.selected, !row.selected );
	} finally {
		f.dispose();
	}
});

test("a click with a pixel of motion on a row still toggles it and keeps the list", () => {
	const f = fixture();
	try {
		f.activate( "open-window:Experimental" );
		const before = f.wheel( 1 );
		const row = defined( before.controls.find( control => control.id === "experimental-anisotropic-filtering" ) );
		// The platform turns a press that moved into drag + drag-end and
		// swallows the click; the window takes it back as the row's click.
		const after = f.drag( "experimental-anisotropic-filtering", 1 );
		const moved = defined( after.controls.find( control => control.id === "experimental-anisotropic-filtering" ) );
		assert.equal( moved.selected, !row.selected );
		assert.equal( moved.rect[1], row.rect[1] );
	} finally {
		f.dispose();
	}
});

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
	return { ...f, saved, activate, edit };
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
	// The row lives on the Other tab (#1806 gave its Image slot to Phone
	// resolution), and opening the window returns to Image.
	const OTHER_TAB = "experimental-tab:5";
	const open = () => {
		f.activate( "open-window:Experimental" );
		return f.activate( OTHER_TAB );
	};
	try {
		assert.equal( selected( open() ), false );
		assert.ok( f.hasText( "Safe loading screen" ) );
		assert.equal( selected( f.activate( "experimental-safe-loading-screen" ) ), true );
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

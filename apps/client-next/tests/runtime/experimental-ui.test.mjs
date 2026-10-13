/*
===========================================================================

experimental-ui.test.mjs - render scale through the production Experimental UI

Numeric choices share the existing draft lifecycle. Controls publish their
selection, Confirm persists it, and every cancellation path discards edits.

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
	return { ...f, saved, activate };
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

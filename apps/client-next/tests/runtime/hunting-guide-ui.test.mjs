/*
===========================================================================

hunting-guide-ui.test.mjs - shard capability and saved preference in the HUD

Exercises the production UI, including both map sizes and Experimental.
The optional reference is deliberately unavailable: controls are admitted by
the shard capability, while resource requests still require player opt-in.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { panelCloseId } = await import( "../../src/engine/foundation/ui/panels.ts" );

const GUIDE_PATH = "/transport/references/" + "a".repeat( 64 ) + ".json";
const SOURCE = { url: "https://fixture.invalid" + GUIDE_PATH, bytes: 100 };
const SETTLE_STEPS = 12, STEP_MS = 100;

/*
================
fixture
================
*/
function fixture( offered, enabled ) {
	const saved = [];
	const f = uiFixture( undefined, undefined, undefined, undefined, undefined, undefined, undefined, {
		saveExperimental: value => saved.push( value )
	} );
	let state = { ...f.state, session: { ...f.state.session, huntingGuide: offered ? SOURCE : undefined } };
	let now = 0, latest;
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { monsterGuide: enabled } ) } );
	/*
	================
	step
	================
	*/
	function step() {
		for ( let index = 0; index < SETTLE_STEPS; index++ ) latest = f.ui.step( state, now += STEP_MS ) ?? latest;
		return defined( latest );
	}
	/*
	================
	activate
	================
	*/
	function activate( id ) {
		f.ui.event( { kind: "activate", id } );
		return step();
	}
	step();
	return {
		...f,
		saved,
		activate,
		step,
		/*
		================
		offer
		================
		*/
		offer( available ) {
			state = {
				...state,
				session: {
					...state.session,
					huntingGuide: available ? SOURCE : undefined,
					revision: state.session.revision + 1
				}
			};
			return step();
		}
	};
}

/*
================
control
================
*/
function control( view, id ) {
	return view.controls.find( row => row.id === id );
}

test("disabled shards hide both controls even with a saved opt-in and make no guide request", () => {
	const f = fixture( false, true );
	try {
		assert.equal( control( f.activate( "open-window:Map" ), "map-hunting-toggle" ), undefined );
		assert.equal( control( f.activate( "map-size" ), "map-hunting-toggle" ), undefined );
		f.activate( "map-hunting-toggle" );
		assert.equal( f.saved.length, 0, "a stale control cannot change the saved preference" );
		f.activate( panelCloseId( "Map" ) );
		f.activate( "open-window:Experimental" );
		assert.equal( control( f.activate( "experimental-tab:1" ), "experimental-monster-guide" ), undefined );
		f.activate( "experimental-monster-guide" );
		f.activate( "experimental-confirm" );
		assert.equal( f.saved.at( -1 ).monsterGuide, true, "hiding the row preserves the saved preference" );
		assert.equal( f.requested.includes( GUIDE_PATH ), false );
	} finally {
		f.dispose();
	}
});

test("enabled shards expose both map sizes and Experimental but fetch only after opt-in", () => {
	const f = fixture( true, false );
	try {
		assert.equal( defined( control( f.activate( "open-window:Map" ), "map-hunting-toggle" ) ).selected, false );
		assert.equal( defined( control( f.activate( "map-size" ), "map-hunting-toggle" ) ).selected, false );
		assert.equal( f.requested.includes( GUIDE_PATH ), false );
		assert.equal( defined( control( f.activate( "map-hunting-toggle" ), "map-hunting-toggle" ) ).selected, true );
		assert.equal( f.saved.at( -1 ).monsterGuide, true );
		assert.equal( f.requested.includes( GUIDE_PATH ), true );
		assert.equal( control( f.offer( false ), "map-hunting-toggle" ), undefined );
		assert.equal( defined( control( f.offer( true ), "map-hunting-toggle" ) ).selected, true );
		assert.equal( control( f.activate( "map-town:1" ), "map-hunting-toggle" ), undefined );
		assert.equal( defined( control( f.activate( "map-world" ), "map-hunting-toggle" ) ).selected, true );
		f.activate( panelCloseId( "Map" ) );
		f.activate( "open-window:Experimental" );
		assert.equal(
			defined( control( f.activate( "experimental-tab:1" ), "experimental-monster-guide" ) ).selected,
			true
		);
	} finally {
		f.dispose();
	}
});

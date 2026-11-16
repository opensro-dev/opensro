/*
===========================================================================

ui-semantics-presentation.test.mjs - the HUD presents semantics only when they change

ui.step publishes quads whenever they differ, and returns its semantics
(the DOM controls the platform bridge reconciles) only when they differ
from the last ones it returned, or on a first product, a reset or a new
frame size. A walking crowd moves minimap dots on every frame without
changing a control, so those frames publish without presenting again.
A twin HUD fed the same views is made to return its semantics on every
step (an edit forgets the last product): what the bridge receives from
the HUD under test must be exactly that sequence with repeats removed.
While a loading state shows, every publication still presents: the
platform's loading detail (clock and transfer counts) refreshes on each.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );
const { sameUiSemantics, uiPublication } = await import( "../../src/engine/foundation/ui/ui-equality.ts" );

const WALKERS = 6, WALK_STEP = 2, FRAME_MS = 16;

/*
================
crowdFixture

The production HUD, settled, with walkers on the minimap. It has no
progression yet: the idle window warm starts when one arrives.
================
*/
function crowdFixture() {
	const fx = uiFixture();
	const local = fx.state.entities[0];
	fx.state.entities = [
		local,
		...Array.from( { length: WALKERS }, ( _, i ) => ({
			...local,
			gid: 100 + i,
			name: `Walker${i}`,
			x: 30 + i * 12,
			z: 20 - i * 9
		}) )
	];
	let now = 0, settled = null;
	for ( let i = 0; i < 40; i++ ) settled = fx.ui.step( fx.state, now += 100 ) ?? settled;
	assert.ok( settled, "the settled HUD presented its semantics" );
	return { fx, settled };
}
/*
================
walk

The next view: every walker one step along x, a new entities array as
presentation builds for a pose batch.
================
*/
function walk( view ) {
	return {
		...view,
		entities: view.entities.map( entity =>
			entity.gid === view.gameplay.localGid ?
				entity :
				{ ...entity, x: entity.x + WALK_STEP }
		)
	};
}
/*
================
forgetLastProduct

An edit of no control: the HUD forgets its last product, so its next step
returns semantics whatever they are (how every publication presented
before this change).
================
*/
function forgetLastProduct( fx ) {
	fx.ui.event( { kind: "edit", id: "test-no-control", value: "", start: 0, end: 0, composing: false } );
}

test("a first product returns its semantics", () => {
	const fx = uiFixture();
	try {
		assert.ok( fx.rawStep( fx.state, 100 ), "the first published product presents its semantics" );
	} finally {
		fx.dispose();
	}
});

test("walkers republish quads without presenting; every semantic change presents on its step", () => {
	const { fx, settled } = crowdFixture(), { fx: twin } = crowdFixture();
	try {
		// Before this change every publication presented: the reference is
		// the twin's semantics on each frame the HUD under test published.
		const presented = [ settled ], reference = [ settled ];
		/** @type {any} */
		let view = fx.state;
		let now = 10000;
		/*
		================
		step

		One frame on both HUDs. Returns what the HUD under test returned and
		whether it published.
		================
		*/
		const step = () => {
			now += FRAME_MS;
			const before = fx.products.length;
			const result = fx.rawStep( view, now );
			forgetLastProduct( twin );
			const expected = twin.rawStep( view, now );
			const published = fx.products.length > before;
			if ( result ) {
				assert.ok( published, "semantics are presented only with a publication" );
				assert.deepEqual( result, expected, "the twin computes the same semantics" );
				presented.push( result );
			}
			if ( published ) {
				assert.ok( expected, "the twin returns semantics on every step" );
				reference.push( expected );
			}
			return { result, published };
		};
		// A progression starts the window warm: its unseen window builds run
		// among the walk frames, compute semantics and present none of them.
		view = { ...view, gameplay: { ...view.gameplay, progression: { level: 10, masteries: [] } } };
		step();
		let walked = 0;
		for ( let i = 0; i < 40; i++ ) {
			view = walk( view );
			const { result, published } = step();
			if ( published ) walked++;
			assert.equal( result, null, `walk frame ${i} presented semantics` );
		}
		assert.ok( walked >= 20, `walkers moved minimap dots on ${walked} of 40 frames` );
		// Still frames: nothing to publish, nothing to present.
		for ( let i = 0; i < 3; i++ ) assert.equal( step().result, null );
		// Each kind of semantic change presents on the step it happens.
		view = {
			...view,
			gameplay: {
				...view.gameplay,
				vitals: [ { gid: view.gameplay.localGid, hp: 50, mp: 50, maxHp: 100, maxMp: 100 } ]
			}
		};
		assert.match( step().result?.message ?? "", /Health 50/, "a message change presents" );
		fx.ui.event( { kind: "key", code: "KeyI" } );
		twin.ui.event( { kind: "key", code: "KeyI" } );
		assert.ok(
			step().result?.controls.some( control => control.id === "main-popup-drag" ),
			"a control change presents"
		);
		view = walk( view );
		assert.equal( step().result, null, "a walk under an open window presents nothing" );
		view = { ...view, width: view.width - 40 };
		assert.ok( step().result, "a new frame size presents" );
		forgetLastProduct( fx );
		assert.ok( step().result, "a reset presents" );
		view = walk( view );
		assert.equal( step().result, null );
		// What the bridge received is today's every-publication sequence with
		// repeats removed: no change lost, no change presented twice.
		const changes = reference.filter( ( semantics, i ) =>
			i === 0 || !sameUiSemantics( reference[i - 1], semantics )
		);
		assert.deepEqual(
			presented.filter( ( semantics, i ) => i === 0 || !sameUiSemantics( presented[i - 1], semantics ) ),
			changes
		);
	} finally {
		fx.dispose();
		twin.dispose();
	}
});

/*
================
loadingProduct

A loading-screen product whose quads alone differ from frame to frame:
the gauge's quad moves while the semantics stay equal.
================
*/
function loadingProduct( gauge, loading ) {
	/** @type {import("../../src/engine/foundation/ui/ui-equality").UiProduct} */
	const product = {
		width: 1600,
		height: 900,
		damageText: false,
		quads: [ {
			texture: "gauge",
			rect: [ 10, 10, gauge, 4 ],
			clip: [ 0, 0, 1600, 900 ],
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		} ],
		semantics: { title: "Silkroad game interface", message: "", controls: [], ...loading }
	};
	return product;
}

test("while a loading state shows, every publication presents, as before present-on-change", () => {
	for (
		const loading of [ { loadingVisible: true, loadingStatus: "Loading world assets" }, { loading: true }, {
			loadingError: "failed"
		} ]
	) {
		let last = null;
		for ( let gauge = 100; gauge < 104; gauge++ ) {
			const next = loadingProduct( gauge, loading );
			assert.equal( uiPublication( last, next ), "present", JSON.stringify( loading ) + " " + gauge );
			last = next;
		}
		// No publication, as before: nothing to refresh the detail with.
		assert.equal( uiPublication( last, loadingProduct( 103, loading ) ), "unchanged" );
	}
	// Out of loading, the same quads-only change publishes without presenting.
	assert.equal( uiPublication( loadingProduct( 100, {} ), loadingProduct( 101, {} ) ), "quads" );
});

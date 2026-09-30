/*
===========================================================================

empty-roster.test.mjs - first-login character catalog admission

An empty roster still owns the dock. Its catalog must load before the
frontend can reveal the button that enters character creation.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterPresentation } = await import( "../../src/engine/runtime/characters/characters.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

/*
================
fixture

Hold the catalog response explicitly so readiness cannot pass merely because
there are no actors to assemble.
================
*/
function fixture() {
	const requests = [];
	let pending = false, available = false, nextId = 0;
	const assets = {
		/*
		================
		available
		Keep one request slot available for catalog admission.
		================
		*/
		available: () => 1,
		/*
		================
		request
		Record demand separately from completion.
		================
		*/
		request( url ) {
			requests.push( url );
			pending = true;
			return ++nextId;
		},
		/*
		================
		take
		Release the catalog only after the test opens the response boundary.
		================
		*/
		take() {
			if ( !pending || !available ) return null;
			pending = false;
			return { kind: "bytes", buffer: new TextEncoder().encode( '{"models":[]}' ).buffer };
		},
		/*
		================
		cancel
		Discard an outstanding request on lifecycle reset.
		================
		*/
		cancel() {
			pending = false;
		}
	};
	// Empty-roster admission only publishes empty actors and retained sources.
	// The omitted rendering operations are deliberately unavailable to this fixture.
	const renderer = { setCharacterActors: ignore, retainCharacterModels: ignore };
	const owner = createCharacterPresentation(
		/** @type {Parameters<typeof createCharacterPresentation>[0]} */ (/** @type {unknown} */ (assets)),
		/** @type {Parameters<typeof createCharacterPresentation>[1]} */ (/** @type {unknown} */ (renderer)),
		"http://localhost",
		ignore,
		createPresentationRandom( 1 )
	);
	return {
		owner,
		requests,
		/*
		================
		step
		An empty array denotes an active dock without any player characters.
		================
		*/
		step: () => owner.step( [], null, 0, undefined, 0, [] ),
		/*
		================
		release
		Allow the already requested catalog to complete.
		================
		*/
		release: () => {
			available = true;
		}
	};
}

/*
================
ignore

Empty actors and sound output have no resources in this admission fixture.
================
*/
function ignore() {}

test("cold empty roster requests its catalog and becomes ready after admission", () => {
	const f = fixture();
	try {
		f.step();
		assert.deepEqual( f.requests, [ "http://localhost/assets/char/roster.json" ] );
		assert.equal( f.owner.dockReady(), false );
		f.release();
		f.step();
		assert.equal( f.owner.error(), null );
		assert.equal( f.owner.dockReady(), true );
	} finally {
		f.owner.dispose();
	}
});

test("reset during cold admission requests the cancelled catalog again", () => {
	const f = fixture();
	try {
		f.step();
		assert.equal( f.owner.dockReady(), false );
		f.owner.reset();
		f.step();
		assert.equal( f.requests.length, 2 );
		assert.equal( f.owner.dockReady(), false );
		f.release();
		f.step();
		assert.equal( f.owner.dockReady(), true );
	} finally {
		f.owner.dispose();
	}
});

test("reset followed by an empty roster reuses the admitted catalog", () => {
	const f = fixture();
	try {
		f.release();
		f.step();
		f.step();
		assert.equal( f.owner.dockReady(), true );
		f.owner.reset();
		assert.equal( f.owner.dockReady(), false );
		f.step();
		f.step();
		assert.equal( f.requests.length, 1 );
		assert.equal( f.owner.dockReady(), true );
	} finally {
		f.owner.dispose();
	}
});

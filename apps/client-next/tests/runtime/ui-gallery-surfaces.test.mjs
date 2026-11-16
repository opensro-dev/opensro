/*
===========================================================================

ui-gallery-surfaces.test.mjs - every gallery surface entry draws its window

The UI gallery opens a "surface:<panel>" entry through the shared recipes
(tools/ui-gallery/entry-recipes.mjs OPENERS, then SERVICE_WINDOWS), and
falls back to the panel's window id. The panel sweep keeps its own opener
table, so a recipe missing from the shared one went unseen: the Fortress
map entry reported Ready and drew nothing (#3676), because the map opens
only on its aide's structure-list answer and the HUD shut it at once. This
runs the gallery's own lookup on every registered panel and requires the
window to publish controls the settled HUD did not have.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { uiPanels } = await import( "../../src/engine/foundation/ui/panels.ts" );
const { OPENERS, SERVICE_WINDOWS } = await import( "../../tools/ui-gallery/entry-recipes.mjs" );

// The settle step the panel sweep uses: one step to the same endpoint.
const STEP_MS = 1600;

for ( const panel of uiPanels() ) {
	test(`the gallery's ${panel} surface opens its window`, async t => {
		const f = uiFixture();
		t.after( () => f.dispose() );
		let now = 0;
		const seen = {
			/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
		};
		const step = () => {
			seen.presentation = f.ui.step( f.state, now += STEP_MS ) ?? seen.presentation;
		};
		step();
		const before = new Set( seen.presentation?.controls.map( control => control.id ) ?? [] );
		// The gallery's order (frame.ts openEntry): a recipe, else the window id.
		const recipe = OPENERS[panel] ?? SERVICE_WINDOWS[panel];
		if ( recipe ) await recipe( f, step );
		else f.ui.event( { kind: "activate", id: "open-window:" + panel } );
		step();
		assert.ok(
			seen.presentation?.controls.some( control => !before.has( control.id ) ),
			`the ${panel} surface drew no window`
		);
	});
}

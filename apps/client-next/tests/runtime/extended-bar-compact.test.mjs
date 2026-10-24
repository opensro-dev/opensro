/*
===========================================================================

extended-bar-compact.test.mjs - the extra skill bar drags on compact

On a phone the extended bar used to sit pinned centre-top: its handle lost
draggable to the compact window rule, the renderer never read extPosition,
and the fit held the group at the top of its band. The handle drags now,
the compact renderer and drag share extendedCompactOrigin, and the fit
band follows the stored position while still holding the bar on screen.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
draw

The ext-drag control of the latest published scene; an event that changes
nothing publishes nothing, so the previous scene stands.
================
*/
function draw( f, last, t ) {
	const scene = f.ui.step( f.state, t ) ?? last.scene;
	const control = scene?.controls.find( c => c.id === "ext-drag" );
	assert.ok( control, "the extended bar handle is admitted" );
	return { scene, control };
}

test("the extended bar follows its drag on compact and stays on screen", () => {
	const f = uiFixture( () => undefined );
	try {
		f.state.width = 609;
		f.state.height = 327;
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "activate", id: "compact-extra" } );
		let last = { scene: undefined }, bar = draw( f, last, 1300 );
		last.scene = bar.scene;
		assert.equal( bar.control.draggable, true, "an unlocked bar may move" );
		const before = bar.control.rect;
		f.ui.event( { kind: "drag", id: "ext-drag", dx: 37, dy: 21 } );
		f.ui.event( { kind: "drag-end", id: "ext-drag", x: 0, y: 0 } );
		bar = draw( f, last, 1400 );
		last.scene = bar.scene;
		assert.equal( bar.control.rect[0], before[0] + 37, "the bar follows the drag sideways" );
		assert.equal( bar.control.rect[1], before[1] + 21, "the bar follows the drag downward" );
		f.state.width = 480;
		f.state.height = 320;
		bar = draw( f, last, 1500 );
		last.scene = bar.scene;
		assert.ok(
			bar.control.rect[0] >= 0 && bar.control.rect[1] >= 0 &&
				bar.control.rect[0] + bar.control.rect[2] <= 480 &&
				bar.control.rect[1] + bar.control.rect[3] <= 320,
			"rotation keeps the whole header inside the smaller viewport"
		);
		f.ui.event( {
			kind: "quickslot-preferences",
			value: {
				open: true,
				vertical: true,
				double: true,
				transparent: true,
				slotLock: false,
				positionLock: true,
				position: null
			}
		} );
		bar = draw( f, last, 1600 );
		last.scene = bar.scene;
		assert.equal( bar.control.draggable, false, "a locked bar does not offer its handle" );
		const locked = bar.control.rect;
		f.ui.event( { kind: "drag", id: "ext-drag", dx: 11, dy: 13 } );
		bar = draw( f, last, 1700 );
		assert.deepEqual( bar.control.rect, locked, "a locked bar stays where it is" );
	} finally {
		f.dispose();
	}
});

/*
===========================================================================

character-label-height.test.mjs - name boards stand at the native height

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { characterLabelHeight, projectCharacterLabels } = await import(
	"../../src/engine/foundation/ui/character-labels.ts"
);

/*
================
published height
================
*/
test("a character's board stands at its published height times its scale, plus 2", () => {
	// A wolf pet: characterInfo 1.0 x 0.5 = 0.5, x 20 = 10. Its low rest
	// pose (bind top 3) no longer decides where the name goes.
	assert.equal( characterLabelHeight( { height: 10, scale: 1 }, 3, false ), 12 );
	assert.equal( characterLabelHeight( { height: 20, scale: 1.5 }, 40, false ), 32 );
});

/*
================
ride and ground item
================
*/
test("a rider's board uses the ride's height plus 7; a ground item stands at 5", () => {
	assert.equal( characterLabelHeight( { height: 25, scale: 1 }, 30, true ), 32 );
	assert.equal( characterLabelHeight( { height: 25, scale: 1, groundItem: true }, 30, false ), 5 );
});

/*
================
no published height
================
*/
test("an actor without a published height falls back to its bind top", () => {
	assert.equal( characterLabelHeight( { scale: 2 }, 8, false ), 18 );
});

/*
================
back-to-front label projection
================
*/
test("projectCharacterLabels sorts character boards back-to-front while keeping each character's quad order", () => {
	const baseQuad = {
		rect: /** @type {const} */ ([ 0, 0, 20, 10 ]),
		uv: /** @type {const} */ ([ 0, 0, 1, 1 ]),
		color: /** @type {const} */ ([ 1, 1, 1, 1 ]),
		clip: /** @type {const} */ ([ 0, 0, 800, 600 ])
	};
	const scene = {
		revision: 1,
		width: 800,
		height: 600,
		quads: [
			{ ...baseQuad, texture: "near-plate", characterAnchor: 1 },
			{ ...baseQuad, texture: "near-name", characterAnchor: 1 },
			{ ...baseQuad, texture: "far-plate", characterAnchor: 2 },
			{ ...baseQuad, texture: "far-name", characterAnchor: 2 },
			{ ...baseQuad, texture: "hud-window" }
		]
	};
	const projected = projectCharacterLabels(
		scene,
		new Map( [
			[ 1, [ 100, 100, 0.35 ] ],
			[ 2, [ 100, 100, 0.75 ] ]
		] )
	);
	assert.deepEqual(
		projected.quads.map( q => [ q.texture, q.depth ] ),
		[
			[ "far-plate", 0.75 ],
			[ "far-name", 0.75 ],
			[ "near-plate", 0.35 ],
			[ "near-name", 0.35 ],
			[ "hud-window", undefined ]
		]
	);
});

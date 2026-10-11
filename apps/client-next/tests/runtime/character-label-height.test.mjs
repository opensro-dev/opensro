/*
===========================================================================

character-label-height.test.mjs - name boards stand at the native height

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { characterLabelHeight } = await import( "../../src/engine/foundation/ui/character-labels.ts" );

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

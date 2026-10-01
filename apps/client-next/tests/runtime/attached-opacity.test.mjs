/*
===========================================================================

attached-opacity.test.mjs - a character's fade reaches its model, not effects

Native fades set alpha on the owner's CSkeletonModel only. Hair and
equipment fade with it; skill effects keep their own blend unless the owner
is fully hidden.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { attachedOpacity } = await import( "../../src/engine/foundation/animation/character-fade.ts" );

test("model parts fade with the owner", () => {
	assert.equal( attachedOpacity( 1, 0.5, true ), 0.5 );
	assert.equal( attachedOpacity( 0.5, 0.5, true ), 0.25 );
	assert.equal( attachedOpacity( 1, 0, true ), 0 );
});

test("effects keep their own opacity through a partial owner fade", () => {
	// The camera fade, body state 4 (0x50) and party concealment are partial.
	for ( const owner of [ 0.99, 0.5, 0x50 / 255, 1 / 255 ] ) {
		assert.equal( attachedOpacity( 1, owner, false ), 1 );
		assert.equal( attachedOpacity( 0.25, owner, false ), 0.25 );
	}
});

test("effects hide with a fully hidden owner", () => {
	assert.equal( attachedOpacity( 1, 0, false ), 0 );
});

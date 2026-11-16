/*
===========================================================================

picture-clip.test.mjs - a portrait is drawn through its round face disc

GDR_PMI_PICTURE and the party, pet and Item Mall pictures are
CIFStaticWithPictureClip controls. Their draw (538600) takes alpha from
the face disc each window sets at +0x380 (pmi_face for the player), so a
rendered head shot shows round inside its slot, never as its square.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const PLAYER_FACE = "/assets/images/Media_extracted/interface/playerminiinfo/pmi_face.png";

test("the player mini info portrait is masked by pmi_face over its own rect", () => {
	const f = uiFixture();
	let clock = 0;
	for ( let i = 0; i < 4; i++ ) f.ui.step( f.state, clock += 100 );
	assert.equal( f.ui.stats().error, null );
	const portraits = f.scenes.at( -1 ).quads.filter( q => q.portraitGid === f.state.gameplay.localGid );
	assert.ok( portraits.length > 0, "the HUD draws the local portrait" );
	for ( const quad of portraits ) {
		assert.equal( quad.mask?.texture, PLAYER_FACE );
		assert.deepEqual( quad.mask?.rect, quad.rect );
	}
});

/*
===========================================================================

pet-mini-portrait.test.mjs - the attack pet's mini window shows its portrait

CIFPetMiniInfo backs its picture with pmi_pet_face, an opaque black disc,
and draws the pet's icon over it. Drawn over the icon, the disc turned a
summoned wolf's picture black.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

// COS_P_WOLF_001 (characterdata 6106): icon cos\cos_p_wolf_01.ddj.
const WOLF = 6106;

test("a summoned wolf's mini window draws its icon over the face disc", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.cosRecords = [ {
			gid: 7,
			refObjId: WOLF,
			band: 3,
			hp: 300,
			mp: 0,
			status: 0,
			dead: false,
			level: 1,
			satiety: 10000,
			name: "Fang"
		} ];
		for ( let i = 0; i < 60; i++ ) f.ui.step( f.state, 1000 + i * 50 );
		const quads = [ ...f.scenes ].reverse().find( scene => scene?.quads )?.quads ?? [];
		const face = quads.findIndex( q => q.texture.endsWith( "/playerminiinfo/pmi_pet_face.png" ) );
		const icon = quads.findIndex( q => q.texture.endsWith( "/icon/cos/cos_p_wolf_01.png" ) );
		assert.ok( face >= 0, "the face disc is drawn" );
		assert.ok( icon >= 0, "the wolf's icon is drawn" );
		assert.ok( icon > face, "the icon sits over the opaque face disc, not under it" );
	} finally {
		f.dispose();
	}
});

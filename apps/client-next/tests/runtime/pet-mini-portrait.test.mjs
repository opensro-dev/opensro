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
		// The portrait assertion runs with the deviation explicitly off
		// (BUG-086 defaults it on): natively CIFPetMiniInfo has no
		// click-to-open button.
		f.ui.event(
			/** @type {any} */ ({
				kind: "experimental-preferences",
				value: { petClickOpensWindow: false }
			})
		);
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
		let semantics = null;
		for ( let i = 0; i < 60; i++ ) semantics = f.ui.step( f.state, 1000 + i * 50 ) ?? semantics;
		const quads = [ ...f.scenes ].reverse().find( scene => scene?.quads )?.quads ?? [];
		const face = quads.findIndex( q => q.texture.endsWith( "/playerminiinfo/pmi_pet_face.png" ) );
		const icon = quads.findIndex( q => q.texture.endsWith( "/icon/cos/cos_p_wolf_01.png" ) );
		assert.ok( face >= 0, "the face disc is drawn" );
		assert.ok( icon >= 0, "the wolf's icon is drawn" );
		assert.ok( icon > face, "the icon sits over the opaque face disc, not under it" );
		assert.equal(
			semantics?.controls.some( c => c.id === "pet-mini-open" ),
			false,
			"natively CIFPetMiniInfo has no click-to-open button"
		);
	} finally {
		f.dispose();
	}
});

test("enabling Pet panel opens window lets CIFPetMiniInfo toggle the companion window", () => {
	const commands = [];
	const f = uiFixture( c => commands.push( c ) );
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
		f.ui.event(
			/** @type {any} */ ({
				kind: "experimental-preferences",
				value: { petClickOpensWindow: true }
			})
		);
		let before = null;
		for ( let i = 0; i < 60; i++ ) before = f.ui.step( f.state, 1000 + i * 50 ) ?? before;
		const button = before?.controls.find( c => c.id === "pet-mini-open" );
		assert.ok( button, "the opt-in button covers CIFPetMiniInfo" );
		assert.equal( button.label, "Fang" );

		f.ui.event( { kind: "activate", id: "pet-mini-open" } );
		let opened = null;
		for ( let i = 0; i < 60; i++ ) opened = f.ui.step( f.state, 4000 + i * 50 ) ?? opened;
		assert.ok(
			commands.some( c => c.kind === "gameplay" && c.command.kind === "cos-select" && c.command.gid === 7 ),
			"clicking the attack pet panel selects the companion"
		);
		assert.ok(
			opened?.controls.some( c => c.id === "cos-tab:0" ),
			"clicking the attack pet panel opens CIFCos"
		);

		f.ui.event( { kind: "activate", id: "pet-mini-open" } );
		let closed = null;
		for ( let i = 0; i < 10; i++ ) closed = f.ui.step( f.state, 7000 + i * 50 ) ?? closed;
		assert.equal(
			closed?.controls.some( c => c.id === "cos-tab:0" ),
			false,
			"clicking again closes CIFCos"
		);
	} finally {
		f.dispose();
	}
});

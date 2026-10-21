/*
===========================================================================

monsterPortraitAssets.test.mjs - reference aliases and ordinary native materials

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	monsterPortraitReferences,
	monsterPortraitRendererFingerprint
} from "../../build/char/buildMonsterPortraitAssets.mjs";

test("native reference joins preserve aliases, material variants and missing models", () => {
	const manifest = {
		models: {
			clone: { kind: "monster", refObjId: 2, bsr: "wolf", materialKind: 3 },
			base: { kind: "monster", refObjId: 1, bsr: "wolf", materialKind: 0 },
			alias: { kind: "monster", refObjId: 3, bsr: "wolf", materialKind: 0 },
			unbuilt: { kind: "monster", refObjId: 4, bsr: "missing" },
			merchant: { kind: "npc", refObjId: 5, bsr: "wolf" },
			unique: { kind: "monster", refObjId: 6, bsr: "wolf", monsterType: 3 },
			quest: { kind: "monster", refObjId: 7, bsr: "wolf", typeId4: 4 }
		},
		resources: {
			wolf: { glb: "/assets/npc/wolf.glb", materialVariants: { "3": "/assets/npc/wolf.material-3.glb" } }
		}
	};
	assert.deepEqual( monsterPortraitReferences( manifest ), [
		{ refObjId: 1, glb: "/assets/npc/wolf.glb" },
		{ refObjId: 2, glb: "/assets/npc/wolf.material-3.glb" },
		{ refObjId: 3, glb: "/assets/npc/wolf.glb" },
		{ refObjId: 4, glb: undefined },
		{ refObjId: 6, glb: "/assets/npc/wolf.glb" },
		{ refObjId: 7, glb: "/assets/npc/wolf.glb" }
	] );
});

test("effect-only program and texture changes invalidate a monster still's cache fingerprint", async () => {
	const manifest = {
		models: { yeoha: { kind: "monster", refObjId: 1943, bsr: "yeoha" } },
		resources: { yeoha: { glb: "/assets/npc/yeoha.glb" } }
	};
	const texture = "/assets/images/Particles_extracted/textures/luster.png";
	const programs = {
		effects: { "monster/luster_green.efp": { scale: 1 } },
		textures: { "textures/luster.png": texture }
	};
	let textureBytes = Buffer.from( "original texture" );
	/*
	================
	readAsset

	All model and presentation bytes stay fixed while only effect inputs change.
	================
	*/
	const readAsset = async file => {
		if ( file === "/assets/effects/programs.json" ) return Buffer.from( JSON.stringify( programs ) );
		if ( file === texture ) return textureBytes;
		return Buffer.from( "unchanged model and presentation" );
	};
	const original = await monsterPortraitRendererFingerprint( manifest, readAsset );
	programs.effects["monster/luster_green.efp"].scale = 2;
	assert.notEqual( await monsterPortraitRendererFingerprint( manifest, readAsset ), original );
	programs.effects["monster/luster_green.efp"].scale = 1;
	textureBytes = Buffer.from( "changed texture" );
	assert.notEqual( await monsterPortraitRendererFingerprint( manifest, readAsset ), original );
	textureBytes = Buffer.from( "original texture" );
	assert.equal( await monsterPortraitRendererFingerprint( manifest, readAsset ), original );
});

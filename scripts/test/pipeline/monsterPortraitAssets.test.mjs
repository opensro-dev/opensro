/*
===========================================================================

monsterPortraitAssets.test.mjs - reference aliases and ordinary native materials

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { monsterPortraitReferences } from "../../build/char/buildMonsterPortraitAssets.mjs";

test("native reference joins preserve aliases, material variants and missing models", () => {
	const manifest = {
		models: {
			clone: { kind: "monster", refObjId: 2, bsr: "wolf", materialKind: 3 },
			base: { kind: "monster", refObjId: 1, bsr: "wolf", materialKind: 0 },
			alias: { kind: "monster", refObjId: 3, bsr: "wolf", materialKind: 0 },
			unbuilt: { kind: "monster", refObjId: 4, bsr: "missing" },
			merchant: { kind: "npc", refObjId: 5, bsr: "wolf" }
		},
		resources: {
			wolf: { glb: "/assets/npc/wolf.glb", materialVariants: { "3": "/assets/npc/wolf.material-3.glb" } }
		}
	};
	assert.deepEqual( monsterPortraitReferences( manifest ), [
		{ refObjId: 1, glb: "/assets/npc/wolf.glb" },
		{ refObjId: 2, glb: "/assets/npc/wolf.material-3.glb" },
		{ refObjId: 3, glb: "/assets/npc/wolf.glb" },
		{ refObjId: 4, glb: undefined }
	] );
});

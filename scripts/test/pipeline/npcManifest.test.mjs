/*
===========================================================================

npcManifest.test.mjs - the v9 NPC manifest keeps one bake per BSR

The bake splits its joined entries into slim reference rows and one
resource per BSR; every reader joins them back. The round trip must be
exact, a v8 manifest must read unchanged, and two references of one BSR
with different bake results must refuse to publish.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";

import { joinedNpcManifest, npcManifestModels, splitNpcManifestModels } from "../../build/shared/npcManifest.mjs";

const TIGER = {
	glb: "/assets/npc/mob/china/tiger.glb",
	clips: [ "stand", "walk" ],
	animationStates: { stand: { stateId: 0 } },
	vat: { manifest: "/assets/npc/vat/mob/china/tiger.vat.json" }
};

/*
================
entry
================
*/
function entry( codename, refObjId, scalePercent ) {
	return {
		codename,
		refObjId,
		kind: "monster",
		bsr: "res/mob/china/tiger.bsr",
		scalePercent,
		soundProfileName: "MOB_CH_TIGER",
		...structuredClone( TIGER )
	};
}

test("a split manifest joins back to the bake's entries", () => {
	const entries = [ entry( "MOB_CH_TIGER", 1, 100 ), entry( "MOB_CH_TIGER_CLON", 2, 80 ) ];
	const { models, resources } = splitNpcManifestModels( entries );
	assert.deepEqual( Object.keys( resources ), [ "res/mob/china/tiger.bsr" ] );
	assert.deepEqual( resources["res/mob/china/tiger.bsr"], TIGER );
	assert.equal( models.MOB_CH_TIGER_CLON.glb, undefined, "a reference row repeats its resource" );
	assert.equal( models.MOB_CH_TIGER_CLON.scalePercent, 80 );
	const joined = npcManifestModels( { models, resources } );
	assert.deepEqual( joined, Object.fromEntries( entries.map( row => [ row.codename, row ] ) ) );
});

test("a v8 manifest reads unchanged and keeps its top-level fields", () => {
	const v8 = { version: 8, vat: { version: 1 }, models: { MOB_CH_TIGER: entry( "MOB_CH_TIGER", 1, 100 ) } };
	assert.deepEqual( joinedNpcManifest( v8 ), v8 );
});

test("references of one BSR with different bake results refuse to publish", () => {
	const drifted = entry( "MOB_CH_TIGER_CLON", 2, 80 );
	drifted.clips = [ "stand" ];
	assert.throws(
		() => splitNpcManifestModels( [ entry( "MOB_CH_TIGER", 1, 100 ), drifted ] ),
		/different bake results/
	);
});

test("a failed bake keeps its row and publishes no resource", () => {
	const failed = { codename: "MOB_BROKEN", refObjId: 3, kind: "monster", bsr: "res/mob/broken.bsr", error: "x" };
	const { models, resources } = splitNpcManifestModels( [ failed ] );
	assert.deepEqual( models.MOB_BROKEN, failed );
	assert.deepEqual( resources, {} );
});

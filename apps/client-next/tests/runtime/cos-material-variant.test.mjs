/*
===========================================================================

cos-material-variant.test.mjs - a companion's authored material variant

#4695: the Spotted Rabbit (COS_P_SPOT_RABBIT) rendered as the plain Rabbit.
Its record authors material kind 1 (characterdata column 109) over the
rabbit body, and the rabbit's published resource carries the slot-1
material variant, but the appearance lookup applied authored variants to
monsters only. A companion takes its record's authored slot the same way:
natively both spawns load the mesh through CICharactor_LoadVisualMeshFromBSR
(853E70; CICCos_DeserializeSpawnPacket 8554E0, CICMonster 861B00).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createAppearanceLookup } = await import( "../../src/engine/runtime/characters/appearance-lookup.ts" );
const { clipFacts } = await import( "../../src/engine/runtime/characters/presentation-catalog.ts" );

const BASE = "/assets/npc/cos/p_rabbit.glb";
const SPOTTED = "/assets/npc/cos/p_rabbit.material-1.glb";

function lookupWith( resource ) {
	return createAppearanceLookup( {
		referenceAppearances: { skin: () => undefined, get: () => undefined },
		published: {
			catalog: new Map( [ [ resource.refObjId, resource ] ] ),
			deathModels: new Map(),
			tradeSkinPools: { china: [], europe: [] },
			tradeBandits: new Map(),
			itemIds: new Map(),
			dress: {}
		},
		structureVisuals: { appearance: () => undefined },
		deathShown: () => false
	} );
}

function body( materialKind, materialVariants ) {
	return {
		refObjId: 17809,
		codename: "COS_P_SPOT_RABBIT",
		glb: BASE,
		clips: [ "stand" ],
		clipFacts: clipFacts( [ "stand" ] ),
		materialKind,
		materialVariants
	};
}

test("a companion with an authored material kind takes its variant model", () => {
	const resource = body( 1, { 1: SPOTTED } );
	const entity = { gid: 5, kind: "cos", refObjId: 17809, name: "Spotted" };
	assert.equal( lookupWith( resource ).resourceFor( entity )?.glb, SPOTTED );
});

test("a companion without an authored kind keeps the base model", () => {
	const resource = body( 0, { 1: SPOTTED } );
	const entity = { gid: 5, kind: "cos", refObjId: 17809, name: "Plain" };
	assert.equal( lookupWith( resource ).resourceFor( entity )?.glb, BASE );
});

test("a companion whose slot was never published keeps the base model", () => {
	const resource = body( 1, { 3: SPOTTED } );
	const entity = { gid: 5, kind: "cos", refObjId: 17809, name: "Spotted" };
	assert.equal( lookupWith( resource ).resourceFor( entity )?.glb, BASE );
});

test("a monster still takes its authored variant", () => {
	const resource = body( 1, { 1: SPOTTED } );
	const entity = { gid: 6, kind: "monster", refObjId: 17809, name: "Clone" };
	assert.equal( lookupWith( resource ).resourceFor( entity )?.glb, SPOTTED );
});

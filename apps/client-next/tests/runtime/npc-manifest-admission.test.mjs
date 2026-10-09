/*
===========================================================================

npc-manifest-admission.test.mjs - the catalog joins v9 reference rows

The NPC manifest stores one bake per BSR (resources) and a slim row per
reference (models). Admission must give every reference its resource with
its own fields over it, and must refuse a row whose BSR is absent.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPresentationCatalog } = await import( "../../src/engine/runtime/characters/presentation-catalog.ts" );

const MANIFEST = "/assets/npc/manifest.json";
const TIGER_BSR = "res/mob/china/tiger.bsr";

/*
================
catalogFixture
================
*/
function catalogFixture() {
	const accepted = [], rejected = [];
	const referenceAppearances = { setReferences() {}, skin: () => undefined, get: () => undefined };
	const published = createPresentationCatalog( {
		resources: {
			accepted: path => accepted.push( path ),
			rejected: ( path, error ) => rejected.push( { path, error } )
		},
		sounds: { catalog() {} },
		referenceAppearances
	} );
	return { published, accepted, rejected };
}

/*
================
admit
================
*/
function admit( published, manifest ) {
	published.admit( { path: MANIFEST, buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer } );
}

test("every reference of one BSR resolves its resource under its own fields", () => {
	const { published, accepted, rejected } = catalogFixture();
	admit( published, {
		format: "sro-mission-npc-models",
		version: 9,
		resources: {
			[TIGER_BSR]: { glb: "/assets/npc/mob/china/tiger.glb", clips: [ "stand", "walk" ] }
		},
		models: {
			MOB_CH_TIGER: { codename: "MOB_CH_TIGER", refObjId: 1, kind: "monster", bsr: TIGER_BSR, scalePercent: 100 },
			MOB_CH_TIGER_CLON: {
				codename: "MOB_CH_TIGER_CLON",
				refObjId: 2,
				kind: "monster",
				bsr: TIGER_BSR,
				scalePercent: 80
			}
		}
	} );
	assert.deepEqual( rejected, [] );
	assert.deepEqual( accepted, [ MANIFEST ] );
	const tiger = defined( published.catalog.get( 1 ) ), clone = defined( published.catalog.get( 2 ) );
	assert.equal( tiger.glb, "/assets/npc/mob/china/tiger.glb" );
	assert.equal( clone.glb, tiger.glb );
	assert.deepEqual( clone.clips, [ "stand", "walk" ] );
	assert.equal( clone.scalePercent, 80 );
	assert.equal( clone.codename, "MOB_CH_TIGER_CLON" );
});

test("a reference whose BSR has no resource rejects the manifest", () => {
	const { published, accepted, rejected } = catalogFixture();
	admit( published, {
		format: "sro-mission-npc-models",
		version: 9,
		resources: {},
		models: { MOB_LOST: { codename: "MOB_LOST", refObjId: 3, kind: "monster", bsr: "res/mob/lost.bsr" } }
	} );
	assert.deepEqual( accepted, [] );
	assert.equal( rejected.length, 1 );
	assert.match( String( rejected[0].error ), /Missing model resource/ );
	assert.equal( published.catalog.get( 3 ), undefined );
});

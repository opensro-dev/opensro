/*
===========================================================================

viewer-queries.test.mjs - the 3D viewer's glow accounting over the composite

The viewer keeps animating a doll that carries effect particles and, for a
still, waits until every one of them is drawn. An animated worn handle (an
avatar dress's wings) carries its ambient set on its own actor, so the
count covers the whole composite, not only the wearer's own particle list.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createViewerQueries } = await import( "../../src/engine/runtime/characters/viewer-queries.ts" );
const { effectProgramModel } = await import( "../../src/engine/foundation/assets/effect-programs.ts" );

const AMBIENT_SMOKE = [ {
	kind: 2,
	stateId: -1,
	animationSetName: "ambient",
	entries: [ {
		effectPath: "system/avatar_devil_smoke.efp",
		boneName: "Dummy_wing",
		vector3c: [ 0, 0, 0 ],
		field00: 0,
		field4c: 0,
		flag53: 0,
		flags50: [ 0, 0, 0 ]
	} ]
} ];

/*
================
queries

The queries over one dressed actor (gid 1) with a wing handle (gid 2),
reading a displayed map the test fills as emission would.
================
*/
function queries( appearance, displayed ) {
	const appearances = new Map( appearance ? [ [ 1, appearance ] ] : [] );
	return createViewerQueries(
		{
			published: { manifest: 1, manifests: [ "roster" ], items: {}, ridesByRider: new Map() },
			appearances,
			output: { displayed }
		},
		() => undefined
	);
}

/*
================
actor
================
*/
function actor( gid, model, attachment ) {
	return {
		gid,
		model,
		pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1,
		...(attachment ? { attachment } : {})
	};
}

test("a handle's ambient set alone makes the doll a glow carrier", () => {
	const bare = queries( { parts: [], model: "assembly", particles: [], auxiliary: [] }, new Map() );
	assert.equal( bare.glows( 1 ), false );
	assert.equal( bare.glowing( 1 ), true, "nothing carried, nothing to wait for" );
	const dressed = queries(
		{
			parts: [],
			model: "assembly",
			particles: [],
			auxiliary: [ { id: 23299, entry: { glb: "wings", particleModifiers: AMBIENT_SMOKE } } ]
		},
		new Map()
	);
	assert.equal( dressed.glows( 1 ), true, "the wings' smoke keeps the doll animating" );
	assert.equal( dressed.glowing( 1 ), false, "the smoke is owed but not drawn yet" );
});

test("glowing answers once the handle's effect actor is drawn", () => {
	const smokeModel = effectProgramModel( "system/avatar_devil_smoke.efp" );
	const appearance = {
		parts: [],
		model: "assembly",
		particles: [],
		auxiliary: [ { id: 23299, entry: { glb: "wings", particleModifiers: AMBIENT_SMOKE } } ]
	};
	const displayed = new Map( [
		[ 1, actor( 1, "assembly" ) ],
		[ 2, actor( 2, "wings", { gid: 1, bone: "Bip01 Spine1", offset: [ 0, 0, 0 ], basis: "compound" } ) ]
	] );
	const view = queries( appearance, displayed );
	assert.equal( view.glowing( 1 ), false );
	displayed.set(
		3,
		actor( 3, smokeModel, { gid: 2, bone: "Dummy_wing", root: false, offset: [ 0, 0, 0 ], basis: "bsr" } )
	);
	assert.equal( view.glowing( 1 ), true, "the effect actor on the handle completes the set" );
});

test("the wearer's own particles still count as before", () => {
	const appearance = { parts: [], model: "assembly", particles: [ { effectPath: "system/x.efp" } ], auxiliary: [] };
	const displayed = new Map( [ [ 1, actor( 1, "assembly" ) ] ] );
	const view = queries( appearance, displayed );
	assert.equal( view.glows( 1 ), true );
	assert.equal( view.glowing( 1 ), false );
	displayed.set(
		9,
		actor(
			9,
			effectProgramModel( "system/x.efp" ),
			{ gid: 1, bone: "equipment:6:EQ0:ai_end", root: false, offset: [ 0, 0, 0 ], basis: "bsr" }
		)
	);
	assert.equal( view.glowing( 1 ), true );
});

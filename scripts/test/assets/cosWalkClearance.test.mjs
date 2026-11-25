/*
===========================================================================

cosWalkClearance.test.mjs - COS walk clips keep their feet off the floor

#4416: a trained bear's lower legs sank under the plaza while it
followed its owner. The v1.150 client places characters at the server
position verbatim (no ground snap), so the sink is authored into
p_bear02_walk.ban and the import grounds that one file
(clipGrounding.mjs). These tests pin the mechanism on a synthetic rig,
prove the built bear clip clears the plane while its stand clip and the
smaller bear stay authored, and census every COS walk and run so the
next rig with this defect is named here instead of by a player.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import { assembleAvatar } from "../../build/char/buildAvatar.mjs";
import { groundClipFeet, groundedBanBasename, measureClipLowestY } from "../../build/char/clipGrounding.mjs";
import { parseBan } from "../../build/char/formats.mjs";
import { dataAssetPath, loadDataAsset } from "../../build/shared/jmxAssetIO.mjs";

// A one-bone rig: root at height 10 and one vertex bound to it whose
// bind position sits half a unit under the ground plane.
function syntheticRig( rootHeights ) {
	const skeleton = {
		bones: [ {
			name: "Bip01",
			parentIndex: -1,
			local: { q: [ 0, 0, 0, 1 ], t: [ 0, 10, 0 ] },
			invWorld: { q: [ 0, 0, 0, 1 ], t: [ 0, -10, 0 ] }
		} ],
		byName: new Map( [ [ "Bip01", 0 ] ] )
	};
	const parts = [ {
		localToGlobal: [ 0 ],
		mesh: {
			rigid: false,
			vertexCount: 1,
			positions: new Float32Array( [ 0, -0.5, 0 ] ),
			boneIndices: new Uint16Array( [ 0, 0 ] ),
			boneWeights: new Float32Array( [ 1, 0 ] )
		}
	} ];
	const clip = {
		name: "synthetic_walk",
		frameCount: rootHeights.length,
		frameTimesMs: rootHeights.map( ( _, i ) => i * 100 ),
		bones: [ {
			name: "Bip01",
			keyCount: rootHeights.length,
			keys: rootHeights.map( y => ({ q: [ 0, 0, 0, 1 ], t: [ 0, y, 0 ] }) )
		} ]
	};
	return { skeleton, parts, clip };
}

test("grounding lifts each root key by the worst sample it neighbours", () => {
	const { skeleton, parts, clip } = syntheticRig( [ 10, 9, 10 ] );
	assert.equal( measureClipLowestY( skeleton, parts, clip ), -1.5 );
	// The dip bottoms at the middle key (-1.5); both intervals neighbour
	// it, so every key carries the full depth and the interpolated lift
	// clears every sample between keys too.
	const lifts = groundClipFeet( skeleton, parts, clip );
	assert.deepEqual( [ ...lifts ].map( v => Math.round( v * 1000 ) / 1000 ), [ 1.5, 1.5, 1.5 ] );
	assert.deepEqual( clip.bones[0].keys.map( k => k.t[1] ), [ 11.5, 10.5, 11.5 ] );
	assert.ok( measureClipLowestY( skeleton, parts, clip, 16 ) >= -0.01 );
});

test("grounding leaves a clip that already clears the plane authored", () => {
	const { skeleton, parts, clip } = syntheticRig( [ 11, 11 ] );
	// The vertex rides 10.5 under a root at 11: it never reaches 0.
	const lifts = groundClipFeet( skeleton, parts, clip );
	assert.deepEqual( [ ...lifts ], [ 0, 0 ] );
	assert.deepEqual( clip.bones[0].keys.map( k => k.t[1] ), [ 11, 11 ] );
});

test("only the registered BANs are grounded", () => {
	assert.equal( groundedBanBasename( "prim\\ani\\cos\\p_bear02\\p_bear02_walk.ban" ), true );
	assert.equal( groundedBanBasename( "res/cos/p_bear02_walk.ban" ), true );
	assert.equal( groundedBanBasename( "prim\\ani\\cos\\p_bear02\\p_bear02_run.ban" ), true );
	assert.equal( groundedBanBasename( "prim\\ani\\cos\\p_seowon\\p_seowon_walk.ban" ), true );
	assert.equal( groundedBanBasename( "prim\\ani\\cos\\p_bear01\\p_bear01_walk.ban" ), false );
	assert.equal( groundedBanBasename( "prim\\ani\\cos\\p_bear02\\p_bear02_stand01.ban" ), false );
	assert.equal( groundedBanBasename( "prim\\ani\\cos\\p_seowon\\p_seowon_run.ban" ), false );
	assert.equal( groundedBanBasename( "" ), false );
	assert.equal( groundedBanBasename( null ), false );
});

/*
================
rootTrackOf

The clip's root-bone track, the one grounding lifts.
================
*/
function rootTrackOf( avatar, clip ) {
	const rootName = avatar.skeleton.bones.find( bone => bone.parentIndex < 0 )?.name;
	return clip.bones.find( bone => bone.name === rootName );
}

test("the built p_bear02 walk keeps its feet at the ground plane (#4416)", async () => {
	const avatar = await assembleAvatar( "res/cos/p_bear02.bsr" );
	const walk = avatar.clips.find( entry => entry.role === "walk" );
	assert.ok( walk, "p_bear02 exports a walk clip" );
	const lowest = measureClipLowestY( avatar.skeleton, avatar.parts, walk.clip, 16 );
	assert.ok( lowest >= -0.15, `p_bear02 walk reaches ${lowest.toFixed( 2 )} under the ground plane` );

	// The grounding engaged on the authored keys: the deepest authored
	// keyframe sits -3.01 under the plane, so some key rose by metres.
	const raw = parseBan( await loadDataAsset( walk.path ) );
	const lifted = rootTrackOf( avatar, walk.clip ), authored = rootTrackOf( avatar, raw );
	const rises = lifted.keys.map( ( key, i ) => key.t[1] - authored.keys[i].t[1] );
	assert.ok( Math.max( ...rises ) > 2.5, `walk root rose by only ${Math.max( ...rises ).toFixed( 2 )}` );
	assert.ok( Math.min( ...rises ) >= 0, "grounding never lowers a key" );

	// The stand clip is not registered and stays authored, key for key.
	const stand = avatar.clips.find( entry => entry.role === "stand" );
	assert.ok( stand, "p_bear02 exports a stand clip" );
	const rawStand = parseBan( await loadDataAsset( stand.path ) );
	assert.deepEqual( rootTrackOf( avatar, stand.clip ).keys, rootTrackOf( avatar, rawStand ).keys );
});

test("the smaller bear rig stays authored (#4416)", async () => {
	const avatar = await assembleAvatar( "res/cos/p_bear01.bsr" );
	const walk = avatar.clips.find( entry => entry.role === "walk" );
	assert.ok( walk, "p_bear01 exports a walk clip" );
	const raw = parseBan( await loadDataAsset( walk.path ) );
	assert.deepEqual( rootTrackOf( avatar, walk.clip ).keys, rootTrackOf( avatar, raw ).keys );
	// Its authored gait brushes the plane (-0.57 measured) and is fine.
	const lowest = measureClipLowestY( avatar.skeleton, avatar.parts, walk.clip, 16 );
	assert.ok( lowest < -0.3 && lowest > -0.8, `p_bear01 walk lowest ${lowest.toFixed( 2 )}` );
});

test("the built p_bear02 run keeps its feet at the ground plane (#4458)", async () => {
	// A following pet runs whenever its owner runs; the run BAN authors
	// the same sink at the faster gait (dense -1.94, deepest key -1.78).
	const avatar = await assembleAvatar( "res/cos/p_bear02.bsr" );
	const run = avatar.clips.find( entry => entry.role === "run" );
	assert.ok( run, "p_bear02 exports a run clip" );
	const lowest = measureClipLowestY( avatar.skeleton, avatar.parts, run.clip, 16 );
	assert.ok( lowest >= -0.15, `p_bear02 run reaches ${lowest.toFixed( 2 )} under the ground plane` );
	const raw = parseBan( await loadDataAsset( run.path ) );
	const lifted = rootTrackOf( avatar, run.clip ), authored = rootTrackOf( avatar, raw );
	const rises = lifted.keys.map( ( key, i ) => key.t[1] - authored.keys[i].t[1] );
	assert.ok( Math.max( ...rises ) > 1.5, `run root rose by only ${Math.max( ...rises ).toFixed( 2 )}` );
	assert.ok( Math.min( ...rises ) >= 0, "grounding never lowers a key" );
});

test("the built p_seowon walk keeps its feet at the ground plane (#4458)", async () => {
	// The monkey rig authors the bear's defect: nearly every walk key
	// plants a foot about a unit under (dense -2.67, deepest key -2.38)
	// while its stand floor is -0.07, so it is grounded, not pinned.
	const avatar = await assembleAvatar( "res/cos/p_seowon.bsr" );
	const walk = avatar.clips.find( entry => entry.role === "walk" );
	assert.ok( walk, "p_seowon exports a walk clip" );
	const lowest = measureClipLowestY( avatar.skeleton, avatar.parts, walk.clip, 16 );
	assert.ok( lowest >= -0.15, `p_seowon walk reaches ${lowest.toFixed( 2 )} under the ground plane` );
	const raw = parseBan( await loadDataAsset( walk.path ) );
	const lifted = rootTrackOf( avatar, walk.clip ), authored = rootTrackOf( avatar, raw );
	const rises = lifted.keys.map( ( key, i ) => key.t[1] - authored.keys[i].t[1] );
	assert.ok( Math.max( ...rises ) > 2, `walk root rose by only ${Math.max( ...rises ).toFixed( 2 )}` );
	assert.ok( Math.min( ...rises ) >= 0, "grounding never lowers a key" );

	// The stand clip is not registered and stays authored, key for key.
	const stand = avatar.clips.find( entry => entry.role === "stand" );
	assert.ok( stand, "p_seowon exports a stand clip" );
	const rawStand = parseBan( await loadDataAsset( stand.path ) );
	assert.deepEqual( rootTrackOf( avatar, stand.clip ).keys, rootTrackOf( avatar, rawStand ).keys );
});

// Known authored sinks the census tolerates by name, measured as the
// locomotion clip's lowest vertex minus the model's own stand lowest
// vertex, keyed by file and role. Any other COS walk or run past -2.0
// fails this test and is named in the output: fix it by registering
// its BAN in clipGrounding.mjs, or pin it here with its measured depth
// and the reason the sink is authored intent.
//   t_behemoth.bsr (walk and run): its origin is authored inside the
//     body (stand already -5.53); both gaits reach -2.29 past that
//     floor at a finger, not a leg.
//   c_pegasus.bsr (run): the run BAN carries a flying episode - after
//     ninety gallop keys at the floor (no key past -0.76) the root
//     rises +9.70 while the feet trail -4.87 under the origin line.
//     Grounding would lift the flight, not fix a gait, so it stays
//     pinned for an explicit direction call. Its walk clears (+0.0).
// Past the line but left authored below it, for the record: t_lizard
// run -1.80 (that rig has no walk clip), p_penguin02 run -1.53 and
// p_seowon run -1.19 relative.
const KNOWN_SINKS = new Map( [
	[ "t_behemoth.bsr walk", -2.29 ],
	[ "t_behemoth.bsr run", -2.29 ],
	[ "c_pegasus.bsr run", -4.88 ]
] );
const SINK_LIMIT = -2.0;

test("census: every COS walk and run keeps its feet within the named sinks", async () => {
	const files = fs.readdirSync( dataAssetPath( "res/cos" ) )
		.filter( file => file.toLowerCase().endsWith( ".bsr" ) )
		.sort();
	assert.ok( files.length >= 54, `census saw only ${files.length} COS resources` );
	const offenders = new Map();
	let sawBear02 = false, sawBear01 = false, sawSeowon = false;
	for ( const file of files ) {
		const avatar = await assembleAvatar( `res/cos/${file}` );
		const stand = avatar.clips.find( entry => entry.role === "stand" );
		const floor = stand ?
			measureClipLowestY( avatar.skeleton, avatar.parts, stand.clip ) :
			measureClipLowestY( avatar.skeleton, avatar.parts, null );
		for ( const role of [ "walk", "run" ] ) {
			const gait = avatar.clips.find( entry => entry.role === role );
			if ( !gait ) continue;
			if ( file === "p_bear02.bsr" ) sawBear02 = true;
			if ( file === "p_bear01.bsr" ) sawBear01 = true;
			if ( file === "p_seowon.bsr" ) sawSeowon = true;
			const dip = measureClipLowestY( avatar.skeleton, avatar.parts, gait.clip ) - floor;
			if ( dip < SINK_LIMIT ) offenders.set( `${file} ${role}`, dip );
		}
	}
	assert.ok( sawBear02 && sawBear01 && sawSeowon, "the census measured both bear rigs and the seowon" );
	assert.deepEqual(
		[ ...offenders.keys() ].sort(),
		[ ...KNOWN_SINKS.keys() ].sort(),
		`COS locomotion sinks past ${SINK_LIMIT}: ${
			[ ...offenders ].map( ( [name, dip] ) => `${name} ${dip.toFixed( 2 )}` ).join( ", " ) || "none"
		}`
	);
	for ( const [name, pinned] of KNOWN_SINKS ) {
		const measured = offenders.get( name );
		assert.ok(
			Math.abs( measured - pinned ) <= 0.3,
			`${name}: pinned sink ${pinned}, measured ${measured?.toFixed( 2 )} - update the pin or fix the clip`
		);
	}
});

/*
===========================================================================

pose-on-demand.test.mjs - a pose builds only the nodes its consumers read

animation-pose.ts answers a socket of a pending pose by building the
marker's ancestor chain alone, and a palette asked for some entries (a
cloth step, cloth-stream.ts clothStreamJoints) by building their joints'
chains alone; a later request in the same evaluation completes what is
missing. On shipped player bodies wearing a shipped dress and holding
glow weapons, a pose that only ever answers such requests steps through a
script (repeated times, clip changes, skipped evaluations, a layered
frame, a bone rotator set and released, a body volume change, restore)
beside a pose that evaluates in full every step: every socket matrix,
every asked palette entry and the whole palette after completion must be
its words, and a partial palette must leave every other word of its
output alone. A dress's cloth stream fed palettes holding only the
entries it asks for must then write what it writes from whole palettes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

const { createCharacterPose, createCharacterPoseInputs } = await import(
	"../../src/engine/foundation/animation/animation-pose.ts"
);
const { appendEquipmentSockets } = await import( "../../src/engine/foundation/animation/equipment-sockets.ts" );
const { characterClothJoints, createCharacterPlanCaches } = await import(
	"../../src/engine/foundation/animation/character-budget.ts"
);
const { createClothStream } = await import( "../../src/engine/foundation/animation/cloth-stream.ts" );
const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

// A word no palette entry holds: what a partial palette must leave alone.
const UNTOUCHED = 0x7fc0dead;
const BODIES = [
	{ key: "CH_M", body: "CHAR_CH_MAN_ADVENTURER", dress: "ITEM_MALL_AVATAR_M_KNIGHT" },
	{ key: "EU_W", body: "CHAR_EU_WOMAN_ADVENTURER", dress: "ITEM_MALL_AVATAR_W_KNIGHT" }
];
const ROTATED_BONE = "Bip01 Spine1";
const CLOTH_FRAMES = 300;

const read = path => readPublishedAssetBytesSync( path, CLIENT_PUBLIC_ROOT );
const roster = JSON.parse( new TextDecoder().decode( read( "/assets/char/roster.json" ) ) );
const decoder = createModelDecoder();

/*
================
decode
================
*/
function decode( glb ) {
	return decoder.character( decoder.decode( read( glb ) ) );
}

/*
================
wearer

A body wearing the dress's cloth piece, its joints joined to the body by
name as characters.ts assembles a part, with the glow weapon and shield
branches a glow emitter holds appended to its nodes.
================
*/
function wearer( { key, body: code, dress } ) {
	const body = decode( roster.models.find( row => row.codename === code ).glb );
	const record = Object.values( roster.dress.equipment ).find( row => row.code === dress );
	const part = decode( record.bodies[key].glb ), piece = part.primitives.find( p => p.cloth );
	assert.ok( piece?.geometry.joints, `${dress} has a skinned cloth piece` );
	const names = new Map( body.nodes.map( ( node, index ) => [ node.name, index ] ) );
	const cloth = { ...piece, joints: piece.joints.map( joint => names.get( part.nodes[joint].name ) ) };
	let nodes = body.nodes;
	for ( const slot of [ 6, 7 ] ) {
		const item = Object.keys( roster.dress.specialGlows ).map( id => roster.dress.equipment[id] )
			.find( entry => entry.slot === slot && entry.bodies[key]?.branches?.length );
		if ( item ) nodes = appendEquipmentSockets( nodes, item.bodies[key].branches, slot );
	}
	return { model: { ...body, nodes, primitives: [ ...body.primitives, cloth ] }, cloth };
}

/*
================
words
================
*/
function words( floats ) {
	return new Uint32Array( floats.buffer, floats.byteOffset, floats.length );
}

/*
================
script

The steps a candidate pose is driven through: the inputs that change, then
what it is asked (sockets, the cloth's entries, the whole palette). Every
skipped step evaluates and asks nothing.
================
*/
function script( model ) {
	const clips = model.clips.map( clip => clip.name ),
		at = model.nodes.findLastIndex( node => node.name.startsWith( "equipment:" ) );
	const marker = model.nodes[at].name, holder = model.nodes[model.nodes[at].parent].name;
	const hand = "Bip01 R Hand", head = "Bip01 Head";
	const sparse = model.clips.reduce( ( a, b ) => b.channels.length < a.channels.length ? b : a ).name;
	assert.notEqual( sparse, "stand" );
	assert.ok( clips.includes( "run" ) && clips.includes( "attack1" ) );
	return [
		{ clip: "stand", time: .1, sockets: [ marker ], cloth: true },
		{ clip: "stand", time: .4, sockets: [ head ] },
		// The same time again: the cloth completes the evaluation's missing nodes.
		{ clip: "stand", time: .4, cloth: true },
		{ clip: "stand", time: .7, sockets: [ hand ], cloth: true, whole: true },
		// Several evaluations nobody reads, then a partial one.
		{ clip: "stand", time: .9 },
		{ clip: "run", time: .2 },
		{ clip: "run", time: .3 },
		{ clip: "run", time: .45, cloth: true },
		{ clip: "run", time: .5, sockets: [ marker, head ] },
		// A whole palette after a partial request of the same evaluation.
		{ clip: "attack1", time: .3, cloth: true, whole: true },
		// The marker's holder moves; then the same samples under another
		// lane, a new evaluation that moves nothing: the marker, skipped by
		// the first, must not keep the matrix of the whole one before it.
		{ clip: "run", time: .35, sockets: [ holder ] },
		{ layers: [ { clip: "run", time: .35, loop: true, weight: 1, lane: "event" } ], sockets: [ marker ] },
		{ clip: sparse, time: .1, sockets: [ hand ], cloth: true },
		{ clip: sparse, time: .2, cloth: true, sockets: [ marker ] },
		{
			layers: [
				{ clip: "run", time: .6, loop: true, weight: .6, lane: "timed" },
				{ clip: "attack1", time: .2, loop: false, weight: .4, lane: "event" }
			],
			cloth: true,
			sockets: [ marker ]
		},
		{ clip: "run", time: .7, cloth: true },
		{ clip: "run", time: .8, rotation: [ .1, .2, 0, Math.sqrt( .95 ) ], sockets: [ head ], cloth: true },
		{ clip: "run", time: .9, rotation: [ 0, .3, .1, Math.sqrt( .9 ) ], cloth: true },
		{ clip: "run", time: 1, rotation: null, sockets: [ marker ], cloth: true },
		{ clip: "run", time: 1.1, cloth: true },
		{ clip: "stand", time: .2, volume: 4, cloth: true, sockets: [ hand ] },
		{ clip: "stand", time: .3, cloth: true, whole: true },
		{ clip: "stand", time: .5, volume: 2, cloth: true },
		// A restart's kept frame (characters.ts keptClothPalette): the
		// pose takes older inputs for one partial palette, then its own.
		{ clip: "attack1", time: .1, keep: true },
		{ clip: "attack1", time: .5 },
		{ clip: "attack1", time: .6, kept: true, cloth: true, sockets: [ marker ] },
		{ clip: "attack1", time: .7, kept: true, sockets: [ head ] },
		{ clip: "attack1", time: .8, cloth: true, whole: true }
	];
}

/*
================
drive

Step a candidate (partial requests only, unless the step asks for the
whole palette) and a reference (every palette whole, every step) through
the script, comparing what the candidate answers with the reference's
words.
================
*/
function drive( label, { model, cloth } ) {
	const caches = createCharacterPlanCaches(), entries = characterClothJoints( cloth, caches );
	assert.ok( entries.length > 0 && entries.length < cloth.joints.length / 4, `${label}: ${entries.length} entries` );
	const candidate = createCharacterPose( model, caches.nodeChannels ), reference = createCharacterPose( model );
	const kept = createCharacterPoseInputs(),
		now = createCharacterPoseInputs(),
		keptReference = createCharacterPose( model );
	const floats = cloth.joints.length * 16, actual = new Float32Array( floats ), expected = new Float32Array( floats );
	let deferred = 0;
	for ( const [index, step] of script( model ).entries() ) {
		const at = `${label} step ${index}`;
		for ( const pose of [ candidate, reference ] ) {
			if ( step.volume !== undefined ) pose.bodyVolume( step.volume, false );
			if ( step.rotation !== undefined ) pose.setBoneRotation( ROTATED_BONE, step.rotation );
			const layers = /** @type {import("../../src/engine/contracts/character").CharacterLayer[] | undefined} */ (
				step.layers
			);
			pose.evaluate( step.clip ?? "", step.time ?? 0, true, layers, true );
		}
		if ( candidate.gpuSample() ) deferred++;
		// The reference materializes every evaluation in full.
		for ( const primitive of model.primitives ) {
			reference.palette( primitive, new Float32Array( primitive.joints.length * 16 ) );
		}
		if ( step.keep ) {
			assert.ok( candidate.inputs( kept ) );
			keptReference.restore( kept );
		}
		if ( step.kept ) {
			assert.ok( candidate.inputs( now ) );
			candidate.restore( kept );
			words( actual ).fill( UNTOUCHED );
			candidate.palette( cloth, actual, 0, entries );
			keptReference.palette( cloth, expected );
			comparePartial( `${at} kept`, actual, expected, entries );
			candidate.restore( now );
		}
		for ( const name of step.sockets ?? [] ) {
			assert.deepEqual(
				[ ...words( candidate.socket( name ) ) ],
				[ ...words( reference.socket( name ) ) ],
				`${at} socket ${name}`
			);
		}
		if ( step.cloth ) {
			words( actual ).fill( UNTOUCHED );
			candidate.palette( cloth, actual, 0, entries );
			reference.palette( cloth, expected );
			comparePartial( at, actual, expected, entries );
		}
		if ( !step.whole ) continue;
		// Completion: every palette, then every node, as the reference built them.
		for ( const primitive of model.primitives ) {
			const a = new Float32Array( primitive.joints.length * 16 ), b = a.slice();
			candidate.palette( primitive, a );
			reference.palette( primitive, b );
			assert.deepEqual( [ ...words( a ) ], [ ...words( b ) ], `${at} whole palette ${primitive.name}` );
		}
		model.nodes.forEach( ( node, n ) => {
			if ( model.nodes.findIndex( other => other.name === node.name ) !== n ) return;
			assert.deepEqual(
				[ ...words( candidate.socket( node.name ) ) ],
				[ ...words( reference.socket( node.name ) ) ],
				`${at} node ${node.name}`
			);
		} );
	}
	// The partial path must have run on poses the GPU samples.
	assert.ok( deferred > 15, `${label}: ${deferred} deferred evaluations` );
}

/*
================
comparePartial

The asked entries hold the whole palette's words; every other word is
untouched.
================
*/
function comparePartial( at, actual, expected, entries ) {
	const asked = new Set( entries );
	for ( let entry = 0; entry < actual.length / 16; entry++ ) {
		const got = [ ...words( actual.subarray( entry * 16, entry * 16 + 16 ) ) ];
		if ( asked.has( entry ) ) {
			assert.deepEqual(
				got,
				[ ...words( expected.subarray( entry * 16, entry * 16 + 16 ) ) ],
				`${at} entry ${entry}`
			);
		} else assert.deepEqual( got, new Array( 16 ).fill( UNTOUCHED ), `${at} unasked entry ${entry}` );
	}
}

test("sockets and asked palette entries of a pending pose equal a whole materialization word for word", () => {
	for ( const body of BODIES ) drive( body.key, wearer( body ) );
});

/*
================
clothRun

A dress's cloth stream over CLOTH_FRAMES frames of a real clip at 60 Hz,
off for frames 100..159 (its off frames keep the pose, its restart
rebuilds the kept frame), fed whole palettes or, when partial, palettes
holding only the entries asked for. Every write and random call, in order.
================
*/
function clothRun( model, cloth, partial ) {
	const pose = createCharacterPose( model ), seed = createPresentationRandom( 0x1188 ), log = [];
	const stream = createClothStream( cloth, () => (log.push( "r" ), seed.range( 0, 32768 )) );
	const floats = cloth.joints.length * 16;
	const keptPalette = new Float32Array( floats );
	/*
	================
	asked
	================
	*/
	const asked = ( whole, entries ) => {
		if ( !partial || !entries ) return whole.slice();
		const out = new Float32Array( floats ).fill( NaN );
		for ( const e of entries ) out.set( whole.subarray( e * 16, e * 16 + 16 ), e * 16 );
		return out;
	};
	const palettes = {
		current( entries ) {
			const whole = new Float32Array( floats );
			pose.palette( cloth, whole );
			return asked( whole, entries );
		},
		keep() {
			pose.palette( cloth, keptPalette );
			return true;
		},
		kept( entries ) {
			return asked( keptPalette, entries );
		}
	};
	for ( let frame = 0; frame < CLOTH_FRAMES; frame++ ) {
		const seconds = frame / 60;
		pose.evaluate( "run", seconds, true );
		const write = stream.update( palettes, seconds, frame < 100 || frame >= 160, {
			direction: [ 0, 0, 1 ],
			speed: 0
		} );
		log.push( write ? `${write.first}:${[ ...write.entries ].join( "," )}` : "-" );
	}
	return log;
}

test("a dress's cloth stream reads no palette entry beyond the ones it asks for", () => {
	for ( const body of BODIES ) {
		const { model, cloth } = wearer( body ), whole = clothRun( model, cloth, false );
		assert.ok( whole.includes( "r" ), `${body.key}: the cloth steps` );
		assert.deepEqual( clothRun( model, cloth, true ), whole, body.key );
	}
});

/*
================
held key after a partial evaluation

A node whose key is held stays unchanged from one evaluation to the next,
but a partial evaluation moved it past a child it never settled. The child
must take the moved parent, not reuse its matrix from before.
================
*/
test("a child skipped by a partial evaluation follows its parent across a held key", () => {
	const node = ( name, parent ) => ({
		name,
		parent,
		translation: [ 0, 0, 0 ],
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	});
	const track = ( index, values ) => ({
		node: index,
		path: "translation",
		interpolation: "LINEAR",
		times: Float32Array.of( 0, .5, 1 ),
		values: Float32Array.from( values )
	});
	const model = /** @type {any} */ ({
		nodes: [ node( "root", -1 ), node( "arm", 0 ), node( "hand", 1 ) ],
		primitives: [],
		images: [],
		clips: [ {
			name: "reach",
			duration: 1,
			// The arm holds x = 5 from .5 on; the hand never moves.
			channels: [ track( 1, [ 0, 0, 0, 5, 0, 0, 5, 0, 0 ] ), track( 2, [ 1, 0, 0, 1, 0, 0, 1, 0, 0 ] ) ]
		} ]
	});
	const pose = createCharacterPose( model );
	const x = name => pose.socket( name )?.[12];
	pose.evaluate( "reach", .1, false, undefined, true );
	assert.equal( x( "hand" ), 2 );
	pose.evaluate( "reach", .6, false, undefined, true );
	assert.equal( x( "arm" ), 5 );
	pose.evaluate( "reach", .8, false, undefined, true );
	assert.equal( x( "hand" ), 6 );
});

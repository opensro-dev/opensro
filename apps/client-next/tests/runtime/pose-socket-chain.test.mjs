/*
===========================================================================

pose-socket-chain.test.mjs - a socket settles only its ancestor chain

animation-pose.ts answers a socket of a pending (GPU-sampled) pose by
settling the marker's ancestors alone and leaves the rest for palette. This
checks, on published models and seeded random poses, that every socket and
every later palette carries the float bits of a pose that only ever
materialized whole.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetJsonSync, readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";

const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { appendEquipmentSockets } = await import( "../../src/engine/foundation/animation/equipment-sockets.ts" );
const { createCharacterDecoder } = await import(
	"../../src/engine/runtime/assets/worker/model/character/character.ts"
);

const TRIALS = 160;

/*
================
decode
================
*/
function decode( glb ) {
	const bytes = readPublishedAssetBytesSync( glb, publicRoot ),
		end = 20 + bytes.readUInt32LE( 12 ),
		bin = bytes.subarray( end + 8 );
	return createCharacterDecoder().decode( {
		json: JSON.parse( bytes.subarray( 20, end ) ),
		binary: bin.buffer.slice( bin.byteOffset, bin.byteOffset + bin.length )
	} );
}

/*
================
geared

A Chinese man with a special-glow weapon and, when the catalogue has one,
a special-glow shield: the compound skeleton a glow emitter holds.
================
*/
function geared() {
	const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot );
	const body = decode( roster.models.find( row => row.codename === "CHAR_CH_MAN_ADVENTURER" ).glb );
	let nodes = body.nodes;
	for ( const slot of [ 6, 7 ] ) {
		const item = Object.keys( roster.dress.specialGlows ).map( id => roster.dress.equipment[id] )
			.find( entry => entry.slot === slot && entry.bodies.CH_M?.branches?.length );
		if ( item ) nodes = appendEquipmentSockets( nodes, item.bodies.CH_M.branches, slot );
	}
	return { ...body, nodes };
}

/*
================
random

A seeded generator (mulberry32), so a failure replays.
================
*/
function random( seed ) {
	return () => {
		seed = (seed + 0x6d2b79f5) | 0;
		let t = Math.imul( seed ^ (seed >>> 15), 1 | seed );
		t = (t + Math.imul( t ^ (t >>> 7), 61 | t )) ^ t;
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

/*
================
bits
================
*/
function bits( matrix ) {
	return [ ...new Uint32Array( Float32Array.from( matrix ).buffer ) ];
}

/*
================
depth
================
*/
function depth( model, n ) {
	let d = 0;
	for ( let p = model.nodes[n].parent; p >= 0; p = model.nodes[p].parent ) d++;
	return d;
}

/*
================
order

The nodes to ask this evaluation: a random subset in random order, or every
node leaf first or root first.
================
*/
function order( model, next ) {
	const all = model.nodes.map( ( _, n ) => n ), kind = Math.floor( next() * 4 );
	if ( kind === 0 ) return all.sort( ( a, b ) => depth( model, b ) - depth( model, a ) );
	if ( kind === 1 ) return all.sort( ( a, b ) => depth( model, a ) - depth( model, b ) );
	for ( let i = all.length - 1; i > 0; i-- ) {
		const j = Math.floor( next() * (i + 1) );
		[all[i], all[j]] = [ all[j], all[i] ];
	}
	return kind === 2 ?
		all.slice( 0, 1 + Math.floor( next() * 4 ) ) :
		all.slice( 0, Math.floor( next() * all.length ) );
}

/*
================
exercise

Drive a socket-first pose, a palette-first twin (the socket reference) and
a pose that never answers a socket (the palette reference) through the same
seeded evaluations. Some evaluations end with sockets alone, so the next one
starts after partial work.
================
*/
function exercise( label, model, seed ) {
	const next = random( seed ),
		actual = createCharacterPose( model ),
		whole = createCharacterPose( model ),
		unasked = createCharacterPose( model );
	const names = model.nodes.map( node => node.name );
	const firsts = names.map( ( name, n ) => names.indexOf( name ) === n );
	const blendable = model.clips.length > 1;
	let deferred = 0, partial = 0, request;
	for ( let trial = 0; trial < TRIALS; trial++ ) {
		// An unchanged request keeps the pending evaluation and what it settled.
		if ( !request || next() > .1 ) {
			const clip = model.clips[Math.floor( next() * model.clips.length )];
			const time = next() < .1 ? 0 : next() * clip.duration * 1.5, loop = next() < .5;
			// A blend materializes at evaluate; the next deferred pose starts from it.
			/** @type {import("../../src/engine/contracts/character").CharacterLayer[] | undefined} */
			const layers = blendable && next() < .15 ?
				[
					{ clip: clip.name, time, loop, weight: .6, lane: "timed" },
					{ clip: model.clips[0].name, time: time / 2, loop: true, weight: .4, lane: "event" }
				] :
				undefined;
			request = { clip: clip.name, time, loop, layers };
		}
		for ( const pose of [ actual, whole, unasked ] ) {
			pose.evaluate( request.clip, request.time, request.loop, request.layers, true );
		}
		if ( actual.gpuSample() ) deferred++;
		for ( const primitive of model.primitives ) {
			whole.palette( primitive, new Float32Array( primitive.joints.length * 16 ) );
		}
		for ( const n of order( model, next ) ) {
			// A repeated name answers its first node, as the socket table does.
			if ( !firsts[n] ) continue;
			partial++;
			assert.deepEqual(
				bits( actual.socket( names[n] ) ),
				bits( whole.socket( names[n] ) ),
				`${label} trial ${trial} socket ${names[n]}`
			);
		}
		if ( next() < .4 ) continue;
		for ( const primitive of model.primitives ) {
			const a = new Float32Array( primitive.joints.length * 16 ), b = a.slice();
			actual.palette( primitive, a );
			unasked.palette( primitive, b );
			assert.deepEqual( bits( a ), bits( b ), `${label} trial ${trial} palette` );
		}
		for ( let n = 0; n < names.length; n++ ) {
			if ( firsts[n] ) assert.deepEqual( bits( actual.socket( names[n] ) ), bits( whole.socket( names[n] ) ) );
		}
	}
	// The socket-first path must have run on pending GPU-sampled poses.
	assert.ok( deferred > TRIALS / 2, `${label}: ${deferred} deferred evaluations` );
	assert.ok( partial > TRIALS, `${label}: ${partial} sockets` );
}

test("sockets and later palettes of a pending pose equal one whole materialization bit for bit", () => {
	const player = geared();
	assert.ok( player.nodes.some( node => node.name.startsWith( "equipment:6:" ) ) );
	assert.ok( player.clips.length > 0 && Math.max( ...player.nodes.map( ( _, n ) => depth( player, n ) ) ) > 10 );
	exercise( "player", player, 1 );
	exercise( "mangnyang", decode( "/assets/npc/mob/china/mangnyang.glb" ), 2 );
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

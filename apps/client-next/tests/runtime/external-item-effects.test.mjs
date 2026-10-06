/*
===========================================================================

external-item-effects.test.mjs - tests for effects.ts, effects.ts,
program.ts, random.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const read = p => readFileSync( CLIENT_PUBLIC_ROOT + "/assets/" + p );
const decoder = createEffectDecoder(),
	named = decoder.decode( read( "skill/namedEffectRecords.json" ) ),
	catalog = decoder.decode( read( "skill/effectRecords.json" ) );
const items = JSON.parse( read( "data/missionPresentation.json" ) ).itemsByRefObjId,
	drops = JSON.parse( read( "itemdrop/manifest.json" ) ).models;
const programs = createEffectPrograms(), durations = new Map();
const source = {
	gid: 17,
	kind: "player",
	refObjId: 1933,
	regionId: 0x6b4f,
	x: 100,
	y: 20,
	z: 30,
	heading: 0,
	name: "test"
};
function harness( item, typeFlags = 0x36c, recordOverride ) {
	let id = 0, admitted = true;
	const jobs = new Map(), sounds = [];
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{
							kind: "effects",
							catalog: url.includes( "namedEffect" ) ?
								(recordOverride ? { ...named, [items[item].codename]: recordOverride } : named) :
								catalog
						} :
						{ kind: "bytes", buffer: read( "skillfx/manifest.json" ) }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		e => sounds.push( e ),
		createPresentationRandom( 1 ),
		id => ({ ...items[id], model: drops[items[id]?.dropModelPath] })
	);
	function duration( path ) {
		if ( !path.includes( "#" ) ) return 1;
		const efp = decodeURIComponent( path.split( "#" )[1] );
		if ( !durations.has( efp ) ) {
			durations.set( efp, programs.decode( read( "effects/programs.json" ), efp ).model.clips[0].duration );
		}
		return durations.get( efp );
	}
	const actor = {
		gid: 17,
		model: "host",
		clip: "stand",
		time: 0,
		loop: true,
		heightFactor: 1,
		pose: { ...source, yaw: 0 }
	};
	const step = ( at, entities = [ source ], detail = 2 ) => {
		const out = owner.step(
			entities,
			{ casts: [] },
			at,
			() => admitted,
			duration,
			[],
			undefined,
			[ actor ],
			detail
		);
		assert.equal( owner.error(), null );
		return out;
	};
	owner.item( { kind: "item-effect", source, item, typeFlags } );
	return { owner, sounds, step, ready: value => admitted = value };
}
for ( const id of [ 63, 64, 65, 66, 69, 3834, 3835, 3836 ] ) {
	test( "native item firework publishes complete sequence " + id, () => {
		const h = harness( id );
		h.ready( false );
		for ( let i = 0; i < 5; i++ ) {
			h.step( i );
		}
		assert.equal( h.sounds.length, 0 );
		h.ready( true );
		const seen = new Set();
		for ( let i = 0; i <= 500; i++ ) {
			const actors = h.step( 5 + i * .05, i > 1 ? [] : [ source ] );
			for ( const a of actors ) {
				if ( a.model.includes( "#" ) ) {
					seen.add( a.gid );
					assert.equal( a.pose.x, 100 );
					assert.equal( a.pose.y, 28 );
				}
			}
		}
		assert.equal( seen.size, [ 69, 3836 ].includes( id ) ? 8 : 1 );
		assert.equal( new Set( h.sounds.map( s => s.id ) ).size, h.sounds.length );
		const stages = named[items[id].codename].stages, soundCount = stages.filter( s => s.sound ).length;
		assert.equal( h.sounds.length, soundCount * seen.size );
		assert.deepEqual( h.step( 40, [] ), [] );
		h.owner.dispose();
	} );
}
test("native emitter advances at most once after a stall and reset removes delayed sounds", () => {
	const h = harness( 69 );
	for ( let i = 0; i < 4; i++ ) h.step( i * .01 );
	h.step( 10 );
	assert.equal( h.sounds.filter( s => s.path.includes( "shot" ) ).length, 2 );
	h.owner.reset();
	assert.deepEqual( h.step( 11 ), [] );
	h.owner.dispose();
});
test("3449 snapshots the existing character and ignores missing targets", () => {
	const e = createEntities();
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ { refObjId: 69, typeFlags: 0x36c } ],
		localPlayerEntry: {
			modelRef: 1933,
			startProfile: { regionId: source.regionId, x: 100, y: 20, z: 30, angle: 0 }
		}
	} );
	const latch = Buffer.alloc( 8 );
	latch.writeUInt32LE( 17 );
	e.receive( { opcode: 0x32a6, payload: latch } );
	const flush = () => {
		const b = e.take();
		if ( b ) e.ack( b.sequence );
		return b?.events ?? [];
	};
	flush();
	const packet = Buffer.alloc( 8 );
	packet.writeUInt32LE( 999 );
	packet.writeUInt32LE( 69, 4 );
	e.receive( { opcode: 0x3449, payload: packet } );
	assert.deepEqual( flush(), [] );
	packet.writeUInt32LE( 17 );
	e.receive( { opcode: 0x3449, payload: packet } );
	const event = flush().find( e => e.kind === "item-effect" );
	assert.equal( defined( event ).source.x, 100 );
	assert.equal( defined( event ).item, 69 );
	assert.equal( defined( event ).typeFlags, 0x36c );
	assert.throws( () => e.receive( { opcode: 0x3449, payload: packet.subarray( 0, 7 ) } ), /Invalid external/ );
	e.dispose();
});

test("cold fireworks finish loading after the source leaves and low detail preserves sound cadence", () => {
	const h = harness( 69 );
	for ( let i = 0; i < 40; i++ ) {
		const actors = h.step( i * .5, [], 1 );
		assert.ok( actors.every( a => !a.model.includes( "#" ) ) );
	}
	assert.equal( h.sounds.length, 16 );
	h.owner.dispose();
});

test("last firework emission returns before the empty-host removal branch, including suppressed visuals", () => {
	const record = named[items[63].codename];
	const h = harness( 63, 0x36c, { ...record, stages: record.stages.slice( 0, 1 ) } );
	let at = 0, actors = [];
	for ( let i = 0; i < 30 && !h.sounds.length; i++ ) {
		at = i * .01;
		actors = h.step( at, [], 1 );
	}
	assert.ok( h.sounds.length );
	assert.ok( actors.some( a => a.model.endsWith( ".glb" ) ), "85448F returns with the host still registered" );
	assert.deepEqual( h.step( at + .01, [], 1 ), [], "854490 can remove the empty host on the following update" );
	h.owner.dispose();
});
for (
	const [flags, key] of [
		[ 0x1ec, -2147483644 ],
		[ 0x8ec, -2147483647 ],
		[ 0x10ec, -2147483646 ],
		[ 0x18ec, -2147483645 ],
		[ 0x20ec, -2147483613 ],
		[ 0x28ec, -2147483612 ],
		[ 0x48ec, -2147483611 ],
		[ 0x96c, "PARAM_CURE_ALL" ],
		[ 0x316c, "PARAM_CURE_ALL" ],
		[ 0x396c, "STATUS_CURE_COS" ]
	]
) {
	test( "item-use built-in/cure branch " + flags.toString( 16 ), () => {
		const h = harness( 63, flags );
		let actors = [];
		for ( let i = 0; i < 5; i++ ) actors = h.step( i * .01 );
		const rec = typeof key === "number" ? catalog[key] : named[key];
		// Missing version-specific built-ins fall through to the named item record.
		const expected = (rec ?? named[items[63].codename]).stages.find( s => s.phase === "ACT_S" && s.resource )
			?.resource;
		assert.ok( actors.some( a => decodeURIComponent( a.model ).includes( expected ) ), expected );
		h.owner.dispose();
	} );
}

test("real Return Scroll admits its authored particles after cold loading and retires the tail", () => {
	const h = harness( 61, 0x9ec );
	h.ready( false );
	for ( let i = 0; i < 10; i++ ) assert.deepEqual( h.step( i ), [] );
	h.ready( true );
	let actors = [];
	for ( let i = 0; i < 5; i++ ) actors = h.step( 10 + i * .01 );
	assert.ok( actors.some( a => decodeURIComponent( a.model ).includes( "system/item_returnscroll.efp" ) ) );
	assert.deepEqual( h.step( 100 ), [] );
	h.owner.dispose();
});

/*
===========================================================================

management.test.mjs - tests for social.ts, skill-catalog.ts, gameplay.ts,
training.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { emptySocial, socialPacket, socialRequest } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { skillCatalog, skillTrainingReason, trainingRequest, masteryTrainingReason } = await import(
	"../../src/engine/foundation/gameplay/skill-catalog.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createTraining } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/training/training.ts"
);
const u32 = n => {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
};
const str = s => [ Buffer.byteLength( s ), 0, ...Buffer.from( s ) ];
const frame = ( opcode, p ) => ({ opcode, payload: Uint8Array.from( p ) });
const partyRow = (
	id,
	name
) => [ 0x37, ...u32( id ), ...str( name ), ...u32( 1907 ), 20, 0x9a, 1, 1, 10, 0, 0, 0, 20, 0, ...u32( 0 ) ];
const guildRow = (
	id,
	name
) => [
	...u32( id ),
	...str( name ),
	0,
	20,
	...u32( 10 ),
	...u32( 0xffffffff ),
	...u32( 0 ),
	...u32( 0 ),
	...u32( 0 ),
	...str( "Title" ),
	...u32( 1907 ),
	0,
	0
];
const guildBody = [
	...u32( 7 ),
	...str( "Guild" ),
	1,
	...u32( 100 ),
	...str( "Notice" ),
	...str( "Contents" ),
	...u32( 0 ),
	0,
	2,
	...guildRow( 1, "Me" ),
	...guildRow( 2, "Other" ),
	0
];
const req = [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ], prereq = [ ...req, { ID: 0, Level: 0 } ];
const row = ( id, level ) => ({
	id,
	group: 10,
	level,
	status: false,
	effectRider: false,
	ui: {
		name: "Authored skill",
		spCost: 3,
		trainable: true,
		targetRequired: false,
		cooldownMs: 50,
		masteries: req,
		prerequisites: prereq
	}
});
test("training uses authority metadata, native wire and ordered levels", () => {
	const catalog = skillCatalog( { refSkillSnapshot: [ row( 7, 1 ), row( 8, 2 ) ] } ),
		progression = { masteries: [], skillPoints: 3 };
	assert.equal( skillTrainingReason( catalog[0], [], catalog, progression ), null );
	assert.match( skillTrainingReason( catalog[1], [], catalog, progression ), /preceding/ );
	assert.equal( skillTrainingReason( catalog[1], [ 7 ], catalog, progression ), null );
	assert.match( skillTrainingReason( catalog[0], [], catalog, { ...progression, skillPoints: 2 } ), /Insufficient/ );
	assert.deepEqual( [ ...trainingRequest( "mastery-train", 258 ).payload ], [ 2, 1, 0, 0, 1 ] );
	assert.deepEqual( [ ...trainingRequest( "skill-train", 0x12345678 ).payload ], [ 0x78, 0x56, 0x34, 0x12 ] );
	assert.throws( () => skillCatalog( { refSkillSnapshot: [ row( 7, 1 ), row( 7, 1 ) ] } ) );
	assert.throws( () =>
		skillCatalog( { refSkillSnapshot: [ { ...row( 7, 1 ), ui: { ...row( 7, 1 ).ui, spCost: -1 } } ] } )
	);
});
test("training transport failure, timeout and unrelated receipts cannot spend twice", () => {
	let fail = true;
	const frames = [],
		owner = createTraining( f => {
			if ( fail ) throw Error( "closed" );
			frames.push( f );
		} );
	assert.throws( () => owner.request( trainingRequest( "skill-train", 7 ), 7, 0 ), /closed/ );
	assert.equal( owner.state().trainingPending, false );
	fail = false;
	owner.request( trainingRequest( "skill-train", 7 ), 7, 0 );
	owner.receipt( 0xb165, 7 );
	owner.receipt( 0xb2cb, 8 );
	assert.equal( owner.state().trainingPending, true );
	assert.throws( () => owner.request( trainingRequest( "skill-train", 7 ), 7, 1 ), /pending/ );
	assert.equal( frames.length, 1 );
	owner.step( 10000 );
	assert.match( owner.state().trainingError, /unknown/ );
	owner.receipt( 0xb2cb, 7 );
	assert.equal( owner.state().trainingPending, false );
	owner.reset();
	owner.dispose();
});

test("racial mastery identity, both mastery slots, stats, prerequisites and native mastery ceiling gate training", () => {
	const catalog = skillCatalog( { refSkillSnapshot: [ row( 7, 1 ), row( 8, 2 ) ] } ),
		base = { ...catalog[0], masteries: [ { ID: 257, Level: 0 }, { ID: 258, Level: 2 } ] };
	const progression = {
		level: 121,
		skillPoints: 100,
		masteries: [ { id: 258, level: 2 } ],
		stats: { strength: 20, intellect: 20 }
	};
	assert.match(
		skillTrainingReason( base, [], catalog, progression ),
		/Mastery/,
		"missing racial mastery is not mastery level zero"
	);
	const valid = { ...progression, masteries: [ { id: 257, level: 0 }, { id: 258, level: 2 } ] };
	assert.equal( skillTrainingReason( base, [], catalog, valid ), null );
	assert.match( skillTrainingReason( { ...base, reqStr: 21 }, [], catalog, valid ), /Strength/ );
	assert.match( skillTrainingReason( { ...base, reqInt: 21 }, [], catalog, valid ), /Intelligence/ );
	assert.match(
		skillTrainingReason( { ...base, prerequisites: [ { ID: 10, Level: 2 } ] }, [], catalog, valid ),
		/Prerequisite/
	);
	assert.equal( masteryTrainingReason( 257, { ...valid, skillPoints: 0 }, {} ), null, "zero to one is free" );
	assert.match(
		masteryTrainingReason( 257, { ...valid, masteries: [ { id: 257, level: 120 } ] }, { 120: 0 } ),
		/level/i
	);
});
test("learned upgrades migrate hotbar once and self-target metadata overrides selected monster", () => {
	const frames = [], g = createGameplay( f => frames.push( f ) );
	g.bootstrap( {
		character: {
			skills: [ 7 ],
			quickSlots: [ 0, 1, 40, 41, 50 ].map( slot => ({ slot, kind: 0x49, payload: 7 }) ),
			skillPoints: 3
		},
		refSkillSnapshot: [ row( 7, 1 ), row( 8, 2 ) ]
	} );
	g.worldReady();
	g.seed( { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 } );
	g.command( { kind: "skill-train", id: 8 }, 0 );
	assert.deepEqual( defined( g.take() ).skills, [ 7 ] );
	g.receive( frame( 0xb2cb, [ 1, ...u32( 8 ) ] ), 1 );
	let s = g.take();
	assert.deepEqual( defined( s ).skills, [ 8 ] );
	assert.equal( defined( defined( s ).quickSlots )[0].payload, 8 );
	assert.equal( defined( s ).trainingPending, false );
	assert.deepEqual(
		frames.filter( f => f.opcode === 0x7541 ).map( f => [ ...f.payload ] ),
		[ 0, 1, 40, 41, 50 ].map( slot => [ 1, slot, 0x49, ...u32( 8 ) ] ),
		"native 5731F0 persists every upgraded hotbar reference"
	);
	g.receive( frame( 0xb2cb, [ 1, ...u32( 8 ) ] ), 2 );
	assert.deepEqual( defined( g.take() ).skills, [ 8 ] );
	assert.equal( frames.filter( f => f.opcode === 0x7541 ).length, 5, "duplicate receipt must not repeat saves" );
	g.command( { kind: "skill", skillId: 8, gid: 2 }, 3, { gid: 2, kind: "monster" } );
	assert.equal( frames.at( -1 ).payload.length, 7 );
	assert.equal( frames.at( -1 ).payload[6], 0 );
	g.resetWorld();
	assert.deepEqual( defined( g.take() ).skills, [ 8 ] );
	g.reset();
	assert.deepEqual( defined( g.take() ).skillCatalog, [] );
	g.dispose();
});
test("party invitation, authoritative membership and self leave use distinct identities", () => {
	let state = emptySocial( "Me" );
	state = socialPacket( state, frame( 0x3393, [ 2, ...u32( 100001 ), 3 ] ) );
	assert.deepEqual( [ ...socialRequest( state, { kind: "social-consent", accept: false } ).payload ], [ 2, 12 ] );
	state = socialPacket( state, frame( 0xb0d5, [ 1, ...u32( 11 ) ] ) );
	state = socialPacket(
		state,
		frame( 0x35d6, [ 3, ...u32( 11 ), 0, 2, ...partyRow( 11, "Me" ), ...partyRow( 22, "Other" ) ] )
	);
	assert.equal( state.members[1].name, "Other" );
	assert.deepEqual( [ ...socialRequest( state, { kind: "party-kick", id: 22 } ).payload ], [ 22, 0, 0, 0 ] );
	assert.throws( () => socialRequest( state, { kind: "party-kick", id: 11 } ) );
	const leave = frame( 0x3e58, [ 3, ...u32( 11 ), 2 ] );
	state = socialPacket( state, leave );
	assert.equal( state.leader, 0 );
	assert.deepEqual( state.members, [] );
});
test("guild baseline and deltas commit only after complete validated frames", () => {
	const initial = emptySocial( "Me" ), f = frame( 0x32c4, guildBody );
	for ( let n = 0; n < f.payload.length; n++ ) {
		assert.throws( () => socialPacket( initial, { ...f, payload: f.payload.subarray( 0, n ) } ) );
	}
	let state = socialPacket( initial, f );
	assert.equal( initial.guild, null );
	assert.equal( defined( defined( state ).guild ).name, "Guild" );
	const update = frame( 0x3b29, [ 5, 0x18, ...u32( 900 ), ...str( "New" ), ...str( "Body" ) ] );
	assert.throws( () => socialPacket( state, { ...update, payload: Uint8Array.from( [ ...update.payload, 0 ] ) } ) );
	assert.equal( defined( defined( state ).guild ).gp, 100 );
	state = socialPacket( state, update );
	assert.equal( defined( defined( state ).guild ).gp, 900 );
	assert.equal( defined( defined( state ).guild ).subject, "New" );
	state = socialPacket( state, frame( 0x3b29, [ 6, ...u32( 2 ), 0x68, ...u32( 500 ), ...str( "Officer" ), 16 ] ) );
	assert.equal( defined( defined( state ).guild ).members[1].name, "Officer" );
	assert.equal( defined( defined( state ).guild ).members[1].grant, "Title" );
	assert.equal( defined( defined( state ).guild ).members[1].donated, 500 );
	// 5EA0E4/5EA0E9: mask 0x20 targets member+4; B2BC's acknowledgement targets grant+0x40.
	state = socialPacket( state, frame( 0xb2bc, [ 1, ...u32( 7 ), ...u32( 2 ), ...str( "Commander" ) ] ) );
	assert.equal( defined( defined( state ).guild ).members[1].grant, "Commander" );
	assert.equal( defined( defined( state ).guild ).members[1].name, "Officer" );
	state = socialPacket( state, frame( 0x3b29, [ 3, ...u32( 1 ), 2 ] ) );
	assert.equal( defined( state ).guild, null );
});
test("guild and party consent share accept bytes but have different decline bytes", () => {
	const state = socialPacket( emptySocial(), frame( 0x3393, [ 5, ...u32( 2 ) ] ) );
	assert.deepEqual( [ ...socialRequest( state, { kind: "social-consent", accept: true } ).payload ], [ 1, 1 ] );
	assert.deepEqual( [ ...socialRequest( state, { kind: "social-consent", accept: false } ).payload ], [ 2, 22 ] );
	assert.throws( () => socialRequest( emptySocial(), { kind: "social-consent", accept: true } ) );
});

test("movement projections retain static metadata and social state without repeated worker cloning", async () => {
	const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
	const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );
	const core = createWorldCore( () => {} ), presentation = createPresentation();
	const flush = () => {
		const batch = core.take();
		assert.ok( batch );
		core.ack( batch.sequence );
		presentation.apply( batch );
		return batch.events.filter( e => e.kind === "gameplay" ).at( -1 )?.state;
	};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } },
		character: { name: "Me", skills: [], quickSlots: [] },
		refSkillSnapshot: [ { ...row( 7, 1 ), token: false, status: false } ]
	} );
	core.step( 0, false );
	let state = flush();
	assert.equal( defined( defined( state ).skillCatalog ).length, 1 );
	const retained = defined( presentation.gameplay() ).skillCatalog;
	core.receive( frame( 0x30b3, [ 2, ...u32( 7 ), 0 ] ), 1 );
	core.step( 1, false );
	state = flush();
	assert.equal( Object.hasOwn( state, "skillCatalog" ), false );
	assert.equal( Object.hasOwn( state, "social" ), false );
	assert.equal( defined( presentation.gameplay() ).skillCatalog, retained );
	assert.equal( defined( defined( presentation.gameplay() ).progression ).skillPoints, 7 );
	core.clear();
	core.step( 2, false );
	state = flush();
	assert.deepEqual( defined( state ).skillCatalog, [] );
	assert.deepEqual( defined( presentation.gameplay() ).skillCatalog, [] );
	core.dispose();
	presentation.dispose();
});

const { createSkillTrainingCache } = await import( "../../src/engine/runtime/ui/hud/skill-training.ts" );
test("training index retains stable publications and invalidates learned, catalog, and reset independently", () => {
	const cache = createSkillTrainingCache(),
		catalog = skillCatalog( { refSkillSnapshot: [ row( 7, 1 ), row( 8, 2 ) ] } ),
		learned = [];
	const first = cache.read( catalog, learned ),
		progression = { skillPoints: 100, masteries: [ { id: 257, level: 20 } ] };
	assert.equal( first.skill( undefined ), undefined );
	assert.equal( first.skill( 8 ), catalog[1] );
	assert.equal( first.hasGroupLevel( 999, 0 ), false );
	for ( let i = 0; i < 100; i++ ) assert.equal( cache.read( catalog, learned ), first );
	for ( let i = 0; i < 100; i++ ) {
		assert.equal(
			cache.read( catalog, structuredClone( learned ) ),
			first,
			"worker copies of unchanged learned values retain the index"
		);
	}
	assert.match( first.reason( catalog[1], progression ), /preceding/ );
	const upgraded = cache.read( catalog, [ 7 ] );
	assert.notEqual( upgraded, first );
	assert.equal( upgraded.reason( catalog[1], progression ), null );
	assert.equal( cache.read( catalog, structuredClone( [ 7 ] ) ), upgraded );
	const replaced = cache.read( catalog, [ 8 ] );
	assert.notEqual( replaced, upgraded );
	assert.equal( replaced.learned( 7 ), false );
	assert.equal( replaced.learned( 8 ), true, "same-length skill replacement invalidates" );
	assert.notEqual( cache.read( catalog, [] ), replaced, "skill removal invalidates" );
	assert.match(
		upgraded.reason( catalog[1], { ...progression, skillPoints: 0 } ),
		/Insufficient/,
		"SP changes are read directly, not cached as eligibility"
	);
	const replacement = catalog.map( r => ({ ...r, group: r.group + 1 }) );
	assert.equal( cache.read( replacement, [ 7 ] ).skill( 8 ), replacement[1] );
	assert.equal( cache.read().skill( 8 ), undefined, "world teardown cannot retain stale metadata" );
	cache.reset();
	assert.notEqual( cache.read( catalog, learned ), first );
});

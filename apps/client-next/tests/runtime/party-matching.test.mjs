/*
===========================================================================

party-matching.test.mjs - tests for gameplay.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const source = "src/engine/foundation/gameplay/party-matching.ts";

const {
	emptyPartyMatching,
	partyMatchRequest,
	partyMatchPacket,
	partyMatchRows,
	partyMatchButtons,
	partyAutoCandidates,
	partyActiveJob,
	partyPurposeAllowed,
	partyDefaultPurpose
} = await import( sourceFileUrl( source ).href );
test("party list atomically replaces populated and empty pages with native string encodings", () => {
	const name = Buffer.from( "Owner" ), title = Buffer.from( "Hunting", "utf16le" ), header = Buffer.alloc( 10 );
	header.writeUInt32LE( 42 );
	header.writeUInt32LE( 99, 4 );
	header.writeUInt16LE( name.length, 8 );
	const size = Buffer.alloc( 2 );
	size.writeUInt16LE( title.length / 2 );
	const payload = Buffer.concat( [
		Buffer.from( [ 1, 1, 2, 1 ] ),
		header,
		name,
		Buffer.from( [ 0, 3, 2, 0, 1, 20 ] ),
		size,
		title
	] );
	const pending = partyMatchRequest( emptyPartyMatching(), { kind: "party-match-page", page: 1 } );
	assert.equal( pending.frame.opcode, 0x7588 );
	const state = partyMatchPacket( pending.state, { opcode: 0xb588, payload } );
	assert.equal( state.pending, null );
	assert.equal( state.rows[0].title, "Hunting" );
	assert.equal( state.rows[0].name, "Owner" );
	assert.throws( () => partyMatchPacket( state, { opcode: 0xb588, payload: payload.subarray( 0, -1 ) } ), /string/ );
	assert.equal( state.rows.length, 1 );
	const join = partyMatchRequest( state, { kind: "party-match-join", id: 42 } );
	assert.deepEqual( [ ...join.frame.payload ], [ 42, 0, 0, 0 ] );
	assert.equal( partyMatchPacket( join.state, { opcode: 0xb588, payload: Uint8Array.of( 2, 7 ) } ).pending, "join" );
	assert.equal( partyMatchPacket( join.state, { opcode: 0xb5bf, payload: Uint8Array.of( 2, 7 ) } ).pending, null );
	assert.equal( partyMatchPacket( state, { opcode: 0xb588, payload: Uint8Array.of( 1, 1, 1, 0 ) } ).rows.length, 0 );
});

const reg = { party: 0, type: 3, purpose: 0, min: 1, max: 90, title: "Party \u4e2d" };
function ack( op, id = 42, r = reg ) {
	const p = Buffer.alloc( 15 + r.title.length * 2 );
	p[0] = 1;
	p.writeUInt32LE( id, 1 );
	p.writeUInt32LE( r.party, 5 );
	p.set( [ r.type, r.purpose, r.min, r.max ], 9 );
	p.writeUInt16LE( r.title.length, 13 );
	p.write( r.title, 15, "utf16le" );
	return { opcode: op, payload: p };
}
test("matching registration, modification and deletion commit only complete native acknowledgements", () => {
	const start = emptyPartyMatching(),
		request = partyMatchRequest( start, { kind: "party-match-register", registration: reg } );
	assert.equal( request.frame.opcode, 0x76ff );
	assert.deepEqual( [ ...request.frame.payload ], [ ...ack( 0xb6ff, 0 ).payload.subarray( 1 ) ] );
	assert.equal( request.state.own, null );
	for ( let end = 0; end < ack( 0xb6ff ).payload.length; end++ ) {
		assert.throws( () =>
			partyMatchPacket( request.state, { opcode: 0xb6ff, payload: ack( 0xb6ff ).payload.subarray( 0, end ) } )
		);
	}
	const registered = partyMatchPacket( request.state, ack( 0xb6ff ) );
	assert.equal( registered.own.title, reg.title );
	assert.equal( registered.pending, null );
	assert.throws(
		() => partyMatchRequest( registered, { kind: "party-match-register", registration: reg } ),
		/ownership/
	);
	const modify = partyMatchRequest( registered, {
		kind: "party-match-modify",
		registration: { ...reg, title: "Other" }
	} );
	assert.equal( modify.frame.opcode, 0x73dc );
	assert.equal( new DataView( modify.frame.payload.buffer ).getUint32( 0, true ), 42 );
	const refusal = partyMatchPacket( modify.state, { opcode: 0xb3dc, payload: Uint8Array.of( 2, 2 ) } );
	assert.equal( refusal.pending, null );
	assert.equal( refusal.own.title, reg.title );
	const deletion = partyMatchRequest( refusal, { kind: "party-match-delete" } );
	assert.equal( deletion.frame.opcode, 0x7535 );
	const removed = partyMatchPacket( deletion.state, { opcode: 0xb535, payload: Uint8Array.of( 1, 42, 0, 0, 0 ) } );
	assert.equal( removed.own, null );
});
function notify() {
	const p = Buffer.alloc( 21 + 1 + 4 + 2 + 4 + 4 + 1 );
	p.writeUInt32LE( 123 );
	p.writeUInt32LE( 42, 4 );
	p[21] = 0x13;
	p.writeUInt32LE( 77, 22 );
	p.writeUInt16LE( 4, 26 );
	p.write( "Peer", 28 );
	p.writeUInt32LE( 1907, 32 );
	p[36] = 30;
	return { opcode: 0x75bf, payload: p };
}
test("owner join request shares the roster decoder and replies with exact correlation and native 10s expiry", () => {
	const start = emptyPartyMatching(), frame = notify(), state = partyMatchPacket( start, frame, "", 500 );
	assert.equal( state.request.member.name, "Peer" );
	assert.equal( state.request.expires, 10500 );
	for ( let end = 0; end < frame.payload.length; end++ ) {
		assert.throws( () => partyMatchPacket( start, { ...frame, payload: frame.payload.subarray( 0, end ) } ) );
	}
	for ( const answer of [ 0, 1, 2 ] ) {
		const reply = partyMatchRequest( state, { kind: "party-match-answer", a: 123, b: 42, answer } );
		assert.equal( reply.frame.opcode, 0x30fa );
		assert.deepEqual( [ ...reply.frame.payload ], [ 123, 0, 0, 0, 42, 0, 0, 0, answer ] );
		assert.equal( reply.state.request, null );
		assert.throws(
			() => partyMatchRequest( reply.state, { kind: "party-match-answer", a: 123, b: 42, answer } ),
			/Stale/
		);
	}
	assert.throws(
		() => partyMatchRequest( state, { kind: "party-match-answer", a: 124, b: 42, answer: 1 } ),
		/Stale/
	);
});
test("native local search uses exact names, overlapping ranges; auto-match limits ten in displayed order", () => {
	const rows = Array.from(
		{ length: 20 },
		( _, i ) => ({ ...reg, id: i + 1, name: i ? "Peer" : "PeerTwo", members: 1, race: i % 2, min: 20, max: 40 })
	);
	assert.equal( partyMatchRows( rows, { name: "Peer", purpose: 4, min: 40, max: 50 } ).length, 19 );
	assert.equal( partyMatchRows( rows, { name: "peer", purpose: 4, min: 1, max: 90 } ).length, 0 );
	assert.equal( partyMatchRows( rows, { name: "", purpose: 4, min: 41, max: 90 } ).length, 0 );
	const ordered = partyMatchRows( rows, { name: "", purpose: 0, min: 1, max: 90 }, "id", true );
	assert.deepEqual( partyAutoCandidates( ordered, 0, 2, 3, 30 ), [ 20, 19, 18, 17, 16, 15, 14, 13, 12, 11 ] );
	assert.equal( partyAutoCandidates( rows, 0, 2, 7, 30 ).length, 0 );
	const pending = partyMatchRequest( { ...emptyPartyMatching(), rows }, {
		kind: "party-match-auto",
		ids: [ 20, 19 ]
	} );
	assert.deepEqual( [ ...pending.frame.payload ], [ 20, 0, 0, 0 ] );
	assert.deepEqual( pending.state.auto, [ 19 ] );
	assert.deepEqual( partyMatchPacket( pending.state, { opcode: 0xb5bf, payload: Uint8Array.of( 1, 1 ) } ).auto, [] );
	assert.deepEqual( partyMatchPacket( pending.state, { opcode: 0xb5bf, payload: Uint8Array.of( 1, 2 ) } ).auto, [
		19
	] );
	const stopped = partyMatchRequest( pending.state, { kind: "party-match-auto-stop" } );
	assert.equal( stopped.frame, null );
	assert.equal( stopped.state.pending, "join" );
	assert.deepEqual( stopped.state.auto, [] );
});

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
test("gameplay owns the ten-second no-reply deadline and reset retires it", () => {
	const frames = [], g = createGameplay( f => frames.push( f ) );
	g.bootstrap( { character: { name: "Owner" } } );
	g.seed( { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 } );
	g.receive( notify(), 500 );
	g.step( 10499 );
	assert.equal( frames.length, 0 );
	g.step( 10500 );
	assert.equal( frames.length, 1 );
	assert.equal( frames[0].opcode, 0x30fa );
	assert.equal( frames[0].payload[8], 2 );
	g.step( 20500 );
	assert.equal( frames.length, 1 );
	g.receive( notify(), 30000 );
	g.reset();
	g.step( 50000 );
	assert.equal( frames.length, 1 );
	g.dispose();
});
test("complete member mask retains guild and opaque native extension fields in wire order", () => {
	const frame = notify(),
		tail = Buffer.from( [ 5, 0, ...Buffer.from( "Guild" ), 9, 1, 0, 0, 0, 2, 0, 0, 0 ] ),
		p = Buffer.concat( [ frame.payload, tail ] );
	p[21] |= 0xc8;
	const state = partyMatchPacket( emptyPartyMatching(), { ...frame, payload: p } );
	assert.equal( state.request.member.guild, "Guild" );
	assert.equal( state.request.member.native41, 9 );
	assert.equal( state.request.member.native50, 1 );
	assert.equal( state.request.member.native54, 2 );
	for ( let i = frame.payload.length; i < p.length; i++ ) {
		assert.throws( () => partyMatchPacket( emptyPartyMatching(), { ...frame, payload: p.subarray( 0, i ) } ) );
	}
});

test("join notify mastery words are d/e, never the opaque c word or trailing flags", () => {
	const frame = notify();
	frame.payload.writeUInt32LE( 1234, 8 );
	frame.payload.writeUInt32LE( 258, 12 );
	frame.payload.writeUInt32LE( 259, 16 );
	frame.payload[20] = 4;
	const r = partyMatchPacket( emptyPartyMatching(), frame ).request;
	assert.deepEqual( [ r.native7c0, r.primary, r.secondary, r.flags ], [ 1234, 258, 259, 4 ] );
});

test("native active job uses the equipped suit, exhaustively across 16-bit type words", () => {
	for ( let flags = 0; flags < 65536; flags++ ) {
		let expected = 4;
		for ( let job = 1; job <= 3; job++ ) {
			if ( flags === 0x3ac + (job << 11) || flags === 0x3ad + (job << 11) ) expected = job;
		}
		assert.equal( partyActiveJob( [ { slot: 8, typeFlags: flags } ] ), expected, flags.toString( 16 ) );
		assert.equal( partyActiveJob( [ { slot: 13, typeFlags: flags } ] ), 4 );
	}
});
test("registration eligibility covers every job/purpose byte and valid defaults", () => {
	for ( let job = 0; job < 256; job++ ) {
		for ( let purpose = 0; purpose < 256; purpose++ ) {
			const expected = job === 4 && (purpose === 0 || purpose === 1) ||
				(job === 1 || job === 3) && purpose === 2 || job === 2 && purpose === 3;
			assert.equal( partyPurposeAllowed( job, purpose ), expected, `${job}/${purpose}` );
		}
	}
	for ( const purpose of [ -1, 0.5, NaN, Infinity ] ) assert.equal( partyPurposeAllowed( 4, purpose ), false );
	for ( let job = 1; job <= 4; job++ ) assert.ok( partyPurposeAllowed( job, partyDefaultPurpose( job ) ) );
});

test("a new listing shows at once and survives pages that do not contain it", () => {
	// Reported: after forming, the listing appeared only after a manual
	// refresh, and the leader could not delete it from another page.
	const request = partyMatchRequest( emptyPartyMatching(), { kind: "party-match-register", registration: reg } );
	const registered = partyMatchPacket( request.state, ack( 0xb6ff ), "Leader", 0, { race: 1, members: 3 } );
	assert.deepEqual( registered.rows[0], {
		id: 42,
		party: 0,
		type: 3,
		purpose: 0,
		min: 1,
		max: 90,
		title: reg.title,
		name: "Leader",
		race: 1,
		members: 3
	} );
	// A page of other parties keeps the own listing: only 0xB535 retires it.
	const paged = partyMatchPacket(
		partyMatchRequest( registered, { kind: "party-match-page", page: 2 } ).state,
		{ opcode: 0xb588, payload: Uint8Array.of( 1, 2, 2, 0 ) },
		"Leader"
	);
	assert.equal( paged.own?.id, 42 );
	assert.equal( partyMatchRequest( paged, { kind: "party-match-delete" } ).frame.opcode, 0x7535 );
});

test("matching buttons follow CIFPartyMatch_RefreshButtons", () => {
	const base = {
		own: null,
		ownName: "",
		localName: "Me",
		inParty: false,
		leader: false,
		members: 1,
		options: 0,
		level: 10,
		rows: 3
	};
	const on = input =>
		Object.entries( partyMatchButtons( { ...base, ...input } ) ).filter( ( [, v] ) => v )
			.map( ( [k] ) => Number( k ) );
	assert.deepEqual( on( {} ), [ 15, 16, 17, 18 ], "solo: join, whisper, auto, form" );
	assert.deepEqual( on( { level: 4 } ), [ 15, 16, 17 ], "form needs level 5 outside a party" );
	assert.deepEqual( on( { inParty: true, members: 2 } ), [ 16 ], "a member cannot form, join or auto" );
	assert.deepEqual( on( { inParty: true, leader: true, members: 2 } ), [ 16, 18 ], "the leader forms" );
	assert.deepEqual( on( { inParty: true, leader: true, members: 4 } ), [ 16 ], "four without EXP share is full" );
	assert.deepEqual( on( { inParty: true, leader: true, members: 4, options: 1 } ), [ 16, 18 ], "eight with it" );
	assert.deepEqual(
		on( { own: { id: 42 }, ownName: "Me" } ),
		[ 16, 19, 20 ],
		"an own listing: modify and delete only"
	);
	assert.deepEqual( on( { rows: 0 } ), [ 18 ], "an empty board offers only form" );
});

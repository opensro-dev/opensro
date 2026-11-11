/*
===========================================================================

party-matching.test.mjs - matching wire state, filters and current-party order

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
	partyMatchAckNotice,
	partyMatchRegisterNotice,
	partyMatchCompletionNotice,
	partyMatchDeleteNotice,
	partyMatchAutoNotice,
	partyMatchAutoStartNotice,
	partyMatchNoResultNotice,
	partyMatchRequest,
	partyMatchPacket,
	partyMatchRows,
	partyMatchButtons,
	partyAutoCandidates,
	partyActiveJob,
	partyIsFull,
	partyPurposeAllowed,
	partyDefaultPurpose,
	partyDefaultTitleKey,
	partyFormTitle,
	PARTY_TITLE_MAX_LENGTH
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
/*
================
ack
================
*/
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
test("a registration window follows the published level cap", () => {
	const wide = { ...reg, max: 110 };
	assert.throws(
		() => partyMatchRequest( emptyPartyMatching(), { kind: "party-match-register", registration: wide } ),
		/Invalid party registration/
	);
	const accepted = partyMatchRequest(
		emptyPartyMatching(),
		{ kind: "party-match-register", registration: wide },
		0,
		110
	);
	assert.equal( accepted.frame.payload[11], 110 );
	assert.throws(
		() =>
			partyMatchRequest(
				emptyPartyMatching(),
				{ kind: "party-match-register", registration: { ...reg, max: 111 } },
				0,
				110
			),
		/Invalid party registration/
	);
});
/*
================
notify
================
*/
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
const { createWorldCore } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/core.ts" ).href
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
	assert.equal( state.request.member.primaryMastery, 1 );
	assert.equal( state.request.member.secondaryMastery, 2 );
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

// The English defaults the shipped catalogue holds (textuisystem.en.json).
const recordDefaults = {
	UIIT_MSG_PARTYMATCH_RECORD_DEFAULT1: "For open hunting on the Silk Road!",
	UIIT_MSG_PARTYMATCH_RECORD_DEFAULT2: "Until the day the Silk Road is all covered with money!",
	UIIT_MSG_PARTYMATCH_RECORD_DEFAULT3: "When will the quests end on the Silk Road?",
	UIIT_MSG_PARTYMATCH_RECORD_DEFAULT4: "No need blood or tears for thieves! Robbery is the only way to go!"
};
const recordCut = value => value.slice( 0, PARTY_TITLE_MAX_LENGTH );
test("the record form title defaults follow the selected purpose", () => {
	// 63BE00 indexes Hunting, Quest, Trader, Thief: the catalogue numbers skip
	// in that order (DEFAULT2 is the trader's, DEFAULT3 the quest's).
	const keys = [
		"UIIT_MSG_PARTYMATCH_RECORD_DEFAULT1",
		"UIIT_MSG_PARTYMATCH_RECORD_DEFAULT3",
		"UIIT_MSG_PARTYMATCH_RECORD_DEFAULT2",
		"UIIT_MSG_PARTYMATCH_RECORD_DEFAULT4"
	];
	keys.forEach( ( key, purpose ) => {
		assert.equal( partyDefaultTitleKey( purpose ), key, String( purpose ) );
		assert.equal(
			partyFormTitle( "", purpose, recordDefaults ),
			recordCut( recordDefaults[key] ),
			String( purpose )
		);
	} );
	// The box holds 50 characters, so a longer localised default is cut, and
	// the cut text still counts as a default when the purpose changes.
	assert.equal( recordCut( recordDefaults.UIIT_MSG_PARTYMATCH_RECORD_DEFAULT4 ).length, PARTY_TITLE_MAX_LENGTH );
	assert.equal(
		partyFormTitle( recordCut( recordDefaults.UIIT_MSG_PARTYMATCH_RECORD_DEFAULT4 ), 0, recordDefaults ),
		recordCut( recordDefaults.UIIT_MSG_PARTYMATCH_RECORD_DEFAULT1 )
	);
	assert.equal(
		partyFormTitle( recordDefaults.UIIT_MSG_PARTYMATCH_RECORD_DEFAULT1, 3, recordDefaults ),
		recordCut( recordDefaults.UIIT_MSG_PARTYMATCH_RECORD_DEFAULT4 )
	);
	// A title the player typed is never replaced.
	assert.equal( partyFormTitle( "My party", 2, recordDefaults ), "My party" );
	// A purpose byte the radio never produces keeps the jobless default.
	assert.equal( partyDefaultTitleKey( 4 ), keys[0] );
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

test("current party stays ahead of sorting and search without changing other rows", () => {
	const rows = [
		{ ...reg, id: 1, name: "Other", members: 1, race: 0 },
		{ ...reg, id: 20, name: "Leader", members: 3, race: 1, purpose: 1, min: 70 },
		{ ...reg, id: 3, name: "Other", members: 1, race: 0 }
	];
	const search = { name: "", purpose: 4, min: 1, max: 90 };
	for ( const key of [ "id", "name", "members", "min", "title" ] ) {
		for ( const descending of [ false, true ] ) {
			const sorted = partyMatchRows( rows, search, key, descending, "Leader" );
			assert.equal( sorted[0].id, 20 );
			assert.equal( sorted.length, 3 );
		}
	}
	const filtered = { name: "Other", purpose: 0, min: 1, max: 20 };
	assert.deepEqual( partyMatchRows( rows, filtered, "id", false, "Leader" ).map( row => row.id ), [ 20, 1, 3 ] );
	assert.deepEqual( partyMatchRows( rows, filtered ).map( row => row.id ), [ 1, 3 ] );
	assert.deepEqual( partyMatchRows( rows, search, "id", true, "Missing" ).map( row => row.id ), [ 20, 3, 1 ] );
	assert.deepEqual( rows.map( row => row.id ), [ 1, 20, 3 ] );
});

test("a join answer raises its native surface per detail (75EBD0)", () => {
	// Detail 1 completes the match: the centre banner (67D030),
	// never the guide log.
	assert.deepEqual( partyMatchAckNotice( 0xb5bf, 1 ), {
		key: "UIIT_MSG_PARTYMATCH_JOIN_COMPLETE_MASTER",
		value: 0,
		banner: true,
		bannerOnly: true
	} );
	// Details 0 and 2 are guide lines at the system type (push 4).
	assert.deepEqual( partyMatchAckNotice( 0xb5bf, 0 ), {
		key: "UIIT_MSG_PARTYERR_CREATE_PARTY_REFUSED",
		value: 0,
		nativeType: 4
	} );
	assert.deepEqual( partyMatchAckNotice( 0xb5bf, 2 ), {
		key: "UIIT_MSG_PARTYMATCH_JOIN_NOREPLY",
		value: 0,
		nativeType: 4
	} );
	// An unlisted detail keeps the no-reply key as before, untyped,
	// and no other opcode raises a line here.
	assert.deepEqual( partyMatchAckNotice( 0xb5bf, 3 ), { key: "UIIT_MSG_PARTYMATCH_JOIN_NOREPLY", value: 0 } );
	assert.equal( partyMatchAckNotice( 0xb588, 1 ), null );
});

test("register refusal names its line in the native order and sends nothing (6356A0)", () => {
	// An existing listing refuses first - listed beats even the
	// partyless level arm below it.
	assert.deepEqual(
		partyMatchRegisterNotice( { listed: true, inParty: false, master: false, partyFull: false, level: 1 } ),
		{ key: "UIIT_MSG_PARTYMATCH_RECORD_ERROR_AGAIN", value: 0, nativeType: 4 }
	);
	assert.deepEqual(
		partyMatchRegisterNotice( { listed: false, inParty: false, master: false, partyFull: false, level: 4 } ),
		{ key: "UIIT_MSG_PARTYMATCH_JOIN_ERROR_LEVEL", value: 0, nativeType: 4 }
	);
	assert.equal(
		partyMatchRegisterNotice( { listed: false, inParty: false, master: false, partyFull: false, level: 5 } ),
		null
	);
	assert.deepEqual(
		partyMatchRegisterNotice( { listed: false, inParty: true, master: false, partyFull: false, level: 40 } ),
		{ key: "UIIT_MSG_PARTYMATCH_JOIN_ERROR_PARTY", value: 0, nativeType: 4 }
	);
	assert.deepEqual(
		partyMatchRegisterNotice( { listed: false, inParty: true, master: true, partyFull: true, level: 40 } ),
		{ key: "UIIT_MSG_PARTYERR_ALREADY_FULL", value: 0, nativeType: 4 }
	);
	assert.equal(
		partyMatchRegisterNotice( { listed: false, inParty: true, master: true, partyFull: false, level: 40 } ),
		null
	);
});

test("register, modify and delete successes close as centre banners", () => {
	assert.deepEqual( partyMatchCompletionNotice( 0xb6ff ), {
		key: "UIIT_MSG_PARTYMATCH_RECORD_COMPLETE",
		value: 0,
		notificationBanner: true,
		bannerOnly: true
	} );
	assert.deepEqual( partyMatchCompletionNotice( 0xb3dc ), {
		key: "UIIT_MSG_PARTYMATCH_MODIFY_COMPLETE",
		value: 0,
		notificationBanner: true,
		bannerOnly: true
	} );
	assert.equal( partyMatchCompletionNotice( 0xb5bf ), null );
	assert.deepEqual( partyMatchDeleteNotice( false ), {
		key: "UIIT_MSG_PARTYMATCH_DELETE_COMPLETE",
		value: 0,
		notificationBanner: true,
		bannerOnly: true
	} );
	assert.deepEqual( partyMatchDeleteNotice( true ), {
		key: "UIIT_MSG_PARTYMATCH_DELETE_COMPLETE2",
		value: 0,
		notificationBanner: true,
		bannerOnly: true
	} );
});

/*
================
partyJoinFrame

One 0x3E58 type-2 JOIN frame carrying one full member row (mask 0x37),
the bytes party-membership.test.mjs pins for the roster lane.
================
*/
function partyJoinFrame( id, name ) {
	const u32 = n => [ n & 255, (n >>> 8) & 255, (n >>> 16) & 255, n >>> 24 ];
	return {
		opcode: 0x3e58,
		payload: Uint8Array.from( [
			2,
			0x37,
			...u32( id ),
			Buffer.byteLength( name ),
			0,
			...Buffer.from( name ),
			...u32( 1907 ),
			20,
			0x9a,
			1,
			1,
			10,
			0,
			0,
			0,
			20,
			0,
			...u32( 0 )
		] )
	};
}

test("the delete banner keeps the COMPLETE2 latch across a relog until a delete answer spends it", () => {
	// Native: the latch is a process-global byte. The roster watcher
	// sets it when the party fills under a standing listing, and only
	// the delete answer reads and clears it - world entry and exit
	// never touch it, so a filled-then-relogged listing still closes
	// with the COMPLETE2 wording.
	const g = createGameplay( () => {} );
	const lastNoticeKey = () => (g.take()?.notices ?? []).at( -1 )?.key;
	const enter = () => {
		g.bootstrap( { academyMember: false } );
		g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	};
	enter();
	g.receive( ack( 0xb6ff ), 0 );
	for ( let i = 0; i < 4; i++ ) g.receive( partyJoinFrame( 100 + i, `Peer${i}` ), 0 );
	g.reset();
	enter();
	g.receive( ack( 0xb6ff ), 0 );
	g.receive( { opcode: 0xb535, payload: Uint8Array.of( 1, 42, 0, 0, 0 ) }, 0 );
	assert.equal( lastNoticeKey(), "UIIT_MSG_PARTYMATCH_DELETE_COMPLETE2" );
	// The answer spent the latch: the next delete closes plainly.
	g.receive( ack( 0xb6ff ), 0 );
	g.receive( { opcode: 0xb535, payload: Uint8Array.of( 1, 42, 0, 0, 0 ) }, 0 );
	assert.equal( lastNoticeKey(), "UIIT_MSG_PARTYMATCH_DELETE_COMPLETE" );
	g.dispose();
});

test("a party that fills under a standing listing withdraws it once (634D30)", () => {
	// 634D30: the roster watcher that arms the COMPLETE2 latch also
	// sends the listing's delete (0x7535, the own record's id) on
	// the fill. A party short of full sends none, and while the
	// fill stands no second delete follows (#3225).
	const sent = [], g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( { character: { name: "Owner" } } );
	g.seed( { gid: 1, regionId: 25000, x: 0, y: 0, z: 0, heading: 0 } );
	g.worldReady();
	g.receive( ack( 0xb6ff ), 0 );
	const deletes = () => sent.filter( frame => frame.opcode === 0x7535 );
	for ( let i = 0; i < 3; i++ ) g.receive( partyJoinFrame( 100 + i, `Peer${i}` ), 0 );
	assert.equal( deletes().length, 0, "a party short of full withdrew its listing" );
	g.receive( partyJoinFrame( 103, "Peer3" ), 0 );
	assert.equal( deletes().length, 1, "the fill sent no delete" );
	assert.deepEqual( [ ...deletes()[0].payload ], [ 42, 0, 0, 0 ] );
	g.receive( partyJoinFrame( 104, "Peer4" ), 0 );
	assert.equal( deletes().length, 1, "the standing fill withdrew twice" );
	g.dispose();
});

test("party fullness follows the native cap for the share option (818820)", () => {
	// Without EXP share the roster caps at four: the register guard's
	// last arm and the delete latch both read this, not a constant 8.
	assert.equal( partyIsFull( 4, 0 ), true );
	assert.equal( partyIsFull( 3, 0 ), false );
	// With EXP share the cap is eight.
	assert.equal( partyIsFull( 8, 1 ), true );
	assert.equal( partyIsFull( 7, 1 ), false );
	assert.equal( partyIsFull( 4, 1 ), false );
	// The query compares the whole option word: any other word is
	// never full, at either count.
	for ( const word of [ 4, 5 ] ) {
		assert.equal( partyIsFull( 4, word ), false );
		assert.equal( partyIsFull( 8, word ), false );
	}
});

test("auto chain reports progress or failure only while its session lives", () => {
	assert.deepEqual(
		partyMatchAutoNotice( { sessionOpen: true, accepted: false, candidatesRemain: true } ),
		{ key: "UIIT_MSG_PARTYMATCH_AUTO_PROGRESS", value: 0, nativeType: 4 }
	);
	assert.deepEqual(
		partyMatchAutoNotice( { sessionOpen: true, accepted: false, candidatesRemain: false } ),
		{ key: "UIIT_MSG_PARTYMATCH_AUTO_FAILURE", value: 0, nativeType: 4 }
	);
	assert.equal( partyMatchAutoNotice( { sessionOpen: true, accepted: true, candidatesRemain: false } ), null );
	assert.equal( partyMatchAutoNotice( { sessionOpen: false, accepted: false, candidatesRemain: true } ), null );
	// A sent round opens with the progress line before any answer
	// (63B383); only an empty round stays on the no-result line.
	assert.deepEqual( partyMatchAutoStartNotice(), {
		key: "UIIT_MSG_PARTYMATCH_AUTO_PROGRESS",
		value: 0,
		nativeType: 4
	} );
	assert.deepEqual( partyMatchNoResultNotice(), {
		key: "UIIT_MSG_PARTYMATCH_PSEARCH_NORESULT",
		value: 0,
		nativeType: 4
	} );
});

/*
================
autoMatchWorld

A world whose party board the 0xB588 answer fills with the given
listing ids (page 1 of 1), ready for an auto-match command.
================
*/
function autoMatchWorld( t, ids ) {
	const sent = [], core = createWorldCore( frame => sent.push( frame ) );
	t.after( () => core.dispose() );
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		core.step( 0, false );
		const batch = core.take();
		if ( batch ) core.ack( batch.sequence );
		return batch?.events ?? [];
	};
	flush();
	core.command( { kind: "party-match-page", page: 1 }, 0 );
	const rows = [];
	for ( const id of ids ) {
		const header = Buffer.alloc( 10 );
		header.writeUInt32LE( id );
		header.writeUInt32LE( 99, 4 );
		header.writeUInt16LE( 4, 8 );
		const size = Buffer.alloc( 2 );
		size.writeUInt16LE( 5 );
		rows.push( Buffer.concat( [
			header,
			Buffer.from( "Peer" ),
			Buffer.from( [ 0, 3, 2, 0, 1, 20 ] ),
			size,
			Buffer.from( "Title", "utf16le" )
		] ) );
	}
	core.receive(
		{ opcode: 0xb588, payload: Buffer.concat( [ Buffer.from( [ 1, 1, 1, ids.length ] ), ...rows ] ) },
		0
	);
	flush();
	const gameplay = events => {
		const event = events.find( row => row.kind === "gameplay" );
		assert.ok( event?.kind === "gameplay", "the world publishes gameplay" );
		return event.state;
	};
	return { core, sent, flush, gameplay };
}

test("a sent auto round prints progress at once; an empty one prints no-result", t => {
	const world = autoMatchWorld( t, [ 7 ] );
	world.core.command( { kind: "party-match-auto", ids: [ 7 ] }, 0 );
	assert.ok( world.sent.some( frame => frame.opcode === 0x75bf ), "the first candidate was not sent" );
	// No join answer has arrived: the line is already printed (63B383).
	const notices = world.gameplay( world.flush() ).notices ?? [];
	assert.deepEqual(
		{ key: notices.at( -1 )?.key, nativeType: notices.at( -1 )?.nativeType },
		{ key: "UIIT_MSG_PARTYMATCH_AUTO_PROGRESS", nativeType: 4 }
	);
	const empty = autoMatchWorld( t, [] );
	empty.core.command( { kind: "party-match-auto", ids: [] }, 0 );
	assert.ok( !empty.sent.some( frame => frame.opcode === 0x75bf ), "an empty round sent a candidate" );
	const emptyNotices = empty.gameplay( empty.flush() ).notices ?? [];
	assert.equal( emptyNotices.at( -1 )?.key, "UIIT_MSG_PARTYMATCH_PSEARCH_NORESULT" );
	assert.ok( !emptyNotices.some( notice => notice.key === "UIIT_MSG_PARTYMATCH_AUTO_PROGRESS" ) );
});

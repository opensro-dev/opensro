/*
===========================================================================

npc-conversation.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";
async function load( file ) {
	return import( sourceFileUrl( "src/engine/" + file ).href );
}
const { createNpcConversation } = await load( "runtime/simulation/worker/session/world/gameplay/npc/npc.ts" );
const { decodeNpcDialogue } = await load( "foundation/gameplay/npc-dialogue.ts" );
const { serverNotification } = await load( "foundation/gameplay/system-notices.ts" );
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
function packet( kind, prompt, options = [] ) {
	const string = s => {
		const p = Buffer.from( s ), n = Buffer.alloc( 2 );
		n.writeUInt16LE( p.length );
		return Buffer.concat( [ n, p ] );
	};
	return {
		opcode: 0x3773,
		payload: Buffer.concat( [
			Buffer.from( [ kind ] ),
			string( prompt ),
			...(kind === 4 ? [ Buffer.from( [ options.length ] ), ...options.map( string ) ] : [])
		] )
	};
}

test("NPC wire grammar, exact choices and malformed admission are atomic", () => {
	const frame = packet( 4, "SN_PROMPT", [ "SN_Q1", "SN_TALK_COMMON_MAXQUEST", "SN_Q2" ] );
	assert.deepEqual( decodeNpcDialogue( frame.payload ).options.map( x => x.choice ), [ 5, 7, 8 ] );
	assert.deepEqual( decodeNpcDialogue( packet( 3, "PROMPT" ).payload ).options.map( x => x.choice ), [ 2, 3 ] );
	assert.deepEqual( decodeNpcDialogue( packet( 1, "PROMPT" ).payload ).options.map( x => x.choice ), [ 1 ] );
	assert.throws( () => decodeNpcDialogue( packet( 6, "PROMPT" ).payload ), /Unsupported/ );
	// Kind 5: the kind-4 rows, then a wide item codename and a u32 count
	// that format into the prompt as %s and %d (763C10 / 5D60C0).
	const narrow = s => {
		const p = Buffer.from( s ), n = Buffer.alloc( 2 );
		n.writeUInt16LE( p.length );
		return Buffer.concat( [ n, p ] );
	};
	const wide = s => {
		const n = Buffer.alloc( 2 );
		n.writeUInt16LE( s.length );
		return Buffer.concat( [ n, Buffer.from( s, "utf16le" ) ] );
	};
	const count60 = Buffer.alloc( 4 );
	count60.writeUInt32LE( 60 );
	const kind5Payload = Buffer.concat( [
		Buffer.from( [ 5 ] ),
		narrow( "GIVE %s FOR %d" ),
		Buffer.from( [ 1 ] ),
		narrow( "SN_TALK_COMMON_EXIT" ),
		wide( "ITEM_ETC_BOX" ),
		count60
	] );
	const decoded5 = decodeNpcDialogue( kind5Payload );
	assert.equal( decoded5.kind, 5 );
	assert.equal( decoded5.prompt, "GIVE %s FOR %d" );
	assert.deepEqual( decoded5.options.map( x => x.choice ), [ 5 ] );
	assert.deepEqual( decoded5.item, { codename: "ITEM_ETC_BOX", count: 60 } );
	assert.throws( () => decodeNpcDialogue( Buffer.concat( [ kind5Payload, Buffer.of( 0 ) ] ) ), /trailing/ );
	assert.throws( () => decodeNpcDialogue( kind5Payload.subarray( 0, kind5Payload.length - 1 ) ), /Truncated/ );
	const owner = createNpcConversation( () => {} );
	owner.select( 7 );
	owner.talk( 0 );
	const before = owner.state();
	for ( let n = 0; n < frame.payload.length; n++ ) {
		assert.throws( () => owner.receive( { ...frame, payload: frame.payload.subarray( 0, n ) } ) );
		assert.deepEqual( owner.state(), before );
	}
	assert.throws( () => owner.receive( { ...frame, payload: Buffer.concat( [ frame.payload, Buffer.of( 0 ) ] ) } ) );
	assert.deepEqual( owner.state(), before );
	owner.receive( frame );
	assert.equal( owner.state().phase, "ready" );
});

test("NPC conversation sequence model preserves one in-flight request and rejects retired replies", () => {
	fc.assert(
		fc.property(
			fc.array( fc.constantFrom( "select", "talk", "reply", "choose", "tick", "close" ), {
				minLength: 1,
				maxLength: 100
			} ),
			events => {
				let phase = "closed", sent = 0;
				const owner = createNpcConversation( () => sent++ ), reply = packet( 3, "SN_PROMPT" );
				for ( const event of events ) {
					const before = owner.state(), oldSent = sent;
					switch ( event ) {
						case "select":
							owner.select( 7 );
							phase = "menu";
							break;
						case "talk":
							if ( phase === "menu" || phase === "ready" ) {
								owner.talk( 0 );
								phase = "waiting";
								assert.equal( sent, oldSent + 1 );
							} else {
								assert.throws( () => owner.talk( 0 ) );
								assert.equal( sent, oldSent );
								assert.deepEqual( owner.state(), before );
							}
							break;
						case "choose":
							if ( phase === "ready" ) {
								owner.choose( 2, 0 );
								phase = "waiting";
								assert.equal( sent, oldSent + 1 );
							} else {
								assert.throws( () => owner.choose( 2, 0 ) );
								assert.equal( sent, oldSent );
							}
							break;
						case "reply":
							owner.receive( reply );
							if ( phase === "waiting" || phase === "uncertain" ) phase = "ready";
							break;
						case "tick":
							owner.step( 10000 );
							if ( phase === "waiting" ) phase = "uncertain";
							break;
						case "close":
							owner.clear();
							phase = "closed";
							break;
					}
					assert.equal( owner.state().phase, phase );
				}
			}
		),
		{ numRuns: 300, seed: 3773 }
	);
});

test("production gameplay connects NPC selection, acceptance, close/reopen and despawn", () => {
	const frames = [], game = createGameplay( f => frames.push( f ) );
	game.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	const npc = { gid: 7, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
	function select() {
		game.command( { kind: "select", gid: 7 }, 0, npc );
		game.receive( { opcode: 0xb45a, payload: Buffer.from( "0107000000000200000000", "hex" ) }, 1 );
		assert.equal( game.take().npcConversation.phase, "menu" );
	}
	select();
	game.command( { kind: "npc-talk" }, 2 );
	assert.equal( frames.at( -1 ).opcode, 0x7338 );
	assert.equal( Buffer.from( frames.at( -1 ).payload ).toString( "hex" ), "0700000002000000" );
	game.receive( packet( 4, "SN_PROMPT", [ "SN_QUEST" ] ), 3 );
	game.command( { kind: "npc-choice", choice: 5 }, 4 );
	assert.deepEqual( [ ...frames.at( -1 ).payload ], [ 5 ] );
	game.receive( packet( 3, "SN_OFFER" ), 5 );
	game.command( { kind: "npc-choice", choice: 2 }, 6 );
	game.receive( { opcode: 0x31ed, payload: Buffer.from( "010500000000000802", "hex" ) }, 7 );
	assert.equal( game.take().npcConversation.phase, "waiting", "quest insert cannot invent dialogue success" );
	game.receive( packet( 1, "SN_ACCEPTED" ), 8 );
	assert.equal( game.take().quests[0].refId, 5 );
	game.command( { kind: "npc-close" }, 9 );
	assert.equal( frames.at( -1 ).opcode, 0x74b3 );
	assert.equal( game.take().npcConversation.phase, "closed" );
	game.receive( packet( 3, "LATE" ), 10 );
	assert.equal( game.take().npcConversation.phase, "closed" );
	assert.throws( () => game.command( { kind: "select", gid: 7 }, 11, npc ), /pending/ );
	game.receive( { opcode: 0xb4b3, payload: Buffer.of( 1 ) }, 12 );
	select();
	game.entityLifecycle( { kind: "despawn", gid: 7 } );
	assert.equal( game.take().npcConversation.phase, "closed" );
	game.receive( packet( 3, "AFTER_REMOVAL" ), 13 );
	assert.equal( game.take().npcConversation.phase, "closed" );
	game.dispose();
});

test("failed NPC send leaves the existing dialogue actionable", () => {
	const owner = createNpcConversation( () => {
		throw Error( "closed transport" );
	} );
	owner.select( 1 );
	assert.throws( () => owner.talk( 0 ), /closed transport/ );
	assert.equal( owner.state().phase, "menu" );
});

test("informational Confirm waits for server close and retires target and conversation together", () => {
	const frames = [], game = createGameplay( f => frames.push( f ) );
	game.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	const npc = { gid: 7, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
	game.command( { kind: "select", gid: 7 }, 0, npc );
	game.receive( { opcode: 0xb45a, payload: Buffer.from( "0107000000000200000000", "hex" ) }, 1 );
	game.command( { kind: "npc-talk" }, 2 );
	game.receive( packet( 1, "SN_BASE" ), 3 );
	game.command( { kind: "npc-choice", choice: 1 }, 4 );
	assert.equal( frames.at( -1 ).opcode, 0x3773 );
	assert.equal( game.take().npcConversation.phase, "waiting" );
	game.receive( { opcode: 0xb4b3, payload: Buffer.of( 1 ) }, 5 );
	const state = game.take();
	assert.equal( state.npcConversation.phase, "closed" );
	assert.equal( state.target, 0 );
	game.receive( packet( 1, "LATE" ), 6 );
	assert.equal( game.take().npcConversation.phase, "closed" );
	game.dispose();
});

// Finite capability/lifecycle matrix: independent recall bit; affirmative send,
// no optimistic notice, success/failure grammar, and retired target rejection.
test("recall appointment uses granted capability and publishes only authoritative success", () => {
	for ( const flags of [ 0, 2, 0x40, 0x80, 0xc0 ] ) {
		const frames = [],
			game = createGameplay( f => frames.push( f ) ),
			npc = { gid: 7, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
		game.seed( { ...npc, gid: 1 } );
		game.command( { kind: "select", gid: 7 }, 0, npc );
		const grant = Buffer.alloc( 11 );
		grant[0] = 1;
		grant.writeUInt32LE( 7, 1 );
		grant.writeUInt32LE( flags, 6 );
		game.receive( { opcode: 0xb45a, payload: grant }, 1 );
		game.take();
		frames.length = 0;
		game.command( { kind: "recall-appoint", gid: 7 }, 2 );
		assert.equal( frames.length, flags & 0x40 ? 1 : 0 );
		if ( flags & 0x40 ) {
			assert.equal( frames[0].opcode, 0x720d );
			assert.deepEqual( [ ...frames[0].payload ], [ 7, 0, 0, 0 ] );
		}
		assert.equal( game.take()?.notices?.length ?? 0, 0 );
		assert.equal( game.receive( { opcode: 0xb20d, payload: Uint8Array.of( 1 ) }, 3 ), true );
		const notice = game.take().notices.at( -1 );
		assert.equal( notice.nativeType, 5 );
		assert.equal( notice.banner, true );
		assert.equal( notice.key, "UIIT_MSG_STATE_REBIRTH_POINT_APPOINT" );
		game.receive( { opcode: 0xb20d, payload: Uint8Array.of( 2, 4 ) }, 4 );
		assert.equal( game.take().notices.length, 1 );
		for ( const payload of [ [], [ 1, 0 ], [ 2 ], [ 2, 4, 0 ] ] ) {
			assert.throws(
				() => game.receive( { opcode: 0xb20d, payload: Uint8Array.from( payload ) }, 5 ),
				/appointment response/
			);
		}
		game.entityLifecycle( { kind: "despawn", gid: 7 } );
		frames.length = 0;
		game.command( { kind: "recall-appoint", gid: 7 }, 6 );
		assert.equal( frames.length, 0 );
		game.dispose();
	}
});

test("So-ok helmet and gift clicks at 0, 1, 59 and 60 boxes show the price or the result", () => {
	// The exact frames the server emits for one So-ok row click: a kind-4
	// closing answer (prompt line, one Exit row) and, except for the gift
	// click on an empty bag, a channel-7 top banner. Short of 60 boxes the
	// banner names the price and the holding; at the price it names what
	// was spent; every gift click answers with its line. Nothing is silent.
	const narrow = s => {
		const p = Buffer.from( s ), n = Buffer.alloc( 2 );
		n.writeUInt16LE( p.length );
		return Buffer.concat( [ n, p ] );
	};
	const answer = symbol =>
		Buffer.concat( [
			Buffer.from( [ 4 ] ),
			narrow( symbol ),
			Buffer.from( [ 1 ] ),
			narrow( "SN_TALK_COMMON_EXIT" )
		] );
	const banner = text =>
		Buffer.concat( [
			Buffer.from( [ 7, text.length, 0 ] ),
			Buffer.from( text, "utf16le" )
		] );
	const NO_BOX = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_E";
	const GRANTED = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_C";
	const cases = [
		{
			boxes: 0,
			helmet: NO_BOX,
			helmetBanner: "A helmet costs 60 Jewel Boxes. You have 0.",
			gift: NO_BOX,
			giftBanner: ""
		},
		{
			boxes: 1,
			helmet: NO_BOX,
			helmetBanner: "A helmet costs 60 Jewel Boxes. You have 1.",
			gift: GRANTED,
			giftBanner: "Exchanged your Jewel Boxes: 1 gift(s) received."
		},
		{
			boxes: 59,
			helmet: NO_BOX,
			helmetBanner: "A helmet costs 60 Jewel Boxes. You have 59.",
			gift: GRANTED,
			giftBanner: "Exchanged your Jewel Boxes: 59 gift(s) received."
		},
		{
			boxes: 60,
			helmet: GRANTED,
			helmetBanner: "Exchanged 60 Jewel Boxes for a helmet.",
			gift: GRANTED,
			giftBanner: "Exchanged your Jewel Boxes: 60 gift(s) received."
		}
	];
	const assertAnswer = ( symbol, bannerText ) => {
		const owner = createNpcConversation( () => {} );
		owner.select( 7 );
		owner.talk( 0 );
		const payload = answer( symbol );
		owner.receive( { opcode: 0x3773, payload } );
		assert.equal( owner.state().phase, "ready" );
		const dialogue = decodeNpcDialogue( payload );
		assert.equal( dialogue.prompt, symbol );
		assert.deepEqual( dialogue.options.map( x => x.choice ), [ 5 ] );
		if ( bannerText ) {
			const notice = serverNotification( 0x3667, banner( bannerText ) );
			assert.equal( notice?.text, bannerText );
			assert.equal( notice?.notificationBanner, true );
		}
	};
	for ( const row of cases ) {
		assertAnswer( row.helmet, row.helmetBanner );
		assertAnswer( row.gift, row.giftBanner );
	}
});

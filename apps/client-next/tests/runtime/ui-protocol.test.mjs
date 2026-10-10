/*
===========================================================================

ui-protocol.test.mjs - tests for chat.ts, quests.ts, quest.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" ),
	{ createQuests } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/quests/quests.ts"
	),
	{ decodeQuest } = await import( "../../src/engine/foundation/gameplay/quest.ts" );

test("login quest owner preserves every published reference and resets stale requests without a supported-ID list", async () => {
	const catalog = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf8" ) );
	const ids = [ ...new Set( catalog.rows.map( line => Number( line.split( "\t" )[0] ) ) ) ];
	assert.ok( ids.length > 200, "exercise the full published catalog, not starter fixtures" );
	// These are serialization fixtures, not claims about each quest script. The
	// login adapter must preserve unimplemented IDs and every optional wire field.
	const activeQuests = ids.map( refId => ({
		refId,
		u08: 2,
		u09: 1,
		flags: 0x5c,
		progress: 123,
		u10: 2,
		contents: [ {
			tag: 1,
			kind: 1,
			description: "PERSISTENCE_FIXTURE",
			objectiveSentinel: false,
			objectiveValues: [ 7, 9 ]
		}, { tag: 2, kind: 2, description: "PERSISTENCE_SENTINEL", objectiveSentinel: true, objectiveValues: [] } ],
		targetIds: [ 1000 + refId ]
	}) );
	const snapshot = () => JSON.parse( JSON.stringify( { character: { activeQuests, completedQuestIds: ids } } ) );
	const q = createQuests( () => {} );
	q.bootstrap( snapshot() );
	assert.deepEqual( q.state().quests, activeQuests );
	assert.deepEqual( q.state().completedQuests, ids );
	q.request( ids.at( -1 ), true );
	q.clear();
	q.bootstrap( snapshot() );
	assert.equal( q.state().questPending, 0 );
	assert.deepEqual( q.state().quests, activeQuests );
	assert.deepEqual( q.state().completedQuests, ids );
	q.receive( { opcode: 0xb29a, payload: Uint8Array.of( 2, 0 ) } );
	assert.deepEqual( q.state().quests, activeQuests, "retired acknowledgement cannot remove restored objectives" );
	const before = q.state(), invalid = snapshot();
	invalid.character.activeQuests.at( -1 ).targetIds = [ -1 ];
	assert.throws( () => q.bootstrap( invalid ) );
	assert.deepEqual( q.state(), before, "bad final row cannot partially replace login state" );
});
test("chat uses UTF-16 units and native keyed acknowledgements without optimistic echo", () => {
	const sent = [], chat = createChat( f => sent.push( f ) );
	chat.bootstrap( { character: { name: "Me" } } );
	chat.request( 2, "\u4f60\u597d", "Friend", 0 );
	assert.equal( sent[0].opcode, 0x7367 );
	assert.equal( Buffer.from( sent[0].payload ).toString( "hex" ), "02ff0600467269656e640200604f7d59" );
	assert.equal( chat.state().lines.length, 0 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().pending, true );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 2, 255 ) }, 1 );
	assert.equal( chat.state().lines[0].text, "\u4f60\u597d" );
	assert.equal( chat.state().pending, false );
	chat.request( 1, "next", "", 10 );
	assert.equal( chat.step( 10010 ), true );
	assert.equal( chat.step( 10011 ), false );
	// The deadline releases the slot (chat.ts): an unanswered line no longer
	// locks chat; it warns and the next line goes out.
	assert.equal( chat.state().pending, false );
	assert.match( chat.state().error, /not confirmed/ );
	chat.request( 1, "after", "", 10012 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().error, null );
	assert.equal( chat.state().pending, false );
	chat.clear();
	assert.equal( chat.state().lines.length, 0 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 0 );
});
test("chat broadcasts reject truncated packets atomically, bound history and suppress own echoes", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Me" } } );
	const p = Buffer.from( "0102000000020068006900", "hex" );
	for ( let n = 0; n < p.length; n++ ) {
		assert.throws( () => chat.receive( { opcode: 0x3667, payload: p.subarray( 0, n ) }, 1 ) );
	}
	assert.equal( chat.state().lines.length, 0 );
	for ( let i = 0; i < 150; i++ ) chat.receive( { opcode: 0x3667, payload: p }, 1 );
	assert.equal( chat.state().lines.length, 128 );
	assert.equal( chat.state().lines[0].text, "hi" );
	chat.receive( { opcode: 0x3667, payload: p }, 2 );
	assert.equal( chat.state().lines.length, 128 );
	assert.throws( () => chat.request( 1, "a".repeat( 101 ), "", 0 ) );
	assert.throws( () => chat.request( 2, "hi", "", 0 ) );
});
test("quest wire flags, sentinel, transaction confirmation and malformed atomicity", () => {
	const sent = [], quests = createQuests( f => sent.push( f ) );
	quests.bootstrap( { character: { activeQuests: [] } } );
	// insert ref 7, u08/u09, flags progress+reward+contents+targets; one objective sentinel.
	const p = Buffer.from( "010700000000005c030000000201010002006869ff0109000000", "hex" );
	const row = decodeQuest( p ).record;
	assert.equal( defined( row ).progress, 3 );
	assert.equal( defined( row ).u10, 2 );
	assert.equal( defined( row ).contents[0].objectiveSentinel, true );
	assert.deepEqual( defined( row ).targetIds, [ 9 ] );
	for ( let n = 0; n < p.length; n++ ) {
		assert.throws( () => quests.receive( { opcode: 0x31ed, payload: p.subarray( 0, n ) } ) );
	}
	assert.equal( quests.state().quests.length, 0 );
	quests.receive( { opcode: 0x31ed, payload: p } );
	quests.request( 7, true );
	assert.equal( sent[0].opcode, 0x729a );
	assert.equal( quests.state().quests.length, 1 );
	assert.equal( quests.state().questPending, 7 );
	quests.receive( { opcode: 0x31ed, payload: Buffer.from( "0307000000", "hex" ) } );
	assert.equal( quests.state().quests.length, 0 );
	assert.equal( quests.state().questPending, 0 );
	assert.throws(
		() => quests.receive( { opcode: 0x31ed, payload: Buffer.concat( [ Buffer.from( [ 2 ] ), p.subarray( 1 ) ] ) } ),
		/without insertion/
	);
	quests.bootstrap( { character: { activeQuests: [ row ] } } );
	assert.equal( quests.state().quests.length, 1 );
	quests.clear();
	assert.equal( quests.state().quests.length, 0 );
});

test("quest transactions settle only on matching refusal or terminal delta, never progress or sound ack", () => {
	// Exhaust the two operations and all native refusal bytes. No request ID exists
	// in refusals, so one reliable operation must remain in flight until settled.
	for ( const reward of [ false, true ] ) {
		for ( let error = 0; error < 256; error++ ) {
			const sent = [], q = createQuests( frame => sent.push( frame ) );
			const insert = Buffer.from( "010700000000000802", "hex" );
			q.receive( { opcode: 0x31ed, payload: insert } );
			q.request( 7, reward );
			const ack = reward ? 0xb29a : 0xb1eb, opposite = reward ? 0xb1eb : 0xb29a;
			q.receive( { opcode: opposite, payload: Uint8Array.of( 2, error ) } );
			q.receive( { opcode: ack, payload: Uint8Array.of( 1, 7, 0, 0, 0 ) } );
			q.receive( { opcode: ack, payload: Uint8Array.of( 3 ) } );
			q.receive( { opcode: 0x31ed, payload: Buffer.concat( [ Buffer.from( [ 2 ] ), insert.subarray( 1 ) ] ) } );
			q.receive( { opcode: 0x31ed, payload: Uint8Array.of( 4, 8, 0, 0, 0 ) } );
			assert.equal( q.state().questPending, 7 );
			assert.throws( () => q.request( 7, reward ), /pending/ );
			const before = q.state();
			for ( const payload of [ [], [ 2 ], [ 2, error, 0 ], [ 1, 7, 0, 0 ], [ 1, 7, 0, 0, 0, 0 ], [ 3, 0 ] ] ) {
				assert.throws( () => q.receive( { opcode: ack, payload: Uint8Array.from( payload ) } ) );
				assert.deepEqual( q.state(), before );
			}
			assert.equal( q.receive( { opcode: ack, payload: Uint8Array.of( 2, error ) } ), true );
			assert.equal( q.state().questPending, 0 );
			assert.deepEqual( q.state().quests, before.quests );
			// Native 75c1d0 sends both terminal operations through history append,
			// even for an unrelated ID. That must not settle the pending operation.
			assert.deepEqual( q.state().completedQuests, [ 8 ] );
			q.request( 7, reward );
			assert.equal( sent.length, 2 );
			q.receive( { opcode: 0x31ed, payload: Uint8Array.of( reward ? 3 : 4, 7, 0, 0, 0 ) } );
			assert.equal( q.state().questPending, 0 );
			assert.deepEqual( q.state().quests, [] );
			assert.deepEqual( q.state().completedQuests, [ 8, 7 ] );
		}
	}
});

test("quest reset and failed send leave no stale transaction", () => {
	let fail = true;
	const q = createQuests( () => {
		if ( fail ) throw Error( "closed" );
	} );
	const bootstrap = { character: { activeQuests: [ { refId: 7, u08: 0, u09: 0, flags: 8, u10: 2 } ] } };
	q.bootstrap( bootstrap );
	assert.throws( () => q.request( 7, true ), /closed/ );
	assert.equal( q.state().questPending, 0 );
	fail = false;
	q.request( 7, true );
	q.clear();
	q.receive( { opcode: 0xb29a, payload: Uint8Array.of( 2, 0 ) } );
	assert.equal( q.state().questPending, 0 );
	assert.deepEqual( q.state().quests, [] );
	q.bootstrap( bootstrap );
	q.request( 7, true );
	q.bootstrap( bootstrap );
	q.request( 7, false );
	q.receive( { opcode: 0xb29a, payload: Uint8Array.of( 2, 0 ) } );
	assert.equal( q.state().questPending, 7 );
	q.receive( { opcode: 0xb1eb, payload: Uint8Array.of( 2, 0 ) } );
	assert.equal( q.state().questPending, 0 );
});

test("quest NPC marker packets and entry registry have atomic admission and independent lifecycle", async () => {
	const { createQuests } = await import(
		sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/quests/quests.ts" ).href
	);
	const q = createQuests( () => {} ), p = Buffer.from( "07000000020301010100020003000a000000", "hex" );
	for ( let n = 0; n < p.length; n++ ) {
		assert.throws( () => q.receive( { opcode: 0x3498, payload: p.subarray( 0, n ) } ) );
	}
	assert.deepEqual( q.state().questMarkers, [] );
	q.receive( { opcode: 0x3498, payload: p } );
	assert.deepEqual( q.state().questMarkers, [ {
		refId: 7,
		flags: 2,
		valueA: 3,
		word: 257,
		tail6: [ 1, 0, 2, 0, 3, 0 ],
		optional: 10
	} ] );
	p[5] = 2;
	q.receive( { opcode: 0x3498, payload: p } );
	assert.equal( q.state().questMarkers.length, 1 );
	assert.equal( q.state().questMarkers[0].valueA, 2 );
	const before = q.state().questMarkers;
	assert.throws( () => q.bootstrap( { character: { trackedQuests: [ { refId: 1 } ] } } ) );
	assert.equal( q.state().questMarkers, before );
	q.bootstrap( { character: { trackedQuests: before } } );
	assert.deepEqual( q.state().questMarkers, before );
	q.receive( { opcode: 0x30ea, payload: Uint8Array.of( 7, 0, 0, 0 ) } );
	assert.deepEqual( q.state().questMarkers, [] );
	q.receive( { opcode: 0x3498, payload: p } );
	q.clear();
	assert.deepEqual( q.state().questMarkers, [] );
});

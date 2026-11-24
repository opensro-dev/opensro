/*
===========================================================================

quest-progress.test.mjs - tests for gameplay.ts, quests.ts,
quest-presentation.ts, quest-banner.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createQuests } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/quests/quests.ts"
);
const { questProgressText } = await import( "../../src/engine/foundation/ui/quest-presentation.ts" );
const { createQuestBanner } = await import( "../../src/engine/runtime/ui/hud/quest-banner.ts" );
const { createNoticeBanner } = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
const fixture = JSON.parse( readFileSync( "../server/internal/game/quest/graesp_wire_fixture.json", "utf8" ) );
const c = ( n, overrides = {} ) => ({
	tag: 1,
	kind: 1,
	description: "counter",
	objectiveSentinel: false,
	objectiveValues: [ n ],
	...overrides
});
const row = ( contents = [ c( 0 ) ], overrides = {} ) => ({
	refId: 150,
	u08: 17,
	u09: 0,
	flags: 24,
	u10: 1,
	contents,
	targetIds: [],
	...overrides
});
function packet( r, op = 2 ) {
	const bytes = [],
		u8 = n => bytes.push( n ),
		u32 = n => {
			const b = Buffer.alloc( 4 );
			b.writeUInt32LE( n );
			bytes.push( ...b );
		};
	u8( op );
	u32( r.refId );
	if ( op <= 2 ) {
		u8( r.u08 );
		u8( r.u09 );
		u8( r.flags );
		if ( r.flags & 4 ) u32( r.progress );
		if ( r.flags & 8 ) u8( r.u10 );
		if ( r.flags & 16 ) {
			u8( r.contents.length );
			for ( const c of r.contents ) {
				u8( c.tag );
				u8( c.kind );
				const s = Buffer.from( c.description );
				u8( s.length & 255 );
				u8( s.length >> 8 );
				bytes.push( ...s );
				u8( c.objectiveSentinel ? 255 : c.objectiveValues.length );
				if ( !c.objectiveSentinel ) c.objectiveValues.forEach( u32 );
			}
		}
		if ( r.flags & 64 ) {
			u8( r.targetIds.length );
			r.targetIds.forEach( u32 );
		}
	}
	return { opcode: 0x31ed, payload: Uint8Array.from( bytes ) };
}

test("identical captions retain separate tagged missions through partial update and reconnect", () => {
	const q = createQuests( () => {} );
	q.receive( packet( row( [ c( 0 ), c( 1, { tag: 2 } ) ] ), 1 ) );
	q.receive( packet( row( [ c( 2, { tag: 2, kind: 2 } ) ] ) ) );
	assert.deepEqual( q.state().quests[0].contents.map( c => [ c.tag, c.objectiveValues[0] ] ), [ [ 1, 0 ], [
		2,
		2
	] ] );
	const saved = JSON.parse( JSON.stringify( q.state().quests ) );
	q.bootstrap( { character: { activeQuests: saved } } );
	assert.deepEqual( q.state().quests[0].contents.map( c => [ c.tag, c.objectiveValues[0] ] ), [ [ 1, 0 ], [
		2,
		2
	] ] );
	assert.deepEqual( q.state().questProgress, [], "reconnect cannot replay progress feedback" );
	q.receive( packet( row( [ c( 1, { kind: 2 } ) ] ) ) );
	assert.deepEqual( q.state().quests[0].contents.map( c => c.objectiveValues[0] ), [ 1, 2 ] );
});

test("server Graesp accept and twenty fatal transitions cross production gameplay and notice lifecycle", () => {
	const game = createGameplay( () => {} ), banner = createQuestBanner( createNoticeBanner() );
	game.bootstrap( { character: { activeQuests: [] } } );
	let state, now = 0;
	for ( const frame of fixture.frames ) {
		game.receive( { opcode: 0x31ed, payload: Buffer.from( frame.payloadHex, "hex" ) }, now );
		state = game.take();
		assert.equal( defined( defined( state ).quests )[0].contents[0].objectiveValues[0], frame.count );
		banner.step( defined( state ).questProgress, now, fixture.text, true );
		assert.deepEqual( defined( state ).notices, [], "native progress is not a status-log entry" );
		if ( frame.count === 0 ) assert.equal( banner.alpha(), 0 );
		else assert.equal( banner.value(), `Hunt 20 Graesps (${frame.count})` );
		now += 100;
	}
	// First completion is transient kind 2; resident/bootstrap state is kind 0.
	assert.equal( banner.value(), "Hunt 20 Graesps (20)" );
	const before = defined( state ).questProgress;
	game.receive( { opcode: 0x31ed, payload: Buffer.from( fixture.frames[20].payloadHex, "hex" ) }, now );
	state = game.take();
	banner.step( defined( state ).questProgress, 9000, fixture.text, true );
	assert.equal( banner.alpha(), 0 );
	assert.equal( defined( before )[0].after.objectiveValues[0], 1 );
	game.bootstrap( { character: { activeQuests: defined( state ).quests } } );
	assert.deepEqual( defined( game.take() ).questProgress, [] );
	game.dispose();
});

test("all native content kind bytes, counter zero and rendered-string comparison discriminate feedback", () => {
	const entries = { counter: "Hunt (%d)", same: "Hunt (%d)", literal: "No placeholders" };
	for ( let kind = 0; kind <= 255; kind++ ) {
		assert.equal(
			questProgressText( { before: c( 0 ), after: c( 1, { kind } ) }, entries ),
			kind === 0 ? null : "Hunt (1)"
		);
	}
	assert.equal( questProgressText( { before: c( 1 ), after: c( 0 ) }, entries ), "Hunt (0)" );
	assert.equal(
		questProgressText( { before: c( 1 ), after: c( 1, { description: "same", kind: 2 } ) }, entries ),
		null
	);
	assert.equal(
		questProgressText(
			{ before: c( 1, { description: "literal" } ), after: c( 2, { description: "literal" } ) },
			entries
		),
		null
	);
	assert.equal( questProgressText( { before: c( 1 ), after: c( 2, { description: "missing" } ) }, entries ), null );
	assert.equal( questProgressText( { before: c( 0 ), after: c( 0xffffffff ) }, entries ), "Hunt (-1)" );
});

test("non-single argument tutorial exception is a prefix match, including no-argument and sentinel branches", () => {
	for (
		const symbol of [
			"CON_QTUTORIAL_EU",
			"CON_QTUTORIAL_EU_STEP",
			"SN_CON_QTUTORIAL_EU",
			"xCON_QTUTORIAL_EU",
			"counter"
		]
	) {
		for ( const values of [ [], [ 2, 3 ] ] ) {
			for ( const sentinel of [ false, true ] ) {
				const after = c( 0, { description: symbol, objectiveValues: values, objectiveSentinel: sentinel } );
				const entries = { counter: "Old", [symbol]: "Next %d %d" };
				const result = questProgressText( { before: c( 0, { description: "absent" } ), after }, entries );
				assert.equal(
					result,
					symbol.startsWith( "CON_QTUTORIAL_EU" ) ?
						(!sentinel && values.length ? "Next 2 3" : "Next %d %d") :
						null
				);
			}
		}
	}
});

test("all delta flags preserve absent fields, merge objective tags and apply native target-list ordering", () => {
	for ( let flags = 0; flags <= 255; flags++ ) {
		const quests = createQuests( () => {} ),
			original = row( [ c( 0 ), c( 8, { tag: 2 } ) ], { flags: 92, progress: 120, u10: 2, targetIds: [ 7 ] } );
		quests.bootstrap( { character: { activeQuests: [ original ] } } );
		const old = quests.state();
		quests.receive( packet( row( [ c( 1 ) ], { flags, progress: 90, u10: 3, targetIds: [ 9 ] } ) ) );
		const next = quests.state().quests[0];
		assert.equal( next.progress, flags & 4 ? 90 : 120 );
		assert.equal( next.u10, flags & 8 ? 3 : 2 );
		assert.deepEqual( next.targetIds, [ ...(flags & 4 ? [ 7 ] : []), ...(flags & 64 ? [ 9 ] : []) ] );
		assert.equal( next.contents[0].objectiveValues[0], flags & 16 ? 1 : 0 );
		assert.equal( next.contents[1].objectiveValues[0], 8 );
		assert.equal( old.quests[0].contents[0].objectiveValues[0], 0 );
	}
});

test("owner commits tag order and detached events atomically; new tags are silent on their first update", () => {
	const q = createQuests( () => {} );
	q.bootstrap( { character: { activeQuests: [ row( [ c( 3, { tag: 3 } ), c( 1 ) ] ) ] } } );
	q.receive( packet( row( [ c( 4, { tag: 3 } ), c( 7, { tag: 2 } ), c( 2 ) ] ) ) );
	const first = q.state();
	assert.deepEqual( first.quests[0].contents.map( c => c.tag ), [ 1, 2, 3 ] );
	assert.deepEqual( first.questProgress.map( e => e.after.tag ), [ 1, 3 ] );
	const full = packet( row( [ c( 5 ) ] ) );
	for ( let i = 0; i < full.payload.length; i++ ) {
		assert.throws( () => q.receive( { ...full, payload: full.payload.subarray( 0, i ) } ) );
	}
	assert.deepEqual( q.state(), first );
	q.receive( full );
	assert.equal( first.questProgress[0].after.objectiveValues[0], 2 );
	for ( const op of [ 3, 4 ] ) {
		q.bootstrap( { character: { activeQuests: [ row() ] } } );
		q.request( 150, false );
		q.receive( packet( row(), op ) );
		assert.deepEqual( q.state().quests, [] );
		assert.deepEqual( q.state().completedQuests, [ 150 ] );
		assert.equal( q.state().questPending, 0 );
		assert.deepEqual( q.state().questProgress, [] );
	}
	q.clear();
	assert.deepEqual( q.state().questProgress, [] );
});

test("a reward pick composes the native pick body and a refused claim keeps its reason", () => {
	const sent = [], q = createQuests( frame => sent.push( frame ) );
	q.bootstrap( { character: { activeQuests: [ row( [ c( 0 ) ], { u10: 2 } ) ] } } );
	q.request( 150, true, [ 10415, 10414 ] );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].opcode, 0x729a );
	assert.deepEqual(
		[ ...sent[0].payload ],
		[ 150, 0, 0, 0, 2, 0xaf, 0x28, 0, 0, 0xae, 0x28, 0, 0 ]
	);
	assert.equal( q.state().questPending, 150 );
	assert.equal( q.state().questRefusal, undefined );
	// The refusal answer ([2, reason]) releases the transaction and keeps
	// the reason for the HUD's error line.
	q.receive( { opcode: 0xb29a, payload: new Uint8Array( [ 2, 1 ] ) } );
	assert.equal( q.state().questPending, 0 );
	assert.deepEqual( q.state().questRefusal, { refId: 150, reason: 1 } );
	// A plain claim stays the bare four bytes, and a refused give-up
	// carries no reward refusal.
	q.request( 150, true );
	assert.deepEqual( [ ...sent[1].payload ], [ 150, 0, 0, 0 ] );
	q.receive( { opcode: 0xb29a, payload: new Uint8Array( [ 1, 150, 0, 0, 0 ] ) } );
	// The success ack alone does not release the transaction; the op-3
	// journal delta does.
	q.receive( packet( row(), 3 ) );
	q.bootstrap( { character: { activeQuests: [ row( [ c( 0 ) ], { u10: 2 } ) ] } } );
	q.request( 150, false );
	assert.equal( sent[2].opcode, 0x71eb );
	assert.equal( q.state().questRefusal, undefined );
	q.receive( { opcode: 0xb1eb, payload: new Uint8Array( [ 2, 0 ] ) } );
	assert.equal( q.state().questRefusal, undefined );
	// Picks on a give-up, a pick past the count byte, and a claim on a
	// non-window quest all refuse before anything is sent.
	assert.throws( () => q.request( 150, false, [ 1 ] ), /reward claim/ );
	assert.throws( () => q.request( 150, true, new Array( 256 ).fill( 1 ) ), /pick budget/ );
	const plain = createQuests( () => {} );
	plain.bootstrap( { character: { activeQuests: [ row() ] } } );
	assert.throws( () => plain.request( 150, true ), /action unavailable/ );
});

test("quest banner waits for localization and chrome, replaces in order, quantizes fade and resets", () => {
	const b = createQuestBanner( createNoticeBanner() ), entries = { counter: "Hunt (%d)" };
	const events = [ { sequence: 1, refId: 150, before: c( 0 ), after: c( 1 ) }, {
		sequence: 2,
		refId: 150,
		before: c( 1 ),
		after: c( 2 )
	} ];
	b.step( events, 0, undefined, true );
	b.step( events, 8000, entries, false );
	assert.equal( b.alpha(), 0 );
	b.step( events, 10000, entries, true );
	assert.equal( b.value(), "Hunt (2)" );
	assert.equal( b.alpha(), 1 );
	b.step( events, 16000, entries, true );
	assert.equal( b.alpha(), 127 / 255 );
	b.step( [ { sequence: 3, refId: 150, before: c( 2 ), after: c( 2 ) } ], 16000, entries, true );
	assert.equal( b.alpha(), 127 / 255, "unchanged event cannot restart lifetime" );
	b.step( [], 17000, entries, true );
	assert.equal( b.alpha(), 0 );
	b.reset();
	assert.equal( b.value(), "" );
	b.step( events, 18000, entries, true );
	assert.equal( b.alpha(), 1 );
});

test("world re-bootstrap clears history without making retained HUD discard new progress", () => {
	const q = createQuests( () => {} ),
		b = createQuestBanner( createNoticeBanner() ),
		entries = { counter: "Hunt (%d)" };
	q.bootstrap( { character: { activeQuests: [ row() ] } } );
	q.receive( packet( row( [ c( 1 ) ] ) ) );
	b.step( q.state().questProgress, 0, entries, true );
	const sequence = defined( q.state().questProgress.at( -1 ) ).sequence;
	q.bootstrap( { character: { activeQuests: q.state().quests } } );
	assert.deepEqual( q.state().questProgress, [] );
	q.receive( packet( row( [ c( 2 ) ] ) ) );
	assert.ok( q.state().questProgress[0].sequence > sequence );
	b.step( q.state().questProgress, 1000, entries, true );
	assert.equal( b.value(), "Hunt (2)" );
});

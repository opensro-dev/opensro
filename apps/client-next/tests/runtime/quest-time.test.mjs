/*
===========================================================================

quest-time.test.mjs - tests for quest-timers.ts, quest-time.ts, gameplay.ts,
quest-banner.ts, ...

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
const { createQuestTimers } = await import( "../../src/engine/runtime/ui/hud/quest-timers.ts" );
const { questDurationText, decrementQuestMinute } = await import( "../../src/engine/foundation/ui/quest-time.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createQuestBanner } = await import( "../../src/engine/runtime/ui/hud/quest-banner.ts" );
const { createNoticeBanner } = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
const { npcInteractionMask } = await import( "../../src/engine/foundation/gameplay/npc-dialogue.ts" );
const { decodeQuestPresentation } = await import( "../../src/engine/foundation/ui/quest-presentation.ts" );
const copyEntries =
	JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) ).entries;
const copy = k => copyEntries[k] ?? "";
const row = ( p, flags = 4 ) => ({
	refId: 19,
	u08: 17,
	u09: 0,
	flags,
	progress: p,
	u10: 1,
	contents: [],
	targetIds: []
});
const pack = ( d, h, m, s = 0 ) => ((d << 10) | (h << 15) | (m << 20) | (s << 26)) >>> 0;

test("packed duration sentinel, zero, hour/day borrowing and reserved bits match native fields", () => {
	assert.equal( questDurationText( undefined, copy ), "No limit" );
	assert.equal( questDurationText( 0xffffffff, copy ), "No limit" );
	assert.equal( questDurationText( 0, copy ), "-- : --" );
	assert.equal( questDurationText( 0, copy, false ), "0Minutes" );
	assert.equal( questDurationText( pack( 2, 3, 4 ), copy ), "51Hours4Minutes" );
	for ( let d = 0; d < 32; d++ ) {
		for ( let h = 0; h < 32; h++ ) {
			for ( let m = 0; m < 64; m++ ) {
				const p = pack( d, h, m, 7 ) | 19, n = decrementQuestMinute( p );
				const expected = m ?
					pack( d, h, m - 1, 7 ) :
					h ?
					pack( d, h - 1, 59, 7 ) :
					d ?
					pack( d - 1, 23, 59, 7 ) :
					pack( 0, 0, 0, 7 );
				assert.equal( n, (expected | 19) >>> 0 );
			}
		}
	}
});

test("ordinary world countdown has no second notices and content deltas do not restart it", () => {
	const timer = createQuestTimers();
	let q = row( pack( 0, 0, 2 ) );
	timer.step( [ q ], 0, 0x10001 );
	q = { ...q, flags: 16 };
	timer.step( [ q ], 59000, 0x10001 );
	assert.deepEqual( timer.step( [ q ], 60000, 0x10001 ).notices, [] );
	assert.equal( timer.text( q, copy ), "1Minutes" );
	assert.deepEqual( timer.step( [ q ], 120000, 0x10001 ).notices, [] );
	assert.equal( timer.text( q, copy ), "0Minutes" );
	q = { ...q, flags: 4, progress: pack( 0, 0, 5 ) };
	timer.step( [ q ], 121000, 0x10001 );
	assert.equal( timer.text( q, copy ), "5Minutes" );
	timer.step( [], 122000, 0x10001 );
	timer.step( [ q ], 200000, 0x10001 );
	assert.equal( timer.text( q, copy ), "5Minutes" );
	timer.reset();
});

test("authored progress-clear metadata changes only the unlimited sentinel and admits through the catalog owner", () => {
	const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf8" ) );
	const at = raw.rows.findIndex( s => s.startsWith( "19\t" ) );
	for ( const byte of [ 0, 1, 255 ] ) {
		const metadata = decodeQuestPresentation( {
			...raw,
			progressClearBytes: raw.progressClearBytes.map( ( v, i ) => i === at ? byte : v )
		} );
		for ( const p of [ undefined, 0xffffffff, pack( 0, 0, 3 ) ] ) {
			const q = row( p ), timer = createQuestTimers();
			timer.step( [ q ], 0, 0x10001, metadata.records );
			assert.equal( timer.text( q, copy ), p === pack( 0, 0, 3 ) ? "3Minutes" : byte ? "-- : --" : "No limit" );
			assert.equal( q.progress, p );
		}
	}
	assert.throws( () => decodeQuestPresentation( { ...raw, progressClearBytes: [] } ) );
});

test("special world emits 60 on minute transition, then 50 through 1 without a zero notice or label repaint", () => {
	for ( const initial of [ 1, 2 ] ) {
		const timer = createQuestTimers(), q = row( pack( 0, 0, initial ) ), notices = [];
		assert.deepEqual( timer.step( [ q ], 0, 7 ).notices, [] );
		let now = 0;
		if ( initial === 2 ) {
			now = 60000;
			notices.push( ...timer.step( [ q ], now, 7 ).notices );
		}
		for ( let i = 0; i < 5; i++ ) {
			now += 10000;
			notices.push( ...timer.step( [ q ], now, 7 ).notices );
		}
		for ( let i = 0; i < 10; i++ ) {
			now += 1000;
			notices.push( ...timer.step( [ q ], now, 7 ).notices );
		}
		assert.deepEqual( notices, [
			...(initial === 2 ? [ 60 ] : []),
			50,
			40,
			30,
			20,
			10,
			9,
			8,
			7,
			6,
			5,
			4,
			3,
			2,
			1
		] );
		assert.equal( timer.text( q, copy ), "1Minutes" );
		assert.equal( timer.step( [ q ], now + 10000, 7 ).changed, false );
	}
	// Native tests the minute field independently of hours, even for 1h1m.
	const timer = createQuestTimers(), q = row( pack( 0, 1, 1 ) );
	timer.step( [ q ], 0, 7 );
	assert.deepEqual( timer.step( [ q ], 10000, 7 ).notices, [ 50 ] );
});

test("unlimited replacement preserves native timer retirement differences; zero cancels every clock phase", () => {
	for ( const world of [ 0x10001, 7 ] ) {
		const timer = createQuestTimers();
		let q = row( pack( 0, 0, 1 ) );
		timer.step( [ q ], 0, world );
		q = { ...q, progress: 0xffffffff };
		timer.step( [ q ], 1000, world );
		assert.equal( timer.text( q, copy ), "No limit" );
		assert.deepEqual( timer.step( [ q ], world === 7 ? 10000 : 60000, world ).notices, world === 7 ? [ 53 ] : [] );
		if ( world === 0x10001 ) assert.equal( timer.step( [ q ], 120000, world ).changed, false );
		q = { ...q, progress: 0 };
		timer.step( [ q ], 130000, world );
		assert.equal( timer.text( q, copy ), "-- : --" );
		assert.equal( timer.step( [ q ], 999999, world ).changed, false );
	}
});

test("server production timer packets enter gameplay, correct the HUD and publish authored expiry through quest chrome", () => {
	const fixture = JSON.parse( readFileSync( "../server/internal/game/quest/timed_quest_wire_fixture.json", "utf8" ) );
	const entries =
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf8" ) ).textEntries;
	const game = createGameplay( () => {} ),
		timer = createQuestTimers(),
		banner = createQuestBanner( createNoticeBanner() );
	game.bootstrap( { character: { activeQuests: [] } } );
	let state;
	for ( const f of fixture.frames ) {
		const now = f.minute * 60000;
		game.receive( { opcode: f.opcode, payload: Buffer.from( f.payloadHex, "hex" ) }, now );
		state = game.take();
		timer.step( defined( state ).quests, now, 0x10001 );
		banner.step( defined( state ).questProgress, now, entries, true, defined( state ).notices, [], copy );
		if ( f.minute < 120 ) {
			const left = 120 - f.minute;
			assert.equal(
				timer.text( defined( defined( state ).quests )[0], copy ),
				(left >= 60 ? Math.floor( left / 60 ) + "Hours" : "") + (left % 60) + "Minutes"
			);
		}
	}
	assert.deepEqual( defined( state ).quests, [] );
	assert.equal( defined( defined( defined( state ).notices ).at( -1 ) ).questBanner, true );
	assert.equal( defined( defined( defined( state ).notices ).at( -1 ) ).bannerOnly, true );
	assert.equal( banner.value(), "You have run out of time (the quest will automatically fail)." );
	game.dispose();
	timer.reset();
	banner.reset();
	assert.equal( banner.alpha(), 0 );
});

test("native interaction acknowledgement gates duplicate quest text; unlocked messages neither deduplicate nor replace the cache", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( { character: { activeQuests: [] } } );
	game.take();
	const say = key => {
		const text = Buffer.from( key ), p = Buffer.alloc( text.length + 2 );
		p.writeUInt16LE( text.length );
		text.copy( p, 2 );
		game.receive( { opcode: 0x36bf, payload: p }, 0 );
		return game.take();
	};
	assert.equal( defined( defined( say( "A" ) ).notices ).length, 1 );
	assert.equal( defined( defined( say( "A" ) ).notices ).length, 2 );
	const ack = mask => {
		const p = Buffer.alloc( mask === 0x800 ? 6 : 5 );
		p[0] = 1;
		p.writeUInt32LE( mask, 1 );
		return p;
	};
	game.receive( { opcode: 0xb338, payload: ack( 2 ) }, 0 );
	game.take();
	assert.equal( defined( defined( say( "A" ) ).notices ).length, 3 );
	assert.equal( say( "A" ), null );
	assert.equal( defined( defined( say( "B" ) ).notices ).length, 4 );
	assert.equal( say( "B" ), null );
	game.command( { kind: "npc-close" }, undefined, 0 );
	game.take();
	assert.equal( defined( defined( say( "A" ) ).notices ).length, 5 );
	game.receive( { opcode: 0xb338, payload: ack( 2 ) }, 0 );
	game.take();
	assert.equal( say( "B" ), null );
	for ( const mask of [ 0, 1, 2, 4, 0x400, 0x800, 0x4000, 0x10000, 0x80000000 ] ) {
		for ( const caps of [ 0, 0x400, 0x1000, 0x1400, 0xffffffff ] ) {
			assert.equal( npcInteractionMask( ack( mask ), caps ), (mask | (caps & 0x1400)) >>> 0 );
		}
	}
	assert.equal( npcInteractionMask( Uint8Array.of( 2, 9 ), 0 ), null );
	for ( const p of [ new Uint8Array(), Uint8Array.of( 1 ), ack( 0x800 ).subarray( 0, 5 ) ] ) {
		assert.throws( () => npcInteractionMask( p, 0 ) );
	}
	game.dispose();
});

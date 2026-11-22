/*
===========================================================================

unique-status.test.mjs - the Unique Status feed and window (port-only)

The worker decodes the 0x3FC4 snapshot and 0x3FC5 updates whole or not
at all, the foundation shapes the window's rows and the notify list, and
the HUD alerts only on state changes it saw happen, only for the saved
list, and only while its Experimental row is on (#3531). Everything here
is port-only, not native.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const {
	createUniqueStatus,
	OP_UNIQUE_STATUS_SNAPSHOT,
	OP_UNIQUE_STATUS_UPDATE
} = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/unique-status/unique-status.ts"
);
const {
	emptyUniqueNotify,
	uniqueNotifyBook,
	uniqueNotifySettings,
	uniqueStatusAgo,
	uniqueStatusAlertText,
	uniqueStatusClock,
	uniqueStatusFlips,
	uniqueStatusRows
} = await import( "../../src/engine/foundation/gameplay/unique-status.ts" );
const { createUniqueStatusHud } = await import( "../../src/engine/runtime/ui/hud/unique-status-hud.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

const TIGER_GIRL = 1954;
const URUCHI = 1977;
const ISYUTARU = 2002;

const ALIVE_A = {
	refObjId: TIGER_GIRL,
	name: "Tiger Girl",
	level: 20,
	regionId: 25000,
	alive: true,
	diedAtMs: 0,
	killer: ""
};
const DEAD_A = { ...ALIVE_A, alive: false, diedAtMs: 1_700_000_000_000, killer: "Ann" };
const ALIVE_B = { refObjId: URUCHI, name: "Uruchi", level: 24, regionId: 25242, alive: true, diedAtMs: 0, killer: "" };
const DEAD_B = { ...ALIVE_B, alive: false, diedAtMs: 1_700_000_100_000, killer: "" };
const DEAD_C = {
	refObjId: ISYUTARU,
	name: "Isyutaru",
	level: 45,
	regionId: 31337,
	alive: false,
	diedAtMs: 1_700_000_200_000,
	killer: "Bo"
};

/*
================
rowBytes

One row in the server's wire layout (wire uniquestatus.go): u32 ref,
u8 name length + name, u8 level, u16 region, u8 alive, u64 diedAtMs,
u8 killer length + killer.
================
*/
function rowBytes( row ) {
	const name = Buffer.from( row.name, "utf-8" ), killer = Buffer.from( row.killer, "utf-8" );
	const body = Buffer.alloc( 4 + 1 + name.length + 1 + 2 + 1 + 8 + 1 + killer.length );
	let at = 0;
	body.writeUInt32LE( row.refObjId, at );
	at += 4;
	body[at++] = name.length;
	name.copy( body, at );
	at += name.length;
	body[at++] = row.level;
	body.writeUInt16LE( row.regionId, at );
	at += 2;
	body[at++] = row.alive ? 1 : 0;
	body.writeBigUInt64LE( BigInt( row.diedAtMs ), at );
	at += 8;
	body[at++] = killer.length;
	killer.copy( body, at );
	return body;
}

/*
================
snapshotFrame
================
*/
function snapshotFrame( nowMs, rows ) {
	const head = Buffer.alloc( 10 );
	head.writeBigUInt64LE( BigInt( nowMs ), 0 );
	head.writeUInt16LE( rows.length, 8 );
	return {
		opcode: OP_UNIQUE_STATUS_SNAPSHOT,
		payload: new Uint8Array( Buffer.concat( [ head, ...rows.map( rowBytes ) ] ) )
	};
}

/*
================
updateFrame
================
*/
function updateFrame( nowMs, row ) {
	const head = Buffer.alloc( 8 );
	head.writeBigUInt64LE( BigInt( nowMs ), 0 );
	return {
		opcode: OP_UNIQUE_STATUS_UPDATE,
		payload: new Uint8Array( Buffer.concat( [ head, rowBytes( row ) ] ) )
	};
}

test("the worker decodes a snapshot whole and applies updates", () => {
	const owner = createUniqueStatus();
	assert.equal( owner.receive( { opcode: 0x3fc3, payload: new Uint8Array( 2 ) } ), false );
	assert.equal( owner.current(), null );

	assert.equal( owner.receive( snapshotFrame( 1_700_000_300_000, [ ALIVE_A, DEAD_C ] ) ), true );
	assert.deepEqual( owner.current(), {
		serverNowMs: 1_700_000_300_000,
		rows: [ ALIVE_A, DEAD_C ],
		revision: 1
	} );

	// An update replaces its row and keeps the list ordered by reference.
	assert.equal( owner.receive( updateFrame( 1_700_000_400_000, DEAD_A ) ), true );
	assert.deepEqual( owner.current()?.rows, [ DEAD_A, DEAD_C ] );
	assert.equal( owner.current()?.revision, 2 );

	// A first-learned row joins the list; a later snapshot replaces it wholesale.
	assert.equal( owner.receive( updateFrame( 1_700_000_500_000, ALIVE_B ) ), true );
	assert.deepEqual(
		owner.current()?.rows.map( ( row ) => row.refObjId ),
		[ TIGER_GIRL, URUCHI, ISYUTARU ]
	);
	assert.equal( owner.receive( snapshotFrame( 1_700_000_600_000, [ ALIVE_B ] ) ), true );
	assert.deepEqual( owner.current()?.rows, [ ALIVE_B ] );

	// Clearing drops the feed but never restarts the revision.
	owner.clear();
	assert.equal( owner.current(), null );
	assert.equal( owner.receive( snapshotFrame( 1_700_000_700_000, [] ) ), true );
	assert.equal( owner.current()?.revision, 5 );
});

test("a malformed status frame is a protocol error and keeps the last feed", () => {
	const owner = createUniqueStatus();
	owner.receive( snapshotFrame( 1_700_000_300_000, [ ALIVE_A ] ) );

	const good = snapshotFrame( 1_700_000_400_000, [ ALIVE_B ] );
	assert.throws(
		() => owner.receive( { ...good, payload: good.payload.subarray( 0, good.payload.length - 1 ) } ),
		/Truncated/
	);
	assert.throws(
		() => owner.receive( { ...good, payload: new Uint8Array( [ ...good.payload, 0 ] ) } ),
		/Invalid/
	);
	// A count past the cap is refused before any row is read.
	const fat = snapshotFrame( 1_700_000_400_000, [] );
	new DataView( fat.payload.buffer ).setUint16( 8, 257, true );
	assert.throws( () => owner.receive( fat ), /count/ );
	// Bad UTF-8 in a name is refused, never decoded lossy.
	const badText = updateFrame( 1_700_000_400_000, ALIVE_A );
	badText.payload[13] = 0xff;
	assert.throws( () => owner.receive( badText ) );
	assert.deepEqual( owner.current()?.rows, [ ALIVE_A ] );
	assert.equal( owner.current()?.revision, 1 );
});

test("the gameplay worker publishes the feed and stays dark without frames", () => {
	// A server with SRO_UNIQUE_STATUS off sends nothing: the published
	// state has no feed and the window has nothing to draw or alert on.
	const quiet = createGameplay( () => {} );
	quiet.bootstrap( {} );
	assert.equal( quiet.take()?.uniqueStatus ?? null, null );
	quiet.dispose();

	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	game.receive( snapshotFrame( 1_700_000_300_000, [ ALIVE_A, DEAD_C ] ), 0 );
	assert.deepEqual( game.take()?.uniqueStatus?.rows, [ ALIVE_A, DEAD_C ] );
	game.receive( updateFrame( 1_700_000_400_000, DEAD_A ), 0 );
	assert.deepEqual( game.take()?.uniqueStatus?.rows, [ DEAD_A, DEAD_C ] );
	game.dispose();
});

test("the Experimental row is off by default", () => {
	assert.equal( experimentalOptions( {} ).uniqueStatus, false );
	assert.equal( experimentalOptions( { uniqueStatus: true } ).uniqueStatus, true );
});

test("the tabs filter and order the rows", () => {
	const rows = [ DEAD_A, ALIVE_B, DEAD_C ];
	const names = ( list ) => list.map( ( row ) => row.name );
	// Alive first, then the highest level, then the name.
	assert.deepEqual( names( uniqueStatusRows( rows, "all", [] ) ), [ "Uruchi", "Isyutaru", "Tiger Girl" ] );
	assert.deepEqual( names( uniqueStatusRows( rows, "alive", [] ) ), [ "Uruchi" ] );
	assert.deepEqual( names( uniqueStatusRows( rows, "dead", [] ) ), [ "Isyutaru", "Tiger Girl" ] );
	assert.deepEqual(
		names( uniqueStatusRows( rows, "notify", [ ISYUTARU, TIGER_GIRL ] ) ),
		[ "Isyutaru", "Tiger Girl" ]
	);
	assert.deepEqual( uniqueStatusRows( rows, "notify", [] ), [] );
	// The source list is never reordered in place.
	assert.deepEqual( names( rows ), [ "Tiger Girl", "Uruchi", "Isyutaru" ] );
});

test("the Died column reads as a server clock and an age", () => {
	assert.equal( uniqueStatusAgo( 1_700_000_000_000, 1_700_000_030_000 ), "just now" );
	assert.equal( uniqueStatusAgo( 1_700_000_000_000, 1_700_000_300_000 ), "5 min ago" );
	assert.equal( uniqueStatusAgo( 1_700_000_000_000, 1_700_007_200_000 ), "2 h ago" );
	assert.equal( uniqueStatusAgo( 1_700_000_000_000, 1_700_007_500_000 ), "2 h 5 min ago" );
	assert.equal( uniqueStatusAgo( 1_700_000_000_000, 1_700_259_200_000 ), "3 d ago" );
	// A death ahead of the local read (the two clocks mid-handshake) is now.
	assert.equal( uniqueStatusAgo( 1_700_000_500_000, 1_700_000_000_000 ), "just now" );
	assert.equal( uniqueStatusClock( Date.UTC( 2026, 0, 2, 3, 4 ) ), "03:04" );
	assert.equal( uniqueStatusClock( Date.UTC( 2026, 11, 31, 23, 59 ) ), "23:59" );
});

test("flips are changes seen, never first sightings", () => {
	assert.deepEqual( uniqueStatusFlips( [], [ ALIVE_A, DEAD_C ] ), [] );
	assert.deepEqual( uniqueStatusFlips( [ ALIVE_A ], [ DEAD_A ] ), [ { row: DEAD_A, spawned: false } ] );
	assert.deepEqual( uniqueStatusFlips( [ DEAD_A ], [ ALIVE_A ] ), [ { row: ALIVE_A, spawned: true } ] );
	assert.deepEqual( uniqueStatusFlips( [ ALIVE_A ], [ ALIVE_A ] ), [] );
	assert.deepEqual( uniqueStatusFlips( [ ALIVE_A ], [] ), [] );

	assert.equal(
		uniqueStatusAlertText( { row: ALIVE_A, spawned: true } ),
		"Tiger Girl has spawned!"
	);
	assert.equal(
		uniqueStatusAlertText( { row: DEAD_A, spawned: false } ),
		"Tiger Girl was killed by Ann."
	);
	assert.equal(
		uniqueStatusAlertText( { row: DEAD_B, spawned: false } ),
		"Uruchi has died."
	);
});

test("a stored notify list reads back sanitized", () => {
	assert.deepEqual( uniqueNotifySettings( null ), emptyUniqueNotify() );
	assert.deepEqual( uniqueNotifySettings( "junk" ), emptyUniqueNotify() );
	assert.deepEqual(
		uniqueNotifySettings( { refs: [ 3, 1.9, 3, -2, "4", 2 ], deathsToo: true, browser: 1 } ),
		{ refs: [ 1, 2, 3 ], deathsToo: true, browser: false }
	);
	assert.deepEqual(
		uniqueNotifyBook( { Ann: { refs: [ 5 ], deathsToo: true, browser: true }, "": { refs: [ 1 ] }, Bo: null } ),
		{
			Ann: { refs: [ 5 ], deathsToo: true, browser: true },
			Bo: { refs: [], deathsToo: false, browser: false }
		}
	);
	assert.deepEqual( uniqueNotifyBook( [] ), {} );
});

test("the window edits a draft and saves it once", () => {
	const hud = createUniqueStatusHud( { Ann: { refs: [ TIGER_GIRL ], deathsToo: false, browser: false } } );
	hud.attach( "Ann" );
	assert.deepEqual( hud.draft(), { refs: [ TIGER_GIRL ], deathsToo: false, browser: false } );
	assert.equal( hud.dirty(), false );
	assert.equal( hud.takeSave(), null );

	hud.toggleMark( URUCHI );
	hud.toggleDeaths();
	assert.equal( hud.dirty(), true );
	assert.deepEqual( hud.takeSave(), { refs: [ TIGER_GIRL, URUCHI ], deathsToo: true, browser: false } );
	assert.equal( hud.dirty(), false );
	assert.equal( hud.takeSave(), null );

	// A different character re-reads the book and drops unsaved edits.
	hud.toggleMark( ISYUTARU );
	hud.attach( "Bo" );
	assert.deepEqual( hud.draft(), emptyUniqueNotify() );
	// The same character keeps them.
	hud.toggleBrowser();
	hud.attach( "Bo" );
	assert.equal( hud.draft().browser, true );
});

test("alerts follow the saved list and never the first feed", () => {
	const hud = createUniqueStatusHud( {} );
	hud.attach( "Ann" );
	hud.toggleMark( TIGER_GIRL );
	hud.takeSave();

	// The first feed is the baseline: the death already in it says nothing.
	const base = { serverNowMs: 1, rows: [ DEAD_A ], revision: 1 };
	assert.deepEqual( hud.reconcile( base, true ), [] );
	// A spawn of a marked unique alerts; its death does not without the toggle.
	const born = { serverNowMs: 2, rows: [ ALIVE_A ], revision: 2 };
	assert.deepEqual( hud.reconcile( born, true ), [ { row: ALIVE_A, spawned: true } ] );
	const gone = { serverNowMs: 3, rows: [ DEAD_A ], revision: 3 };
	assert.deepEqual( hud.reconcile( gone, true ), [] );

	hud.toggleDeaths();
	hud.takeSave();
	const bornAgain = { serverNowMs: 4, rows: [ ALIVE_A ], revision: 4 };
	assert.deepEqual( hud.reconcile( bornAgain, true ), [ { row: ALIVE_A, spawned: true } ] );
	const goneAgain = { serverNowMs: 5, rows: [ DEAD_A ], revision: 5 };
	assert.deepEqual( hud.reconcile( goneAgain, true ), [ { row: DEAD_A, spawned: false } ] );

	// Unmarked uniques never alert: Uruchi arrives dead, then spawns.
	const wide = { serverNowMs: 6, rows: [ DEAD_A, DEAD_B ], revision: 6 };
	assert.deepEqual( hud.reconcile( wide, true ), [] );
	const bornB = { serverNowMs: 7, rows: [ DEAD_A, ALIVE_B ], revision: 7 };
	assert.deepEqual( hud.reconcile( bornB, true ), [] );
});

/*
================
statusFixture

The production UI with the Experimental status row set as given, and
the notify saves recorded.
================
*/
function statusFixture( enabled ) {
	const saved = [];
	const f = uiFixture(
		() => {},
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{
			uniqueNotifyBook: () => ({}),
			saveUniqueNotify: ( character, value ) => saved.push( [ character, value ] )
		}
	);
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { uniqueStatus: enabled } ) } );
	return { f, saved };
}

test("the production UI draws the window, edits and saves the notify list", t => {
	const { f, saved } = statusFixture( true );
	t.after( () => f.dispose() );
	let now = 0;
	let view;
	const settle = ( steps ) => {
		for ( let i = 0; i < steps; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
		return defined( view );
	};
	settle( 12 );
	Object.assign( f.state.gameplay, {
		uniqueStatus: { serverNowMs: 1_700_000_300_000, rows: [ ALIVE_A, DEAD_C ], revision: 1 }
	} );
	settle( 4 );
	// Nothing is drawn until the menu entry opens the window.
	assert.ok( !settle( 1 ).controls.some( ( control ) => control.id === "unique-status-close" ) );
	f.ui.event( { kind: "activate", id: "unique-status-open" } );
	settle( 2 );
	assert.ok( settle( 1 ).controls.some( ( control ) => control.id === "unique-status-close" ) );
	assert.ok( f.hasText( "Unique Status" ) );
	assert.ok( f.hasText( "Tiger Girl" ) );
	assert.ok( f.hasText( "Isyutaru" ) );
	assert.ok( f.hasText( "Alive" ) );
	assert.ok( f.hasText( "Dead" ) );
	assert.ok( f.hasText( "Bo" ) );
	assert.ok( f.hasText( "Times are server time." ) );

	// The Dead tab hides the living; the mark is a draft until Save.
	f.ui.event( { kind: "activate", id: "unique-status-tab:dead" } );
	settle( 2 );
	assert.ok( !f.hasText( "Tiger Girl" ) );
	assert.ok( f.hasText( "Isyutaru" ) );
	f.ui.event( { kind: "activate", id: "unique-status-tab:all" } );
	f.ui.event( { kind: "activate", id: "unique-status-mark:2002" } );
	settle( 2 );
	assert.deepEqual( saved, [] );
	f.ui.event( { kind: "activate", id: "unique-status-save" } );
	settle( 1 );
	assert.deepEqual( saved, [ [ "Player", { refs: [ 2002 ], deathsToo: false, browser: false } ] ] );

	f.ui.event( { kind: "activate", id: "unique-status-close" } );
	settle( 2 );
	assert.ok( !settle( 1 ).controls.some( ( control ) => control.id === "unique-status-close" ) );
	assert.ok( !f.hasText( "Tiger Girl" ) );
});

test("the production UI draws no window while the Experimental row is off", t => {
	const { f } = statusFixture( false );
	t.after( () => f.dispose() );
	let now = 0;
	let view;
	const settle = ( steps ) => {
		for ( let i = 0; i < steps; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
		return defined( view );
	};
	settle( 12 );
	Object.assign( f.state.gameplay, {
		uniqueStatus: { serverNowMs: 1_700_000_300_000, rows: [ ALIVE_A, DEAD_C ], revision: 1 }
	} );
	settle( 4 );
	// The menu carries no entry, so the window can never be opened.
	assert.ok( !settle( 1 ).controls.some( ( control ) => control.id === "unique-status-open" ) );
	assert.ok( !settle( 1 ).controls.some( ( control ) => control.id === "unique-status-close" ) );
	assert.ok( !f.hasText( "Tiger Girl" ) );
});

test("with the row off the window closes and changes are consumed silently", () => {
	const hud = createUniqueStatusHud( {} );
	hud.attach( "Ann" );
	hud.toggleMark( TIGER_GIRL );
	hud.toggleDeaths();
	hud.takeSave();
	hud.openWindow();

	const base = { serverNowMs: 1, rows: [ DEAD_A ], revision: 1 };
	assert.deepEqual( hud.reconcile( base, true ), [] );
	// Off: the window closes, and the spawn is consumed, not queued.
	const born = { serverNowMs: 2, rows: [ ALIVE_A ], revision: 2 };
	assert.deepEqual( hud.reconcile( born, false ), [] );
	assert.equal( hud.state().open, false );
	// Back on: no alert for the change that happened while off.
	const gone = { serverNowMs: 3, rows: [ DEAD_A ], revision: 3 };
	assert.deepEqual( hud.reconcile( gone, true ), [ { row: DEAD_A, spawned: false } ] );

	// Leaving the world forgets the baseline: the next first feed is silent.
	hud.reset();
	assert.deepEqual( hud.reconcile( born, true ), [] );
});

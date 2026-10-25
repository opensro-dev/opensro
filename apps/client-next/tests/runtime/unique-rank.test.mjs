/*
===========================================================================

unique-rank.test.mjs - the unique kill damage ranking (port-only 0x3FBC)

The worker decodes the frame whole or not at all, the window's rows show
place, damage, share and party place, the window opens once per kill, and
the production UI draws it and closes it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";
const { createUniqueDamageRank, OP_UNIQUE_DAMAGE_RANK } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/unique-rank/unique-rank.ts"
);
const { uniqueRankLines, groupedDamage, damageShare, partyPlace } = await import(
	"../../src/engine/foundation/gameplay/unique-rank.ts"
);
const { createUniqueRankHud } = await import( "../../src/engine/runtime/ui/hud/unique-rank-hud.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

/*
================
rankingFixture

The production UI with the Experimental ranking row set as given.
================
*/
function rankingFixture( enabled ) {
	const f = uiFixture();
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { uniqueDamageRank: enabled } ) } );
	return f;
}

const TIGER_GIRL = 1954;
const REFS = new Map( [ [ TIGER_GIRL, { symbol: "SN_MOB_CH_TIGERWOMAN", name: "Tiger Girl" } ] ] );

/*
================
rankFrame

The server's body (wire.EncodeUniqueDamageRank) for the given rows and
self place.
================
*/
function rankFrame( rows, self, total = 1000 ) {
	const parts = [];
	const head = Buffer.alloc( 13 );
	head.writeUInt32LE( TIGER_GIRL, 0 );
	head.writeBigUInt64LE( BigInt( total ), 4 );
	head.writeUInt8( rows.length, 12 );
	parts.push( head );
	for ( const row of rows ) {
		const name = Buffer.from( row.name, "utf-8" ), tail = Buffer.alloc( 5 );
		tail.writeUInt32LE( row.damage, 0 );
		tail.writeUInt8( row.partyRank, 4 );
		parts.push( Buffer.from( [ name.length ] ), name, tail );
	}
	const own = Buffer.alloc( 7 );
	own.writeUInt16LE( self.rank, 0 );
	own.writeUInt32LE( self.damage, 2 );
	own.writeUInt8( self.partyRank, 6 );
	parts.push( own );
	return { opcode: OP_UNIQUE_DAMAGE_RANK, payload: new Uint8Array( Buffer.concat( parts ) ) };
}

const ROWS = [
	{ name: "Ann", damage: 600, partyRank: 1 },
	{ name: "Bo", damage: 300, partyRank: 0 }
];

test("the worker decodes a ranking whole and numbers each one", () => {
	const owner = createUniqueDamageRank();
	assert.equal( owner.receive( { opcode: 0x3fb0, payload: new Uint8Array( 2 ) }, REFS ), false );
	assert.equal( owner.receive( rankFrame( ROWS, { rank: 2, damage: 300, partyRank: 0 } ), REFS ), true );
	assert.deepEqual( owner.current(), {
		sequence: 1,
		refObjId: TIGER_GIRL,
		uniqueName: "Tiger Girl",
		total: 1000,
		rows: ROWS,
		selfRank: 2,
		selfDamage: 300,
		selfPartyRank: 0
	} );

	// A truncated or overlong frame is a protocol error and keeps the last ranking.
	const good = rankFrame( ROWS, { rank: 2, damage: 300, partyRank: 0 } );
	assert.throws(
		() => owner.receive( { ...good, payload: good.payload.subarray( 0, good.payload.length - 1 ) }, REFS ),
		/Invalid|Truncated/
	);
	assert.throws(
		() => owner.receive( { ...good, payload: new Uint8Array( [ ...good.payload, 0 ] ) }, REFS ),
		/Invalid/
	);
	const many = rankFrame( Array( 13 ).fill( ROWS[1] ), { rank: 1, damage: 1, partyRank: 0 } );
	assert.throws( () => owner.receive( many, REFS ), /count/ );
	assert.equal( owner.current()?.sequence, 1 );

	// Clearing drops the ranking but never restarts the numbering.
	owner.clear();
	assert.equal( owner.current(), null );
	owner.receive( rankFrame( ROWS, { rank: 1, damage: 600, partyRank: 1 } ), new Map() );
	assert.equal( owner.current()?.sequence, 2 );
	assert.equal( owner.current()?.uniqueName, "" );
});

test("the ranking has its own opcode, and the crow's Tailwind still reaches pet training", () => {
	// pettailwind.go's private 0x3FB7: recipient u32, book u8, stacks u8, speed x10 u16.
	assert.notEqual( OP_UNIQUE_DAMAGE_RANK, 0x3fb7 );
	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	const tailwind = new Uint8Array( 8 ), v = new DataView( tailwind.buffer );
	v.setUint32( 0, 100002, true );
	tailwind[4] = 57;
	tailwind[5] = 10;
	v.setUint16( 6, 300, true );
	game.receive( { opcode: 0x3fb7, payload: tailwind }, 0 );
	game.receive( rankFrame( ROWS, { rank: 2, damage: 300, partyRank: 0 } ), 0 );
	const state = game.take();
	assert.deepEqual( state?.petTailwind, { gid: 100002, book: 57, stacks: 10, speedPercent: 30 } );
	assert.equal( state?.uniqueDamageRank?.selfRank, 2 );
	game.dispose();
});

test("the rows show grouped damage, one-decimal shares and party places", () => {
	assert.equal( groupedDamage( 1234567 ), "1,234,567" );
	assert.equal( groupedDamage( 999 ), "999" );
	assert.equal( damageShare( 1, 3 ), "33.3%" );
	assert.equal( damageShare( 5, 0 ), "0.0%" );
	assert.equal( partyPlace( 0 ), "-" );
	assert.equal( partyPlace( 3 ), "3" );

	const ranking = {
		sequence: 1,
		refObjId: 1,
		uniqueName: "",
		total: 1000,
		rows: ROWS,
		selfRank: 2,
		selfDamage: 300,
		selfPartyRank: 0
	};
	const inside = uniqueRankLines( ranking, "Bo" );
	assert.deepEqual(
		inside.top.map( line => [ line.rank, line.name, line.damage, line.share, line.party, line.self ] ),
		[
			[ "1", "Ann", "600", "60.0%", "1", false ],
			[ "2", "Bo", "300", "30.0%", "-", true ]
		]
	);
	assert.equal( inside.own, null );

	// Placed below the list: the viewer's own row follows it.
	const outside = uniqueRankLines( { ...ranking, selfRank: 15, selfDamage: 4, selfPartyRank: 2 }, "Cy" );
	assert.ok( outside.top.every( line => !line.self ) );
	assert.deepEqual( outside.own, { rank: "15", name: "Cy", damage: "4", share: "0.4%", party: "2", self: true } );
});

test("the window opens once per ranking and again in a new session", () => {
	const hud = createUniqueRankHud();
	const ranking = {
		sequence: 1,
		refObjId: 1,
		uniqueName: "",
		total: 1,
		rows: [],
		selfRank: 0,
		selfDamage: 0,
		selfPartyRank: 0
	};
	assert.equal( hud.reconcile( { uniqueDamageRank: null }, true ), false );
	assert.equal( hud.reconcile( { uniqueDamageRank: ranking }, true ), true );
	assert.equal( hud.state(), ranking );
	hud.close();
	assert.equal( hud.reconcile( { uniqueDamageRank: ranking }, true ), false );
	assert.equal( hud.state(), null );
	const next = { ...ranking, sequence: 2 };
	assert.equal( hud.reconcile( { uniqueDamageRank: next }, true ), true );

	// Leaving the world (a teleport) closes the window, and the ranking the
	// worker still publishes on arrival does not reopen it.
	hud.reset();
	assert.equal( hud.state(), null );
	assert.equal( hud.reconcile( { uniqueDamageRank: next }, true ), false );

	// A cleared worker publishes null; its numbering may start again at 1.
	assert.equal( hud.reconcile( { uniqueDamageRank: null }, true ), false );
	assert.equal( hud.reconcile( { uniqueDamageRank: ranking }, true ), true );
});

test("the production UI draws the ranking and closes it", t => {
	const f = rankingFixture( true );
	t.after( () => f.dispose() );
	let now = 0;
	let view;
	for ( let i = 0; i < 12; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
	Object.assign( f.state.gameplay, {
		uniqueDamageRank: {
			sequence: 1,
			refObjId: TIGER_GIRL,
			uniqueName: "Tiger Girl",
			total: 1000,
			rows: ROWS,
			selfRank: 2,
			selfDamage: 300,
			selfPartyRank: 0
		}
	} );
	for ( let i = 0; i < 4; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
	assert.ok( f.hasText( "Tiger Girl defeated" ) );
	assert.ok( f.hasText( "60.0%" ) );
	assert.ok( f.hasText( "Ann" ) );
	assert.ok( defined( view ).controls.some( control => control.id === "unique-rank-close" ) );

	f.ui.event( { kind: "activate", id: "unique-rank-ok" } );
	for ( let i = 0; i < 2; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
	assert.ok( !defined( view ).controls.some( control => control.id === "unique-rank-close" ) );
	assert.ok( !f.hasText( "Tiger Girl defeated" ) );
});

test("a teleport does not reopen a closed ranking", t => {
	const f = rankingFixture( true );
	t.after( () => f.dispose() );
	let now = 0;
	let view;
	const settle = steps => {
		for ( let i = 0; i < steps; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
		return defined( view );
	};
	const open = () => settle( 1 ).controls.some( control => control.id === "unique-rank-close" );
	settle( 12 );
	Object.assign( f.state.gameplay, {
		uniqueDamageRank: {
			sequence: 1,
			refObjId: TIGER_GIRL,
			uniqueName: "Tiger Girl",
			total: 1000,
			rows: ROWS,
			selfRank: 2,
			selfDamage: 300,
			selfPartyRank: 0
		}
	} );
	settle( 4 );
	assert.ok( open() );
	f.ui.event( { kind: "activate", id: "unique-rank-close" } );
	settle( 2 );
	assert.ok( !open() );

	// The loading screen and arrival: the worker still publishes the ranking.
	f.state.session = { ...f.state.session, phase: "entering-world", revision: f.state.session.revision + 1 };
	settle( 4 );
	f.state.session = { ...f.state.session, phase: "world", revision: f.state.session.revision + 1 };
	settle( 4 );
	assert.ok( !open() );
	assert.ok( !f.hasText( "Tiger Girl defeated" ) );
});

test("the window shows only with the Experimental row on", () => {
	const hud = createUniqueRankHud();
	const ranking = {
		sequence: 1,
		refObjId: 1,
		uniqueName: "",
		total: 1,
		rows: [],
		selfRank: 0,
		selfDamage: 0,
		selfPartyRank: 0
	};
	// Off: the ranking is consumed, so turning the row on never opens an old kill.
	assert.equal( hud.reconcile( { uniqueDamageRank: ranking }, false ), false );
	assert.equal( hud.state(), null );
	assert.equal( hud.reconcile( { uniqueDamageRank: ranking }, true ), false );
	const next = { ...ranking, sequence: 2 };
	assert.equal( hud.reconcile( { uniqueDamageRank: next }, true ), true );
	// Turning the row off closes an open window.
	hud.reconcile( { uniqueDamageRank: next }, false );
	assert.equal( hud.state(), null );
});

test("the row is off by default, and the production UI then never opens it", t => {
	assert.equal( experimentalOptions().uniqueDamageRank, false );
	assert.equal( experimentalOptions( { uniqueDamageRank: 1 } ).uniqueDamageRank, false );
	assert.equal( experimentalOptions( { uniqueDamageRank: true } ).uniqueDamageRank, true );
	const f = rankingFixture( false );
	t.after( () => f.dispose() );
	let now = 0;
	let view;
	for ( let i = 0; i < 12; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
	Object.assign( f.state.gameplay, {
		uniqueDamageRank: {
			sequence: 1,
			refObjId: TIGER_GIRL,
			uniqueName: "Tiger Girl",
			total: 1000,
			rows: ROWS,
			selfRank: 2,
			selfDamage: 300,
			selfPartyRank: 0
		}
	} );
	for ( let i = 0; i < 4; i++ ) view = f.ui.step( f.state, now += 100 ) ?? view;
	assert.ok( !defined( view ).controls.some( control => control.id === "unique-rank-close" ) );
	assert.ok( !f.hasText( "Tiger Girl defeated" ) );
});

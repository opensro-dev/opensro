/*
===========================================================================

guild-war.test.mjs - native war packets, dialog transitions and clocks

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { emptySocial, socialPacket, socialRequest } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { packWarPeriod, advanceGuildWarClock, WAR_UNLIMITED } = await import(
	"../../src/engine/foundation/gameplay/guild-war.ts"
);
const { createGuildWarHud } = await import( "../../src/engine/runtime/ui/hud/guild-war-hud.ts" );

/*
================
writer
================
*/
function writer() {
	const bytes = [];
	return {
		u8( value ) {
			bytes.push( value );
			return this;
		},
		u32( value ) {
			bytes.push( value & 255, value >>> 8 & 255, value >>> 16 & 255, value >>> 24 );
			return this;
		},
		str( value ) {
			const raw = new TextEncoder().encode( value );
			bytes.push( raw.length & 255, raw.length >>> 8, ...raw );
			return this;
		},
		frame( opcode ) {
			return { opcode, payload: Uint8Array.from( bytes ) };
		}
	};
}

/*
================
masterState
================
*/
function masterState() {
	return {
		...emptySocial( "Alice" ),
		self: 10,
		guild: {
			id: 7,
			name: "Red",
			level: 2,
			gp: 0,
			subject: "",
			contents: "",
			crest: 0,
			members: [ {
				id: 10,
				name: "Alice",
				grade: 0,
				level: 1,
				donated: 0,
				permissions: 0xffffffff,
				grant: "",
				model: 1907,
				role: 0,
				offline: 0
			} ]
		}
	};
}

/*
================
warRow
================
*/
function warRow( w ) {
	return w.u32( 77 ).u32( 90 ).u8( 2 ).u32( 800 ).u32( 7 ).u32( 8 ).u32( 100 ).u32( 25 ).str( "Blue" );
}

test("declaration and surrender use the native opcodes and exact terms", () => {
	const state = masterState(),
		terms = { name: "Blue", mode: 0, period: packWarPeriod( 1, 2, 3 ), scoreIndex: 2, stake: 400 };
	const request = socialRequest( state, { kind: "guild-war-declare", terms } );
	assert.deepEqual(
		request,
		writer().str( "Blue" ).u8( 0 ).u32( 1 << 10 | 2 << 15 | 30 << 20 ).u8( 2 ).u32( 400 ).frame( 0x771b )
	);
	const active = defined( socialPacket( state, warRow( writer().u8( 0x19 ) ).frame( 0x3b29 ) ) );
	assert.deepEqual(
		socialRequest( active, { kind: "guild-war-surrender", id: 77 } ),
		writer().u32( 77 ).frame( 0x7465 )
	);
	assert.throws( () => socialRequest( state, { kind: "guild-war-surrender", id: 77 } ) );
	assert.throws( () => socialRequest( state, { kind: "guild-war-declare", terms: { ...terms, stake: 500000001 } } ) );
});

test("kind ten decodes all proposal fields and uses the shared consent owner", () => {
	const incoming = writer().u8( 10 ).u32( 123 ).str( "Blue" ).u8( 0 ).u32( WAR_UNLIMITED ).u8( 7 ).u32( 400 ).frame(
		0x3393
	);
	const next = defined( socialPacket( masterState(), incoming ) );
	assert.deepEqual( next.invitation?.war, {
		name: "Blue",
		mode: 0,
		period: WAR_UNLIMITED,
		scoreIndex: 7,
		stake: 400
	} );
	assert.deepEqual( [ ...socialRequest( next, { kind: "social-consent", accept: true } ).payload ], [ 1, 1 ] );
	assert.deepEqual( [ ...socialRequest( next, { kind: "social-consent", accept: false } ).payload ], [ 2, 0x16 ] );
});

test("entry merges native rows and score receipts update the existing member owner", () => {
	let state = defined( socialPacket( masterState(), warRow( writer().u8( 1 ) ).frame( 0x32bb ), { now: 1000 } ) );
	assert.equal( state.wars?.[0]?.localScore, 100 );
	state = defined(
		socialPacket(
			state,
			writer().u8( 0x1d ).u8( 1 ).u32( 77 ).u32( 251 ).u32( 10 ).str( "" ).str( "Bob" ).frame( 0x3b29 )
		)
	);
	assert.equal( state.wars?.[0]?.localScore, 351 );
	assert.equal( state.guild?.members[0]?.warScore, 251 );
	assert.equal( state.guild?.members[0]?.warKills, 1 );
	state = defined( socialPacket( masterState(), writer().u8( 0 ).frame( 0x32bb ) ) );
	assert.deepEqual( state.wars, [] );
});

test("surrender countdown preserves hostility until the end receipt", () => {
	let state = defined( socialPacket( masterState(), warRow( writer().u8( 1 ) ).frame( 0x32bb ), { now: 1000 } ) );
	state = defined( socialPacket( state, writer().u8( 0x1b ).u32( 77 ).frame( 0x3b29 ), { now: 1000 } ) );
	assert.equal( state.notice?.value, 60 );
	assert.equal( state.warResult?.key, "UIIT_CTL_GUILDWAR_ENDCOUNT" );
	assert.equal( advanceGuildWarClock( state, 1999 ), state );
	state = advanceGuildWarClock( state, 2000 );
	assert.equal( state.notice?.value, 59 );
	assert.equal( state.wars?.length, 1 );
	state = defined( socialPacket( state, writer().u8( 0x1c ).u32( 77 ).u32( 7 ).frame( 0x3b29 ), { now: 2000 } ) );
	assert.equal( state.warResult?.key, "UIIT_MSG_GUILDWAR_WINERGUILD" );
	assert.deepEqual( state.wars, [] );
	assert.equal( state.warCountdown, undefined );
});

test("native duration controls synchronize unlimited and finite selections", () => {
	const hud = createGuildWarHud(), social = masterState();
	hud.command( "war-declare", social );
	assert.equal( hud.state().draft.scoreIndex, 2 );
	assert.equal( hud.state().draft.days, 1 );
	hud.command( "war-combo:24", social );
	hud.command( "war-choice:31", social );
	assert.equal( hud.state().terms.period, WAR_UNLIMITED );
	assert.deepEqual( [ hud.state().draft.days, hud.state().draft.hours, hud.state().draft.minutes ], [ 31, 24, 6 ] );
	hud.command( "war-combo:25", social );
	hud.command( "war-choice:2", social );
	assert.deepEqual( [ hud.state().draft.days, hud.state().draft.hours, hud.state().draft.minutes ], [ 0, 2, 0 ] );
	hud.type( "war-name", "Blue", 1000 );
	hud.command( "war-confirm", social );
	assert.equal( hud.state().mode, "confirm" );
	assert.equal( hud.command( "war-confirm", social )?.kind, "guild-war-declare" );
	assert.equal( hud.state().mode, "closed" );
});

test("money input uses carried gold and losing master status retires the modal", () => {
	const hud = createGuildWarHud(), social = masterState();
	hud.command( "war-declare", social );
	hud.command( "war-money-open", social );
	hud.type( "war-money", "1200", 900 );
	hud.command( "war-money-ok", social );
	assert.equal( hud.state().draft.stake, 900 );
	hud.reconcile( { ...social, self: 20 } );
	assert.equal( hud.state().mode, "closed" );
	assert.equal( hud.state().money, null );
});

test("contribution headers use native fixed ordering and dense ranks", () => {
	const hud = createGuildWarHud(), base = masterState();
	const members = [ { ...base.guild.members[0], id: 30, name: "Zed", warScore: 100 }, {
		...base.guild.members[0],
		id: 10,
		name: "Ada",
		warScore: 100
	}, { ...base.guild.members[0], id: 20, name: "Bob", warScore: 0 } ];
	assert.deepEqual( hud.members( members ).map( m => [ m.id, m.rank ] ), [ [ 10, 1 ], [ 20, 2 ], [ 30, 1 ] ] );
	hud.command( "war-contribution-sort:61", base );
	assert.deepEqual( hud.members( members ).map( m => m.id ), [ 10, 30, 20 ] );
	hud.command( "war-contribution-sort:60", base );
	assert.deepEqual( hud.members( members ).map( m => m.name ), [ "Ada", "Bob", "Zed" ] );
	hud.command( "war-contribution-sort:60", base );
	assert.deepEqual( hud.members( members ).map( m => m.name ), [ "Ada", "Bob", "Zed" ] );
});

test("war results survive panel changes without replaying after dismissal", () => {
	const hud = createGuildWarHud(), social = masterState();
	const ended = { ...social, warResult: { key: "UIIT_MSG_GUILDWAR_WINERGUILD", names: [ "Red" ], sequence: 1 } };
	hud.reconcile( ended );
	hud.reset();
	assert.equal( hud.state().result?.sequence, 1 );
	hud.command( "war-result-close", ended );
	hud.reset();
	hud.reconcile( ended );
	assert.equal( hud.state().result, undefined );
	hud.reset( true );
	hud.reconcile( ended );
	assert.equal( hud.state().result?.sequence, 1 );
});

test("proposal timeouts distinguish the sender and recipient pending modes", () => {
	const frame = writer().u8( 0x32 ).u8( 2 ).str( "Blue" ).frame( 0x3b29 );
	assert.equal( socialPacket( masterState(), frame )?.notice, undefined );
	const source = { ...masterState(), warPending: /** @type {const} */ (1) };
	assert.equal( socialPacket( source, frame )?.notice?.key, "UIIT_MSG_GUILDWARERR_REQUISITION_TIME_OUT" );
	const proposal = writer().u8( 10 ).u32( 123 ).str( "Blue" ).u8( 0 ).u32( WAR_UNLIMITED ).u8( 7 ).u32( 400 ).frame(
		0x3393
	);
	const target = defined( socialPacket( masterState(), proposal ) );
	assert.equal( target.warPending, 2 );
	assert.equal( socialPacket( target, frame )?.invitation, null );
	assert.equal( socialPacket( source, writer().u8( 1 ).frame( 0xb71b ) )?.warPending, 0 );
});

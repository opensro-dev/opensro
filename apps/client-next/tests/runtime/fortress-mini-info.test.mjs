/*
===========================================================================

fortress-mini-info.test.mjs - rank boundaries and fortress countdown lifecycle

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { fortressMiniIndicators } = await import( "../../src/engine/foundation/ui/fortress-mini-info.ts" );
const {
	fortressBootstrap,
	fortressPacket,
	advanceFortressCountdowns,
	fortressCountdownNotices
} = await import( "../../src/engine/foundation/gameplay/fortress.ts" );

/*
================
state
================
*/
function state() {
	return {
		...fortressBootstrap( {} ),
		worldId: 7,
		listId: 1,
		worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
		fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME" } ],
		wars: [ { id: 1, name: "Owner", flags: 1 } ],
		localKills: 0,
		localDeaths: 3
	};
}

/*
================
text
================
*/
function text( key ) {
	return {
		UIIT_STT_FORT_PK_NUM_01: "Kills: %d",
		UIIT_STT_FORT_PK_NUM_02: "Deaths: %d",
		PARAM_MINUTE: "m",
		PARAM_SECOND: "s"
	}[key] ?? key;
}

test("all native battle-rank thresholds are inclusive and independent of guild status", () => {
	const thresholds = [ 15, 25, 45, 70, 100, 150 ];
	const names = [
		"ASSAULTING_SOILDER",
		"ELITE_ASSAULTING_SOILDER",
		"CENTURION",
		"ASSAULTING_LEADER",
		"ELITE_IMPERIAL_GUARD",
		"COMBAT_COMMANDER"
	];
	for ( let rank = 0; rank < thresholds.length; rank++ ) {
		for ( const offset of [ -1, 0, 1 ] ) {
			const s = { ...state(), localKills: thresholds[rank] + offset };
			const rows = fortressMiniIndicators( s, false, text );
			const expected = rank - Number( offset < 0 );
			if ( expected < 0 ) {
				assert.deepEqual( rows, [] );
				continue;
			}
			assert.equal( rows.length, 1 );
			assert.equal( rows[0].text, `SN_SKILL_${names[expected]}\nKills: ${s.localKills}\nDeaths: 3` );
			assert.ok( rows[0].image );
			assert.match( rows[0].image, /icon\/etc\/rank_.+\.png$/ );
		}
	}
	assert.equal(
		fortressMiniIndicators( { ...state(), localKills: 0xffffffff }, false, text )[0].text.split( "\n" )[0],
		"SN_SKILL_COMBAT_COMMANDER"
	);
	assert.deepEqual( fortressMiniIndicators( { ...state(), localKills: 150, worldId: 1 }, false, text ), [] );
	assert.deepEqual(
		fortressMiniIndicators(
			{ ...state(), localKills: 150, wars: [ { id: 1, name: "Owner", flags: 0 } ] },
			true,
			text
		),
		[]
	);
});

test("guild status remains visible away from the siege and hides after withdrawal or war end", () => {
	const s = { ...state(), worldId: 1, wars: [ { id: 1, name: "Owner", flags: 1, captureWait: 90, stoneWait: 29 } ] };
	const rows = fortressMiniIndicators( s, true, text );
	assert.equal( rows.length, 1 );
	assert.equal( rows[0].control, "GDR_PMI_FORTRESS_INFO" );
	assert.match( rows[0].text, /FORTRESS_NAME/ );
	assert.match( rows[0].text, /Owner/ );
	assert.match( rows[0].text, /1m 30s/ );
	assert.match( rows[0].text, /29s/ );
	assert.deepEqual( fortressMiniIndicators( s, false, text ), [] );
	assert.deepEqual( fortressMiniIndicators( { ...s, listId: 0 }, true, text ), [] );
});

test("tower-fall countdown ticks once per second, clamps, and emits only native announcement boundaries", () => {
	const s = fortressPacket( state(), { opcode: 0x3887, payload: Uint8Array.of( 10, 1, 0, 0, 0 ) }, 1000 );
	assert.ok( s );
	assert.ok( s );
	assert.equal( s.wars[0].stoneWait, 180 );
	assert.equal( fortressCountdownNotices( s )[0].value, 180 );
	assert.equal( advanceFortressCountdowns( s, 1999 ), s );
	let next = advanceFortressCountdowns( s, 2000 );
	assert.equal( next.wars[0].stoneWait, 179 );
	assert.deepEqual( fortressCountdownNotices( next ), [] );
	assert.equal( advanceFortressCountdowns( next, 2000 ), next );
	for ( let now = 3000; now <= 91000; now += 1000 ) next = advanceFortressCountdowns( next, now );
	assert.equal( next.wars[0].stoneWait, 90 );
	assert.equal( fortressCountdownNotices( next )[0].value, 90 );
	for ( let now = 92000; now <= 152000; now += 1000 ) next = advanceFortressCountdowns( next, now );
	assert.equal( next.wars[0].stoneWait, 29 );
	assert.equal( fortressCountdownNotices( next )[0].value, 29 );
	for ( let now = 153000; now <= 200000; now += 1000 ) next = advanceFortressCountdowns( next, now );
	assert.equal( next.wars[0].stoneWait, 0 );
	assert.deepEqual( fortressCountdownNotices( next ), [] );
	assert.equal( advanceFortressCountdowns( next, 300000 ), next );
});

test("overdue timers decrement once, retain an active baseline on tower reset, and clear on war end", () => {
	const packet = payload => ({ opcode: 0x3887, payload: Uint8Array.from( payload ) });
	let s = fortressPacket( state(), packet( [ 10, 1, 0, 0, 0 ] ), 1000 );
	assert.ok( s );
	s = advanceFortressCountdowns( s, 91000 );
	assert.equal( s.wars[0].stoneWait, 179 );
	s = fortressPacket( s, packet( [ 10, 1, 0, 0, 0 ] ), 91999 );
	assert.ok( s );
	assert.equal( s.countdownAtMs, 91000 );
	s = advanceFortressCountdowns( s, 92000 );
	assert.equal( s.wars[0].stoneWait, 179 );
	s = fortressPacket( s, packet( [ 6 ] ), 92500 );
	assert.ok( s );
	assert.equal( s.wars[0].stoneWait, 0 );
	assert.equal( s.wars[0].captureWait, 0 );
	assert.deepEqual( fortressCountdownNotices( s ), [] );
	s = advanceFortressCountdowns( s, 93000 );
	assert.equal( s.countdownAtMs, undefined );
});

test("initial fortress lists retain both countdowns and a new session starts a fresh timer", () => {
	const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ];
	const payload = Uint8Array.from( [
		0,
		1,
		...u32( 1 ),
		1,
		0,
		65,
		...Array( 16 ).fill( 0 ),
		1,
		...u32( 90 ),
		1,
		...u32( 29 ),
		1,
		...u32( 1 )
	] );
	const frame = { opcode: 0x3887, payload };
	const first = fortressPacket( state(), frame, 5000 );
	assert.ok( first );
	assert.equal( first.wars[0].captureWait, 90 );
	assert.equal( first.wars[0].stoneWait, 29 );
	assert.deepEqual( fortressCountdownNotices( first ).map( n => n.value ), [ 90, 29 ] );
	const tick = advanceFortressCountdowns( first, 6000 );
	assert.equal( tick.wars[0].captureWait, 89 );
	assert.equal( tick.wars[0].stoneWait, 28 );
	const reentry = fortressPacket( state(), frame, 15000 );
	assert.ok( reentry );
	assert.equal( advanceFortressCountdowns( reentry, 15999 ), reentry );
	assert.throws( () => fortressPacket( state(), { ...frame, payload: payload.slice( 0, -1 ) }, 0 ) );
});

/*
===========================================================================

game-options.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { defaultGameOptions, initialGameOptions, gameOptions, gameOptionRows } = await load(
	"foundation/gameplay/game-options.ts"
);
const { nameVisible, nameInRange, hiddenSilkCos } = await load( "foundation/ui/name-visibility.ts" );
const { vitalWarning } = await load( "foundation/ui/vital-warning.ts" );
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const { quickStatus } = await load( "foundation/ui/quick-status.ts" );
const options = defaultGameOptions(),
	local = { gid: 1, kind: "local-player", name: "Me", heading: 0, regionId: 257, x: 0, y: 0, z: 0, guildId: 7 },
	other = { ...local, gid: 2, kind: "player", name: "Peer", guildId: 8 };
test("name distance follows prediction across regions instead of the spawn record", () => {
	const monster = { ...other, kind: "monster", regionId: 258, x: 50 },
		live = { regionId: 258, x: 100, y: 0, z: 0, angle: 0 };
	assert.equal( nameVisible( monster, local, false, options ), false );
	assert.equal( nameVisible( monster, local, false, options, live ), true );
	assert.equal( nameVisible( { ...monster, regionId: 257, x: 20 }, local, false, options, live ), false );
	assert.equal( nameVisible( local, local, false, options, live ), true );
});
const packet = ( type, extra = [] ) => ({ opcode: 0x3393, payload: Uint8Array.of( type, 2, 0, 0, 0, ...extra ) });
test("all local option bytes are separate and defaults have only two disabled fields", () => {
	assert.equal( gameOptionRows().length, 23 );
	assert.equal( new Set( gameOptionRows().map( r => r[1] ) ).size, 23 );
	assert.deepEqual( Object.keys( options ).filter( k => !options[k] ), [ "windowMode", "hideSilkCos" ] );
	const copy = gameOptions( options );
	assert.notEqual( copy, options );
	assert.throws( () => gameOptions( { ...options, gm: true } ) );
	for ( const [key] of gameOptionRows() ) {
		assert.equal( gameOptions( { ...options, [key]: false } )[key], false );
		assert.throws( () => gameOptions( { ...options, [key]: 1 } ) );
	}
});
test("native hover, distance, own/other, guild and owned COS name branches", () => {
	const off = {
		...options,
		ownName: false,
		playerNames: false,
		monsterNames: false,
		npcNames: false,
		guildNames: false
	};
	for ( const kind of [ "local-player", "player", "monster", "npc", "cos" ] ) {
		const e = { ...other, kind };
		assert.equal( nameVisible( e, local, false, off ), false );
		assert.equal( nameVisible( { ...e, x: 1000 }, local, true, off ), true );
	}
	assert.equal( nameVisible( { ...other, x: 300 }, local, false, options ), false );
	assert.equal( nameVisible( { ...other, x: 299 }, local, false, options ), true );
	assert.equal( nameVisible( { ...other, guildId: 7 }, local, false, { ...off, guildNames: true } ), true );
	// 85E2E0 hides a COS only while it is in a ride link; an owned pet shows its name.
	assert.equal( nameVisible( { ...other, kind: "cos", ownerGid: 1 }, local, false, options ), true );
	assert.equal( nameVisible( { ...other, kind: "cos" }, local, false, options, undefined, true ), false );
	assert.equal( nameVisible( { ...other, kind: "cos" }, local, true, options, undefined, true ), true, "hover wins" );
	assert.equal( nameVisible( { ...other, kind: "cos" }, local, false, options ), true );
	for ( const band of [ 1, 2, 3, 4, 5 ] ) {
		assert.equal( hiddenSilkCos( { ...other, kind: "cos", tidWord: (band << 11) | 0x1c6 }, true ), band === 4 );
	}
});
test("quick status flags affect overhead bars independently of name display", () => {
	const game = {
		localGid: 1,
		target: 3,
		vitals: [ { gid: 1, hp: 20, mp: 60, maxHp: 100, maxMp: 100 } ],
		social: { members: [ { name: "Peer", status: 0x85 } ] }
	};
	assert.deepEqual( quickStatus( local, local, game, options ), { hp: Math.fround( .2 ), mp: Math.fround( .6 ) } );
	assert.equal( quickStatus( local, local, game, { ...options, ownStatus: false } ), null );
	assert.deepEqual( quickStatus( other, local, game, options ), { hp: .5, mp: .8 } );
	assert.equal( quickStatus( other, local, game, { ...options, partyStatus: false } ), null );
});
test("warnings use strict float threshold, no zero-health warning", () => {
	assert.equal( vitalWarning( 29, 100 ), true );
	assert.equal( vitalWarning( 30, 100 ), false );
	assert.equal( vitalWarning( 0, 100 ), false );
	assert.equal( vitalWarning( 1, 0 ), false );
	const sounds = [], g = createGameplay( () => {}, ( key, at ) => sounds.push( [ key, at ] ) );
	g.bootstrap( { character: { hp: 29, mp: 20, maxHp: 100, maxMp: 100 } } );
	g.seed( local );
	g.options( options );
	g.step( 1, local );
	g.step( 2, local );
	assert.deepEqual( sounds, [ [ "SND_ALARM", 1 ], [ "SND_ALARM", 1 ] ] );
	g.options( { ...options, warningSound: false } );
	g.step( 3, local );
	g.options( options );
	g.step( 4, local );
	assert.equal( sounds.length, 2, "enabling sound while already low does not retrigger" );
	g.reset();
	g.step( 5 );
	assert.equal( sounds.length, 2 );
	g.dispose();
});
test("party and exchange options dispatch different native refusal packets", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( {} );
	g.seed( local );
	g.options( { ...options, partyInvites: false, exchangeRequests: false } );
	for ( const [type, extra, want] of [ [ 1, [], [ 1, 0 ] ], [ 2, [ 3 ], [ 2, 12 ] ], [ 3, [ 4 ], [ 2, 23 ] ] ] ) {
		assert.equal( g.receive( packet( type, extra ), 0 ), true );
		assert.deepEqual( [ ...sent.at( -1 ).payload ], want );
		assert.equal( g.take().social.invitation, null );
	}
	const n = sent.length;
	g.receive( packet( 5 ), 1 );
	assert.equal( sent.length, n );
	assert.equal( g.take().social.invitation.type, 5 );
	g.options( options );
	g.receive( packet( 2, [ 7 ] ), 2 );
	assert.deepEqual( g.take().social.invitation, { type: 2, gid: 2, options: 7 } );
	g.dispose();
});
test("GM eligibility resets and beginner gate uses maximum attained level", () => {
	const sent = [], g = createGameplay( f => sent.push( f ) );
	g.bootstrap( { character: { gmPrivilege: true, pcRoomEvent: true, level: 18, maxLevel: 20 } } );
	g.seed( local );
	assert.equal( g.take().eligibility.gm, true );
	assert.equal( g.command( { kind: "beginner-mark", enabled: true }, 0, undefined, local ), null );
	assert.equal( sent.length, 0 );
	g.command( { kind: "beginner-mark", enabled: false }, 0, undefined, { ...local, visualFlags: 3 } );
	assert.deepEqual( [ ...sent[0].payload ], [ 2 ] );
	g.reset();
	assert.deepEqual( g.take().eligibility, { gm: false, pcRoomEvent: false } );
	g.dispose();
});

test("startup disables warnings and every overhead status category; Reset remains separate", () => {
	const initial = initialGameOptions();
	assert.deepEqual( Object.keys( initial ).filter( k => !initial[k] ), [
		"ownStatus",
		"cosStatus",
		"partyStatus",
		"monsterStatus",
		"windowMode",
		"hideSilkCos",
		"hpWarning",
		"mpWarning",
		"warningSound"
	] );
	const game = {
		localGid: 1,
		target: 2,
		vitals: [ { gid: 1, hp: 20, mp: 60, maxHp: 100, maxMp: 100 } ],
		social: { members: [ { name: "Peer", status: 0x85 } ] }
	};
	assert.equal( quickStatus( local, local, game, initial ), null );
	assert.equal( quickStatus( other, local, game, initial ), null );
	const sounds = [], g = createGameplay( () => {}, key => sounds.push( key ) );
	g.bootstrap( { character: { hp: 29, mp: 20, maxHp: 100, maxMp: 100 } } );
	g.seed( local );
	g.step( 1, local );
	assert.deepEqual( sounds, [] );
	g.options( options );
	g.step( 2, local );
	assert.deepEqual( sounds, [], "enabling while low must not retrigger the already latched edge" );
	g.dispose();
	assert.equal( defaultGameOptions().ownStatus, true );
	assert.equal( defaultGameOptions().hpWarning, true );
	assert.deepEqual( gameOptions( { ...initial, ownStatus: true, warningSound: true } ), {
		...initial,
		ownStatus: true,
		warningSound: true
	}, "explicit persisted choices survive validation" );
});

test("the overhead board range is strict 300 units from the local player, across regions", () => {
	const local = { gid: 1, kind: "local-player", regionId: 1, x: 1900, y: 0, z: 0 };
	const at = ( regionId, x, y = 0 ) => ({ gid: 2, kind: "player", regionId, x, y, z: 0 });
	assert.equal( nameInRange( at( 1, 1900 - 299 ), local ), true );
	assert.equal( nameInRange( at( 1, 1900 - 300 ), local ), false );
	// Height counts: the range is a sphere, as 85E2E0 measures it.
	assert.equal( nameInRange( at( 1, 1900, 300 ), local ), false );
	// The neighbouring region's x = 100 lies 120 units east of x = 1900.
	assert.equal( nameInRange( at( 2, 100 ), local ), true );
	assert.equal( nameInRange( at( 1, 1900 ), undefined ), false );
});

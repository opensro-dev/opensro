/*
===========================================================================

crests-war-buffs.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { mediaExtractedRoot } from "../../../../scripts/build/world/paths.mjs";
import { pathToFileURL } from "node:url";
await mkdir( "temp/artifacts/crests-war-buffs", { recursive: true } );
async function load( path, name ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { decodeGuildCrest, guildCrestFiles, entityCrestFiles } = await load( "foundation/ui/guild-crest.ts", "crest" );
const { socialPacket, emptySocial } = await load( "foundation/gameplay/social.ts", "social" );
const { buffBoard: renderBuffBoard } = await load( "foundation/ui/buff-board.ts", "buff" );
function buffBoard( game, time ) {
	return renderBuffBoard( {
		...game,
		buffSlots: (game.attachedEffects ?? []).filter( e => e.gid === game.localGid && e.token ).map( (
			effect,
			serial
		) => ({
			state: "active",
			serial,
			effect,
			secondary: !!game.skillCatalog?.find( s => s.id === effect.skill )?.buffSecondary
		}) )
	}, time );
}
const { createUiAssets } = await load( "runtime/ui/resources/resources.ts", "resources" );
const { fortressBootstrap, fortressPacket } = await load( "foundation/gameplay/fortress.ts", "fortress" );
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ],
	str = s => [ s.length & 255, s.length >>> 8, ...new TextEncoder().encode( s ) ];
const base = { ...emptySocial( "Self" ), guild: { id: 10, name: "Local", members: [] } };
const war = (
	id,
	a = 10,
	b = 20
) => [
	...u32( id ),
	...u32( 90 ),
	2,
	...u32( 100 ),
	...u32( a ),
	...u32( b ),
	...u32( 3 ),
	...u32( 8 ),
	...str( "Enemy" )
];
const packet = ( s, op, p ) => socialPacket( s, { opcode: op, payload: Uint8Array.from( p ) } );
test("native palette indices are opaque RGB and vertically inverted exactly once", async () => {
	const native = await readFile( join( mediaExtractedRoot, "silk.dat" ) );
	const bytes = Uint8Array.from( { length: 256 }, ( _, i ) => i ), rgba = decodeGuildCrest( bytes );
	for ( let y = 0; y < 16; y++ ) {
		for ( let x = 0; x < 16; x++ ) {
			const index = (15 - y) * 16 + x;
			assert.deepEqual( [ ...rgba.slice( (y * 16 + x) * 4, (y * 16 + x) * 4 + 4 ) ], [
				...native.slice( index * 3, index * 3 + 3 ),
				255
			] );
		}
	}
	assert.throws( () => decodeGuildCrest( new Uint8Array( 255 ) ) );
	assert.throws( () => decodeGuildCrest( new Uint8Array( 257 ) ) );
	assert.deepEqual( guildCrestFiles( 1, 10, [ 2, 20, 3 ] ), [ { file: "G1_10_2.crb", left: -20 }, {
		file: "A1_20_3.crb",
		left: -36
	} ] );
	assert.deepEqual( guildCrestFiles( 1, 10, [ 0, 20, 0xffffffff ] ), [] );
});
test("war updates retain ending relations, orient scores and reject partial writes", () => {
	let s = packet( base, 0x32bb, [ 1, ...war( 7 ) ] );
	assert.equal( s.wars[0].localScore, 3 );
	s = packet( s, 0x32bb, [ 1, ...war( 8, 20, 10 ) ] );
	assert.equal( s.wars.length, 2 );
	assert.equal( s.wars[1].localScore, 8 );
	const score = [ 0x1d, 1, ...u32( 7 ), ...u32( 5 ), ...u32( 100 ), ...str( "Killer" ) ];
	s = packet( s, 0x3b29, score );
	assert.equal( s.wars[0].localScore, 8 );
	s = packet( s, 0x3b29, [ 0x1a, ...u32( 7 ) ] );
	assert.equal( s.wars[0].ending, true );
	assert.equal( s.wars.length, 2 );
	const before = JSON.stringify( s );
	assert.throws( () => packet( s, 0x3b29, [ 0x23, ...u32( 20 ), 4, 0, 65 ] ) );
	assert.equal( JSON.stringify( s ), before );
	s = packet( s, 0x3b29, [ 0x23, ...u32( 20 ), ...str( "Renamed" ) ] );
	assert.ok( s.wars.every( w => w.name === "Renamed" ) );
	s = packet( s, 0x3b29, [ 0x1c, ...u32( 7 ), ...u32( 10 ) ] );
	assert.deepEqual( s.wars.map( w => w.id ), [ 8 ] );
	assert.deepEqual( packet( s, 0x3b29, [ 1 ] ).wars, [] );
	assert.deepEqual( packet( base, 0x32bb, [ 1, ...u32( 0 ) ] ).wars, [] );
});
test("war proposal replies reach the message board and never repeat or block the guild", async () => {
	const { createHudMessages } = await load( "runtime/ui/hud/messages.ts", "messages" );
	// 762040 slot 0 refreshes the window, 0x32 carries the reply, and the jump
	// table folds the remaining subOps onto a block that reads no body.
	assert.equal( packet( base, 0x3b29, [ 0 ] ).notice, undefined );
	assert.equal( packet( { ...base, guild: null }, 0x3b29, [ 0 ] ).error, null );
	for ( const subOp of [ 4, 7, 0x24, 0x31 ] ) {
		assert.equal( packet( { ...base, guild: null }, 0x3b29, [ subOp ] ).error, null );
	}
	const refusal = packet( base, 0x3b29, [ 0x32, 0, ...str( "Enemy" ) ] );
	assert.deepEqual( refusal.notice, { key: "UIIT_MSG_GUILDWAR_WARREFUSAL", value: 0, text: "Enemy" } );
	const timeout = packet( { ...base, warPending: 1 }, 0x3b29, [ 0x32, 2, ...str( "Enemy" ) ] );
	assert.equal( timeout.notice.key, "UIIT_MSG_GUILDWARERR_REQUISITION_TIME_OUT" );
	// Result three opens the native two-line suggestion modal.
	assert.equal( packet( base, 0x3b29, [ 0x32, 3 ] ).warResult?.additionalKey, "UIIT_MSG_GUILDWAR_SUGGESTIONS_02" );
	assert.equal( packet( base, 0x3b29, [ 0x32, 7 ] ).notice, undefined );
	assert.throws( () => packet( base, 0x3b29, [ 0x32, 0 ] ), /Truncated/ );
	assert.equal( packet( refusal, 0x3b29, [ 0x32, 3 ] ).notice, undefined, "a drained notice never repeats" );
	const messages = createHudMessages( () => 0 );
	const rows = messages.step(
		0,
		[],
		1,
		0,
		[ { ...refusal.notice, sequence: 1 } ],
		key => key === "UIIT_MSG_GUILDWAR_WARREFUSAL" ? "%s refused the war." : "",
		false
	);
	assert.equal( rows.at( -1 ).value, "Enemy refused the war." );
});
test("crest update arms preserve the other image and clear revision zero", () => {
	const entity = { gid: 2, guildId: 20, guildName: "Enemy", guildCrests: [ 3, 40, 5 ] },
		game = { localGid: 1, social: base };
	game.social = packet( base, 0x34f3, [ 1, ...u32( 20 ), ...str( "Enemy" ), ...u32( 9 ) ] );
	assert.deepEqual( entityCrestFiles( entity, game, 2 ).map( r => r.file ), [ "G2_20_9.crb", "A2_40_5.crb" ] );
	game.social = packet( game.social, 0x34f3, [ 2, ...u32( 40 ), 1, ...str( "Enemy" ), ...u32( 0 ) ] );
	assert.deepEqual( entityCrestFiles( entity, game, 2 ).map( r => r.file ), [ "G2_20_9.crb" ] );
});
test("local timers use restored full duration and preserve exhausted effects", () => {
	const effects = Array.from(
		{ length: 10 },
		( _, i ) => ({
			gid: 1,
			skill: i + 1,
			token: i + 10,
			phase: 2,
			remainingMs: 5000,
			durationMs: 10000,
			receivedAtMs: 1000
		})
	);
	const game = {
		localGid: 1,
		attachedEffects: effects,
		skillCatalog: effects.map( e => ({
			id: e.skill,
			name: "Buff",
			icon: "skill/china/sword_smash_a.ddj",
			buffSecondary: e.skill === 10
		}) )
	};
	let rows = buffBoard( game, 3000 );
	assert.equal( rows[0].fraction, .3 );
	// 6E5D40 stores kind 1 for every 0xB419 effect, so the phase byte must not
	// reach the slot kind: the alternate bar belongs to 0x3691 kind 3.
	game.attachedEffects = [ { ...effects[0], phase: 3 } ];
	assert.equal( buffBoard( game, 3000 ).length, 1 );
	game.attachedEffects = effects;
	rows = buffBoard( game, 3000 );
	assert.deepEqual( [ rows[8].x, rows[8].y ], [ 0, 27 ] );
	assert.deepEqual( [ rows[9].x, rows[9].y ], [ 0, 56 ] );
	assert.equal( buffBoard( game, 999999 )[0].fraction, 0 );
	assert.equal( game.attachedEffects.length, 10 );
	game.attachedEffects = [ { ...effects[0], remainingMs: undefined, durationMs: undefined } ];
	assert.equal( buffBoard( game, 100 ).at( 0 ).fraction, 1 );
	game.attachedEffects = [];
	assert.deepEqual( buffBoard( game, 100 ), [] );
});
test("COS timer admission excludes unrelated signed item parameters and preserves sentinels", async () => {
	const { cosTimerReference } = await load( "foundation/gameplay/cos-timer.ts", "cosreferences" );
	const pet = (3 << 2) | (3 << 5) | (13 << 7) | (15 << 11);
	assert.equal(
		cosTimerReference( { typeFlags: 0x32c, nativeFields: { itemParam1_29c: -100, itemParam3_2a4: -50 } } ),
		null
	);
	assert.equal(
		cosTimerReference( {
			typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (1 << 11),
			nativeFields: { itemParam1_29c: 1440 }
		} ),
		null,
		"summoner minutes are not pet-skill seconds"
	);
	assert.deepEqual(
		cosTimerReference( { typeFlags: pet, nativeFields: { itemParam1_29c: 1800, itemParam3_2a4: -1 } } ),
		{ durationSec: 1800, aux: 0xffffffff }
	);
	assert.deepEqual( cosTimerReference( { typeFlags: pet, nativeFields: { itemParam1_29c: -1 } } ), {
		durationSec: 0xffffffff,
		aux: 0xffffffff
	} );
	assert.throws(
		() => cosTimerReference( { typeFlags: pet, nativeFields: { itemParam1_29c: -2 } } ),
		/Invalid COS window duration/
	);
	assert.throws(
		() => cosTimerReference( { typeFlags: pet, nativeFields: { itemParam1_29c: 1800, itemParam3_2a4: -2 } } ),
		/Invalid COS window aux/
	);
});
test("the kind-3 item window decodes, counts down and takes the two-bar slot", async () => {
	const { cosTimerPacket, cosTimerBars } = await load( "foundation/gameplay/cos-timer.ts", "costimer" );
	// The exact bytes internal/game/item/wire EncodeCosSummonTimer3691 emits:
	// [u8 3][u32 id][u32 remainingSec][u32 packedExtra], little-endian, no tail.
	const wire = Uint8Array.from( [ 3, 0x39, 0x30, 0, 0, 0x80, 0x9d, 0, 0, 0x05, 0, 0x0a, 0 ] );
	const update = cosTimerPacket( { opcode: 0x3691, payload: wire }, 1000 );
	assert.deepEqual( update, {
		kind: "set",
		timer: { itemRefObjId: 12345, remainingSec: 40320, packedExtra: 0x000a0005, receivedAtMs: 1000 }
	} );
	assert.equal(
		cosTimerPacket( { opcode: 0x3691, payload: Uint8Array.from( [ 9, 1, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 )
			.timer.itemRefObjId,
		1,
		"subtype 9 shares the kind-3 arm"
	);
	// 6E6150 takes a zero pair as its REMOVE selector, not an empty window.
	assert.deepEqual(
		cosTimerPacket(
			{ opcode: 0x3691, payload: Uint8Array.from( [ 3, 0x39, 0x30, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 ] ) },
			0
		),
		{ kind: "remove", itemRefObjId: 12345 }
	);
	// Subtype 8 and the remain-time arms are native windows this owner does not
	// publish; they must not be mistaken for a summon window or throw.
	assert.equal(
		cosTimerPacket( { opcode: 0x3691, payload: Uint8Array.from( [ 8, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 ),
		null
	);
	assert.equal( cosTimerPacket( { opcode: 0xb419, payload: wire }, 0 ), null );
	assert.throws(
		() => cosTimerPacket( { opcode: 0x3691, payload: Uint8Array.from( [ 3, 1, 0, 0, 0 ] ) }, 0 ),
		/Invalid COS summon timer/
	);
	assert.throws( () => cosTimerPacket( { opcode: 0x3691, payload: new Uint8Array( 0 ) }, 0 ), /Empty/ );
	assert.throws(
		() =>
			cosTimerPacket( {
				opcode: 0x3691,
				payload: new Uint8Array( 13 ).map( ( v, i ) => i === 0 ? 3 : i === 5 ? 1 : 0 )
			}, 0 ),
		/window reference/
	);
	// 6E6E00 kind 3 seeds the accumulator to limit - remaining, so the third
	// word is what is LEFT: a fresh summon carries its whole duration.
	const fresh = { itemRefObjId: 1, remainingSec: 100, packedExtra: 0, receivedAtMs: 0 };
	assert.deepEqual( cosTimerBars( fresh, { durationSec: 100, aux: 0xffffffff }, 0 ), { primary: 1, secondary: 1 } );
	assert.deepEqual( cosTimerBars( fresh, { durationSec: 100, aux: 0xffffffff }, 25000 ), {
		primary: .75,
		secondary: 1
	}, "aux -1 is the native no-limit sentinel" );
	assert.equal(
		cosTimerBars( { ...fresh, remainingSec: 50 }, { durationSec: 100, aux: 0xffffffff }, 0 ).primary,
		.5,
		"a half-spent summon starts half full"
	);
	assert.equal(
		cosTimerBars( fresh, { durationSec: 100, aux: 0xffffffff }, 999999 ).primary,
		0,
		"an exhausted window clamps, it does not go negative"
	);
	assert.equal(
		cosTimerBars( fresh, { durationSec: 100, aux: 0 }, 0 ).secondary,
		1,
		"a zero aux fails the native ordering guard and holds the bar full"
	);
	// The aux is Param3 (+0x2A4), not Param2 (+0x2A0): the record's 20 params are
	// contiguous ahead of its 20 descriptions. ITEM_MALL_PET_SKILL_COLD carries
	// Param2 1 but Param3 -1, so reading the wrong one empties the second bar.
	assert.equal(
		cosTimerBars( { ...fresh, packedExtra: 0 }, { durationSec: 1800, aux: 0xffffffff }, 0 ).secondary,
		1,
		"Param3 -1 is the no-limit sentinel"
	);
	assert.equal(
		cosTimerBars( { ...fresh, packedExtra: 0 }, { durationSec: 1800, aux: 1 }, 0 ).secondary,
		0,
		"Param2 would have rendered an empty bar"
	);
	// A real aux runs its own remaining, taken from the packed low u16.
	assert.equal(
		cosTimerBars( { ...fresh, packedExtra: 0x000a0014 }, { durationSec: 100, aux: 40000 }, 0 ).secondary,
		.5
	);
	assert.equal(
		cosTimerBars( { ...fresh, packedExtra: 0x000a0014 }, { durationSec: 100, aux: 40000 }, 10000 ).secondary,
		.25
	);
	assert.equal(
		cosTimerBars( fresh, { durationSec: 0, aux: 0 }, 0 ),
		null,
		"a summoner without a window has no slot"
	);
	// 6E6E00 reads Param1 as SECONDS: ITEM_MALL_PET_SKILL_COLD carries 1800,
	// labelled ì‚¬ìš©ì‹œê°„(ì´ˆ), a 30-minute usage window. No scaling belongs here.
	assert.equal(
		cosTimerBars( { itemRefObjId: 1, remainingSec: 1800, packedExtra: 0, receivedAtMs: 0 }, {
			durationSec: 1800,
			aux: 0xffffffff
		}, 900 * 1000 ).primary,
		.5,
		"a 30-minute pet-skill window is half spent after 15 minutes"
	);
	const { buffBoard } = await load( "foundation/ui/buff-board.ts", "buffcos" );
	const reference = { durationSec: 100, aux: 0xffffffff, icon: "icon/etc/battery_form.ddj", name: "Pet Skill" };
	const game = {
		localGid: 1,
		attachedEffects: [],
		skillCatalog: [],
		cosWindows: [ { ...fresh, itemRefObjId: 12345, reference } ]
	};
	const rows = buffBoard( game, 25000 );
	assert.equal( rows.length, 1 );
	assert.equal( rows[0].id, "cos:12345" );
	assert.equal( rows[0].label, "Pet Skill" );
	assert.deepEqual( [ rows[0].x, rows[0].y ], [ 0, 0 ], "an item window joins the primary list" );
	assert.equal( rows[0].fraction, .75 );
	assert.equal( rows[0].secondary, 1 );
	// 6E6150 keys rows by item id, so distinct items stack side by side.
	const two = buffBoard( {
		...game,
		cosWindows: [ { ...fresh, itemRefObjId: 12345, reference }, { ...fresh, itemRefObjId: 24001, reference } ]
	}, 0 );
	assert.deepEqual( two.map( r => r.id ), [ "cos:12345", "cos:24001" ] );
	assert.deepEqual( [ two[1].x, two[1].y ], [ 23, 0 ], "the second row takes the next primary cell" );
	assert.equal( buffBoard( { ...game, cosWindows: [] }, 0 ).length, 0 );
});
test("a region reset drops kind-3 windows, a pet despawn does not, and the re-raise restores them", async () => {
	const { createGameplay } = await load(
		"runtime/simulation/worker/session/world/gameplay/gameplay.ts",
		"gameplaycos"
	);
	const game = createGameplay( () => {} );
	game.bootstrap( {
		refObjSnapshot: [ { refObjId: 3914, tidWord: 0x11c6, kind: "cos" } ],
		refItemSnapshot: [ {
			refObjId: 24001,
			typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (15 << 11),
			name: "Pet Skill",
			nativeFields: { itemParam1_29c: 1800, itemParam3_2a4: -1 }
		}, { refObjId: 107, typeFlags: 0x32c, nativeFields: { itemParam1_29c: -100, itemParam3_2a4: -50 } } ]
	} );
	// The bytes enterworld's re-raise emits for a window with 91 seconds left.
	const raise = Uint8Array.from( [ 3, ...u32( 24001 ), ...u32( 91 ), ...u32( 0 ) ] );
	game.receive( { opcode: 0x3691, payload: raise }, 0 );
	let windows = game.take().cosWindows;
	assert.equal( windows.length, 1 );
	assert.equal( windows[0].remainingSec, 91 );
	assert.equal( windows[0].reference.aux, 0xffffffff );
	// Dismissing the active pet reaches no kind-3 eraser natively.
	game.receive( {
		opcode: 0x3158,
		payload: Uint8Array.from( [ ...u32( 8 ), ...u32( 3914 ), ...u32( 87829 ), ...new Array( 9 ).fill( 0 ) ] )
	}, 1 );
	assert.equal( game.take().activeCos?.gid, 8 );
	game.receive( { opcode: 0x36ab, payload: Uint8Array.from( u32( 8 ) ) }, 2 );
	const dismissed = game.take();
	assert.equal( dismissed.activeCos, undefined );
	assert.equal( dismissed.cosWindows.length, 1, "the window outlives its pet" );
	// 0x3369 -> 74B250 -> 6E6270(board, 0) empties the board before re-entry.
	game.resetWorld();
	assert.deepEqual( game.take().cosWindows, [] );
	// The reference outlives the reset, so the server's re-raise lands again.
	game.receive( { opcode: 0x3691, payload: raise }, 10 );
	windows = game.take().cosWindows;
	assert.equal( windows.length, 1 );
	assert.equal( windows[0].receivedAtMs, 10 );
	// A relog replaces the snapshot; a window whose reference is gone is not published.
	game.bootstrap( { refItemSnapshot: [] } );
	game.receive( { opcode: 0x3691, payload: raise }, 20 );
	assert.deepEqual( game.take().cosWindows, [] );
	game.dispose();
});
test("missing crest never creates a HUD load failure or retry storm; new revisions load", () => {
	let id = 0;
	const requests = [], jobs = new Map();
	const resources = createUiAssets(
		{
			available: () => 32,
			request( url, limit, decode ) {
				requests.push( { url, limit, decode } );
				jobs.set( ++id, { kind: "error", error: "404" } );
				return id;
			},
			take: i => jobs.get( i ),
			cancel: i => jobs.delete( i )
		},
		() => {},
		"http://localhost"
	);
	const first = "http://localhost/marks/G1_10_1.crb", second = first.replace( "_1.crb", "_2.crb" );
	resources.step( [ first ], 0 );
	resources.step( [ first ], 1 );
	resources.step( [ first ], 100000 );
	assert.equal( requests.length, 1 );
	assert.equal( requests[0].decode, "crest" );
	assert.equal( requests[0].limit, 256 );
	assert.equal( resources.error(), null );
	assert.equal( resources.stats().pending, 0 );
	resources.step( [ second ], 100001 );
	assert.equal( requests.length, 2 );
	resources.dispose();
});
test("fortress owner and registration deltas preserve independent state", () => {
	const seed = fortressBootstrap( {} ),
		s = { ...seed, wars: [ { id: 1, name: "Old", flags: 1 } ], registered: [ 10 ] };
	const apply = p => fortressPacket( s, { opcode: 0x3887, payload: Uint8Array.from( p ) } );
	assert.equal(
		apply( [ 8, ...u32( 1 ), ...str( "New" ), ...u32( 0 ), ...u32( 0 ), ...u32( 0 ), ...u32( 0 ) ] ).wars[0].name,
		"New"
	);
	assert.equal( apply( [ 12, ...u32( 1 ), 0 ] ).listId, 1 );
	assert.equal( apply( [ 13, ...u32( 1 ), 1 ] ).listId, 0 );
	assert.equal( apply( [ 17, ...u32( 1 ), ...u32( 12 ), ...u32( 3 ) ] ).localKills, 12 );
	assert.equal( apply( [ 18, ...u32( 1 ), 6 ] ).staffFlags, 6 );
	assert.deepEqual( s.registered, [ 10 ] );
});

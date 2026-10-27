/*
===========================================================================

item-process.test.mjs - tests for inventory.ts, alchemy.ts

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
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { gachaPrizes } = await import(
	"../../src/engine/foundation/gameplay/gacha-catalog.ts"
);
test("Go Alchemy result corpus reaches the production client inventory and native sound branches", () => {
	const corpus = JSON.parse(
		readFileSync(
			new URL( "../../../server/internal/game/item/alchemy/testdata/alchemy_wire.json", import.meta.url ),
			"utf8"
		)
	);
	for ( const row of corpus ) {
		const sounds = [], owner = createInventory( () => {}, h => sounds.push( h ) );
		owner.bootstrap( {
			refItemSnapshot: [
				...new Map(
					[ ...row.before, ...row.after ].map(
						i => [ i.refObjId, { refObjId: i.refObjId, typeFlags: i.typeFlags } ]
					)
				).values()
			],
			equipItems: row.before
		} );
		owner.process( { kind: "alchemy-open" }, 0 );
		owner.process( { kind: "alchemy-start", mode: row.mode, slots: row.slots ?? [ 13, 14 ] }, 0 );
		for ( const frame of row.frames ) owner.receive( frame.opcode, Uint8Array.from( frame.payload ) );
		const actual = owner.state().inventory.map( i => ({
			slot: i.slot,
			refObjId: i.refObjId,
			plus: i.plus ?? 0,
			quantity: i.quantity
		}) ).sort( ( a, b ) => a.slot - b.slot );
		const expected = row.after.map( i => ({
			slot: i.slot,
			refObjId: i.refObjId,
			plus: i.plus,
			quantity: i.quantity
		}) ).sort( ( a, b ) => a.slot - b.slot );
		assert.deepEqual( actual, expected, row.name );
		assert.equal( owner.state().alchemy.pending, false, row.name );
		for ( const expected of row.after ) {
			if ( (expected.typeFlags & 0x7e) !== 0x2c ) continue;
			const actual = owner.state().inventory.find( i => i.slot === expected.slot ),
				body = Buffer.from( expected.body );
			assert.equal( defined( actual ).variance, body.readBigUInt64LE( 5 ).toString(), row.name );
			const magic = Array.from( { length: body[17] }, ( _, i ) => body.readBigUInt64LE( 18 + 8 * i ).toString() );
			assert.deepEqual( defined( actual ).magic ?? [], magic, row.name );
		}
		assert.deepEqual(
			sounds,
			[ "advanced", "dissolve" ].includes( row.mode ) ?
				[ "SND_ELIXIR_USE" ] :
				[ "SND_ELIXIR_USE", row.success ? "SND_ELIXIR_SUCCESS" : "SND_ELIXIR_FAILURE" ],
			row.name
		);
	}
});
function equipment( plus = 0 ) {
	const p = Buffer.alloc( 18 );
	p.writeUInt32LE( 1 );
	p[4] = plus;
	p.writeUInt32LE( 10, 13 );
	return [ ...p ];
}
function setup() {
	const sounds = [], frames = [], owner = createInventory( f => frames.push( f ), h => sounds.push( h ) );
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x2c }, { refObjId: 2, typeFlags: 0xf6c }, {
			refObjId: 3,
			typeFlags: 0x176c
		} ],
		equipItems: [ { slot: 13, refObjId: 1, body: equipment() }, {
			slot: 14,
			refObjId: 2,
			body: [ 2, 0, 0, 0, 1, 0 ]
		} ]
	} );
	return { owner, sounds, frames };
}
test("Alchemy serializes wire slots and emits use/result only after an atomic reply", () => {
	const { owner, sounds, frames } = setup();
	owner.process( { kind: "alchemy-open" }, 0 );
	owner.process( { kind: "alchemy-start", mode: "reinforce", slots: [ 13, 14 ] }, 0 );
	assert.deepEqual( [ ...frames[0].payload ], [ 2, 13, 14 ] );
	assert.deepEqual( sounds, [] );
	const p = Buffer.from( [ 1, 1, 13, ...equipment( 1 ) ] ), before = owner.state();
	for ( let i = 0; i < p.length; i++ ) {
		assert.throws( () => owner.receive( 0xb373, p.subarray( 0, i ) ) );
		assert.deepEqual( owner.state(), before );
		assert.deepEqual( sounds, [] );
	}
	owner.receive( 0xb373, p );
	assert.equal( defined( owner.state().inventory.find( i => i.slot === 13 ) ).plus, 1 );
	assert.deepEqual( sounds, [ "SND_ELIXIR_USE", "SND_ELIXIR_SUCCESS" ] );
	owner.receive( 0xb373, Uint8Array.of( 1, 0, 13, 1 ) );
	assert.equal( owner.state().inventory.some( i => i.slot === 13 ), false );
	assert.deepEqual( sounds.slice( -2 ), [ "SND_ELIXIR_USE", "SND_ELIXIR_FAILURE" ] );
});
test("Alchemy cancellation uses failure, compound arming is silent, hidden results stay silent", () => {
	const { owner, sounds } = setup();
	owner.process( { kind: "alchemy-open" }, 0 );
	owner.receive( 0xb651, Uint8Array.of( 2, 0x23 ) );
	assert.deepEqual( sounds, [ "SND_ELIXIR_USE", "SND_ELIXIR_FAILURE" ] );
	sounds.length = 0;
	owner.receive( 0xb16f, Uint8Array.of( 1, 1 ) );
	assert.deepEqual( sounds, [] );
	owner.receive( 0x3359, Uint8Array.of( 1, 0, 3, 0, 0, 0 ) );
	owner.receive( 0xb549, Uint8Array.of( 1 ) );
	assert.deepEqual( sounds, [ "SND_ELIXIR_USE", "SND_ELIXIR_USE" ] );
	owner.process( { kind: "alchemy-close" }, 0 );
	owner.receive( 0xb373, Uint8Array.from( [ 1, 1, 13, ...equipment( 2 ) ] ) );
	assert.equal( sounds.length, 2 );
	assert.equal( defined( owner.state().inventory.find( i => i.slot === 13 ) ).plus, 2 );
});
function open( owner ) {
	owner.process( { kind: "gacha-open", gid: 7 }, 0 );
	owner.receive( 0xb338, Uint8Array.of( 1, 0, 0, 1, 0 ) );
}
const { createAlchemy } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/alchemy/alchemy.ts"
);
test("compound serializes quantity and slots, subtracts completed count, locks retries, and cancels once", () => {
	const a = createAlchemy(), items = new Map( [ [ 13, { quantity: 5 } ], [ 14, { quantity: 8 } ] ] );
	a.open();
	const request = a.start( "compound", [ 13, 14 ], items, 100, 3 );
	assert.equal( request.opcode, 0x716f );
	assert.deepEqual( [ ...request.payload ], [ 2, 1, 3, 0, 0, 0, 2, 13, 14 ] );
	a.compound( true, 1 );
	assert.equal( a.state().remaining, 2 );
	assert.equal( a.state().pending, true );
	assert.deepEqual( [ ...defined( a.cancel() ).payload ], [ 1 ] );
	assert.equal( a.cancel(), null );
	assert.deepEqual( a.compound( true, 2 ), [] );
	assert.equal( a.state().pending, false );
	assert.throws( () => a.start( "compound", [ 13, 14 ], items, 2099, 1 ), /unavailable/ );
	assert.equal( a.step( 2099 ), false );
	assert.equal( a.step( 2100 ), true );
	a.start( "compound", [ 13, 14 ], items, 2100, 3 );
	a.compound( true, 3 );
	assert.equal( a.state().remaining, 0 );
	assert.equal( a.state().pending, false );
});
test("compound advances mixed stacks using per-step counts instead of restarting from the total", () => {
	const a = createAlchemy(),
		items = new Map( [ [ 13, { quantity: 20, typeFlags: 0x35ec } ], [ 14, { quantity: 2, typeFlags: 0x25ec } ], [
			15,
			{ quantity: 4, typeFlags: 0x25ec }
		], [ 16, { quantity: 7, typeFlags: 0x25ec } ] ] );
	a.open();
	a.start( "compound", [ 13, 14, 15, 16 ], items, 0, 9 );
	a.compound( true, 2 );
	assert.equal( a.state().remaining, 7 );
	a.compound( true, 4 );
	assert.equal( a.state().remaining, 3 );
	assert.equal( a.state().pending, true );
	a.compound( true, 3 );
	assert.equal( a.state().remaining, 0 );
	assert.equal( a.state().pending, false );
});
test("advanced pads five native slots; dissolve uses its separate opcode; invalid requests preserve state", () => {
	const a = createAlchemy(),
		items = new Map( [ [ 13, { quantity: 5, typeFlags: 0x032c } ], [ 14, { quantity: 8 } ] ] );
	a.open();
	const before = a.state();
	for ( const count of [ 0, NaN, 6 ] ) assert.throws( () => a.start( "advanced", [ 13, 14 ], items, 0, count ) );
	assert.deepEqual( a.state(), before );
	assert.throws( () => a.start( "advanced", [ 13, 14 ], items, 0, 2 ), /quantity/ );
	assert.deepEqual( [ ...a.start( "advanced", [ 13, 14 ], items, 0, 1 ).payload ], [
		2,
		2,
		1,
		0,
		0,
		0,
		2,
		13,
		14,
		12,
		12,
		12
	] );
	a.compound( true, 1 );
	assert.equal( a.state().pending, false );
	assert.throws( () => a.start( "dissolve", [ 13, 14 ], items, 2000, 2 ), /quantity/ );
	assert.throws( () => a.start( "dissolve", [ 13, 14, 15 ], items, 2000, 1 ), /unavailable/ );
	const dissolve = a.start( "dissolve", [ 13, 14 ], items, 2000, 1 );
	assert.equal( dissolve.opcode, 0x7549 );
	assert.deepEqual( [ ...dissolve.payload ], [ 2, 13, 14 ] );
	a.compound( false, undefined, 7 );
	assert.equal( a.state().pending, false );
	assert.equal( a.state().error, 7 );
});
test("native single-selection compound and dissolution requests retain their distinct layouts", () => {
	const a = createAlchemy(), items = new Map( [ [ 13, { quantity: 5, typeFlags: 0x032c } ] ] );
	a.open();
	assert.deepEqual( [ ...a.start( "compound", [ 13 ], items, 0, 2 ).payload ], [ 2, 1, 2, 0, 0, 0, 1, 13 ] );
	a.compound( true, 2 );
	assert.deepEqual( [ ...a.start( "dissolve", [ 13 ], items, 2000, 1 ).payload ], [ 1, 13, 12 ] );
	const b = createAlchemy();
	b.open();
	for ( const excluded of [ 0x03ac, 0x06ac, 0x832c ] ) {
		const bag = new Map( [ [ 13, { quantity: 1, typeFlags: excluded } ], [ 14, {
			quantity: 1,
			typeFlags: 0x0d6c
		} ] ] );
		for ( const mode of [ "reinforce", "attribute", "magic", "dissolve" ] ) {
			assert.throws( () => b.start( mode, [ 13, 14 ], bag, 0, 1 ), /equipment/ );
		}
	}
});
test("Magic Pang timers send exactly once and native result cards gate the win cue", () => {
	const { owner, sounds, frames } = setup();
	open( owner );
	owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 100 );
	assert.deepEqual( sounds, [ "SND_GACHA_MOVE", "SND_GACHA_TURN" ] );
	owner.step( 4099 );
	assert.equal( frames.length, 1 );
	owner.step( 4100 );
	assert.equal( frames[1].opcode, 0x7053 );
	assert.deepEqual( [ ...frames[1].payload ], [ 7, 0, 0, 0, 1, 0, 0, 0, 14 ] );
	owner.step( 5000 );
	assert.equal( frames.length, 2 );
	owner.receive( 0xb053, Uint8Array.of( 1, 1 ) );
	assert.equal( sounds.length, 2 );
	const delta = Buffer.alloc( 23 );
	delta[0] = 14;
	delta[1] = 0x21;
	delta.writeUInt32LE( 3, 2 );
	delta[6] = 2;
	delta.writeBigUInt64LE( 4140n, 7 );
	delta.writeBigUInt64LE( 1n, 15 );
	const before = owner.state().inventory;
	for ( let i = 0; i < delta.length; i++ ) {
		assert.throws( () => owner.receive( 0x3645, delta.subarray( 0, i ) ) );
		assert.deepEqual( owner.state().inventory, before );
	}
	owner.receive( 0x3645, delta );
	owner.receive( 0xb053, Uint8Array.of( 1, 1 ) );
	assert.equal( sounds.at( -1 ), "SND_GACHA_WIN" );
	assert.deepEqual( owner.state().gacha.reward, { refObjId: 4140, quantity: 1 } );
});
test("Magic Pang loss, failed opening, card invalidation and reset do not invent cues", () => {
	const { owner, sounds, frames } = setup();
	assert.equal( owner.receive( 0xb338, Uint8Array.of( 1, 1, 0, 0, 0 ) ), false );
	open( owner );
	owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 0 );
	owner.receive( 0x3645, Uint8Array.of( 14, 8, 0, 0 ) );
	owner.step( 4000 );
	assert.equal( frames.length, 1 );
	assert.equal( owner.state().gacha.phase, "idle" );
	assert.equal( sounds.length, 2 );
	owner.clear();
	owner.step( 9000 );
	assert.equal( frames.length, 1 );
	const b = setup();
	open( b.owner );
	b.owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 0 );
	b.owner.receive( 0xb053, Uint8Array.of( 1, 0 ) );
	assert.equal( b.sounds.at( -1 ), "SND_GACHA_END" );
});

/*
================
Magic Pang refusal

766A4E sends a refusal straight to the notice dispatcher: no result
presentation, no cue, the bag untouched. Port-only, not native: native
leaves its busy byte set and the window cannot close; a refused roll
returns the window to idle, so it closes and rolls again.
================
*/
test("a refused Magic Pang roll presents no result and leaves the window idle", () => {
	// The server's refusal body is [2][u16 code]; these are its native codes
	// for a missing entry, a bad slot and a missing gacha service.
	for ( const code of [ 2, 0x1806, 0x1c0f ] ) {
		const { owner, sounds, frames } = setup();
		open( owner );
		owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 0 );
		owner.step( 4000 );
		assert.equal( owner.state().gacha.phase, "waiting" );
		const before = owner.state().gacha, items = owner.state().inventory, cues = [ ...sounds ];
		owner.receive( 0xb053, Uint8Array.of( 2, code & 0xff, (code >> 8) & 0xff ) );
		assert.deepEqual( owner.state().gacha, { ...before, phase: "idle" } );
		assert.deepEqual( owner.state().inventory, items );
		assert.deepEqual( sounds, cues );
		// It rolls again, as after a lost roll.
		owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 5000 );
		owner.step( 9000 );
		assert.equal( frames.at( -1 ).opcode, 0x7053 );
	}
});

test("after a refused Magic Pang roll the window closes", () => {
	const { owner } = setup();
	open( owner );
	owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 0 );
	owner.step( 4000 );
	owner.process( { kind: "gacha-close" }, 4000 );
	assert.equal( owner.state().gacha.visible, true, "a pending roll keeps the window" );
	owner.receive( 0xb053, Uint8Array.of( 2, 7, 0 ) );
	owner.process( { kind: "gacha-close" }, 4100 );
	assert.equal( owner.state().gacha.visible, false );
	assert.equal( owner.state().gacha.phase, "closed" );
});

test("a Magic Pang roll never holds the bag, as native CIFInventory is never blocked by it", () => {
	const { owner, frames } = setup();
	open( owner );
	owner.process( { kind: "gacha-roll", entry: 1, slot: 14 }, 0 );
	assert.equal( owner.state().gacha.phase, "rolling" );
	assert.equal( owner.state().inventoryPending, false );
	owner.step( 4000 );
	assert.equal( owner.state().gacha.phase, "waiting" );
	assert.equal( owner.state().inventoryPending, false );
	const move = owner.move( 13, 15, 1, 4000 );
	assert.equal( move.opcode, 0x706d );
	assert.equal( frames.at( -1 ), move );
});
test("a reinforcement records its outcome for the window (62B0B0)", () => {
	const { owner } = setup();
	owner.process( { kind: "alchemy-open" }, 0 );
	owner.process( { kind: "alchemy-start", mode: "reinforce", slots: [ 13, 14 ] }, 0 );
	const before = defined( owner.state().inventory.find( i => i.slot === 13 ) );
	owner.receive( 0xb373, Uint8Array.from( [ 1, 1, 13, ...equipment( 1 ) ] ) );
	assert.deepEqual( owner.state().alchemy.outcome, {
		sequence: 1,
		flags: 0x10,
		plus: 1,
		previousPlus: before.plus,
		durability: defined( owner.state().inventory.find( i => i.slot === 13 ) ).durability,
		previousDurability: before.durability
	} );
	owner.receive( 0xb373, Uint8Array.of( 1, 0, 13, 1 ) );
	const destroyed = defined( owner.state().alchemy.outcome );
	assert.deepEqual( [ destroyed.sequence, destroyed.flags, destroyed.plus, destroyed.previousPlus ], [
		2,
		0x40,
		0,
		1
	] );
	// A refusal finishes no reinforcement.
	owner.receive( 0xb373, Uint8Array.of( 2, 0x10 ) );
	assert.equal( defined( owner.state().alchemy.outcome ).sequence, 2 );
});
test("a stone whose roll failed records a failure outcome; a stone refusal records none (62B0B0 0x80)", () => {
	const { owner } = setup();
	owner.process( { kind: "alchemy-open" }, 0 );
	owner.process( { kind: "alchemy-start", mode: "attribute", slots: [ 13, 14 ] }, 0 );
	const target = defined( owner.state().inventory.find( i => i.slot === 13 ) );
	// 0xB651 [2, 0x23]: the roll failed and the stone is spent. The window
	// must show the failure effect and line, not stay silent.
	owner.receive( 0xb651, Uint8Array.of( 2, 0x23 ) );
	assert.deepEqual( owner.state().alchemy.outcome, {
		sequence: 1,
		flags: 0x80,
		plus: target.plus,
		previousPlus: target.plus,
		durability: target.durability,
		previousDurability: target.durability
	} );
	assert.deepEqual( result.alchemyResultLines( defined( owner.state().alchemy.outcome ), key => key ), [
		"UIIT_MSG_REINFORCERR_FAIL"
	] );
	// A refused stone (wrong degree, 0x21) finishes nothing: the new attempt
	// cleared the last outcome and the refusal records none.
	owner.process( { kind: "alchemy-start", mode: "attribute", slots: [ 13, 14 ] }, 0 );
	owner.receive( 0xb651, Uint8Array.of( 2, 0x21 ) );
	assert.equal( owner.state().alchemy.outcome, undefined );
});
const result = await import( "../../src/engine/foundation/ui/alchemy-result.ts" );
test("62B0B0 words the outcome and plays its effect once", () => {
	const copy = key =>
		({
			UIIT_MSG_REINFORCERR_SUCCESS: "success [%d]",
			UIIT_MSG_REINFORCERR_FAIL: "fail",
			UIIT_MSG_REINFORCERR_FAIL_RESULT_OPTLV_ZERO: "level gone",
			UIIT_MSG_REINFORCERR_FAIL_RESULT_OPTLV_DOWN: "level [%d] (down %d)",
			UIIT_MSG_REINFORCERR_FAILDOWN_DURABILITY: "durability [%d] (down %d)",
			UIIT_MSG_REINFORCERR_BREAKDOWN: "destroyed"
		})[key] ?? key;
	const outcome = flags => ({ sequence: 1, flags, plus: 3, previousPlus: 5, durability: 40, previousDurability: 52 });
	assert.deepEqual( result.alchemyResultLines( outcome( 0x10 ), copy ), [ "success [3]" ] );
	assert.deepEqual( result.alchemyResultLines( outcome( 0x20 ), copy ), [ "fail" ] );
	assert.deepEqual( result.alchemyResultLines( outcome( 0x23 ), copy ), [
		"level [3] (down 2)",
		"durability [40] (down 12)"
	] );
	assert.deepEqual( result.alchemyResultLines( { ...outcome( 0x21 ), plus: 0 }, copy ), [ "level gone" ] );
	assert.deepEqual( result.alchemyResultLines( outcome( 0x40 ), copy ), [ "destroyed" ] );
	// 62B0B0's 0x80 arm: a stone's failed roll says the alchemy failed.
	assert.deepEqual( result.alchemyResultLines( outcome( 0x80 ), copy ), [ "fail" ] );
	assert.equal( result.alchemyEffectTexture( 0x80 ), "interface/alchemy/alcm_effect_fail_1" );
	assert.equal( result.alchemyEffectTexture( 0x10 ), "interface/alchemy/alcm_effect_success" );
	assert.equal( result.alchemyEffectTexture( 0x40 ), "interface/alchemy/alcm_effect_fail_1" );
	// Sixteen 64 px cells of a 4x4 atlas, 50 ms each, then nothing.
	assert.deepEqual( result.alchemyEffectCell( 0 ), [ 0, 0, .25, .25 ] );
	assert.deepEqual( result.alchemyEffectCell( 5 * 50 ), [ .25, .25, .25, .25 ] );
	assert.deepEqual( result.alchemyEffectCell( 15 * 50 + 49 ), [ .75, .75, .25, .25 ] );
	assert.equal( result.alchemyEffectCell( 16 * 50 ), null );
});

test("Gori's exchange request carries the operator's gid, and a voucher update words the chat line from its recorded reward", () => {
	const frames = [], owner = createInventory( f => frames.push( f ), () => {} );
	owner.bootstrap( {
		refItemSnapshot: [
			{ refObjId: 2, typeFlags: 0xf6c },
			{ refObjId: 3, typeFlags: 0x176c },
			{ refObjId: 4140, typeFlags: 0x2c, name: "Rare Spear" }
		],
		equipItems: [ { slot: 14, refObjId: 2, body: [ 2, 0, 0, 0, 1, 0 ] } ]
	} );
	const request = owner.process( { kind: "gacha-exchange", gid: 7 }, 0 );
	assert.equal( request.opcode, 0x71be );
	assert.deepEqual( [ ...request.payload ], [ 7, 0, 0, 0 ] );
	assert.deepEqual( [ ...frames.at( -1 ).payload ], [ 7, 0, 0, 0 ] );
	assert.throws( () => owner.process( { kind: "gacha-exchange", gid: 0 }, 0 ), /unavailable/ );
	// The roll turns the ticket into a voucher; the ticket is not a voucher, so no line yet.
	const delta = Buffer.alloc( 23 );
	delta[0] = 14;
	delta[1] = 0x21;
	delta.writeUInt32LE( 3, 2 );
	delta[6] = 2;
	delta.writeBigUInt64LE( 4140n, 7 );
	delta.writeBigUInt64LE( 1n, 15 );
	owner.receive( 0x3645, delta );
	assert.equal( owner.takeExchangeNotice(), null );
	// The exchange empties the voucher by quantity, which is what 7654B0 words.
	owner.receive( 0x3645, Uint8Array.of( 14, 8, 0, 0 ) );
	assert.deepEqual( owner.takeExchangeNotice(), {
		key: "UIIT_MSG_GACHA_EXCHANGE",
		value: 1,
		arguments: [ "Rare Spear" ],
		formatKinds: [ "s", "d" ]
	} );
	assert.equal( owner.takeExchangeNotice(), null, "the line is raised once" );
});

test("Magic Pang prize rows rebuild when references or bootstrap update tooltips, names, or icons", () => {
	const owner = createInventory( () => {}, () => {} );
	const firstPrizeRef = owner.state().gacha.prizes[0].refObjId;
	assert.equal( owner.state().gacha.prizes[0].item, undefined );

	const tooltipV1 = {
		kind: 4,
		degree: 1,
		fields: { maxStack: 50, varianceIntMin1c0: 0, varianceIntMax1c4: 0, descKey: "UIIT_DESC_PRIZE_V1" }
	};
	const tooltipV2 = {
		kind: 4,
		degree: 1,
		fields: { maxStack: 50, varianceIntMin1c0: 0, varianceIntMax1c4: 0, descKey: "UIIT_DESC_PRIZE_V2" }
	};

	owner.references( [
		{
			refObjId: firstPrizeRef,
			typeFlags: 0xf6c,
			name: "First Prize Name",
			icon: "icon\\item\\etc\\prize_v1.ddj",
			tooltip: tooltipV1
		}
	] );
	const afterFirstReferences = owner.state().gacha.prizes[0];
	assert.equal( afterFirstReferences.icon, "icon\\item\\etc\\prize_v1.ddj" );
	assert.equal( afterFirstReferences.item?.name, "First Prize Name" );
	assert.deepEqual( afterFirstReferences.item?.tooltip, tooltipV1 );

	// Updating the same ref entry keeps map sizes equal (1 icon, 1 ref, 1 name, 1 tooltip)
	// and must still invalidate the cached gacha prize rows.
	owner.references( [
		{
			refObjId: firstPrizeRef,
			typeFlags: 0xf6c,
			name: "Updated Prize Name",
			icon: "icon\\item\\etc\\prize_v2.ddj",
			tooltip: tooltipV2
		}
	] );
	const afterSecondReferences = owner.state().gacha.prizes[0];
	assert.equal( afterSecondReferences.icon, "icon\\item\\etc\\prize_v2.ddj" );
	assert.equal( afterSecondReferences.item?.name, "Updated Prize Name" );
	assert.deepEqual( afterSecondReferences.item?.tooltip, tooltipV2 );
});

test("Magic Pang prize catalogue serves degree-9 Seal of Star under Pool V2 and falls back to retail v1.150 when off", () => {
	assert.deepEqual( gachaPrizes(), [
		{ entry: 1, refObjId: 4146, quantity: 1, nameSymbol: "SN_ITEM_CH_TBLADE_09_A_RARE" },
		{ entry: 2, refObjId: 4038, quantity: 1, nameSymbol: "SN_ITEM_CH_SWORD_09_A_RARE" },
		{ entry: 3, refObjId: 4110, quantity: 1, nameSymbol: "SN_ITEM_CH_SPEAR_09_A_RARE" },
		{ entry: 4, refObjId: 4074, quantity: 1, nameSymbol: "SN_ITEM_CH_BLADE_09_A_RARE" },
		{ entry: 5, refObjId: 4182, quantity: 1, nameSymbol: "SN_ITEM_CH_BOW_09_A_RARE" },
		{ entry: 6, refObjId: 4218, quantity: 1, nameSymbol: "SN_ITEM_CH_SHIELD_09_A_RARE" },
		{ entry: 7, refObjId: 6891, quantity: 1, nameSymbol: "SN_ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09" },
		{ entry: 8, refObjId: 6855, quantity: 1, nameSymbol: "SN_ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09" },
		{ entry: 9, refObjId: 3884, quantity: 1, nameSymbol: "SN_ITEM_MALL_GOLD_TIME_SERVICE_TICKET_4W" },
		{ entry: 10, refObjId: 5912, quantity: 1000, nameSymbol: "SN_ITEM_MALL_HP_SUPERSET_5_BAG" },
		{ entry: 11, refObjId: 5913, quantity: 1000, nameSymbol: "SN_ITEM_MALL_MP_SUPERSET_5_BAG" },
		{ entry: 12, refObjId: 3783, quantity: 11, nameSymbol: "SN_ITEM_MALL_RESURRECTION_100P_SCROLL" }
	] );
	assert.deepEqual(
		gachaPrizes( false ).slice( 0, 6 ),
		[
			{ entry: 1, refObjId: 4140, quantity: 1, nameSymbol: "SN_ITEM_CH_TBLADE_07_A_RARE" },
			{ entry: 2, refObjId: 4035, quantity: 1, nameSymbol: "SN_ITEM_CH_SWORD_08_A_RARE" },
			{ entry: 3, refObjId: 4104, quantity: 1, nameSymbol: "SN_ITEM_CH_SPEAR_07_A_RARE" },
			{ entry: 4, refObjId: 4071, quantity: 1, nameSymbol: "SN_ITEM_CH_BLADE_08_A_RARE" },
			{ entry: 5, refObjId: 4176, quantity: 1, nameSymbol: "SN_ITEM_CH_BOW_07_A_RARE" },
			{ entry: 6, refObjId: 4215, quantity: 1, nameSymbol: "SN_ITEM_CH_SHIELD_08_A_RARE" }
		]
	);

	const v2Owner = createInventory( () => {}, () => {} );
	v2Owner.references( [
		{ refObjId: 4146, typeFlags: 0x06ac, name: "Demon Polearm", icon: "item\\china\\weapon\\tblade_09.ddj" }
	] );
	assert.equal( v2Owner.state().gacha.prizes[0].refObjId, 4146 );
	assert.equal( v2Owner.state().gacha.prizes[0].item?.name, "Demon Polearm" );

	const v1Owner = createInventory( () => {}, () => {} );
	v1Owner.references( [
		{ refObjId: 4140, typeFlags: 0x06ac, name: "Heavenly Polearm", icon: "item\\china\\weapon\\tblade_07.ddj" }
	] );
	assert.equal( v1Owner.state().gacha.prizes[0].refObjId, 4140 );
	assert.equal( v1Owner.state().gacha.prizes[0].item?.name, "Heavenly Polearm" );
});

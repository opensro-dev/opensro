/*
===========================================================================

buff-lifecycle.test.mjs - tests for combat.ts, buff-board.ts,
presentation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ];

test("Tailwind shows independent recipient charges and full-stack readiness", () => {
	const game = {
		...fixture().c.state(),
		localGid: 10,
		petTailwind: { gid: 10, book: 57, stacks: 10, speedPercent: 30 }
	};
	const row = buffBoard( game, 0 ).find( entry => entry.id === "pet-tailwind" );
	assert.equal(
		buffBoard( game, 0, new Set(), undefined, false ).some( entry => entry.id === "pet-tailwind" ),
		false
	);
	assert.equal( row?.stack, 10 );
	assert.equal( row?.ready, true );
	assert.match( row?.helpText ?? "", /Movement speed \+30%/ );
	assert.equal(
		buffBoard( { ...game, petTailwind: { ...game.petTailwind, gid: 11 } }, 0 ).some( entry =>
			entry.id === "pet-tailwind"
		),
		false
	);
	assert.equal(
		buffBoard( { ...game, petTailwind: { ...game.petTailwind, book: 55 } }, 0 ).find( entry =>
			entry.id === "pet-tailwind"
		)?.ready,
		false
	);
});
function fixture() {
	const events = [], c = createCombat( () => undefined, e => events.push( e ) );
	const catalog = [ 1, 2, 3 ].map( id => ({
		id,
		icon: "skill/china/sword_smash_a.ddj",
		name: "Buff",
		buffSecondary: id === 2
	}) );
	c.references(
		catalog.map( row => ({
			id: row.id,
			status: true,
			effectRider: false,
			effectDurationMs: 1000,
			zeroEffectDuration: false
		}) )
	);
	c.cooldownReferences( 10, catalog );
	return {
		c,
		events,
		catalog,
		apply( gid, skill, token, phase = 2 ) {
			c.receive( 0xb419, Uint8Array.from( [ ...u32( gid ), ...u32( skill ), ...u32( token ), phase ] ), 100 );
		},
		end( ...tokens ) {
			c.receive( 0xb6a0, Uint8Array.from( [ tokens.length, ...tokens.flatMap( u32 ) ] ), 500 );
		}
	};
}
test("B6A0 resolves each token, tears down peers silently, and primary feedback survives duplicate packets", () => {
	const f = fixture();
	f.apply( 10, 1, 100 );
	f.apply( 10, 3, 101 );
	f.apply( 11, 1, 102 );
	f.apply( 10, 2, 103 );
	f.end( 100, 100, 101, 102, 103, 999 );
	f.end( 100 );
	assert.equal( f.c.state().attachedEffects.length, 0 );
	assert.deepEqual( f.events, [ { kind: "buff-ended", gid: 10, at: 500 }, {
		kind: "buff-ended",
		gid: 10,
		at: 500
	} ] );
	assert.equal( f.c.state().buffSlots.length, 2 );
	assert.ok( f.c.state().buffSlots.every( s => s.state === "departing" ) );
});

test("buff dismissal retains the attached rank identity when another rank shares its group", () => {
	const f = fixture();
	f.apply( 10, 1, 100 );
	const catalog = [ ...f.catalog ].reverse().map( s => ({
		...s,
		group: 77,
		level: s.id,
		buffCancel: "direct",
		buffCancelInstance: true
	}) );
	const row = buffBoard( { ...f.c.state(), localGid: 10, skillCatalog: catalog, skills: [ 3 ] }, 200 )[0];
	assert.deepEqual( row.cancel, { mode: "direct", skillId: 1, token: 100, instance: 100 } );
	assert.equal( defined( row.helpSource ).skill, 1 );
});
test("packet phase does not select the local buff list, and zero-token transient effects do not add a slot", () => {
	const f = fixture();
	f.apply( 10, 1, 100, 1 );
	f.apply( 10, 2, 101, 3 );
	f.apply( 10, 1, 0 );
	assert.deepEqual( f.c.state().buffSlots.map( s => s.secondary ), [ false, true ] );
	f.end( 100, 101 );
	assert.equal( f.events.length, 1 );
});
test("teardown validation is atomic and ordinary UI timer exhaustion does not expire server effects", () => {
	const f = fixture();
	f.apply( 10, 1, 100 );
	assert.throws( () => f.c.receive( 0xb6a0, Uint8Array.of( 2, ...u32( 100 ) ), 500 ), /Invalid/ );
	f.c.step( 5000 );
	assert.equal( f.c.state().attachedEffects.length, 1 );
	assert.equal( f.c.state().buffSlots[0].state, "active" );
	assert.equal( f.events.length, 0 );
});
test("native departure keeps the slot for nine timer frames and disables active effect interaction", () => {
	const f = fixture();
	f.apply( 10, 1, 100 );
	f.end( 100 );
	const game = () => ({ ...f.c.state(), localGid: 10, skillCatalog: f.catalog });
	assert.deepEqual( buffBoard( game(), 500 )[0].departure, { frame: 0, alpha: 1 } );
	assert.deepEqual( buffBoard( game(), 900 )[0].departure, { frame: 4, alpha: 170 / 255 } );
	assert.equal( defined( buffBoard( game(), 1100 )[0].departure ).alpha, 0 );
	assert.equal( buffBoard( game(), 500 )[0].helpSource, undefined );
	f.c.step( 1399 );
	assert.equal( f.c.state().buffSlots.length, 1 );
	f.c.step( 1400 );
	assert.equal( f.c.state().buffSlots.length, 0 );
});
test("new application during departure has separate slot identity and reset/reseed is silent", () => {
	const f = fixture();
	f.apply( 10, 1, 100 );
	f.end( 100 );
	f.apply( 10, 1, 101 );
	assert.equal( new Set( f.c.state().buffSlots.map( s => s.serial ) ).size, 2 );
	f.c.seedEffects( 10, [ { id: 1, token: 102, status: 2, remaining: 500 } ], 600 );
	assert.equal( f.c.state().buffSlots.length, 1 );
	assert.equal( f.events.length, 1 );
	f.c.remove( 10 );
	assert.equal( f.c.state().buffSlots.length, 0 );
	assert.equal( f.events.length, 1 );
	f.c.clear();
	assert.equal( f.c.state().buffSlots.length, 0 );
});
test("explicit dura zero excludes a slot whereas absent dura admits an untimed slot", () => {
	const f = fixture();
	f.c.references( [ { id: 1, status: true, effectRider: false, effectDurationMs: 0, zeroEffectDuration: true }, {
		id: 2,
		status: true,
		effectRider: false,
		effectDurationMs: 0,
		zeroEffectDuration: false
	} ] );
	f.apply( 10, 1, 100 );
	f.apply( 10, 2, 101 );
	assert.deepEqual( f.c.state().buffSlots.map( s => s.effect.skill ), [ 2 ] );
});
test("special C0000000 timer uses strict expiry and ordinary timers remain authoritative", () => {
	const f = fixture();
	f.c.seedEffects( 10, [ { id: 0xc0000000, token: 0, status: 2, remaining: 100 } ], 0 );
	f.c.step( 100 );
	assert.equal( f.events.length, 0 );
	f.c.step( 101 );
	assert.equal( f.events.length, 1 );
	assert.equal( f.c.state().buffSlots[0].state, "departing" );
	f.c.step( 102 );
	assert.equal( f.events.length, 1 );
});
test("efr=3 keeps the board gauge full without changing effect-world lifetime", () => {
	const f = fixture();
	f.c.references( [ {
		id: 1,
		status: true,
		effectRider: false,
		effectDurationMs: 1000,
		indefiniteBuffTimer: true
	} ] );
	f.apply( 10, 1, 100 );
	assert.equal( f.c.state().attachedEffects[0].remainingMs, 1000 );
	assert.equal( buffBoard( { ...f.c.state(), localGid: 10, skillCatalog: f.catalog }, 900 )[0].fraction, 1 );
	f.end( 100 );
	assert.equal( f.events.length, 1 );
});
test("lnks fourth parameter suppresses detection board insertion, not the effect object", () => {
	const f = fixture();
	f.c.references( [ {
		id: 1,
		status: false,
		effectRider: false,
		effectDurationMs: 1000,
		huntingPoint: false,
		stealthDuration: false,
		hideDetectionBuff: true
	} ] );
	f.c.receive( 0xb5ed, Uint8Array.from( [ ...u32( 1 ), ...u32( 100 ), ...u32( 20 ), 0, 0 ] ), 100 );
	assert.equal( f.c.state().attachedEffects.length, 1 );
	assert.equal( f.c.state().buffSlots.length, 0 );
	f.end( 100 );
	assert.equal( f.c.state().attachedEffects.length, 0 );
	assert.equal( f.events.length, 0 );
});
test("ordered feedback survives coalesced gameplay snapshots and drains once", () => {
	const p = createPresentation();
	p.apply( {
		sequence: 1,
		events: [ { kind: "reset", epoch: 1 }, { kind: "buff-ended", gid: 10, at: 100 }, {
			kind: "gameplay",
			state: { localGid: 10, attachedEffects: [] }
		}, { kind: "buff-ended", gid: 10, at: 101 } ]
	} );
	assert.equal( p.takeSounds().length, 2 );
	assert.deepEqual( p.takeSounds(), [] );
	p.apply( { sequence: 2, events: [ { kind: "buff-ended", gid: 10, at: 102 }, { kind: "reset", epoch: 2 } ] } );
	assert.deepEqual( p.takeSounds(), [] );
	p.dispose();
});

test("B6A0 resolves a retained cast object and finalizes it once without buff feedback", () => {
	const f = fixture();
	f.c.receive( 0xb245, Uint8Array.from( [ 1, 0, ...u32( 1 ), ...u32( 10 ), ...u32( 200 ), ...u32( 11 ), 0 ] ), 100 );
	f.events.length = 0;
	f.end( 200, 200 );
	f.end( 200 );
	assert.equal( f.c.state().casts[0].cancelledAtMs, 500 );
	assert.deepEqual( f.events.map( e => e.kind ), [ "cast-finalize" ] );
	assert.equal( f.c.state().buffSlots.length, 0 );
});

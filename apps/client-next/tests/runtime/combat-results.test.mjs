/*
===========================================================================

combat-results.test.mjs - tests for combat.ts, random-idle.ts,
cast-displacement.ts, movement.ts, ...

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
const { advanceRandomIdle } = await import( "../../src/engine/foundation/animation/random-idle.ts" );
const { displacementSegment } = await import( "../../src/engine/foundation/gameplay/cast-displacement.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
function writer() {
	const bytes = [];
	return {
		u8( n ) {
			bytes.push( n & 255 );
			return this;
		},
		u16( n ) {
			return this.u8( n ).u8( n >>> 8 );
		},
		u32( n ) {
			return this.u16( n ).u16( n >>> 16 );
		},
		bytes() {
			return Uint8Array.from( bytes );
		}
	};
}
function header() {
	return writer().u8( 1 ).u8( 0 ).u32( 7 ).u32( 1 ).u32( 9 ).u32( 2 );
}
function hp( c, gid, n ) {
	const w = writer().u32( gid ).u8( 0 ).u8( 0 ).u8( 1 ).u32( n );
	c.receive( 0x33a6, w.bytes() );
}

test("natural recovery applies server HP/MP pulses once without a skill cast", () => {
	const c = createCombat();
	c.seed( 1, { hp: 100, mp: 50, maxHp: 1000, maxMp: 1000 } );
	for ( const [time, hp, mp] of [ [ 4000, 108, 58 ], [ 8000, 188, 138 ] ] ) {
		const packet = writer().u32( 1 ).u16( 0x10 ).u8( 3 ).u32( hp ).u32( mp ).bytes();
		c.receive( 0x33a6, packet, time );
		c.receive( 0x33a6, packet, time + 1 );
		assert.equal( defined( c.state().vitals.find( v => v.gid === 1 ) ).hp, hp );
		assert.equal( defined( c.state().vitals.find( v => v.gid === 1 ) ).mp, mp );
	}
	c.step( 12000 );
	assert.equal( defined( c.state().vitals.find( v => v.gid === 1 ) ).hp, 188, "client clock cannot award recovery" );
	assert.equal( c.state().casts.length, 0 );
});

test("self recovery casts carry no damage rows and reconcile native healing once", () => {
	const c = createCombat();
	c.seed( 1, { hp: 1, mp: 29, maxHp: 200 } );
	c.cooldownReferences( 1, [ { id: 7, cooldownMs: 2100, cooldownGroup: 0 } ] );
	const open = writer().u8( 1 ).u8( 0 ).u32( 7 ).u32( 1 ).u32( 9 ).u32( 1 ).u8( 0 ).bytes();
	assert.equal( open.length, 19 );
	assert.equal( c.receive( 0xb245, open, 100 ), true );
	assert.equal(
		defined( c.state().vitals.find( v => v.gid === 1 ) ).hp,
		1,
		"cast animation cannot heal independently"
	);
	const vitals = writer().u32( 1 ).u16( 0x40 ).u8( 3 ).u32( 90 ).u32( 0 ).bytes();
	c.receive( 0x33a6, vitals, 101 );
	c.receive( 0x33a6, vitals, 102 );
	assert.equal( defined( c.state().vitals.find( v => v.gid === 1 ) ).hp, 90 );
	assert.equal( defined( c.state().vitals.find( v => v.gid === 1 ) ).mp, 0 );
	c.receive( 0xb505, writer().u8( 2 ).u8( 0 ).u32( 9 ).bytes(), 1101 );
	assert.deepEqual( c.takeCancellations(), [ 9 ] );
	c.step( 1301 );
	assert.equal( c.state().casts.length, 0 );
});
test("v1.150 MP and cooldown refusals preserve active casts and authoritative vitals", () => {
	const c = createCombat();
	c.seed( 1, { hp: 100, mp: 19, maxHp: 100 } );
	hp( c, 2, 100 );
	const accepted = header().u8( 1 ).u8( 1 ).u8( 1 ).u32( 2 ).u8( 0 ).u32( 10 << 8 ).u32( 0 ).bytes();
	c.receive( 0xb245, accepted, 100 );
	c.receive( 0x33a6, writer().u32( 1 ).u8( 0 ).u8( 0 ).u8( 3 ).u32( 100 ).u32( 0 ).bytes() );
	const casts = c.state().casts, vitals = c.state().vitals;
	for ( const code of [ 4, 5 ] ) {
		assert.equal( c.receive( 0xb245, Uint8Array.of( 2, code ), 200 ), true );
		assert.equal( c.state().error, `Cast rejected: ${code}` );
		assert.deepEqual( c.state().casts, casts );
		assert.deepEqual( c.state().vitals, vitals );
	}
});
test("target-major multi-hit results preserve victims, fatal flags and packed impact variants", () => {
	const c = createCombat();
	hp( c, 2, 100 );
	hp( c, 3, 100 );
	const p = header().u8( 1 ).u8( 2 ).u8( 2 ).u32( 2 ).u8( 0 ).u32( 10 << 8 | 3 ).u32( 4 ).u8( 2 ).u32( 3 ).u8( 0 )
		.u32( 20 << 8 ).u32( 0 ).u8( 128 ).u32( 30 << 8 ).u32( 0 ).bytes();
	assert.equal( c.receive( 0xb245, p, 100 ), true );
	assert.deepEqual( c.state().vitals.map( v => v.hp ), [ 90, 0 ] );
	assert.deepEqual( defined( c.state().casts[0].results ).map( r => r.impacts.map( h => h.damage ) ), [ [ 10, 0 ], [
		20,
		30
	] ] );
	c.receive( 0xb245, p, 200 );
	assert.deepEqual( c.state().vitals.map( v => v.hp ), [ 90, 0 ] );
});
test("all truncations reject atomically; continuation results append to the proper target", () => {
	const p = header().u8( 1 ).u8( 1 ).u8( 1 ).u32( 3 ).u8( 4 ).u32( 8 << 8 ).u32( 0 ).u16( 0x5c87 ).u16( 100 ).u16(
		-2
	).u16( 200 ).bytes();
	for ( let length = 2; length < p.length; length++ ) {
		const c = createCombat();
		hp( c, 3, 99 );
		assert.throws( () => c.receive( 0xb245, p.slice( 0, length ) ) );
		assert.equal( c.state().casts.length, 0 );
		assert.equal( c.state().vitals[0].hp, 99 );
		assert.deepEqual( c.takeDisplacements(), [] );
	}
	const c = createCombat();
	hp( c, 3, 99 );
	c.receive( 0xb245, p );
	const q = writer().u8( 1 ).u32( 9 ).u32( 2 ).u8( 1 ).u8( 1 ).u8( 1 ).u32( 3 ).u8( 7 ).u32( 10 << 8 ).u16( 4 ).u16(
		5
	).bytes();
	c.receive( 0xb505, q, 50 );
	assert.equal( c.state().vitals[0].hp, 91 );
	assert.equal( defined( c.state().casts[0].results )[0].impacts.length, 2 );
	assert.deepEqual( defined( c.state().casts[0].results )[0].impacts[1].auxiliary, [ 4, 5 ] );
	assert.deepEqual( c.takeDisplacements()[0].destination, { regionId: 0x5c87, x: 100, y: -2, z: 200 } );
});
test("both steering coordinates preserve native order and replace pending input through motion ownership", () => {
	const c = createCombat(),
		p = header().u8( 10 ).u16( 0x5c87 ).u16( 100 ).u16( 0 ).u16( 200 ).u16( 0x5c87 ).u16( 150 ).u16( 0 ).u16( 200 )
			.bytes();
	c.receive( 0xb245, p );
	const d = c.takeDisplacements();
	assert.deepEqual( d.map( row => row.kind ), [ 8, 2 ] );
	const from = { regionId: 0x5c87, x: 0, y: 0, z: 200, angle: 0 };
	assert.equal( displacementSegment( from, d[0], 0, 50 ).duration, 200 );
	assert.equal( displacementSegment( from, d[1], 0, 50 ).duration, 0 );
});
test("idle waits 15 seconds, rolls motion before delay, falls back, and resets on activity", () => {
	const s = { remaining: 15, previous: 0, started: 0 },
		calls = [],
		range = ( lo, hi ) => {
			calls.push( [ lo, hi ] );
			return lo === 0 ? 1 : 12345;
		};
	advanceRandomIdle( s, 14.999, true, [ "idle122" ], range, () => 1 );
	assert.equal( s.clip, undefined );
	advanceRandomIdle( s, 15, true, [ "idle122" ], range, () => 1 );
	assert.equal( s.clip, "idle122" );
	assert.deepEqual( calls, [ [ 0, 3 ], [ 10000, 15000 ] ] );
	advanceRandomIdle( s, 15.1, true, [ "idle122" ], range, () => 1 );
	assert.equal( calls.length, 2 );
	assert.equal( s.remaining, 15, "active native state 7 resets the idle countdown" );
	advanceRandomIdle( s, 15.2, false, [ "idle122" ], range, () => 1 );
	assert.equal( s.clip, undefined );
	assert.equal( s.remaining, 15 );
	advanceRandomIdle( s, 20, true, [ "idle122" ], range, () => 1 );
	assert.equal( s.clip, undefined );
});

test("completed idle keeps its 200ms exit without rolling another motion", () => {
	const s = { remaining: 15, previous: 0, started: 0 },
		calls = [],
		range = ( lo, hi ) => {
			calls.push( [ lo, hi ] );
			return lo;
		};
	const step = now => advanceRandomIdle( s, now, true, [ "idle122" ], range, () => 1 );
	step( 15 );
	step( 15.9 );
	step( 16.1 );
	assert.equal( s.clip, "idle122" );
	step( 16.201 );
	assert.equal( s.clip, undefined );
	assert.equal( calls.length, 2 );
	assert.ok( s.remaining > 14 && s.remaining < 15 );
});

test("local and remote forced movement cancel only cast-owned travel and keep in-flight requests", () => {
	const m = createMovement( () => {} ), pose = { regionId: 0x5c87, x: 100, y: 0, z: 100, angle: 0 };
	m.seed( pose );
	m.request( { ...pose, x: 150 }, 0 );
	const command = { gid: 1, token: 9, kind: 8, destination: { ...pose, x: 300 } };
	m.displace( command, 0 );
	// The request was not yet answered when the displacement arrived, so the
	// server handles it after the dash: its receipt still owns the outcome.
	assert.equal( m.state().pendingMoves, 1 );
	assert.equal( m.state().acknowledgedMove, 0 );
	m.step( 100 );
	assert.equal( defined( m.state().pose ).x, 150 );
	m.cancelCast( 8, 100 );
	m.step( 200 );
	assert.equal( defined( m.state().pose ).x, 200 );
	m.cancelCast( 9, 200 );
	m.step( 1000 );
	assert.equal( defined( m.state().pose ).x, 200 );
	const remote = createEntityMotion(), entity = { ...pose, gid: 1, heading: 0 };
	remote.displace( entity, { ...command, kind: 5 }, 0 );
	assert.deepEqual( remote.cancelCast( 9, 100 ), [] );
	assert.equal( remote.step( 400 )[0].x, 200 );
	assert.equal( remote.step( 800 )[0].x, 300 );
	assert.deepEqual( remote.step( 900 ), [] );
	m.displace( { ...command, kind: 2 }, 1000 );
	assert.equal( defined( m.state().pose ).x, 300 );
	assert.equal( m.step( 2000 ), false );
});

test("only accepted local casts start cooldowns; duplicates, refusals and cast retirement cannot restart or erase them", () => {
	const c = createCombat();
	c.cooldownReferences( 1, [ { id: 7, cooldownMs: 5000, cooldownGroup: 3 } ] );
	const accepted = header().u8( 1 ).u8( 1 ).u8( 1 ).u32( 2 ).u8( 0 ).u32( 10 << 8 ).u32( 0 ).bytes();
	c.receive( 0xb245, Uint8Array.of( 2, 4 ), 50 );
	assert.deepEqual( c.state().skillCooldowns, [] );
	c.receive( 0xb245, accepted, 100 );
	assert.deepEqual( c.state().skillCooldowns, [ { skill: 7, group: 3, startedAtMs: 100, durationMs: 5000 } ] );
	c.receive( 0xb245, accepted, 200 );
	assert.equal( c.state().skillCooldowns[0].startedAtMs, 100 );
	c.receive( 0xb505, Uint8Array.of( 2, 0, 9, 0, 0, 0 ), 300 );
	c.step( 600 );
	assert.equal( c.state().casts.length, 0 );
	assert.equal( c.state().skillCooldowns.length, 1 );
	c.step( 5600 );
	assert.deepEqual( c.state().skillCooldowns, [] );
	c.clear();
	const other = accepted.slice();
	new DataView( other.buffer ).setUint32( 6, 2, true );
	c.cooldownReferences( 1, [ { id: 7, cooldownMs: 5000 } ] );
	c.receive( 0xb245, other, 6000 );
	assert.deepEqual( c.state().skillCooldowns, [] );
});
/*
================
knockback decoration

CIDecoDamageEffect_Initialize (8D5440) attaches SYSTEM_KNOCKBACK only for a
type-5 knockback; a type-4 knockdown walks its waypoint undecorated.
================
*/
test("a type-5 knockback decorates its target with SYSTEM_KNOCKBACK", () => {
	for ( const type of [ 4, 5 ] ) {
		const events = [], c = createCombat( undefined, e => events.push( e ) );
		hp( c, 3, 99 );
		c.receive(
			0xb245,
			header().u8( 1 ).u8( 1 ).u8( 1 ).u32( 3 ).u8( type ).u32( 8 << 8 ).u32( 0 ).u16( 0x5c87 ).u16( 100 ).u16(
				-2
			)
				.u16( 200 ).bytes()
		);
		const marks = events.filter( e => e.kind === "system-effect" );
		assert.deepEqual( marks, type === 5 ? [ { kind: "system-effect", gid: 3, effect: 0x8000001d } ] : [] );
		assert.equal( c.takeDisplacements()[0].kind, type );
	}
});

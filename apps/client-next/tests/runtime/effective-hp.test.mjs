/*
===========================================================================

effective-hp.test.mjs - tests for effective-hp.ts, damage-feedback.ts,
combat.ts, presentation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";
import { defined } from "../helpers/defined.mjs";
const { createEffectiveHp } = await import( "../../src/engine/runtime/presentation/effective-hp.ts" );
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );
const hit = ( damage, fatal = false, type = 0 ) => ({ damage, fatal, type, flags: 0, secondaryAmount: 0 });
const seed = ( hp = 100 ) => ({ kind: "hp-seed", gid: 2, hp });
const result = ( key = "9:2:0", fatal = false ) => ({ kind: "hp-result", gid: 2, key, fatal });
const refresh = ( hp, sourceFlags = 1, atMs = 0 ) => ({ kind: "hp-refresh", gid: 2, hp, sourceFlags, atMs });
const cast = {
	token: 9,
	caster: 1,
	target: 2,
	skill: 7,
	damage: 30,
	fatal: false,
	results: [ { target: 2, impacts: [ hit( 10 ), hit( 20 ) ] } ]
};

test("retail HP waits for each actual callback; a fatal result does not publish LIFE", () => {
	const h = createEffectiveHp(),
		events = [],
		c = createCombat( () => undefined, e => {
			events.push( e );
			h.receive( e );
		} );
	c.seed( 2, { hp: 100 } );
	const p = new Uint8Array( 35 ), v = new DataView( p.buffer );
	p[0] = 1;
	v.setUint32( 2, 7, true );
	v.setUint32( 6, 1, true );
	v.setUint32( 10, 9, true );
	v.setUint32( 14, 2, true );
	p.set( [ 1, 1, 1 ], 18 );
	v.setUint32( 21, 2, true );
	p[25] = 128;
	v.setUint32( 26, 100 << 8, true );
	// Packed normal impact has 4 secondary bytes, making the payload 34 bytes.
	c.receive( 0xb245, p.slice( 0, 34 ), 100 );
	assert.equal( h.hp( 2 ), 100 );
	assert.equal( h.dead( 2 ), false );
	assert.equal( c.state().vitals[0].hp, 0, "worker legality remains authoritative" );
	assert.equal( events.filter( e => e.kind === "hp-result" ).length, 1 );
	const baseline = new Uint8Array( 11 ), bv = new DataView( baseline.buffer );
	bv.setUint32( 0, 2, true );
	bv.setUint16( 4, 4, true );
	baseline[6] = 1;
	c.receive( 0x33a6, baseline, 101 );
	assert.equal( h.hp( 2 ), 100 );
	assert.equal( h.dead( 2 ), false, "combat baseline waits for fatal impact" );
	assert.deepEqual( c.state().environmentalDamage, [], "fatal combat must not emit a second gold damage number" );
	h.impact( 2, "9:2:0", hit( 100, true ), 500 );
	assert.equal( h.hp( 2 ), 0 );
	assert.equal( h.dead( 2 ), true );
	assert.equal( "takeFatalities" in c, false, "attack result cannot fabricate LIFE/dead" );
});

test("source gate adjusts by wire delta, then checkpoints reconcile in receipt order", () => {
	for ( const flags of [ 0, 2, 0x10, 0x20, 0x32, 1, 4, 0x40, 0x400, 0x402 ] ) {
		const h = createEffectiveHp();
		h.receive( seed() );
		h.receive( result() );
		h.receive( refresh( 70, flags, 100 ) );
		assert.equal( h.hp( 2 ), (flags & 0xffffffcd) === 0 ? 70 : 100, `source ${flags}` );
		h.receive( refresh( 85, 0x10, 110 ) );
		assert.equal( h.hp( 2 ), (flags & 0xffffffcd) === 0 ? 85 : 115, "delta is against wire 70, not displayed HP" );
		h.impact( 2, "9:2:0", hit( 30 ), 200 );
		assert.equal( h.hp( 2 ), 85, "both checkpoints retire in order" );
	}
});

test("a status-free monster's baseline binds its cast result, so a bonus frame lowers the bar by its own damage", () => {
	const h = createEffectiveHp(), c = createCombat( () => undefined, e => h.receive( e ) );
	const bytes = hex => Uint8Array.from( hex.match( /../g ), byte => parseInt( byte, 16 ) );
	const target = 0x61a81;
	c.seed( target, { hp: 100 } );
	// The server's own burst for a sword base attack with the attack-pet
	// expansion on, byte for byte: the cast result with two impacts of 6
	// (apps/server skill_cast_accept_fixture.json), then the survivor's HP
	// baseline with an empty status word (monsterImpactAbnormalFrames).
	c.receive(
		0xb245,
		bytes( "010002000000a386010001000000811a0600010201811a0600000106000000000000000106000000000000" ),
		100
	);
	c.receive( 0x33a6, bytes( "811a06000001055800000000000000" ), 100 );
	assert.equal( h.hp( target ), 100, "neither impact has landed" );
	// A pet's bonus hit for 5 while both impacts are pending: HP only, source zero.
	c.receive( 0x33a6, bytes( "811a060000000153000000" ), 150 );
	assert.equal( h.hp( target ), 95, "the delta is measured from the baseline's 88, not from the spawn's 100" );
	h.impact( target, `1:${target}:0`, hit( 6 ), 300 );
	assert.equal( h.hp( target ), 89 );
	h.impact( target, `1:${target}:1`, hit( 6 ), 600 );
	assert.equal( h.hp( target ), 83, "the bar ends on the server's HP and never rose" );
});

test("multi-hit/multi-cast checkpoints bind only previously unbound results", () => {
	const h = createEffectiveHp();
	h.receive( seed() );
	h.receive( result( "a" ) );
	h.receive( result( "b" ) );
	h.receive( refresh( 70 ) );
	h.receive( result( "c" ) );
	h.receive( refresh( 65 ) );
	h.impact( 2, "c", hit( 5 ), 10 );
	assert.equal( h.hp( 2 ), 95, "later checkpoint cannot overtake earlier results" );
	h.impact( 2, "a", hit( 10 ), 20 );
	assert.equal( h.hp( 2 ), 85 );
	h.impact( 2, "b", hit( 20 ), 30 );
	assert.equal( h.hp( 2 ), 65 );
});

test("projectile transfer survives cancellation and settles HP at arrival exactly once", () => {
	const h = createEffectiveHp(), f = createDamageFeedback( h.release );
	h.receive( seed() );
	h.receive( result() );
	h.receive( result( "9:2:1" ) );
	h.receive( refresh( 70 ) );
	const take = ( casts, triggers, transfers = [], ms = 0 ) => {
		for ( const e of f.take( casts, triggers, () => 0, ms / 1000, ms, transfers ) ) {
			h.impact( e.target, e.key, e.impact, ms );
		}
	};
	take( [ cast ], [], [ { kind: "launch", cast, index: 0, flight: 42, target: 2, at: 0 } ] );
	assert.equal( h.hp( 2 ), 100 );
	take( [ { ...cast, cancelledAtMs: 100 } ], [], [], 100 );
	assert.equal( h.hp( 2 ), 80, "cancellation flushes only the untransferred second result" );
	take( [], [], [ { kind: "arrival", flight: 42, at: .3 } ], 300 );
	assert.equal( h.hp( 2 ), 70 );
	take( [], [], [ { kind: "arrival", flight: 42, at: .4 } ], 400 );
	assert.equal( h.hp( 2 ), 70 );
});

test("erased projectile releases its checkpoint without applying its damage", () => {
	const h = createEffectiveHp(), f = createDamageFeedback( h.release );
	h.receive( seed() );
	h.receive( result() );
	h.receive( refresh( 90 ) );
	f.take( [ cast ], [], () => 0, 0, 0, [ { kind: "launch", cast, index: 0, flight: 42, target: 2, at: 0 } ] );
	assert.deepEqual( f.take( [ cast ], [], () => 0, .2, 200, [ { kind: "drop", flight: 42, at: .2 } ] ), [] );
	assert.equal( h.hp( 2 ), 90, "retired reference releases authoritative checkpoint" );
});

test("late continuation applies behind the callback cursor rather than waiting a second animation", () => {
	const h = createEffectiveHp(), f = createDamageFeedback(), empty = { ...cast, results: [] };
	h.receive( seed() );
	f.take( [ empty ], [ { cast: empty, phase: "SHOT", event: 1, at: 0 } ], () => 0, 0, 0 );
	h.receive( result() );
	h.receive( refresh( 90 ) );
	const hits = f.take( [ cast ], [], () => 0, .2, 200 );
	assert.equal( hits.length, 1 );
	for ( const e of hits ) h.impact( e.target, e.key, e.impact, 200 );
	assert.equal( h.hp( 2 ), 90 );
	assert.deepEqual( f.take( [ cast ], [], () => 0, .3, 300 ), [] );
});

test("unreferenced refresh, environment death, revive, pure visual and stale checkpoint branches", () => {
	const h = createEffectiveHp();
	h.receive( seed() );
	h.receive( refresh( 80, 0x400 ) );
	assert.equal( h.hp( 2 ), 80 );
	h.receive( refresh( 0, 0x402 ) );
	assert.equal( h.dead( 2 ), true );
	h.receive( refresh( 100, 0 ) );
	h.receive( { kind: "hp-revive", gid: 2 } );
	assert.equal( h.dead( 2 ), false );
	assert.equal( h.hp( 2 ), 100 );
	h.impact( 2, "visual", hit( 50, false, 7 ), 10 );
	assert.equal( h.hp( 2 ), 100 );
	h.receive( result() );
	h.receive( refresh( 90, 1, 20 ) );
	h.step( 5020 );
	assert.equal( h.hp( 2 ), 100 );
	h.step( 5021 );
	assert.equal( h.hp( 2 ), 100, "timeout discards rather than applies" );
	h.impact( 2, "9:2:0", hit( 8 ), 5022 );
	assert.equal( h.hp( 2 ), 90, "last release applies the detached checkpoint via 85E73C" );
	h.remove( 2 );
	assert.equal( h.hp( 2 ), undefined );
	h.receive( seed( 7 ) );
	assert.equal( h.dead( 2 ), false );
	h.clear();
	assert.equal( h.hp( 2 ), undefined );
});

test("projection applies HP ingress in transaction order and rejects invalid batches atomically", () => {
	const p = createPresentation();
	p.apply( {
		sequence: 1,
		events: [ seed(), result(), refresh( 90 ), {
			kind: "gameplay",
			state: { localGid: 2, vitals: [ { gid: 2, hp: 90, mp: 7 } ], casts: [] }
		} ]
	} );
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 100 );
	assert.equal( defined( p.gameplay() ).vitals[0].mp, 7 );
	assert.throws( () => p.apply( { sequence: 2, events: [ seed( 999 ), { kind: "state", entity: { gid: 99 } } ] } ) );
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 100 );
	p.impact( 2, "9:2:0", hit( 10 ), 100 );
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 90 );
	p.apply( { sequence: 2, events: [ { kind: "reset", epoch: 1 } ] } );
	assert.equal( p.gameplay(), null );
	assert.equal( p.dead( 2 ), false );
});

test("projection exposes the death state when the fatal impact lands, not at fatal receipt", () => {
	const p = createPresentation();
	p.apply( {
		sequence: 1,
		events: [ seed(), result( "9:2:0", true ), refresh( 0, 0x402, 100 ), {
			kind: "gameplay",
			state: { localGid: 2, vitals: [ { gid: 2, hp: 0 } ], casts: [] }
		} ]
	} );
	assert.equal(
		defined( p.gameplay() ).vitals[0].deathState,
		undefined,
		"77A33A: a pending fatal holds zero HP out of the death state"
	);
	p.impact( 2, "9:2:0", hit( 100, true ), 500 );
	assert.equal( defined( p.gameplay() ).vitals[0].deathState, true );
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 0 );
	const q = createPresentation();
	q.apply( {
		sequence: 1,
		events: [ seed(), refresh( 0, 0x402, 100 ), {
			kind: "gameplay",
			state: { localGid: 2, vitals: [ { gid: 2, hp: 0 } ], casts: [] }
		} ]
	} );
	assert.equal(
		defined( q.gameplay() ).vitals[0].deathState,
		true,
		"zero HP without a pending fatal enters it at once"
	);
});

test("projection identity changes only with its snapshot, finishing casts or effective HP", () => {
	const p = createPresentation(), state = { localGid: 2, vitals: [ { gid: 2, hp: 90, mp: 7 } ], casts: [] };
	p.apply( { sequence: 1, events: [ seed(), result(), refresh( 90 ), { kind: "gameplay", state } ] } );
	const idle = p.gameplay();
	p.step( 1000 );
	assert.equal( p.gameplay(), idle, "frames without input changes share one projection" );
	assert.equal( defined( idle ).vitals[0].hp, 100 );
	p.apply( { sequence: 2, events: [ result( "9:2:1" ) ] } );
	assert.equal( p.gameplay(), idle, "result bookkeeping alone is not a projection change" );
	p.impact( 2, "9:2:0", hit( 10 ), 1100 );
	const struck = p.gameplay();
	assert.notEqual( struck, idle );
	assert.equal( defined( struck ).vitals[0].hp, 90 );
	assert.equal( defined( struck ).vitals[0], state.vitals[0], "rows matching the wire keep their identity" );
	p.apply( { sequence: 3, events: [ { kind: "cast-finalize", cast: { ...cast, cancelledAtMs: 1200 } } ] } );
	const finishing = p.gameplay();
	assert.notEqual( finishing, struck );
	assert.equal( defined( finishing ).casts.length, 1 );
	p.finishedCasts();
	const retired = p.gameplay();
	assert.notEqual( retired, finishing );
	assert.equal( defined( retired ).casts.length, 0 );
	p.finishedCasts();
	assert.equal( p.gameplay(), retired, "retiring nothing is not a change" );
	p.apply( {
		sequence: 4,
		events: [ { kind: "gameplay", state: { ...state, vitals: [ { gid: 2, hp: 90, mp: 6 } ] } } ]
	} );
	assert.notEqual( p.gameplay(), retired );
	assert.equal( defined( p.gameplay() ).vitals[0].mp, 6 );
});

test("finalized results survive worker snapshot retirement until presentation consumes them", () => {
	const p = createPresentation(), finished = { ...cast, cancelledAtMs: 100 };
	// Both native result rows must reserve their ingress references. Omitting
	// the second registration used to permit an unowned extra 20-HP debit.
	p.apply( {
		sequence: 1,
		events: [ seed(), result(), result( "9:2:1" ), refresh( 90 ), { kind: "cast-finalize", cast: finished }, {
			kind: "gameplay",
			state: { localGid: 2, vitals: [ { gid: 2, hp: 90 } ], casts: [] }
		} ]
	} );
	assert.deepEqual( defined( p.gameplay() ).casts, [ finished ] );
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 100 );
	const feedback = createDamageFeedback( p.release );
	for ( const event of feedback.take( defined( p.gameplay() ).casts, [], () => 0, .5, 500 ) ) {
		p.impact( event.target, event.key, event.impact, 500 );
	}
	p.finishedCasts();
	assert.equal( defined( p.gameplay() ).casts.length, 0 );
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 90 );
});

test("caster despawn retains cancellation results for surviving targets", () => {
	const events = [], c = createCombat( () => undefined, event => events.push( event ) );
	c.seed( 2, { hp: 100 } );
	const p = new Uint8Array( 34 ), v = new DataView( p.buffer );
	p[0] = 1;
	p[1] = 0;
	v.setUint32( 2, 7, true );
	v.setUint32( 6, 1, true );
	v.setUint32( 10, 9, true );
	v.setUint32( 14, 2, true );
	p[18] = 1;
	p[19] = 1;
	p[20] = 1;
	v.setUint32( 21, 2, true );
	p[25] = 0;
	v.setUint32( 26, 10 << 8, true );
	c.receive( 0xb245, p, 100 );
	c.remove( 1, 150 );
	const done = events.find( e => e.kind === "cast-finalize" );
	assert.equal( done.cast.cancelledAtMs, 150 );
	assert.equal( done.cast.results[0].impacts[0].damage, 10 );
	assert.equal( c.state().casts.length, 0 );
	assert.equal( c.state().vitals[0].hp, 90 );
});

test("hawk commands do not reserve cast-vector HP checkpoints before their native impact callback", () => {
	const h = createEffectiveHp(),
		events = [],
		c = createCombat( () => undefined, e => {
			events.push( e );
			h.receive( e );
		} );
	c.seedEffects( 1, [ { id: 78, token: 88, status: 0 } ] );
	c.seed( 2, { hp: 100 } );
	const p = new Uint8Array( 10 ), v = new DataView( p.buffer );
	v.setUint32( 0, 88, true );
	v.setUint32( 4, 2, true );
	v.setUint16( 8, 10, true );
	c.receive( 0x357a, p, 100 );
	assert.equal( h.hp( 2 ), 100 );
	assert.equal( events.some( e => e.kind === "hp-result" ), false );
	h.impact( 2, "hawk:88:1", hit( 10 ), 200, "hawk" );
	assert.equal( h.hp( 2 ), 90 );
	h.receive( refresh( 90, 1, 201 ) );
	assert.equal( h.hp( 2 ), 90 );
});

test("immediate revival retires fatal flights and detached HP checkpoints without replay", () => {
	for ( const timedOut of [ false, true ] ) {
		const h = createEffectiveHp(),
			feedback = createDamageFeedback( h.release ),
			fatal = { ...cast, results: [ { target: 2, impacts: [ hit( 100, true ) ] } ] };
		h.receive( seed() );
		h.receive( result( "9:2:0", true ) );
		h.receive( refresh( 0, 0x402, 100 ) );
		feedback.take( [ fatal ], [], () => 0, .1, 100, [ {
			kind: "launch",
			cast: fatal,
			index: 0,
			flight: 42,
			target: 2,
			at: .1
		} ] );
		if ( timedOut ) h.step( 5101 );
		h.receive( refresh( 200, 0, 5200 ) );
		h.receive( { kind: "hp-revive", gid: 2 } );
		assert.equal( h.hp( 2 ), 200 );
		assert.equal( h.dead( 2 ), false );
		assert.deepEqual(
			[ ...feedback.pendingDeaths( [ fatal ], h.currentResult ) ],
			[],
			"previous-life flight cannot defer a new death"
		);
		const arrivals = feedback.take( [], [], () => 0, 6, 6000, [ { kind: "arrival", flight: 42, at: 6 } ] );
		assert.equal( arrivals.length, 1, "independent visual flight still completes" );
		for ( const event of arrivals ) assert.equal( h.impact( event.target, event.key, event.impact, 6000 ), false );
		h.release( "9:2:0", 6001 );
		h.step( 12000 );
		assert.equal( h.hp( 2 ), 200 );
		assert.equal( h.dead( 2 ), false );
		h.receive( result( "10:2:0" ) );
		h.receive( refresh( 170, 1, 12010 ) );
		assert.equal( h.impact( 2, "10:2:0", hit( 30 ), 12100 ), true );
		assert.equal( h.hp( 2 ), 170 );
		assert.equal( h.impact( 2, "10:2:0", hit( 30 ), 12101 ), false, "consumed results cannot debit twice" );
	}
});

test("revival retires only its actor and permits the next life to die normally", () => {
	const h = createEffectiveHp();
	h.receive( seed() );
	h.receive( result( "old", true ) );
	h.receive( refresh( 0, 0x402 ) );
	h.receive( { kind: "hp-seed", gid: 3, hp: 50 } );
	h.receive( { kind: "hp-result", gid: 3, key: "peer", fatal: false } );
	h.receive( refresh( 200, 0 ) );
	h.receive( { kind: "hp-revive", gid: 2 } );
	assert.equal( h.impact( 2, "peer", hit( 1 ), 1 ), false, "a key cannot be used for another actor" );
	assert.equal( h.impact( 3, "peer", hit( 10 ), 1 ), true );
	assert.equal( h.hp( 3 ), 40 );
	h.receive( result( "new", true ) );
	h.receive( refresh( 0, 0x402 ) );
	assert.equal( h.impact( 2, "new", hit( 200, true ), 2 ), true );
	assert.equal( h.dead( 2 ), true );
	assert.equal( h.hp( 2 ), 0 );
	h.receive( refresh( 200, 0 ) );
	h.receive( { kind: "hp-revive", gid: 2 } );
	assert.equal( h.impact( 2, "old", hit( 100, true ), 3 ), false );
	assert.equal( h.dead( 2 ), false );
});

test("old callbacks cannot cross repeated revivals in any delivery order", () => {
	fc.assert(
		fc.property(
			fc.array( fc.array( fc.constantFrom( "impact", "release", "timeout" ), { minLength: 1, maxLength: 12 } ), {
				minLength: 1,
				maxLength: 20
			} ),
			cycles => {
				const h = createEffectiveHp();
				h.receive( seed() );
				let now = 0;
				for ( const [life, callbacks] of cycles.entries() ) {
					const key = `fatal:${life}`;
					h.receive( result( key, true ) );
					h.receive( refresh( 0, 0x402, ++now ) );
					h.receive( refresh( 200, 0, ++now ) );
					h.receive( { kind: "hp-revive", gid: 2 } );
					for ( const callback of callbacks ) {
						now += 6001;
						if ( callback === "impact" ) assert.equal( h.impact( 2, key, hit( 200, true ), now ), false );
						else if ( callback === "release" ) h.release( key, now );
						else h.step( now );
						assert.equal( h.hp( 2 ), 200 );
						assert.equal( h.dead( 2 ), false );
					}
				}
			}
		),
		{ numRuns: 100, seed: 20260913 }
	);
});

import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );
const { createEffectiveHp } = await import( "../../src/engine/runtime/presentation/effective-hp.ts" );
const { createCharacterStateIndex } = await import( "../../src/engine/runtime/characters/state-index.ts" );
const { attachedEffectReferences } = await import( "../../src/engine/foundation/gameplay/attached-effects.ts" );
const { appendCastResults, castResultAt, castResultStageCount, requestCastCancellation } = await import(
	"../../src/engine/foundation/gameplay/cast-results.ts"
);
const { createSessionHttp } = await import( "../../src/engine/runtime/simulation/worker/session/http/http.ts" );

function writer() {
	const b = [];
	const w = {
		u8( n ) {
			b.push( n & 255 );
			return w;
		},
		u16( n ) {
			return w.u8( n ).u8( n >>> 8 );
		},
		u32( n ) {
			return w.u16( n ).u16( n >>> 16 );
		},
		pos( p ) {
			return w.u16( p.regionId ).u16( p.x ).u16( p.y ).u16( p.z );
		},
		bytes() {
			return Uint8Array.from( b );
		}
	};
	return w;
}
const hit = ( damage = 10, type = 0, fatal = false ) => ({ damage, type, fatal, flags: 0, secondaryAmount: 0 });
const row = ( target, impacts = [ hit() ] ) => ({ target, impacts });
function payload(
	w,
	{
		target = 2,
		results = [ row( 2 ) ],
		stages = results[0]?.impacts.length ?? 0,
		hasResults = true,
		travel,
		correction
	} = {}
) {
	w.u32( target ).u8( Number( hasResults ) + (travel ? 8 : 0) + (correction ? 2 : 0) );
	if ( hasResults ) {
		w.u8( stages ).u8( results.length );
		for ( const r of results ) {
			w.u32( r.target );
			assert.equal( r.impacts.length, stages );
			for ( const i of r.impacts ) {
				w.u8( i.type | (i.fatal ? 128 : 0) );
				if ( [ 0, 4, 5, 7 ].includes( i.type ) ) {
					w.u32( (i.damage << 8) | i.flags );
					if ( i.type === 7 ) w.u16( 1 ).u16( 2 );
					else w.u32( i.secondaryAmount );
					if ( i.type === 4 || i.type === 5 ) w.pos( i.displacement );
				}
			}
		}
	}
	if ( travel ) w.pos( travel );
	if ( correction ) w.pos( correction );
	return w.bytes();
}
const broadcast = ( skill = 6, token = 90, caster = 1, options ) =>
	payload( writer().u8( 1 ).u8( 0 ).u32( skill ).u32( caster ).u32( token ), options );
const release = ( token = 90, options ) => payload( writer().u8( 1 ).u32( token ), options );
const stop = ( token = 90 ) => writer().u8( 2 ).u8( 0 ).u32( token ).bytes();
const extinguish = ( token = 90 ) => writer().u8( 1 ).u32( token ).bytes();
const effect = ( skill, token, gid = 1 ) => writer().u32( gid ).u32( skill ).u32( token ).bytes();
const reference = ( id, extra = {} ) => ({ id, status: false, effectRider: false, ...extra });
const refs = [
	reference( 6 ),
	reference( 7, { linkedSkillId: 6 } ),
	reference( 8, { linkedSkillId: 6 } ),
	reference( 3032, { cancellationDeferred: true } )
];
function fixture() {
	const events = [],
		hp = createEffectiveHp(),
		entities = new Map( [ 1, 2, 3 ].map( gid => [ gid, { gid, kind: "player", name: `Player${gid}` } ] ) );
	const combat = createCombat( gid => entities.get( gid ), event => {
		events.push( event );
		if ( event.kind.startsWith( "hp-" ) ) hp.receive( event );
	} );
	combat.references( refs );
	combat.seed( 2, { hp: 100 } );
	combat.seed( 3, { hp: 100 } );
	events.length = 0;
	return { combat, events, hp, feedback: createDamageFeedback( hp.release ) };
}
const apply = ( f, casts, triggers = [], index = () => 0, ms = 100, transfers = [] ) => {
	const hits = f.feedback.take( casts, triggers, index, ms / 1000, ms, transfers );
	for ( const h of hits ) f.hp.impact( h.target, h.key, h.impact, ms );
	return hits;
};

test("retail chain 6 -> 7 -> 8 continues root token, skill, original target and clock", () => {
	const { combat, events } = fixture();
	combat.receive( 0xb245, broadcast(), 100 );
	combat.receive( 0xb505, release( 90, { results: [], hasResults: false } ), 110 );
	combat.receive( 0xb245, broadcast( 7, 91, 1, { target: 3, results: [ row( 3, [ hit( 20 ) ] ) ] } ), 200 );
	combat.receive( 0xb245, broadcast( 8, 92, 1, { results: [ row( 2, [ hit( 30 ) ] ) ] } ), 300 );
	const [root] = combat.state().casts;
	assert.equal( combat.state().casts.length, 1 );
	assert.equal( root.token, 90 );
	assert.equal( root.skill, 6 );
	assert.equal( root.target, 2 );
	assert.equal( root.receivedAtMs, 100 );
	assert.equal( root.shotAtMs, 110 );
	assert.equal( root.resultStageCount, 3 );
	assert.deepEqual( events.filter( e => e.kind === "hp-result" ).map( e => e.key ), [
		"90:2:0",
		"90:3:1",
		"90:2:2"
	] );
	assert.equal(
		castResultAt( defined( defined( root.results ).find( r => r.target === 3 ) ).impacts, 0 ),
		undefined
	);
	assert.equal(
		defined( castResultAt( defined( defined( root.results ).find( r => r.target === 3 ) ).impacts, 1 ) ).damage,
		20
	);
});
test("linked lookup precedes the wire-token duplicate and nonzero-token gates", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 100 );
	f.combat.receive( 0xb245, broadcast( 7, 90 ), 200 );
	f.combat.receive( 0xb245, broadcast( 8, 0 ), 300 );
	assert.equal( f.combat.state().casts.length, 1 );
	assert.equal( f.combat.state().casts[0].resultStageCount, 3 );
	assert.throws( () => f.combat.receive( 0xb245, broadcast( 6, 0 ) ), /Invalid cast token/ );
});
test("ordinary duplicate broadcasts remain idempotent", () => {
	const f = fixture();
	const packet = broadcast();
	f.combat.receive( 0xb245, packet, 100 );
	f.combat.receive( 0xb245, packet, 200 );
	assert.equal( f.events.filter( e => e.kind === "hp-result" ).length, 1 );
	assert.equal( f.combat.state().casts[0].receivedAtMs, 100 );
});
test("self-linked and zero-linked records start ordinary casts", () => {
	for ( const linkedSkillId of [ 0, 6 ] ) {
		const f = fixture();
		f.combat.references( [ reference( 6, { linkedSkillId } ) ] );
		f.combat.receive( 0xb245, broadcast() );
		assert.equal( f.combat.state().casts.length, 1 );
	}
});
test("continuation uses the first nonzero target without replacing an existing one", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast( 6, 90, 1, { target: 0, results: [], hasResults: false } ), 100 );
	f.combat.receive( 0xb245, broadcast( 7, 91, 1, { target: 3, results: [ row( 3 ) ] } ), 200 );
	assert.equal( f.combat.state().casts[0].target, 3 );
});
test("empty-target batches still advance the global result stage cursor", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast( 6, 90, 1, { results: [], stages: 2 } ), 100 );
	f.combat.receive( 0xb245, broadcast( 7, 91 ), 200 );
	assert.equal( f.combat.state().casts[0].resultStageCount, 3 );
	assert.deepEqual( f.events.filter( e => e.kind === "hp-result" ).map( e => e.key ), [ "90:2:2" ] );
});
test("B505 continuation uses the same global matrix alignment as linked B245", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 100 );
	f.combat.receive( 0xb505, release( 90, { results: [ row( 3 ) ] } ), 200 );
	assert.deepEqual( f.events.filter( e => e.kind === "hp-result" ).map( e => e.key ), [ "90:2:0", "90:3:1" ] );
	assert.equal( f.combat.state().casts[0].shotAtMs, 200 );
});
test("late linked results are consumed behind the ROOT callback cursor exactly once", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 100 );
	let root = f.combat.state().casts[0];
	assert.equal( apply( f, [ root ], [ { cast: root, phase: "SHOT", event: 1, at: .1 } ], () => 0 ).length, 1 );
	apply( f, [ root ], [ { cast: root, phase: "SHOT", event: 2, at: .2 } ], () => 1, 200 );
	f.combat.receive( 0xb245, broadcast( 7, 91, 1, { results: [ row( 3, [ hit( 20 ) ] ) ] } ), 300 );
	root = f.combat.state().casts[0];
	assert.deepEqual( apply( f, [ root ], [], () => 0, 300 ).map( h => h.key ), [ "90:3:1" ] );
	assert.equal( f.hp.hp( 2 ), 90 );
	assert.equal( f.hp.hp( 3 ), 80 );
	assert.deepEqual( apply( f, [ root ], [], () => 0, 400 ), [] );
});
test("an early callback cannot consume a newly named target from a later stage", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 100 );
	f.combat.receive( 0xb245, broadcast( 7, 91, 1, { results: [ row( 3 ) ] } ), 200 );
	const root = f.combat.state().casts[0];
	assert.deepEqual(
		apply( f, [ root ], [ { cast: root, phase: "SHOT", event: 1, at: .2 } ], () => 0, 200 ).map( h => h.target ),
		[ 2 ]
	);
	assert.equal( f.hp.hp( 3 ), 100 );
});
test("projectile-transferred root results survive a linked continuation and cancellation", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 100 );
	let root = f.combat.state().casts[0];
	apply( f, [ root ], [], () => 0, 100, [ { kind: "launch", cast: root, index: 0, flight: 42, target: 2, at: .1 } ] );
	f.combat.receive( 0xb245, broadcast( 7, 91, 1, { results: [ row( 2, [ hit( 20 ) ] ) ] } ), 200 );
	f.combat.receive( 0xb505, stop(), 250 );
	root = f.combat.state().casts[0];
	assert.deepEqual( apply( f, [ root ], [], () => 0, 250 ).map( h => h.key ), [ "90:2:1" ] );
	assert.equal( f.hp.hp( 2 ), 80 );
	assert.deepEqual( apply( f, [], [], () => 0, 300, [ { kind: "arrival", flight: 42, at: .3 } ] ).map( h => h.key ), [
		"90:2:0"
	] );
	assert.equal( f.hp.hp( 2 ), 70 );
});
test("missing linked root applies temporary results without publishing an active cast", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast( 7, 91 ), 100 );
	assert.deepEqual( f.combat.state().casts, [] );
	const done = f.events.find( e => e.kind === "cast-finalize" ).cast;
	assert.equal( done.resultOnly, true );
	assert.equal( done.skill, 0 );
	assert.ok( done.token < 0 );
	assert.notEqual( done.token, 91 );
	assert.equal( f.hp.hp( 2 ), 100 );
	assert.equal( apply( f, [ done ] ).length, 1 );
	assert.equal( f.hp.hp( 2 ), 90 );
	assert.deepEqual( apply( f, [ done ] ), [] );
	const indexed = createCharacterStateIndex().update( [], { casts: [ done ], vitals: [] } );
	assert.equal( indexed.castByActor.size, 0 );
});
test("temporary results do not collide with a live wire token or a second temporary", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast( 6, 90, 3 ), 100 );
	f.combat.receive( 0xb245, broadcast( 7, 90, 1 ), 200 );
	f.combat.receive( 0xb245, broadcast( 7, 90, 1 ), 300 );
	const done = f.events.filter( e => e.kind === "cast-finalize" ).map( e => e.cast );
	assert.equal( f.combat.state().casts.length, 1 );
	assert.equal( new Set( done.map( c => c.token ) ).size, 2 );
	assert.ok( done.every( c => c.token !== 90 ) );
});
test("a cancelled/retired root cannot receive a later linked broadcast", () => {
	for ( const retire of [ false, true ] ) {
		const f = fixture();
		f.combat.receive( 0xb245, broadcast(), 100 );
		f.combat.receive( 0xb505, stop(), 200 );
		if ( retire ) f.combat.step( 400 );
		f.events.length = 0;
		f.combat.receive( 0xb245, broadcast( 7, 91 ), 500 );
		assert.ok( f.events.find( e => e.kind === "cast-finalize" ).cast.resultOnly );
	}
});
test("same caster/skill lookup chooses the first still-active owned instance", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast( 6, 99 ), 100 );
	f.combat.receive( 0xb245, broadcast( 6, 90 ), 100 );
	f.combat.receive( 0xb245, broadcast( 7, 500 ), 200 );
	assert.equal( defined( f.combat.state().casts.find( c => c.token === 99 ) ).resultStageCount, 2 );
	assert.equal( defined( f.combat.state().casts.find( c => c.token === 90 ) ).resultStageCount, 1 );
});
test("a matching attached kind-2 skill is not mistaken for a missing root/new cast", () => {
	const f = fixture();
	f.combat.receive( 0xb419, effect( 6, 50 ), 50 );
	f.combat.receive( 0xb245, broadcast( 6, 90 ), 100 );
	f.events.length = 0;
	f.combat.receive( 0xb245, broadcast( 7, 91 ), 200 );
	assert.equal( f.combat.state().casts[0].resultStageCount, 1 );
	assert.equal( f.events.length, 0, "native kind-2 result vector has zero capacity" );
	assert.equal( f.combat.state().attachedEffects[0].token, 50 );
});
test("a cast preceding a same-skill attached effect remains the linked destination", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 50 );
	f.combat.receive( 0xb419, effect( 6, 50 ), 50 );
	f.combat.receive( 0xb245, broadcast( 7, 91 ), 200 );
	assert.equal( f.combat.state().casts[0].resultStageCount, 2 );
});
test("temporary travel is cancelled immediately, not transferred to the wire token", () => {
	const f = fixture(), destination = { regionId: 1, x: 20, y: 0, z: 0 };
	f.combat.receive( 0xb245, broadcast( 7, 91, 1, { travel: destination, correction: destination } ), 100 );
	const movements = f.combat.takeDisplacements(), done = f.events.find( e => e.kind === "cast-finalize" ).cast;
	assert.deepEqual( movements.map( m => m.kind ), [ 8, 2 ] );
	assert.ok( movements.every( m => m.token === done.token ) );
	assert.deepEqual( f.combat.takeCancellations(), [ done.token ] );
});
test("result-owned knockback is not cancelled with temporary trajectory ownership", () => {
	const f = fixture(), destination = { regionId: 1, x: 20, y: 0, z: 0 };
	f.combat.receive(
		0xb245,
		broadcast( 7, 91, 1, { results: [ row( 2, [ { ...hit( 10, 5 ), displacement: destination } ] ) ] } ),
		100
	);
	assert.deepEqual( f.combat.takeDisplacements().map( m => m.kind ), [ 5 ] );
	assert.deepEqual( f.combat.takeCancellations(), [] );
});
test("temporary results survive coalesced gameplay through the existing presentation journal", () => {
	const f = fixture(), p = createPresentation();
	f.combat.receive( 0xb245, broadcast( 7, 91 ), 100 );
	p.apply( {
		sequence: 1,
		events: [ { kind: "hp-seed", gid: 2, hp: 100 }, ...f.events, {
			kind: "gameplay",
			state: { localGid: 1, casts: [], vitals: [ { gid: 2, hp: 90 } ] }
		} ]
	} );
	const feedback = createDamageFeedback( p.release ), casts = defined( p.gameplay() ).casts;
	assert.equal( casts.length, 1 );
	for ( const h of feedback.take( casts, [], () => -1, .1, 100 ) ) {
		assert.equal( p.impact( h.target, h.key, h.impact, 100 ), true );
	}
	assert.equal( defined( p.gameplay() ).vitals[0].hp, 90 );
	p.finishedCasts();
	assert.deepEqual( defined( p.gameplay() ).casts, [] );
});
for ( const opcode of [ 0xb505, 0xb6a0 ] ) {
	test(`3032 exemption: ${opcode.toString( 16 )} records request without flushing, motion cancellation or retirement`, () => {
		const f = fixture();
		f.combat.receive( 0xb245, broadcast( 3032 ), 100 );
		f.combat.receive( opcode, opcode === 0xb505 ? stop() : extinguish(), 200 );
		const root = f.combat.state().casts[0];
		assert.equal( root.cancellationDeferred, true );
		assert.equal( root.cancellationRequestedAtMs, 200 );
		assert.equal( root.cancelledAtMs, undefined );
		assert.deepEqual( f.combat.takeCancellations(), [] );
		assert.equal( f.events.some( e => e.kind === "cast-finalize" ), false );
		assert.deepEqual( apply( f, [ root ], [], () => -1, 200 ), [] );
		f.combat.step( 10000 );
		assert.equal( f.combat.state().casts.length, 1 );
		f.combat.receive( 0xb505, release( 90, { results: [ row( 3 ) ] } ), 10100 );
		assert.equal( f.combat.state().casts[0].resultStageCount, 2 );
	});
}
for ( const opcode of [ 0xb505, 0xb6a0 ] ) {
	test(`ordinary ${opcode.toString( 16 )} applies cancellation once and retires at 200 ms`, () => {
		const f = fixture();
		f.combat.receive( 0xb245, broadcast(), 100 );
		const packet = opcode === 0xb505 ? stop() : extinguish();
		f.combat.receive( opcode, packet, 200 );
		f.combat.receive( opcode, packet, 250 );
		assert.deepEqual( f.combat.takeCancellations(), [ 90 ] );
		assert.equal( f.events.filter( e => e.kind === "cast-finalize" ).length, 1 );
		assert.equal( f.combat.state().casts[0].cancellationRequestedAtMs, 200 );
		f.combat.step( 399 );
		assert.equal( f.combat.state().casts.length, 1 );
		f.combat.step( 400 );
		assert.equal( f.combat.state().casts.length, 0 );
	});
}
test("B505 defers on an exempt attached effect; B6A0 forcibly extinguishes that same kind-2 object", () => {
	const f = fixture();
	f.combat.receive( 0xb419, effect( 3032, 50 ), 100 );
	f.combat.receive( 0xb505, stop( 50 ), 200 );
	assert.equal( f.combat.state().attachedEffects[0].cancellationRequestedAtMs, 200 );
	f.combat.receive( 0xb6a0, extinguish( 50 ), 300 );
	assert.deepEqual( f.combat.state().attachedEffects, [] );
});
test("despawn is not blocked by the exemption, and discards rather than applies its pending results", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast( 3032 ), 100 );
	f.combat.remove( 1, 200 );
	assert.deepEqual( f.combat.state().casts, [] );
	const done = f.events.find( e => e.kind === "cast-finalize" ).cast;
	assert.equal( done.discardPendingResults, true );
	assert.deepEqual( apply( f, [ done ], [], () => -1, 200 ), [] );
	assert.equal( f.hp.hp( 2 ), 100 );
	assert.equal( f.hp.currentResult( 2, "90:2:0" ), false );
});
test("deferred requests still record requested flag after cancellation was already applied", () => {
	const c = {
		token: 1,
		caster: 1,
		skill: 3032,
		target: 2,
		damage: 0,
		fatal: false,
		cancellationDeferred: true,
		cancelledAtMs: 100
	};
	const result = requestCastCancellation( c, 200 );
	assert.equal( result.cancellationRequestedAtMs, 200 );
	assert.equal( result.cancelledAtMs, 100 );
});
test("cancel after continuation flushes only outstanding stages with correct target keys", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast(), 100 );
	let root = f.combat.state().casts[0];
	apply( f, [ root ], [ { cast: root, phase: "SHOT", event: 1, at: .1 } ], () => 0 );
	f.combat.receive( 0xb245, broadcast( 7, 91, 1, { results: [ row( 3, [ hit( 20 ) ] ) ] } ), 200 );
	f.combat.receive( 0xb505, stop(), 300 );
	assert.deepEqual( apply( f, f.combat.state().casts, [], () => -1, 300 ).map( h => h.key ), [ "90:3:1" ] );
});
for ( const trim of [ 1, 4, 9 ] ) {
	test(`truncated continuation (${trim} bytes missing) leaves results, HP and movement unchanged`, () => {
		const f = fixture();
		f.combat.receive( 0xb245, broadcast(), 100 );
		const before = f.combat.state(), count = f.events.length;
		assert.throws( () => f.combat.receive( 0xb245, broadcast( 7, 91 ).slice( 0, -trim ), 200 ), /Truncated/ );
		assert.deepEqual( f.combat.state(), before );
		assert.equal( f.events.length, count );
		assert.deepEqual( f.combat.takeDisplacements(), [] );
	});
}
test("result/stage capacity is validated before mutation", () => {
	const base = {
		token: 1,
		caster: 1,
		target: 2,
		skill: 6,
		damage: 0,
		fatal: false,
		results: [],
		resultStageCount: 16384
	};
	assert.throws( () => appendCastResults( base, [ row( 2 ) ], 1 ), /stage capacity/ );
	assert.deepEqual( base.results, [] );
	assert.equal( base.resultStageCount, 16384 );
});
for ( const value of [ -1, 1.5, 2 ** 32, NaN, "6" ] ) {
	test(`reject invalid linked reference ${value}`, () =>
		assert.throws( () => attachedEffectReferences( [ reference( 7, { linkedSkillId: value } ) ] ), /linked/ ));
}
for ( const value of [ 0, 1, "true", null ] ) {
	test(`reject non-boolean cancellation reference ${value}`, () =>
		assert.throws(
			() => attachedEffectReferences( [ reference( 3032, { cancellationDeferred: value } ) ] ),
			/authority/
		));
}
test("reset clears old instance identity and policy; preserved references retain native lookup rules", () => {
	const f = fixture();
	f.combat.receive( 0xb245, broadcast() );
	f.combat.clear( true );
	f.combat.receive( 0xb245, broadcast( 7, 91 ) );
	assert.deepEqual( f.combat.state().casts, [] );
	f.combat.clear();
	f.combat.receive( 0xb245, broadcast( 7, 91 ) );
	assert.equal( f.combat.state().casts[0].skill, 7 );
});
test("HTTP admission refuses old immutable reference schema and accepts versioned lifecycle authority", async t => {
	const http = createSessionHttp();
	let data = JSON.stringify( { refSkillSnapshot: refs, refItemSnapshot: [] } );
	t.mock.method( globalThis, "fetch", async () => new Response( data ) );
	async function load() {
		const bytes = new TextEncoder().encode( data ),
			hash = Buffer.from( await crypto.subtle.digest( "SHA-256", bytes ) ).toString( "hex" );
		return http.references(
			{ path: `/transport/references/${hash}.json`, sha256: hash, bytes: bytes.length },
			"https://fixture.invalid",
			new AbortController().signal
		);
	}
	await assert.rejects( load(), /rebuild the server/ );
	data = JSON.stringify( {
		referencesVersion: 3,
		skillLifecycleVersion: 1,
		refSkillSnapshot: refs,
		refItemSnapshot: [],
		refObjSnapshot: []
	} );
	const loaded = await load();
	assert.deepEqual( loaded.refSkillSnapshot, refs );
});

test("stage lookup preserves legacy dense and continuation-sparse rows at the full capacity bound", () => {
	for ( const sparse of [ false, true ] ) {
		const impacts = Array.from( { length: 8192 }, ( _, i ) => ({ ...hit(), ...(sparse ? { stage: i * 2 } : {}) }) );
		for ( let stage = 0; stage < 16384; stage++ ) {
			const expected = sparse ? (stage % 2 === 0 ? impacts[stage / 2] : undefined) : impacts[stage];
			assert.equal( castResultAt( impacts, stage ), expected );
		}
	}
});

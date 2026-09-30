/*
===========================================================================

combat-followup.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { characterMaterialVariants } from "../../../../scripts/build/char/materialVariants.mjs";
import { convertedTexturePath } from "../../../../scripts/build/shared/jmxAssetIO.mjs";
import { dataExtractedRoot } from "../../../../scripts/build/world/paths.mjs";
import path from "node:path";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { monsterMaterialSlot } = await load( "foundation/rendering/monster-scale.ts" );
const { impactSource } = await load( "foundation/animation/impact-source.ts" );
const { createDamageFeedback } = await load( "runtime/characters/damage-feedback.ts" );
const { createCombat } = await load( "runtime/simulation/worker/session/world/gameplay/combat/combat.ts" );
const { attachedEffect, attachedEffectReferences } = await load( "foundation/gameplay/attached-effects.ts" );
const hit = { type: 0, damage: 12, fatal: false, flags: 0, secondaryAmount: 0 };
const cast = {
	token: 1,
	caster: 1,
	target: 2,
	skill: 7,
	damage: 24,
	fatal: false,
	results: [ { target: 2, impacts: [ hit, hit ] }, { target: 3, impacts: [ hit ] } ]
};

test("fatal presentation waits for its hit index or projectile arrival, including cancellation flush", () => {
	const feedback = createDamageFeedback(),
		fatal = { ...hit, fatal: true },
		action = { ...cast, results: [ { target: 2, impacts: [ hit, fatal ] } ] },
		trigger = { cast: action, phase: "SHOT", event: 1, at: 1 };
	assert.deepEqual( [ ...feedback.pendingDeaths( [ action ] ) ], [ 2 ] );
	feedback.take( [ action ], [ trigger ], () => 0, 1, 1000 );
	assert.deepEqual( [ ...feedback.pendingDeaths( [ action ] ) ], [ 2 ] );
	feedback.take( [ action ], [ { ...trigger, event: 2 } ], () => 1, 2, 2000 );
	assert.equal( feedback.pendingDeaths( [ action ] ).size, 0 );
	feedback.reset();
	const launch = { kind: "launch", flight: -1, cast: action, target: 2, index: 1, at: 1 };
	feedback.take( [ action ], [], () => -1, 1, 1000, [ launch ] );
	const cancel = { ...action, cancelledAtMs: 1100 };
	feedback.take( [ cancel ], [], () => -1, 1.1, 1100 );
	assert.deepEqual(
		[ ...feedback.pendingDeaths( [] ) ],
		[ 2 ],
		"retiring the cast does not retire an in-flight fatal result"
	);
	feedback.take( [], [], () => -1, 2, 2000, [ { ...launch, kind: "arrival", at: 2 } ] );
	assert.equal( feedback.pendingDeaths( [] ).size, 0 );
});

test("native grade chooses material slots independently of motion and enlargement", () => {
	for (
		const [kind, normal, champion] of [ [ 0, 0, 2 ], [ 1, 1, 2 ], [ 2, 0, 2 ], [ 3, 3, 3 ], [ 4, 4, 4 ], [
			255,
			0,
			2
		] ]
	) {
		assert.equal( monsterMaterialSlot( 0, 0xc6, kind ), normal );
		assert.equal( monsterMaterialSlot( 1, 0xc6, kind ), champion );
		assert.equal( monsterMaterialSlot( 4, 0xc6, kind ), normal );
		assert.equal( monsterMaterialSlot( 6, 0xc6, kind ), normal );
	}
	assert.equal( monsterMaterialSlot( 1, 0x10c6, 4 ), 0 );
	const p = "res/mob/china/chakji.bsr",
		sets = characterMaterialVariants( readFileSync( path.join( dataExtractedRoot, p ) ), p );
	assert.deepEqual( [ ...sets.keys() ], [ 0, 1, 2 ] );
	assert.match( sets.get( 2 ), /chakji_champ\.bmt$/ );
	assert.match( convertedTexturePath( "prim/mtrl/mob/oasis/redeyeghost_champ.ddj" ), /redeyeghost_champ\.ddj\.png$/ );
	assert.ok( existsSync( convertedTexturePath( "prim/mtrl/mob/oasis/redeyeghost_champ.ddj" ) ) );
});
test("published champion GLB changes material images while retaining identical animations and geometry", () => {
	const m = JSON.parse( readFileSync( "../../.generated/client-public/assets/npc/manifest.json", "utf8" ) );
	const row = m.models.MOB_CH_CHAKJI;
	assert.equal( row.materialKind, 0 );
	const json = p => {
		const b = readFileSync( "../../.generated/client-public" + p );
		return JSON.parse( b.subarray( 20, 20 + b.readUInt32LE( 12 ) ) );
	};
	const normal = json( row.glb ), champion = json( row.materialVariants[2] );
	assert.deepEqual( normal.animations, champion.animations );
	assert.deepEqual( normal.meshes, champion.meshes );
	const imageBytes = ( p, j ) => {
		const b = readFileSync( "../../.generated/client-public" + p ), start = 28 + b.readUInt32LE( 12 );
		return j.images.map( i => {
			const v = j.bufferViews[i.bufferView];
			return b.subarray( start + (v.byteOffset ?? 0), start + (v.byteOffset ?? 0) + v.byteLength );
		} );
	};
	assert.notDeepEqual( imageBytes( row.glb, normal ), imageBytes( row.materialVariants[2], champion ) );
	for ( const r of Object.values( m.models ) ) {
		for ( const file of Object.values( r.materialVariants ?? {} ) ) {
			assert.ok( existsSync( "../../.generated/client-public" + file ), file );
		}
	}
});
test("victim defense outranks attacker actions and their tie rules differ", () => {
	const records = new Map( [
		[ 1, { priority: 2, defense: "d1", attack: null } ],
		[ 2, { priority: 2, defense: "d2", attack: null } ],
		[ 3, { priority: 9, defense: null, attack: "a1" } ],
		[ 4, { priority: 9, defense: null, attack: "a2" } ]
	] );
	const effects = [ { gid: 2, skill: 1 }, { gid: 2, skill: 2 }, { gid: 1, skill: 3 }, { gid: 1, skill: 4 } ];
	assert.deepEqual( impactSource( 1, 2, effects, id => records.get( id ) ), { gid: 2, skill: 1, defensive: true } );
	assert.deepEqual( impactSource( 1, 3, effects, id => records.get( id ) ), { gid: 1, skill: 4, defensive: false } );
	assert.deepEqual( impactSource( undefined, 2, [], () => undefined ), { gid: 2, skill: 0, defensive: true } );
	assert.deepEqual( impactSource( 1, 2, [], () => undefined ), { gid: 1, skill: 0, defensive: false } );
});

test("executing casts enter native impact selection without a status token", () => {
	const records = new Map( [
		[ 2, { priority: 1, attack: "sword", defense: null } ],
		[ 3, { priority: 2, attack: "imbue", defense: null } ],
		[ 4, { priority: 1, attack: "equal", defense: null } ],
		[ 5, { priority: 0, attack: null, defense: "shield" } ]
	] );
	const record = id => records.get( id ), cast = { caster: 1, skill: 2, receivedAtMs: 100 };
	assert.deepEqual( impactSource( 1, 2, [], record, cast ), { gid: 1, skill: 2, defensive: false } );
	assert.equal(
		impactSource( 1, 2, [ { gid: 1, skill: 3, token: 9 } ], record, cast ).skill,
		3,
		"higher priority imbue still wins"
	);
	assert.equal(
		impactSource( 1, 2, [ { gid: 1, skill: 4, token: 9, receivedAtMs: 90 } ], record, cast ).skill,
		2,
		"later equal priority cast wins"
	);
	assert.equal(
		impactSource( 1, 2, [ { gid: 1, skill: 4, token: 9, receivedAtMs: 110 } ], record, cast ).skill,
		4,
		"later equal priority buff wins"
	);
	assert.deepEqual( impactSource( 1, 2, [ { gid: 2, skill: 5, token: 9 } ], record, cast ), {
		gid: 2,
		skill: 5,
		defensive: true
	} );
	assert.equal( impactSource( undefined, 2, [], record, cast ).skill, 0, "missing caster stays missing" );
	assert.equal(
		impactSource( 1, 2, [], record, { ...cast, skill: 99 } ).skill,
		0,
		"absent authored resource is not fabricated"
	);
});
test("cancellation flushes only unconsumed target results, and continuation is not duplicated", () => {
	const feedback = createDamageFeedback(), trigger = { cast, phase: "SHOT", event: 1, at: 1 };
	assert.equal( feedback.take( [ cast ], [ trigger ], () => 0, 1, 1000 ).length, 2 );
	const cancelled = { ...cast, cancelledAtMs: 1100 };
	const remaining = feedback.take( [ cancelled ], [], () => 0, 1.1, 1100 );
	assert.equal( remaining.length, 1 );
	assert.equal( remaining[0].target, 2 );
	assert.equal( remaining[0].source, "flush" );
	assert.deepEqual( feedback.take( [ cancelled ], [], () => 0, 1.2, 1200 ), [] );
	feedback.reset();
	assert.equal( feedback.take( [ cancelled ], [], () => 0, 1.1, 1100 ).length, 3 );
	feedback.take( [], [], () => 0, 2, 2000 );
	assert.equal( feedback.take( [ cast ], [ trigger ], () => 0, 2, 2000 ).length, 2 );
});
test("environmental HP feedback uses the wire baseline and survives repeated snapshots without duplicate events", () => {
	const combat = createCombat();
	combat.seed( 2, { hp: 100 } );
	const packet = ( hp, reason ) => {
		const p = new Uint8Array( 11 ), v = new DataView( p.buffer );
		v.setUint32( 0, 2, true );
		v.setUint16( 4, reason, true );
		p[6] = 1;
		v.setUint32( 7, hp, true );
		return p;
	};
	combat.receive( 0x33a6, packet( 70, 0x400 ), 100 );
	assert.equal( combat.state().environmentalDamage[0].damage, 30 );
	const snapshot = combat.state().environmentalDamage;
	combat.receive( 0x33a6, packet( 70, 0x400 ), 110 );
	assert.equal( combat.state().environmentalDamage.length, 1 );
	combat.receive( 0x33a6, packet( 60, 0 ), 120 );
	assert.equal( combat.state().environmentalDamage.length, 1 );
	combat.receive( 0x33a6, packet( 0, 0x402 ), 130 );
	assert.equal( combat.state().environmentalDamage[1].damage, 60 );
	assert.equal( snapshot.length, 1 );
	combat.step( 1130 );
	assert.deepEqual( combat.state().environmentalDamage, [] );
	combat.clear();
	combat.receive( 0x33a6, packet( 25, 0x400 ), 1200 );
	assert.deepEqual( combat.state().environmentalDamage, [], "unseeded HP does not invent damage" );
});
test("attached-effect apply and teardown distinguish instance identity, preserve unregistered instances, and reject malformed packets atomically", () => {
	const combat = createCombat(),
		packet = ( token, size = 12 ) => {
			const p = new Uint8Array( size ), v = new DataView( p.buffer );
			v.setUint32( 0, 2, true );
			v.setUint32( 4, 7, true );
			v.setUint32( 8, token, true );
			if ( size % 4 === 1 ) p[12] = 1;
			if ( size >= 16 ) v.setUint32( size - 4, 99, true );
			return p;
		};
	for ( const size of [ 12, 13, 16, 17 ] ) {
		const refs = new Map( [ [ 7, { status: size % 4 === 1, effectRider: size >= 16 } ] ] );
		const r = attachedEffect( packet( 3, size ), refs );
		for ( const other of [ 12, 13, 16, 17 ].filter( n => n !== size ) ) {
			assert.throws( () => attachedEffect( packet( 3, other ), refs ) );
		}
		assert.equal( r.phase, size % 4 === 1 ? 1 : 2 );
		assert.equal( r.extra, size >= 16 ? 99 : undefined );
	}
	combat.references( [ { id: 7, status: false, effectRider: false } ] );
	assert.throws( () => combat.receive( 0xb419, packet( 3, 13 ) ) );
	assert.equal( combat.state().attachedEffects.length, 0 );
	combat.receive( 0xb419, packet( 3 ) );
	combat.receive( 0xb419, packet( 4 ) );
	combat.receive( 0xb419, packet( 0 ) );
	assert.throws( () => combat.receive( 0xb6a0, Uint8Array.of( 2, 3, 0, 0, 0 ) ) );
	assert.equal( combat.state().attachedEffects.length, 3 );
	combat.receive( 0xb6a0, Uint8Array.of( 1, 3, 0, 0, 0 ) );
	assert.deepEqual( combat.state().attachedEffects.map( r => r.token ), [ 4, 0 ] );
	combat.remove( 2 );
	assert.deepEqual( combat.state().attachedEffects, [] );
});
test("late target-major results are flushed when their callback has already passed", () => {
	const feedback = createDamageFeedback(), empty = { ...cast, results: [] };
	assert.deepEqual(
		feedback.take( [ empty ], [ { cast: empty, phase: "SHOT", event: 1, at: 1 } ], () => 0, 1, 1000 ),
		[]
	);
	const late = feedback.take( [ cast ], [], () => 0, 1.2, 1200 );
	assert.equal( late.length, 2 );
	assert.ok( late.every( x => x.source === "flush" && x.at === 1.2 ) );
	assert.deepEqual( feedback.take( [ cast ], [], () => 0, 1.3, 1300 ), [] );
});
test("spawned effects seed the same instance registry and cannot return after teardown or despawn", () => {
	const combat = createCombat();
	combat.seedEffects( 2, [ { id: 7, token: 90, status: 2 }, { id: 8, status: 1 } ] );
	assert.deepEqual( combat.state().attachedEffects, [ { gid: 2, skill: 7, token: 90, phase: 2, restored: true } ] );
	combat.receive( 0xb6a0, Uint8Array.of( 1, 90, 0, 0, 0 ) );
	assert.equal( combat.state().attachedEffects.length, 0 );
	combat.remove( 2 );
	assert.deepEqual( combat.state().attachedEffects, [] );
	combat.seedEffects( 2, [ { id: 9, token: 91, status: 2 } ] );
	assert.equal( combat.state().attachedEffects[0].skill, 9 );
	combat.clear();
	assert.deepEqual( combat.state().attachedEffects, [] );
});
test("direct and projectile results have disjoint owners through cancellation, arrival, and duplicate events", () => {
	const feedback = createDamageFeedback(), trigger = { cast, phase: "SHOT", event: 1, at: 1 };
	const launch = { kind: "launch", flight: -1, cast, target: 2, index: 0, at: 1, soundSkill: 99 };
	const direct = feedback.take( [ cast ], [ trigger ], () => 0, 1, 1000, [ launch ] );
	assert.equal( direct.length, 1 );
	assert.equal( direct[0].target, 3 );
	assert.equal( direct[0].source, "flush" );
	const cancel = { ...cast, cancelledAtMs: 1100 };
	const flushed = feedback.take( [ cancel ], [], () => 0, 1.1, 1100 );
	assert.equal( flushed.length, 1 );
	assert.equal( flushed[0].target, 2 );
	assert.equal( flushed[0].impact, hit );
	const arrival = { ...launch, kind: "arrival", at: 1.7 };
	const arrived = feedback.take( [], [], () => 0, 1.7, 1700, [ arrival ] );
	assert.equal( arrived.length, 1 );
	assert.equal( arrived[0].source, "cast" );
	assert.equal( arrived[0].soundSkill, 99 );
	assert.equal( arrived[0].at, 1.7 );
	assert.deepEqual( feedback.take( [], [], () => 0, 1.8, 1800, [ arrival ] ), [] );
	feedback.reset();
	feedback.take( [ cast ], [ trigger ], () => 0, 2, 2000, [ launch ] );
	feedback.take( [], [], () => 0, 2.1, 2100, [ { ...launch, kind: "discard" } ] );
	assert.deepEqual( feedback.take( [], [], () => 0, 2.2, 2200, [ arrival ] ), [] );
});
test("effect metadata admission and reset cannot guess optional packet fields", () => {
	const packet = Uint8Array.of( 1, 0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0 );
	assert.throws( () => attachedEffect( packet, new Map() ) );
	for (
		const row of [ { id: 7, status: false }, { id: 7, status: 0, effectRider: false }, {
			id: 0,
			status: false,
			effectRider: false
		} ]
	) assert.throws( () => attachedEffectReferences( [ row ] ) );
	assert.throws(
		() => attachedEffectReferences( [ { id: 7, status: false } ] ),
		/reference 7: effectRider must be boolean \(received undefined\)/
	);
	const row = { id: 7, status: false, effectRider: false };
	assert.throws( () => attachedEffectReferences( [ row, row ] ) );
	const combat = createCombat();
	combat.references( [ row ] );
	combat.receive( 0xb419, packet );
	combat.clear();
	assert.throws( () => combat.receive( 0xb419, packet ) );
	assert.deepEqual( combat.state().attachedEffects, [] );
});

test("local entry effects cross the entity lifecycle once and preserve remaining time across travel", async () => {
	const { createWorldCore } = await load( "runtime/simulation/worker/session/world/core.ts" );
	const { effectRemainingMs } = await load( "foundation/gameplay/attached-effects.ts" );
	const core = createWorldCore( () => {} ),
		base = {
			protocolVersion: 2,
			nativeResult: 1,
			refObjSnapshot: [],
			character: { name: "fixture", skills: [] },
			refSkillSnapshot: [ {
				id: 7,
				group: 1,
				level: 1,
				token: true,
				status: true,
				effectRider: true,
				effectDurationMs: 5000
			} ],
			localPlayerEntry: {
				modelRef: 1907,
				startProfile: { regionId: 257, x: 0, y: 0, z: 0, angle: 0 },
				spawnSkills: [ { id: 7, token: 99, remaining: 4000, status: 1 } ]
			}
		};
	const drain = () => {
		core.step( 0, false );
		const b = core.take();
		if ( b ) core.ack( b.sequence );
		return b?.events.findLast( e => e.kind === "gameplay" )?.state;
	};
	core.bootstrap( base );
	drain();
	core.receive( { opcode: 0x32a6, payload: Uint8Array.of( 1, 0, 0, 0, 0, 0, 0, 0 ) }, 1000 );
	const effect = drain().attachedEffects[0];
	assert.equal( effect.restored, true );
	assert.equal( effectRemainingMs( effect, 2000 ), 3000 );
	core.receive( { opcode: 0xb6a0, payload: Uint8Array.of( 1, 99, 0, 0, 0 ) }, 2000 );
	assert.deepEqual( drain().attachedEffects, [] );
	core.receive( { opcode: 0x3369, payload: Uint8Array.of( 1, 1 ) }, 2100 );
	drain();
	core.bootstrap( { ...base, localPlayerEntry: { ...base.localPlayerEntry, spawnSkills: [] } } );
	drain();
	core.receive( { opcode: 0x32a6, payload: Uint8Array.of( 1, 0, 0, 0, 0, 0, 0, 0 ) }, 2200 );
	assert.deepEqual( drain().attachedEffects, [] );
	core.dispose();
	const { entrySpawnSkills, spawnSkillReferences } = await load( "foundation/gameplay/spawn-skills.ts" );
	const refs = spawnSkillReferences( [ { id: 7, token: true, status: true } ] );
	assert.throws( () => entrySpawnSkills( [ { id: 7, status: 1, token: 99 } ], refs ) );
});

test("transient native effects cannot retain attached hit-source priority", () => {
	const action = { priority: 3, attack: "imbue", defense: null };
	assert.equal( impactSource( 1, 2, [ { gid: 1, skill: 7, token: 0, phase: 2 } ], () => action ).skill, 0 );
	assert.equal(
		impactSource( 1, 2, [ { gid: 1, skill: 7, token: 0, phase: 2, restored: true } ], () => action ).skill,
		7
	);
});

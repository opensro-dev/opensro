/*
===========================================================================

skill-pulse.test.mjs - independent linked damage without casting ownership

Exercise the production decoder and combat owner using native B3C6 mode 2.
Malformed result tails must fail before HP or presentation events change.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { skillPulse } = await import( "../../src/engine/foundation/gameplay/combat-result.ts" );

/*
================
pulsePacket

Native mode, caster, skill, stage count, target count, target and result record.
================
*/
function pulsePacket() {
	return Uint8Array.from( [ 2, 1, 0, 0, 0, 145, 35, 0, 0, 1, 1, 2, 0, 0, 0, 0, 1, 25, 0, 0, 0, 0, 0, 0 ] );
}

test("linked pulses damage once without opening a cast or cooldown", () => {
	/** @type {import('../../src/engine/contracts/effective-hp').CombatPresentationEvent[]} */
	const events = [];
	const combat = createCombat( () => undefined, event => events.push( event ) );
	combat.seed( 2, { hp: 100 } );
	assert.equal( combat.receive( 0xb3c6, pulsePacket(), 2000 ), true );
	assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 75 );
	assert.equal( combat.state().casts.length, 0 );
	const finalized = events.filter( event => event.kind === "cast-finalize" );
	assert.equal( finalized.length, 1 );
	const result = finalized[0];
	assert.ok( result?.kind === "cast-finalize" );
	assert.equal( result.cast.skill, 9105 );
	assert.equal( result.cast.resultOnly, true );
	assert.ok( result.cast.token < 0 );
	combat.receive( 0xb3c6, pulsePacket(), 4000 );
	assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 50 );
	assert.equal( combat.state().casts.length, 0 );
});

test("truncated or trailing pulse bytes leave owner state unchanged", () => {
	const packet = pulsePacket();
	for ( let length = 0; length < packet.length; length++ ) {
		const combat = createCombat();
		combat.seed( 2, { hp: 100 } );
		assert.throws( () => combat.receive( 0xb3c6, packet.slice( 0, length ), 2000 ) );
		assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 100 );
	}
	assert.throws( () => skillPulse( Uint8Array.from( [ ...packet, 0 ] ) ) );
	assert.equal( skillPulse( Uint8Array.of( 4 ) ), null );
});

test("compact bootstrap flags admit the linked source before pulse and paired teardown", () => {
	const combat = createCombat();
	// SpawnSkillRow omits false optional flags; this is the actual JSON shape.
	combat.references( [ { id: 9105, status: false, effectRider: true, effectDurationMs: 12000 } ] );
	combat.seed( 2, { hp: 100 } );
	combat.cooldownReferences( 1, [] );
	const source = Uint8Array.from( [ 145, 35, 0, 0, 10, 0, 0, 0, 2, 0, 0, 0, 0, 0 ] );
	const recipient = Uint8Array.from( [ 2, 0, 0, 0, 145, 35, 0, 0, 11, 0, 0, 0, 0, 0, 0, 0 ] );
	assert.equal( combat.receive( 0xb419, recipient, 0 ), true );
	assert.equal( combat.receive( 0xb5ed, source, 0 ), true );
	assert.equal( combat.state().attachedEffects.length, 2 );
	assert.deepEqual( combat.state().huntingPoints, [] );
	assert.equal( combat.state().attachedEffects.find( row => row.token === 10 )?.subject?.gid, 2 );
	combat.receive( 0xb3c6, pulsePacket(), 2000 );
	assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 75 );
	combat.receive( 0xb6a0, Uint8Array.from( [ 2, 10, 0, 0, 0, 11, 0, 0, 0 ] ), 12001 );
	assert.equal( combat.state().attachedEffects.length, 0 );
});

test("trap results retain their origin after object removal without opening a cast", () => {
	const entity = {
		gid: 1,
		refObjId: 0xffffffff,
		kind: "skill-object",
		name: "",
		regionId: 257,
		x: 30,
		y: 8,
		z: 40,
		heading: 0,
		skillObject: { skillId: 9105 }
	};
	const objects = new Map( [ [ 1, entity ] ] );
	const events = [];
	const combat = createCombat( gid => objects.get( gid ), event => events.push( event ) );
	const packet = Uint8Array.from( [ 3, 1, 0, 0, 0, ...pulsePacket().slice( 9 ) ] );
	combat.seed( 2, { hp: 100 } );
	assert.equal( combat.receive( 0xb3c6, packet, 2000 ), true );
	objects.clear();
	assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 75 );
	assert.equal( combat.state().casts.length, 0 );
	const result = events.find( event => event.kind === "cast-finalize" );
	assert.equal( result.cast.skill, 9105 );
	assert.deepEqual( result.cast.effectPosition, { regionId: 257, x: 30, y: 8, z: 40, angle: 0 } );
	// 7756D0 ignores an unknown object's remaining bytes.
	assert.equal( combat.receive( 0xb3c6, packet.slice( 0, 5 ), 2001 ), true );
	assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 75 );
});

test("malformed known trap results cannot mutate HP", () => {
	const packet = Uint8Array.from( [ 3, 1, 0, 0, 0, ...pulsePacket().slice( 9 ) ] );
	for ( let length = 5; length < packet.length; length++ ) {
		const combat = createCombat( () => ({
			gid: 1,
			refObjId: 0xffffffff,
			kind: "skill-object",
			name: "",
			regionId: 257,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			skillObject: { skillId: 9105 }
		}) );
		combat.seed( 2, { hp: 100 } );
		assert.throws( () => combat.receive( 0xb3c6, packet.slice( 0, length ), 2000 ) );
		assert.equal( combat.state().vitals.find( row => row.gid === 2 )?.hp, 100 );
	}
});

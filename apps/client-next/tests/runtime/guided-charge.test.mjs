/*
===========================================================================

guided-charge.test.mjs - server-authorized charge lifetime regression

Targeted Sprint Assault and remote travel share the arrival owner with ground
teleports. UI targeting metadata cannot decide whether a received cast travels.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);

const SKILL = 18601;
const CASTER = 7;
const TOKEN = 9;
const ARRIVAL_MS = 530;
const METADATA = {
	id: SKILL,
	group: 655,
	level: 1,
	name: "Sprint Assault",
	spCost: 0,
	trainable: true,
	targetRequired: true,
	cooldownMs: 10000,
	masteries: [],
	prerequisites: []
};

/*
================
castPacket

Encode a guided or stationary native cast with no impact batch. Arrival
authority comes from bit 8, independently of the skill's target UI metadata.
================
*/
function castPacket( guided ) {
	const packet = Buffer.alloc( guided ? 27 : 19 );
	packet[0] = 1;
	packet.writeUInt32LE( SKILL, 2 );
	packet.writeUInt32LE( CASTER, 6 );
	packet.writeUInt32LE( TOKEN, 10 );
	if ( guided ) {
		packet[18] = 8;
		packet.writeUInt16LE( 0x61a8, 19 );
		packet.writeInt16LE( 315, 21 );
		packet.writeInt16LE( 20, 23 );
		packet.writeInt16LE( 100, 25 );
	}
	return packet;
}

test("targeted and remote charges close on arrival without ground-target metadata", () => {
	for ( const local of [ true, false ] ) {
		const combat = createCombat();
		if ( local ) combat.cooldownReferences( CASTER, [ { ...METADATA, groundTarget: false } ] );
		combat.receive( 0xb245, castPacket( true ), 100 );
		assert.equal( combat.takeDisplacements()[0].kind, 8 );
		combat.guidedArrival( TOKEN, ARRIVAL_MS );
		assert.equal( combat.guidedActive( CASTER, 200 ), true );
		combat.step( ARRIVAL_MS - 1 );
		assert.deepEqual( combat.takeCancellations(), [] );
		combat.step( ARRIVAL_MS );
		assert.equal( combat.state().casts[0].cancelledAtMs, ARRIVAL_MS );
		assert.deepEqual( combat.takeCancellations(), [ TOKEN ] );
		assert.equal( combat.guidedActive( CASTER, ARRIVAL_MS ), false );
		combat.step( ARRIVAL_MS + 1 );
		assert.deepEqual( combat.takeCancellations(), [] );
	}
});

test("stationary casts cannot acquire an arrival deadline from targeting metadata", () => {
	const combat = createCombat();
	combat.cooldownReferences( CASTER, [ { ...METADATA, groundTarget: true } ] );
	combat.receive( 0xb245, castPacket( false ), 100 );
	combat.guidedArrival( TOKEN, ARRIVAL_MS );
	combat.step( ARRIVAL_MS );
	assert.equal( combat.guidedActive( CASTER, 200 ), false );
	assert.equal( combat.state().casts[0].cancelledAtMs, undefined );
	assert.deepEqual( combat.takeCancellations(), [] );
});

test("cast control releases stationary WAIT but preserves existing and newly installed guided WAIT", () => {
	for ( const local of [ true, false ] ) {
		for ( const initialTravel of [ true, false ] ) {
			for ( const continuedTravel of [ true, false ] ) {
				const combat = createCombat();
				if ( local ) combat.cooldownReferences( CASTER, [ METADATA ] );
				combat.receive( 0xb245, castPacket( initialTravel ), 100 );
				const body = castPacket( continuedTravel ).subarray( 14 );
				const control = Buffer.alloc( 5 + body.length );
				control[0] = 1;
				control.writeUInt32LE( TOKEN, 1 );
				body.copy( control, 5 );
				combat.receive( 0xb505, control, 200 );
				const guided = initialTravel || continuedTravel;
				assert.equal( combat.state().casts[0].shotAtMs, guided ? undefined : 200 );
				combat.receive( 0xb505, control, 250 );
				assert.equal( combat.state().casts[0].shotAtMs, guided ? undefined : 200 );
				combat.guidedArrival( TOKEN, ARRIVAL_MS );
				combat.step( ARRIVAL_MS );
				assert.equal( combat.state().casts[0].cancelledAtMs, guided ? ARRIVAL_MS : undefined );
			}
		}
	}
});

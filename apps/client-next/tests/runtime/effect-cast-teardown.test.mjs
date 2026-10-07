/*
===========================================================================

effect-cast-teardown.test.mjs - tests for combat.ts: a cast whose effect
attached under its own token ends when that token is torn down

Crystal Wall (SKILL_CH_COLD_BINGBYEOK_A_01) as the server sends it: the cast
(B245), its effect under the same token (B419), and the teardown five
seconds later (B6A0). The ice kept standing around the caster because only
the effect was dropped and the cast stayed in the table for good.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createCastMotionLock } = await import( "../../src/engine/foundation/gameplay/cast-motion-lock.ts" );

const CASTER = 100003;
// The frames the server sends for one Crystal Wall (casthalt probe, token 1).
const CAST = Buffer.from( "010063000000a386010001000000a386010000", "hex" );
const ATTACH = Buffer.from( "a38601006300000001000000", "hex" );
const TEARDOWN = Buffer.from( "0101000000", "hex" );

test("a torn-down token ends both its effect and the cast that owns it", () => {
	const combat = createCombat();
	combat.cooldownReferences( CASTER, [] );
	combat.references( [ { id: 99, status: false, effectRider: false } ] );
	assert.equal( combat.receive( 0xb245, CAST, 1000 ), true );
	assert.equal( combat.receive( 0xb419, ATTACH, 1000 ), true );
	assert.equal( combat.state().casts.length, 1 );
	assert.equal( combat.receive( 0xb6a0, TEARDOWN, 6000 ), true );
	assert.equal( combat.state().attachedEffects.length, 0 );
	assert.equal( combat.state().casts[0]?.cancelledAtMs, 6000, "the wall's cast never ended" );
	combat.step( 6200 );
	assert.equal( combat.state().casts.length, 0 );
});

test("Fire Wall teardown releases the caster's movement lock", () => {
	const skillId = 136, combat = createCombat(), lock = createCastMotionLock();
	const cast = Buffer.from( CAST ), attach = Buffer.from( ATTACH );
	cast.writeUInt32LE( skillId, 2 );
	attach.writeUInt32LE( skillId, 4 );
	lock.catalog( [ {
		id: skillId,
		group: 1,
		level: 1,
		name: "Fire Wall",
		spCost: 0,
		trainable: false,
		targetRequired: false,
		cooldownMs: 0,
		actionMs: 1500,
		holdsCaster: true,
		masteries: [],
		prerequisites: []
	} ] );
	combat.cooldownReferences( CASTER, [] );
	combat.references( [ { id: skillId, status: false, effectRider: false } ] );
	assert.equal( combat.receive( 0xb245, cast, 1000 ), true );
	assert.equal( combat.receive( 0xb419, attach, 1000 ), true );
	assert.equal( lock.locked( combat.state().casts, CASTER, 60000, false ), true );
	assert.equal( combat.receive( 0xb6a0, TEARDOWN, 60001 ), true );
	assert.equal( lock.locked( combat.state().casts, CASTER, 60001, false ), false );
	assert.equal( combat.state().attachedEffects.length, 0 );
});

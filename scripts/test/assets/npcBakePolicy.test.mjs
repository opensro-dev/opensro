/*
===========================================================================

npcBakePolicy.test.mjs - which clip policy a shared NPC model bakes with

A BSR that a town NPC shares with a monster, a COS or a ride bakes once,
with the monster policy, whichever consumer the roster lists first
(monsterPolicyBsrs). The v1.696 lamp genie and its jinn pet share
res/cos/p_jinn.bsr. Fixture rosters only; no game data.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { monsterPolicyBsrs } from "../../build/char/buildNpcModelAssets.mjs";

const MODELS = {
	NPC_GACHA_LAMP_GENIE: "res/cos/p_jinn.bsr",
	COS_P_JINN: "res/cos/p_jinn.bsr",
	NPC_CH_SMITH: "res/npc/npc_ch_smith.bsr",
	MOB_CH_TIGER: "res/mob/china/tiger.bsr",
	COS_C_HORSE: "res/cos/c_horse.bsr",
	NPC_CH_STABLE: "res/cos/c_horse_ride.bsr"
};
const MONSTERS = new Set( [ "COS_P_JINN", "MOB_CH_TIGER", "COS_C_HORSE" ] );
const RIDES = { COS_C_HORSE: "res/cos/c_horse_ride.bsr" };

/*
================
policy

The monster-policy set for a roster listed in the given order.
================
*/
function policy( codenames ) {
	return monsterPolicyBsrs( codenames.map( codename => ({ codename }) ), {
		resolveModel: codename => MODELS[codename] ? { bsrPath: MODELS[codename] } : null,
		isMonster: codename => MONSTERS.has( codename ),
		rideModelPath: codename => RIDES[codename] ?? null
	} );
}

test("an NPC that shares a pet's model bakes it with the monster policy, in either roster order", () => {
	for ( const order of [ [ "NPC_GACHA_LAMP_GENIE", "COS_P_JINN" ], [ "COS_P_JINN", "NPC_GACHA_LAMP_GENIE" ] ] ) {
		assert.equal( policy( order ).has( "res/cos/p_jinn.bsr" ), true, order.join( "," ) );
	}
});

test("an NPC on its own model keeps the NPC policy", () => {
	const paths = policy( Object.keys( MODELS ) );
	assert.equal( paths.has( "res/npc/npc_ch_smith.bsr" ), false );
	assert.equal( paths.has( "res/mob/china/tiger.bsr" ), true );
});

test("a ride model counts as a monster bake for an NPC that shares it", () => {
	assert.equal( policy( [ "NPC_CH_STABLE", "COS_C_HORSE" ] ).has( "res/cos/c_horse_ride.bsr" ), true );
});

test("an unresolved model adds nothing", () => {
	assert.deepEqual( [ ...policy( [ "MOB_UNKNOWN" ] ) ], [] );
});

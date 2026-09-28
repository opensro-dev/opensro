/*
===========================================================================

level-recovery.test.mjs - authoritative level recovery reaches the HUD.

Feeds the shipped packet consumers and presentation HP owner. EXP and
maximum-stat changes cannot substitute for the absolute current-gauge packet.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";

const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createEffectiveHp } = await import( "../../src/engine/runtime/presentation/effective-hp.ts" );
const { bootstrapProgression, progressionPacket } = await import(
	"../../src/engine/foundation/gameplay/progression.ts"
);

const PLAYER_GID = 100002;
const BASE_STATS_OPCODE = 0x343c;
const VITALS_OPCODE = 0x33a6;

test("level recovery updates authoritative and displayed HP without damage feedback", () => {
	const hp = createEffectiveHp();
	const combat = createCombat( () => undefined, event => {
		if ( event.kind === "hp-seed" || event.kind === "hp-refresh" ) {
			hp.receive( event );
		}
	} );
	combat.seed( PLAYER_GID, { hp: 17, mp: 9 } );
	const before = bootstrapProgression( { character: { level: 1, experience: "50" } } );
	const maxima = Buffer.from(
		"06000000070000000a0000000c000000040006000d000d00e4000000e400000016001600",
		"hex"
	);
	const progression = progressionPacket( before, BASE_STATS_OPCODE, maxima );
	assert.ok( progression );
	assert.equal( progression.stats?.maxHp, 228 );
	assert.equal( progression.stats?.maxMp, 228 );
	assert.equal( hp.hp( PLAYER_GID ), 17, "maximum updates do not fabricate a heal" );

	const recovery = Buffer.from( "a2860100800003e4000000e4000000", "hex" );
	assert.equal( combat.receive( VITALS_OPCODE, recovery, 100 ), true );
	const current = combat.state().vitals.find( row => row.gid === PLAYER_GID );
	assert.equal( current?.hp, progression.stats?.maxHp );
	assert.equal( current?.mp, progression.stats?.maxMp );
	assert.equal( hp.hp( PLAYER_GID ), 228 );
	assert.equal( hp.dead( PLAYER_GID ), false );
	assert.deepEqual( combat.state().environmentalDamage, [] );
});

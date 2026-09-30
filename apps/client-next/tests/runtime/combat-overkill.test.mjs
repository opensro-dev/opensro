/*
===========================================================================

combat-overkill.test.mjs - full damage feedback with bounded victim HP

Consume the server's committed fixture through the production browser decoder,
HP owner and animation feedback owner. A fatal hit keeps its full damage value.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );

const FIXTURE = new URL(
	"../../../server/internal/game/item/wire/testdata/skill_action_result_fixture.json",
	import.meta.url
);
const RESULT_INDEX = 0;

/*
================
fullHitFeedback

Replay a committed result and flush its animation callback through the shared
feedback owner. No display-side arithmetic may replace the transmitted hit.
================
*/
function fullHitFeedback( scenario, expected ) {
	const combat = createCombat();
	combat.seed( expected.targetGid, { hp: scenario.beforeHp, maxHp: expected.initialHp } );
	assert.equal( combat.receive( scenario.opcode, Buffer.from( scenario.payloadHex, "hex" ), 100 ), true );
	const state = combat.state();
	assert.equal( defined( state.vitals.find( row => row.gid === expected.targetGid ) ).hp, scenario.currentHp );
	const cast = defined( state.casts[0] );
	const impact = defined( defined( cast.results )[0].impacts[0] );
	assert.equal( impact.damage, scenario.requestedDamage );
	assert.equal( impact.fatal, scenario.fatal );
	const feedback = createDamageFeedback();
	const hits = feedback.take( [ { ...cast, cancelledAtMs: 100 } ], [], () => RESULT_INDEX, 1, 100 );
	assert.equal( hits.length, 1 );
	assert.equal( hits[0].impact.damage, scenario.requestedDamage );
	assert.equal( feedback.take( [ { ...cast, cancelledAtMs: 100 } ], [], () => RESULT_INDEX, 1, 100 ).length, 0 );
}

test("server overkill remains full through browser HP and damage feedback owners", () => {
	const fixture = JSON.parse( readFileSync( FIXTURE, "utf8" ) );
	assert.ok( fixture.scenarios.some( row => row.requestedDamage > row.beforeHp ) );
	for ( const scenario of fixture.scenarios ) fullHitFeedback( scenario, fixture.expect );
});

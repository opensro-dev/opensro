/*
===========================================================================

actor-clock.test.mjs - a published actor's clocks come from its entry when it has one

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { actorClock } = await import( "../../src/engine/foundation/animation/actor-clock.ts" );

test("an actor's entry wins over its own clocks, and an actor with none carries its own", () => {
	const actor = Object.freeze( { gid: 1, time: 1, layers: [], modelAnimation: undefined } );
	const entry = { time: 2, layers: undefined, modelAnimation: undefined };
	assert.equal( actorClock( actor, new Map( [ [ actor, entry ] ] ) ), entry );
	assert.equal( actorClock( actor, new Map() ), actor );
	assert.equal( actorClock( actor, undefined ), actor );
});

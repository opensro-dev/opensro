/*
===========================================================================

quest-locations.test.mjs - tests for quest-locations.ts

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { questLocationTargets } = await import( "../../src/engine/foundation/ui/quest-locations.ts" );
const quest = refId => ({ refId, u08: 0, u09: 0, flags: 0, contents: [], targetIds: [] });

test("the bell and dig quests name the spot their tool is used at, once each", () => {
	const spots = questLocationTargets( [ quest( 349 ), quest( 363 ), quest( 357 ), quest( 1 ) ], true );
	assert.deepEqual( spots, [
		{ regionId: 23980, x: 1218, y: 230, z: 706 },
		{ regionId: 25954, x: 1672, y: 180, z: 232 }
	] );
});

test("nothing is marked while the quest target markers row is off or no such quest is active", () => {
	assert.deepEqual( questLocationTargets( [ quest( 349 ) ], false ), [] );
	assert.deepEqual( questLocationTargets( [ quest( 5 ), quest( 6 ) ], true ), [] );
	assert.deepEqual( questLocationTargets( undefined, true ), [] );
});

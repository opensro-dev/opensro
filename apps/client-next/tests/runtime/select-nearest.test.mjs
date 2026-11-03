/*
===========================================================================

select-nearest.test.mjs - the Experimental Auto Select action

The monster choice (861580's 70 units) and the Actions window row, off by
default and on only with the option.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import "../helpers/native-source-loader.mjs";

const { nearestMonster } = await import( "../../src/engine/foundation/gameplay/select-nearest.ts" );
const { offeredActions, AUTO_SELECT_ACTION_ID } = await import( "../../src/engine/foundation/ui/action-layout.ts" );

const REGION = 0x6a68;
const LOCAL = { regionId: REGION, x: 100, y: 0, z: 100 };

/** @returns {any} */
function monster( gid, x, z, y = 0, appearanceState ) {
	return { gid, regionId: REGION, x, y, z, ...(appearanceState ? { appearanceState } : {}) };
}

test("Auto Select takes the nearest monster inside the reach", () => {
	const near = monster( 1, 130, 100 ), far = monster( 2, 150, 100 );
	assert.equal( nearestMonster( [ far, near ], LOCAL ), near );
});

test("Auto Select's reach ends at 70 units, height included", () => {
	assert.equal( nearestMonster( [ monster( 1, 171, 100 ) ], LOCAL ), undefined );
	assert.equal( nearestMonster( [ monster( 1, 160, 100, 60 ) ], LOCAL ), undefined );
	assert.ok( nearestMonster( [ monster( 1, 170, 100 ) ], LOCAL ) );
});

test("Auto Select passes over the dead", () => {
	assert.equal( nearestMonster( [ monster( 1, 110, 100, 0, [ 2 ] ) ], LOCAL ), undefined );
});

test("the Auto Select row exists only with the Experimental option", () => {
	const native = [ { id: 1000, slot: 100, name: "sit", icon: "action/sit.ddj" } ];
	assert.equal( offeredActions( native, false ), native );
	assert.deepEqual(
		offeredActions( native, true ).map( row => row.id ),
		[ 1000, AUTO_SELECT_ACTION_ID ]
	);
});

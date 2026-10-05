/*
===========================================================================

repair.test.mjs - the repair price the shop shows and the request it sends

Loads the shipped module through the shared native loader. The prices
match the server's RepairQuote cases (combat/durability_test.go), so the
confirmation shows what the smith charges.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { itemRepairCost, repairAllCost, repairRequest, repairNotice, REPAIR_ONE_SLOT, REPAIR_ALL_SLOTS } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/repair.ts" ).href
);

/*
================
sword

An equipped sword whose maximum is 100 (variance field 0 = 31 over 40..100).
================
*/
function sword( durability, fields = {} ) {
	return {
		slot: 6,
		refObjId: 1,
		typeFlags: (3 << 2) | (1 << 5) | (6 << 7) | (2 << 11),
		quantity: 1,
		plus: 0,
		variance: "31",
		durability,
		magic: [],
		tooltip: {
			fields: {
				varianceIntMin1c0: 40,
				varianceIntMax1c4: 100,
				canRepair: 1,
				repairCostB4: 200,
				reviveCostB8: 50,
				...fields
			}
		}
	};
}

test("the repair price is the native per-point price over the missing points", () => {
	assert.equal( itemRepairCost( sword( 40 ) ), 120 );
	assert.equal( itemRepairCost( sword( 0 ) ), 248, "a broken item repairs from 1 and pays its revive cost" );
	assert.equal( itemRepairCost( sword( 40, { repairCostB4: 50 } ) ), 60, "the price is at least 1 a point" );
	assert.equal( itemRepairCost( sword( 100 ) ), 0 );
	assert.equal( itemRepairCost( sword( 40, { canRepair: 0 } ) ), 0, "an unrepairable item costs nothing" );
	assert.equal( repairAllCost( [ sword( 40 ), sword( 0 ) ] ), 368 );
});

test("the request names the smith, the mode and only for one item its slot", () => {
	assert.deepEqual( [ ...repairRequest( 0x01020304, REPAIR_ONE_SLOT, 6 ).payload ], [ 4, 3, 2, 1, 1, 6 ] );
	const all = repairRequest( 7, REPAIR_ALL_SLOTS );
	assert.equal( all.opcode, 0x746f );
	assert.deepEqual( [ ...all.payload ], [ 7, 0, 0, 0, 2 ] );
	assert.equal( repairNotice( 0xb46f, Uint8Array.of( 1 ) ), null );
	assert.ok( repairNotice( 0xb46f, Uint8Array.of( 2, 7 ) ), "a refusal raises its category-13 notice" );
});

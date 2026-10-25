/*
===========================================================================

planeBranchesGate.test.mjs - shrinking-ledger gate for dungeon region-bit
branches (#1646)

Verifies that check_plane_branches.mjs counts IsDungeonRegion calls and
DungeonRegionBit references outside comments, exempts the plane owners, and
refuses a new branch through the shared ledger ratchet.

===========================================================================
*/

import assert from "node:assert/strict";
import { test } from "node:test";
import { countPlaneBranches, evaluatePlaneRatchet, parseBaseline } from "../../checks/check_plane_branches.mjs";

test("countPlaneBranches counts region-bit branches outside comments and exempts the plane owners", () => {
	const sample = [
		"// IsDungeonRegion(x) in a line comment is ignored",
		"/* DungeonRegionBit in a block comment is ignored */",
		"if simulation.IsDungeonRegion(from.RegionID) != IsDungeonRegion(to.RegionID) {",
		"\treturn r&worldgeom.DungeonRegionBit != 0 // IsDungeonRegion( trailing comment",
		"isDungeonRegionCache := 1 // a different name",
		"test := simulation.IsDungeonRegion // a stored predicate branches too"
	].join( "\n" );

	assert.equal( countPlaneBranches( sample, "apps/server/internal/game/world/simulation/monsterchase.go" ), 4 );
	assert.equal( countPlaneBranches( sample, "apps/server/internal/game/world/movement/plane.go" ), 0 );
	assert.equal( countPlaneBranches( sample, "apps/server/internal/game/world/geometry.go" ), 0 );
});

test("evaluatePlaneRatchet refuses a new branch and a raised count, and asks to lower a migrated one", () => {
	const baseline = parseBaseline( [ "# comment", "apps/server/a.go 2" ].join( "\n" ) );
	const exact = evaluatePlaneRatchet( new Map( [ [ "apps/server/a.go", 2 ] ] ), baseline, () => true );
	assert.deepEqual( exact.problems, [] );

	const added = evaluatePlaneRatchet(
		new Map( [ [ "apps/server/a.go", 2 ], [ "apps/server/b.go", 1 ] ] ),
		baseline,
		() => true
	);
	assert.match( added.problems.join( "\n" ), /introduce dungeon region-bit branches outside the plane owner/ );

	const raised = evaluatePlaneRatchet( new Map( [ [ "apps/server/a.go", 3 ] ] ), baseline, () => true );
	assert.match( raised.problems.join( "\n" ), /exceed their plane-branches-baseline\.txt count/ );

	const migrated = evaluatePlaneRatchet( new Map(), baseline, () => true );
	assert.match( migrated.problems.join( "\n" ), /check_plane_branches\.mjs --update/ );
	const updated = evaluatePlaneRatchet( new Map(), baseline, () => true, true );
	assert.deepEqual( updated.problems, [] );
	assert.equal( updated.nextBaseline.size, 0 );
});

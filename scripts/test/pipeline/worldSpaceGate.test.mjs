/*
===========================================================================

worldSpaceGate.test.mjs - shrinking-ledger gate for world-space literals

Verifies that check_world_space.mjs counts `* 1920` / `1920 *` and raw
`0x8000` literals outside comments, exempts the world-space owner and
documented non-region 0x8000 files, and enforces the shrinking-ledger
ratchet.

===========================================================================
*/

import assert from "node:assert/strict";
import { test } from "node:test";
import {
	countWorldSpaceLiterals,
	evaluateWorldSpaceRatchet,
	formatBaseline,
	parseBaseline
} from "../../checks/check_world_space.mjs";

test("countWorldSpaceLiterals counts * 1920 and 0x8000 outside comments and exempts owners/allowlist", () => {
	const sample = [
		"// (a.regionId & 0x8000) * 1920 in a comment is ignored",
		" * 1920 * dx in a block comment is ignored",
		"const dungeon = (pose.regionId & 0x8000) !== 0; // trailing 0x8000 * 1920 comment ignored",
		"const wx = sx * 1920 + 1920 * sz;"
	].join( "\n" );

	assert.equal( countWorldSpaceLiterals( sample, "apps/client-next/src/engine/runtime/foo.ts" ), 3 );
	assert.equal(
		countWorldSpaceLiterals( sample, "apps/client-next/src/engine/foundation/world/world-space.ts" ),
		0
	);
	// In a non-region 0x8000 file, 0x8000 is ignored but * 1920 is still counted.
	assert.equal(
		countWorldSpaceLiterals( sample, "apps/client-next/src/engine/foundation/rendering/texel-readback.ts" ),
		2
	);
});

test("evaluateWorldSpaceRatchet enforces the shrinking ledger rules", () => {
	const baseline = parseBaseline(
		[
			"# comment",
			"apps/client-next/src/a.ts 3",
			"apps/client-next/src/b.ts 2"
		].join( "\n" )
	);

	// Exact match passes.
	const exact = evaluateWorldSpaceRatchet(
		new Map( [
			[ "apps/client-next/src/a.ts", 3 ],
			[ "apps/client-next/src/b.ts", 2 ]
		] ),
		baseline,
		() => true,
		false
	);
	assert.deepEqual( exact.problems, [] );

	// New unlisted file or raised count fails even with --update.
	const raised = evaluateWorldSpaceRatchet(
		new Map( [
			[ "apps/client-next/src/a.ts", 4 ],
			[ "apps/client-next/src/b.ts", 2 ],
			[ "apps/client-next/src/c.ts", 1 ]
		] ),
		baseline,
		() => true,
		true
	);
	assert.equal( raised.problems.length, 2 );

	// Lowered count or zeroed file fails without --update, and updates cleanly with --update.
	const lowered = new Map( [ [ "apps/client-next/src/a.ts", 1 ] ] );
	const withoutUpdate = evaluateWorldSpaceRatchet( lowered, baseline, () => true, false );
	assert.equal( withoutUpdate.problems.length, 1 );

	const withUpdate = evaluateWorldSpaceRatchet( lowered, baseline, () => true, true );
	assert.deepEqual( withUpdate.problems, [] );
	assert.equal( withUpdate.loweredCount, 1 );
	assert.equal( withUpdate.removedCount, 1 );
	assert.ok( formatBaseline( withUpdate.nextBaseline ).endsWith( "apps/client-next/src/a.ts 1\n" ) );
});

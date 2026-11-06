/*
===========================================================================

goTestBudget.test.mjs - the test budget judges CPU, not the wall clock

===========================================================================
*/

import assert from "node:assert/strict";
import { test } from "node:test";
import { overBudgetPackages, parseTestRuns } from "../../checks/goTestBudget.mjs";

const BUDGET = 30;
const LEDGER = [ "opensro.online/server/internal/game/action" ];

function outputFor( rows ) {
	return rows.map( ( row ) => `ok  \t${row}` ).join( "\n" );
}

test("parseTestRuns reads timed and cached package lines only", () => {
	const runs = parseTestRuns(
		[
			"ok  \topensro.online/server/internal/data/store\t85.2s",
			"ok  \topensro.online/server/internal/game/quest\t(cached)",
			"FAIL\topensro.online/server/internal/game/action\t12.0s",
			"?   \topensro.online/server/tools/testcpu\t[no test files]"
		].join( "\n" )
	);
	assert.deepEqual( runs, [
		{ pkg: "opensro.online/server/internal/data/store", wallSeconds: 85.2 },
		{ pkg: "opensro.online/server/internal/game/quest", wallSeconds: null }
	] );
});

test("a busy box does not convict: wall over, CPU under passes", () => {
	// The 2026-11-06 storm (opensro#2582): store read 85.2s of wall
	// while several agent suites shared the box; its tests burn a
	// fraction of that in CPU.
	const runs = parseTestRuns( outputFor( [ "opensro.online/server/internal/data/store\t85.2s" ] ) );
	const cpu = new Map( [ [ "opensro.online/server/internal/data/store", 21.4 ] ] );
	const verdict = overBudgetPackages( runs, cpu, LEDGER, BUDGET );
	assert.deepEqual( verdict.offenders, [] );
	assert.deepEqual( verdict.over, [] );
	assert.deepEqual( verdict.forgiven, [
		{ pkg: "opensro.online/server/internal/data/store", wallSeconds: 85.2, cpuSeconds: 21.4 }
	] );
});

test("a package slow on a quiet box still trips: CPU over budget", () => {
	const runs = parseTestRuns( outputFor( [ "opensro.online/server/internal/game/world/movement\t41.0s" ] ) );
	const cpu = new Map( [ [ "opensro.online/server/internal/game/world/movement", 39.6 ] ] );
	const verdict = overBudgetPackages( runs, cpu, LEDGER, BUDGET );
	assert.deepEqual( verdict.offenders.map( ( entry ) => entry.pkg ), [
		"opensro.online/server/internal/game/world/movement"
	] );
	assert.equal( verdict.over[0].cpuSeconds, 39.6 );
	assert.equal( verdict.over[0].listed, false );
});

test("the ledger exempts its package however slow it is", () => {
	const runs = parseTestRuns( outputFor( [ "opensro.online/server/internal/game/action\t293.0s" ] ) );
	const cpu = new Map( [ [ "opensro.online/server/internal/game/action", 210.5 ] ] );
	const verdict = overBudgetPackages( runs, cpu, LEDGER, BUDGET );
	assert.deepEqual( verdict.offenders, [] );
	assert.equal( verdict.over.length, 1 );
	assert.equal( verdict.over[0].listed, true );
});

test("no CPU record keeps the wall verdict", () => {
	const runs = parseTestRuns( outputFor( [ "opensro.online/server/internal/game/enterworld\t34.2s" ] ) );
	const verdict = overBudgetPackages( runs, new Map(), LEDGER, BUDGET );
	assert.deepEqual( verdict.offenders.map( ( entry ) => entry.pkg ), [
		"opensro.online/server/internal/game/enterworld"
	] );
	assert.equal( verdict.offenders[0].cpuSeconds, null );
});

test("cached packages are never timed, and the budget line is exclusive", () => {
	const runs = parseTestRuns(
		outputFor( [
			"opensro.online/server/internal/game/quest\t(cached)",
			"opensro.online/server/internal/data/store\t30.0s"
		] )
	);
	const cpu = new Map( [ [ "opensro.online/server/internal/data/store", 30.0 ] ] );
	const verdict = overBudgetPackages( runs, cpu, LEDGER, BUDGET );
	assert.deepEqual( verdict.over, [] );
	assert.deepEqual( verdict.offenders, [] );
});

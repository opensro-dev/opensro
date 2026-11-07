/*
===========================================================================

goTestBudget.test.mjs - the Go test budget's arithmetic

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";

import { budgetOffenders, overBudgetPackages, packageTestTimes, soloVerdicts } from "../../checks/goTestBudget.mjs";

// One busy full run: two packages far over, one just over, one exactly at
// the budget, one cached, one failed (its line never counts as a time).
const OUTPUT = [
	"ok  \topensro.online/server/internal/game/action\t135.4s",
	"ok  \topensro.online/server/internal/data/store\t85.2s",
	"ok  \topensro.online/server/internal/config\t31s",
	"ok  \topensro.online/server/internal/game/quest\t30s",
	"ok  \topensro.online/server/internal/game/world\t0.4s",
	"ok  \topensro.online/server/internal/transport\t(cached)",
	"FAIL\topensro.online/server/internal/game/social\t45s"
].join( "\n" );

/*
================
packageTestTimes
================
*/
test("packageTestTimes reads timed ok lines only", () => {
	const times = packageTestTimes( OUTPUT );
	assert.equal( times.get( "opensro.online/server/internal/game/action" ), 135.4 );
	assert.equal( times.get( "opensro.online/server/internal/game/world" ), 0.4 );
	// Cached packages spent nothing in this run; a failure is not a time.
	assert.equal( times.has( "opensro.online/server/internal/transport" ), false );
	assert.equal( times.has( "opensro.online/server/internal/game/social" ), false );
});

/*
================
overBudgetPackages
================
*/
test("overBudgetPackages lists every package over the budget, strictly", () => {
	assert.deepEqual( overBudgetPackages( OUTPUT, 30 ), [
		{ pkg: "opensro.online/server/internal/game/action", seconds: 135.4 },
		{ pkg: "opensro.online/server/internal/data/store", seconds: 85.2 },
		{ pkg: "opensro.online/server/internal/config", seconds: 31 }
	] );
	// Exactly at the budget is inside it.
	assert.equal( overBudgetPackages( OUTPUT, 30 ).some( ( { pkg } ) => pkg.endsWith( "/quest" ) ), false );
});

/*
================
budgetOffenders
================
*/
test("budgetOffenders leaves ledgered packages to the ledger", () => {
	const ledger = [ "opensro.online/server/internal/game/action" ];
	assert.deepEqual( budgetOffenders( OUTPUT, 30, ledger ), [
		{ pkg: "opensro.online/server/internal/data/store", seconds: 85.2 },
		{ pkg: "opensro.online/server/internal/config", seconds: 31 }
	] );
	assert.deepEqual(
		budgetOffenders( OUTPUT, 30, [
			...ledger,
			"opensro.online/server/internal/data/store",
			"opensro.online/server/internal/config"
		] ),
		[]
	);
});

/*
================
soloVerdicts
================
*/
test("soloVerdicts judges the solo times, and no solo time never fits", () => {
	const offenders = [
		{ pkg: "opensro.online/server/internal/data/store", seconds: 85.2 },
		{ pkg: "opensro.online/server/internal/config", seconds: 31 },
		{ pkg: "opensro.online/server/internal/game/action", seconds: 135.4 }
	];
	const solo = new Map( [
		[ "opensro.online/server/internal/data/store", 12.5 ],
		[ "opensro.online/server/internal/config", 31.5 ]
	] );
	assert.deepEqual( soloVerdicts( offenders, solo, 30 ), [
		{ pkg: "opensro.online/server/internal/data/store", seconds: 85.2, solo: 12.5, fits: true },
		{ pkg: "opensro.online/server/internal/config", seconds: 31, solo: 31.5, fits: false },
		{ pkg: "opensro.online/server/internal/game/action", seconds: 135.4, solo: undefined, fits: false }
	] );
});

/*
===========================================================================

goTestBudget.mjs - the server gate's per-package test budget, as data

The decision the everyday test step's acceptance makes, kept pure so
a test can pin it (scripts/test/pipeline/goTestBudget.test.mjs): parse
the package runs out of `go test` output, join them with the CPU time
the -exec supervisor recorded per package (check_go_server.mjs
collects the files apps/server/tools/testcpu writes), and name the
packages over budget.

The budget is CPU seconds. A busy box inflates a package's wall
clock, not the CPU its tests burn, so contention alone no longer
rejects an all-green run (opensro#2582); a package whose tests
genuinely burn more than the budget still trips it on a quiet box.
A package with no CPU record (its supervisor wrote none) keeps the
wall-clock verdict, so a missing measurement can never wave a slow
package through.

===========================================================================
*/

/*
================
parseTestRuns

The packages `go test` ran, with the wall seconds it printed; a
cached package ("(cached)") cost nothing and is not timed (null).
Failure lines are go test's exit status's business, not the budget's.
================
*/
export function parseTestRuns( output ) {
	const runs = [];
	for ( const match of output.matchAll( /^ok\s+(\S+)\s+(?:([0-9.]+)s|\(cached\))/gm ) ) {
		runs.push( { pkg: match[1], wallSeconds: match[2] === undefined ? null : Number( match[2] ) } );
	}
	return runs;
}

/*
================
overBudgetPackages

Split the timed runs at the budget. A run's deciding seconds are its
recorded CPU when the supervisor reported one, else its wall clock.
over: every run past the budget, ledgered or not, so the ledger can
only shrink. offenders: the over-budget runs the ledger does not
name - a non-empty list fails the gate. forgiven: runs the wall
clock alone would have convicted but the CPU acquits - the busy-box
case, printed so a storm leaves evidence instead of a rejection.
================
*/
export function overBudgetPackages( runs, cpuSecondsByPackage, ledger, budgetSeconds ) {
	const over = [];
	const forgiven = [];
	for ( const run of runs ) {
		if ( run.wallSeconds === null ) {
			continue;
		}
		const cpuSeconds = cpuSecondsByPackage.get( run.pkg ) ?? null;
		const deciding = cpuSeconds ?? run.wallSeconds;
		if ( deciding > budgetSeconds ) {
			over.push( {
				pkg: run.pkg,
				wallSeconds: run.wallSeconds,
				cpuSeconds,
				listed: ledger.includes( run.pkg )
			} );
		} else if ( cpuSeconds !== null && run.wallSeconds > budgetSeconds ) {
			forgiven.push( { pkg: run.pkg, wallSeconds: run.wallSeconds, cpuSeconds } );
		}
	}
	return { over, forgiven, offenders: over.filter( ( entry ) => !entry.listed ) };
}

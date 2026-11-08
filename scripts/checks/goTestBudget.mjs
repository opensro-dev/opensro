/*
===========================================================================

goTestBudget.mjs - the Go test budget's arithmetic

Parsing `go test` output into per-package times and judging them against
the server gate's per-package budget (check_go_server.mjs). Pure text in,
verdicts out: the gate owns running go, this file owns what the numbers
mean, so the two are tested apart.

===========================================================================
*/

/*
================
packageTestTimes

Every package's test time in a `go test` run's output, as seconds. Only
"ok" lines with a trailing duration count: a cached package ("ok pkg
(cached)") spent nothing in this run, and a failed package's line starts
with FAIL, never ok.
================
*/
/**
 * @param {string} output
 * @returns {Map<string, number>}
 */
export function packageTestTimes( output ) {
	const times = new Map();
	for ( const match of output.matchAll( /^ok\s+(\S+)\s+([0-9.]+)s/gm ) ) {
		times.set( match[1], Number( match[2] ) );
	}
	return times;
}

/*
================
overBudgetPackages

Every package timed over the budget, in output order, ledgered or not:
the listing the gate prints so the ledger can only shrink.
================
*/
/**
 * @param {string} output
 * @param {number} budgetSeconds
 * @returns {{ pkg: string, seconds: number }[]}
 */
export function overBudgetPackages( output, budgetSeconds ) {
	const over = [];
	for ( const [pkg, seconds] of packageTestTimes( output ) ) {
		if ( seconds > budgetSeconds ) {
			over.push( { pkg, seconds } );
		}
	}
	return over;
}

/*
================
budgetOffenders

The packages a run must answer for: timed over the budget and not named
in the ledger. A ledgered package is the ledger's business (it may stay
over until its speed work lands), never this run's rejection.
================
*/
/**
 * @param {string} output
 * @param {number} budgetSeconds
 * @param {readonly string[]} ledger
 * @returns {{ pkg: string, seconds: number }[]}
 */
export function budgetOffenders( output, budgetSeconds, ledger ) {
	return overBudgetPackages( output, budgetSeconds ).filter( ( { pkg } ) => !ledger.includes( pkg ) );
}

/*
================
soloVerdicts

Judges offenders by their serial re-measure times (check_go_server.mjs
runs them alone after a busy full run): a package fits when it finished
inside the budget alone. No solo time means no verdict in its favour -
the package produced no timed ok line when run alone.
================
*/
/**
 * @param {{ pkg: string, seconds: number }[]} offenders
 * @param {Map<string, number>} soloTimes
 * @param {number} budgetSeconds
 * @returns {{ pkg: string, seconds: number, solo: number | undefined, fits: boolean }[]}
 */
export function soloVerdicts( offenders, soloTimes, budgetSeconds ) {
	return offenders.map( ( { pkg, seconds } ) => {
		const solo = soloTimes.get( pkg );
		return { pkg, seconds, solo, fits: solo !== undefined && solo <= budgetSeconds };
	} );
}

/*
================
isLintRunnerRefusal

Whether a golangci-lint run's output is its parallel-runner refusal
("parallel golangci-lint is running", exit 3): the tool found another
instance holding its cache lock and refused to start. That refusal is
shared-box contention, not a lint finding, so check_go_server.mjs waits
and retries it instead of failing the gate (#2971). The wording is
matched loosely (parallel + golangci-lint + running) so a version's
punctuation does not silently turn contention back into a failure.
================
*/
/**
 * @param {string} output
 * @returns {boolean}
 */
export function isLintRunnerRefusal( output ) {
	return /parallel golangci-lint is running/i.test( output );
}

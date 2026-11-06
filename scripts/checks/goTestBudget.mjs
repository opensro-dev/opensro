/*
===========================================================================

goTestBudget.mjs - confirm slow Go packages with process CPU accounting

The normal run keeps Go's input-aware test cache. Only unlisted packages
over the wall-time budget run again, uncached, through goTestCPU.go. A
recheck must pass and use no more than the budget in user + system CPU
seconds. Waiting for CPU or I/O does not consume this budget; Go's test
timeout still bounds hangs. CPU seconds sum work across the test process's
threads using os.ProcessState's platform accounting (Windows excludes
separately spawned subprocesses).

===========================================================================
*/

const CPU_MARKER = "SRO_TEST_CPU_SECONDS=";

/*
================
readCPUMeasurements

go test -json attributes wrapper output to its package, even when packages
run concurrently. Require one finite measurement and a package pass; a
missing timer, cached result or failed test is never budget evidence.
================
*/
export function readCPUMeasurements( output ) {
	const packages = new Map();
	for ( const line of output.split( /\r?\n/ ) ) {
		let event;
		try {
			event = JSON.parse( line );
		} catch {
			continue; // Go may also print toolchain diagnostics.
		}
		if ( !event?.Package ) continue;
		let measurement = packages.get( event.Package );
		if ( !measurement ) {
			measurement = { samples: [], passed: false, cached: false };
			packages.set( event.Package, measurement );
		}
		if ( event.Action === "output" && typeof event.Output === "string" ) {
			for ( const row of event.Output.split( /\r?\n/ ) ) {
				if ( row.startsWith( CPU_MARKER ) ) {
					const value = row.slice( CPU_MARKER.length ).trim();
					measurement.samples.push( value === "" ? NaN : Number( value ) );
				}
				if ( /^ok\s+\S+\s+\(cached\)/.test( row ) ) measurement.cached = true;
			}
		}
		if ( !event.Test && (event.Action === "pass" || event.Action === "fail") ) {
			measurement.passed = event.Action === "pass";
		}
	}
	return packages;
}

/*
================
goTestOutput

Keep failed rechecks readable in the gate's ordinary text report.
================
*/
export function goTestOutput( output ) {
	return output.split( /\r?\n/ ).map( ( line ) => {
		try {
			return JSON.parse( line ).Output ?? "";
		} catch {
			return line ? line + "\n" : "";
		}
	} ).join( "" );
}

/*
================
checkTestBudget

measure(packages) runs the real uncached CPU recheck. Injecting that one
boundary lets tests pin busy and quiet outcomes without timed sleeps or
loading the host. The shrinking ledger retains its existing exemptions.
================
*/
export async function checkTestBudget( output, { budgetSeconds, ledger, measure, log = console.log } ) {
	const pending = [];
	for ( const match of output.matchAll( /^ok\s+(\S+)\s+([0-9.]+)s/gm ) ) {
		const seconds = Number( match[2] );
		if ( seconds <= budgetSeconds ) continue;
		const pkg = match[1];
		const listed = ledger.includes( pkg );
		log( `test budget: ${pkg} took ${seconds}s wall (budget ${budgetSeconds}s)${listed ? ", in ledger" : ""}` );
		if ( !listed ) pending.push( pkg );
	}
	if ( pending.length === 0 ) return true;
	log( `test budget: rechecking ${pending.length} package(s) against ${budgetSeconds}s CPU (user + system)` );
	const result = await measure( pending );
	if ( !result.ok ) {
		log( "test budget: CPU recheck failed\n" + goTestOutput( result.output ) );
		return false;
	}
	const measurements = readCPUMeasurements( result.output );
	let accepted = true;
	for ( const pkg of pending ) {
		const measurement = measurements.get( pkg );
		const seconds = measurement?.samples[0];
		if (
			!measurement?.passed || measurement.cached || measurement.samples.length !== 1 ||
			!Number.isFinite( seconds ) || seconds < 0
		) {
			log( `test budget: ${pkg} has no valid uncached CPU measurement` );
			accepted = false;
			continue;
		}
		log( `test budget: ${pkg} used ${seconds}s CPU (budget ${budgetSeconds}s)` );
		if ( seconds > budgetSeconds ) accepted = false;
	}
	if ( !accepted ) {
		log(
			"test budget: make over-budget packages faster or move evidence sweeps to tier.Long (internal/testsupport/tier)"
		);
	}
	return accepted;
}

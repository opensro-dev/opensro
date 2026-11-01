/*
===========================================================================

failureReport.mjs - how a failed gate step reports itself

The Go server gate (check_go_server.mjs) runs its steps concurrently and
captures each one's output. This module owns what a failure looks like:
the step's label, how its command ended, the packages its output names
as failed, then the full output. It is separate from the gate so the
report can be unit-tested (scripts/test/pipeline/failureReport.test.mjs)
without running a gate.

===========================================================================
*/

/*
================
exitDescription

How a step's command ended, for the failure report: the signal that
killed it, its exit code, or - exit 0 - the step's own acceptance check
that rejected output the command was happy with (gofmt's file list, the
test budget, the release contract).
================
*/
export function exitDescription( result ) {
	if ( result.signal ) return `killed by ${result.signal}`;
	if ( result.code === null || result.code === undefined ) return "exit status unknown";
	if ( result.code === 0 ) return "exit 0, rejected by the step's acceptance check";
	return `exit ${result.code}`;
}

/*
================
failingPackages

The packages a step's output names as failed, in order, duplicates
removed: the "FAIL <pkg>" lines of a `go test` run, including the
"[build failed]" ones. Only spaces and tabs may follow FAIL: `go test`
also prints a bare FAIL line (the failing binary's own last line, and
the run's summary), and a \s+ there would swallow the newline and take
the next line's first word for a package, dropping the real name.
Empty for non-test steps, and for the killed-command failure: `go test`
prints ok as each package completes, so a command killed mid-run leaves
an all-ok output and no name.
================
*/
export function failingPackages( output ) {
	const seen = new Set();
	for ( const match of output.matchAll( /^FAIL[ \t]+(\S+)/gm ) ) {
		seen.add( match[1] );
	}
	return [ ...seen ];
}

/*
================
failureSummary

One failed step in one line, for the thrown error: the label, how the
command ended and its failing packages, so a log that keeps only the
error still names them.
================
*/
export function failureSummary( result ) {
	const packages = failingPackages( result.output );
	const named = packages.length > 0 ? `: ${packages.join( ", " )}` : "";
	return `${result.label} failed (${exitDescription( result )}${named})`;
}

/*
================
runsGoTest

Whether a step's command is `go test` (the tests and race steps), the only
kind whose output names failing packages. A failed lint or vet step names
files and lines instead, so its missing package is no sign that the
command itself broke.
================
*/
export function runsGoTest( label ) {
	return label.startsWith( "tests" ) || label === "race";
}

/*
================
failureReport

One failed step's printed block: the summary line, the failing packages
(or, for a go test step, the note that the output names none), then the
full captured output.
================
*/
export function failureReport( result ) {
	const lines = [ `--- ${failureSummary( result )} ---` ];
	if ( runsGoTest( result.label ) && failingPackages( result.output ).length === 0 && result.code !== 0 ) {
		lines.push(
			"the output names no failing package: the command itself failed " +
				"(killed, or a toolchain-level error) before reporting one"
		);
	}
	lines.push( result.output );
	return lines.join( "\n" );
}

/*
================
printFailureReports

Write every failed step's report to stream, resolving once the stream has
taken all of it. The gate throws next. On Linux a write to a pipe is
asynchronous, and a process that throws before the write drains exits
with the report cut at the pipe buffer (about 128 KiB on the CI runner's
Node): the go test tail, which carries the FAIL lines, never arrives.
================
*/
export function printFailureReports( failed, stream = process.stderr ) {
	const text = failed.map( ( result ) => `\n${failureReport( result )}\n` ).join( "" );
	return new Promise( ( resolve, reject ) => {
		stream.write( text, ( error ) => error ? reject( error ) : resolve() );
	} );
}

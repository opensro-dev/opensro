/*
===========================================================================

release-probe.mjs - what every release probe of a staged candidate shares

The release gate (release-gate.mjs) and the rendered smoke
(release-smoke.mjs) take the same inputs, identify the same candidate entry,
record phases in the shape the host keeps as browser evidence
(ops/release/client_deploy.record_smoke), and treat the same browser output
as a runtime error. Each owns only its own flow.

===========================================================================
*/
import { createHash } from "node:crypto";
import { mkdir, readFile } from "node:fs/promises";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";

export const HTTP_OK = 200;
export const PROBE_VIEWPORT = { width: 1024, height: 768 };
// The runtime's own error reports (platform.report) carry this prefix.
const RUNTIME_ERROR_MARK = "[SRO runtime]";

/*
================
readProbeInputs

CANDIDATE_JSON and OUTPUT_DIRECTORY from argv, the dedicated probe account
from RELEASE_PROBE_ACCOUNT and the host from RELEASE_ORIGIN. The output
directory exists on return.
================
*/
export async function readProbeInputs( tool ) {
	const [candidatePath, destination] = process.argv.slice( 2 );
	if ( !candidatePath || !destination ) throw Error( `Usage: ${tool} CANDIDATE_JSON OUTPUT_DIRECTORY` );
	const candidate = JSON.parse( await readFile( candidatePath, "utf8" ) );
	const credentials = JSON.parse( process.env.RELEASE_PROBE_ACCOUNT ?? "{}" );
	assertCharacterAllowed( credentials.character );
	if ( !credentials.username || !credentials.password || !credentials.character || !credentials.shard ) {
		throw Error( "Missing release probe account" );
	}
	if ( !process.env.RELEASE_ORIGIN ) throw Error( "Missing RELEASE_ORIGIN for the host under test" );
	const origin = new URL( process.env.RELEASE_ORIGIN ).origin;
	await mkdir( destination, { recursive: true } );
	return {
		candidate,
		credentials,
		origin,
		destination,
		entryUrl: `${origin}/releases/candidates/${candidate.candidate}/index.html`
	};
}

/*
================
assertCandidateEntry

The entry served over HTTPS must be the approved bytes.
================
*/
export function assertCandidateEntry( bytes, candidate ) {
	if ( createHash( "sha256" ).update( bytes ).digest( "hex" ) !== candidate.entrySha256 ) {
		throw Error( "HTTPS served a different candidate entry" );
	}
}

/*
================
createProbeResult

The report every probe writes, before any phase has run.
================
*/
export function createProbeResult( candidate, fields = {} ) {
	return {
		...candidate,
		...fields,
		verdict: "FAIL",
		phases: {},
		phaseTimings: [],
		errors: [],
		startedAt: Date.now()
	};
}

/*
================
recordPhase

Mark a phase passed with its time since the start and since the previous
phase, and print it so a CI log shows progress before the report exists.
================
*/
export function recordPhase( result, phase ) {
	const elapsedMs = Date.now() - result.startedAt;
	const previousMs = result.phaseTimings.at( -1 )?.elapsedMs ?? 0;
	result.phases[phase] = "PASS";
	const timing = { phase, elapsedMs, durationMs: elapsedMs - previousMs };
	result.phaseTimings.push( timing );
	console.log( "Release phase:", JSON.stringify( timing ) );
}

/*
================
watchRuntimeErrors

Collect uncaught page errors and the runtime's own error reports.
================
*/
export function watchRuntimeErrors( page, errors ) {
	page.on( "pageerror", error => errors.push( String( error ) ) );
	page.on( "console", message => {
		if ( message.type() === "error" && message.text().includes( RUNTIME_ERROR_MARK ) ) {
			errors.push( message.text() );
		}
	} );
}

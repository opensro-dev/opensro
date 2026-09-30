/*
===========================================================================

release-probe.test.mjs - tests for tools/beta/release-probe.mjs

The inputs, entry identity and phase evidence every release probe shares.

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import {
	assertCandidateEntry,
	createProbeResult,
	readProbeInputs,
	recordPhase
} from "../../tools/beta/release-probe.mjs";

const ACCOUNT = { username: "probe", password: "secret", character: "asd2", shard: "global-official" };

/*
================
withInputs

Run readProbeInputs against a candidate file, argv and environment, restoring
the process state afterwards.
================
*/
async function withInputs( t, env, run ) {
	const directory = await mkdtemp( path.join( tmpdir(), "release-probe-" ) );
	t.after( () => rm( directory, { recursive: true, force: true } ) );
	const candidatePath = path.join( directory, "candidate.json" );
	await writeFile( candidatePath, JSON.stringify( { candidate: "abc", entrySha256: "00" } ) );
	const argv = process.argv, saved = { ...process.env };
	process.argv = [ argv[0], "tool", candidatePath, path.join( directory, "out" ) ];
	Object.assign( process.env, env );
	for ( const key of [ "RELEASE_PROBE_ACCOUNT", "RELEASE_ORIGIN" ] ) if ( !(key in env) ) delete process.env[key];
	try {
		return await run( directory );
	} finally {
		process.argv = argv;
		process.env = saved;
	}
}

test("probe inputs name the candidate entry under the host origin and create the output directory", async t => {
	await withInputs(
		t,
		{ RELEASE_PROBE_ACCOUNT: JSON.stringify( ACCOUNT ), RELEASE_ORIGIN: "https://opensro.example/ignored/path" },
		async directory => {
			const inputs = await readProbeInputs( "tool" );
			assert.equal( inputs.origin, "https://opensro.example" );
			assert.equal( inputs.entryUrl, "https://opensro.example/releases/candidates/abc/index.html" );
			assert.deepEqual( inputs.credentials, ACCOUNT );
			assert.ok( (await readdir( directory )).includes( "out" ) );
		}
	);
});

test("probe inputs refuse an incomplete account or a missing origin", async t => {
	await withInputs( t, {
		RELEASE_PROBE_ACCOUNT: JSON.stringify( { ...ACCOUNT, shard: "" } ),
		RELEASE_ORIGIN: "https://a.example"
	}, () => assert.rejects( readProbeInputs( "tool" ), /Missing release probe account/ ) );
	await withInputs(
		t,
		{ RELEASE_PROBE_ACCOUNT: JSON.stringify( ACCOUNT ) },
		() => assert.rejects( readProbeInputs( "tool" ), /Missing RELEASE_ORIGIN/ )
	);
});

test("the served entry must hash to the approved candidate", () => {
	const bytes = Buffer.from( "<html>entry</html>" );
	const candidate = { entrySha256: createHash( "sha256" ).update( bytes ).digest( "hex" ) };
	assertCandidateEntry( bytes, candidate );
	assert.throws(
		() => assertCandidateEntry( Buffer.from( "<html>other</html>" ), candidate ),
		/different candidate/
	);
});

test("phases record their time since the start and since the previous phase", t => {
	t.mock.method( console, "log", () => {} );
	const result = createProbeResult( { candidate: "abc" }, { gate: "v1" } );
	assert.equal( result.verdict, "FAIL" );
	assert.equal( result.gate, "v1" );
	result.startedAt = Date.now() - 100;
	recordPhase( result, "login" );
	recordPhase( result, "roster" );
	assert.deepEqual( result.phases, { login: "PASS", roster: "PASS" } );
	const [login, roster] = result.phaseTimings;
	assert.ok( login.elapsedMs >= 100 && login.durationMs === login.elapsedMs );
	assert.equal( roster.durationMs, roster.elapsedMs - login.elapsedMs );
});

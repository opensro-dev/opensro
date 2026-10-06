/*
===========================================================================

checkStamps.test.mjs - a gate's stamp hashes the generated files it reads

A worktree reads the shared build through SRO_GENERATED_ROOT and the server
projection through SRO_SERVER_GAME_DATA_ROOT. An `!` input named by its
checkout path must follow those overrides, or a gate would be skipped after
the shared assets changed.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const repository = fileURLToPath( new URL( "../../../", import.meta.url ) );

// Fingerprints one `!` input while its redirected file is missing, then
// holds "a", then "b"; prints the three digests.
const DRIVER = `
import { writeFileSync } from "node:fs";
const { fingerprintOf } = await import( "./scripts/checks/checkStamps.mjs" );
const [ input, file ] = process.argv.slice( 1 );
const digest = () => fingerprintOf( [], () => true, [ input ] );
const digests = [ digest() ];
writeFileSync( file, "a" );
digests.push( digest() );
writeFileSync( file, "b" );
digests.push( digest() );
console.log( JSON.stringify( digests ) );
`;

/*
================
digestsOf
================
*/
function digestsOf( input, file, env ) {
	const result = spawnSync( process.execPath, [ "--input-type=module", "-e", DRIVER, input, file ], {
		cwd: repository,
		env: { ...process.env, SRO_GENERATED_ROOT: "", SRO_SERVER_GAME_DATA_ROOT: "", ...env },
		encoding: "utf8",
		windowsHide: true
	} );
	assert.equal( result.status, 0, result.stderr );
	return JSON.parse( result.stdout );
}

test("an ! input under .generated follows SRO_GENERATED_ROOT and the server projection its own override", t => {
	const shared = mkdtempSync( path.join( os.tmpdir(), "check-stamps-" ) );
	t.after( () => rmSync( shared, { recursive: true, force: true } ) );
	mkdirSync( path.join( shared, "generated", "client-public" ), { recursive: true } );
	mkdirSync( path.join( shared, "server" ) );

	const client = digestsOf(
		"!.generated/client-public/stamp-probe.json",
		path.join( shared, "generated", "client-public", "stamp-probe.json" ),
		{ SRO_GENERATED_ROOT: path.join( shared, "generated" ) }
	);
	assert.equal( new Set( client ).size, 3, "missing, a and b must hash apart" );

	const server = digestsOf(
		"!apps/server/.generated/game-data/1.150/server/stamp-probe.json",
		path.join( shared, "server", "stamp-probe.json" ),
		{ SRO_SERVER_GAME_DATA_ROOT: path.join( shared, "server" ) }
	);
	assert.equal( new Set( server ).size, 3, "missing, a and b must hash apart" );
});

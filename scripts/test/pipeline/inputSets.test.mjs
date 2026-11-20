/*
===========================================================================

inputSets.test.mjs - the named input sets gates and release steps key on

The checked-in sets are what the client check and the Go server gate read,
so their names must resolve, and a missing or misformatted set must refuse
rather than fingerprint nothing (a gate keyed on nothing would always be
"up to date"). run_if_changed with no listed inputs reads the set of its
own name.

===========================================================================
*/

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { INPUT_SETS_FORMAT, inputSet } from "../../lib/inputSets.mjs";

const repository = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );
const wrapper = path.join( repository, "scripts", "checks", "run_if_changed.mjs" );

test("the gates' sets resolve to what they read", () => {
	assert.ok( inputSet( "client" ).includes( "apps/client-next" ) );
	assert.ok( inputSet( "server" ).includes( "apps/server" ) );
	assert.ok( inputSet( "server" ).includes( "@go,version" ) );
});

test("an unknown set or another format is refused", t => {
	const scratch = mkdtempSync( path.join( os.tmpdir(), "input-sets-" ) );
	t.after( () => rmSync( scratch, { recursive: true, force: true } ) );
	const file = path.join( scratch, "sets.json" );
	writeFileSync( file, JSON.stringify( { format: INPUT_SETS_FORMAT, sets: { client: [ "a" ], empty: [] } } ) );
	assert.deepEqual( inputSet( "client", file ), [ "a" ] );
	assert.throws( () => inputSet( "missing", file ), /names no input set "missing"/ );
	assert.throws( () => inputSet( "empty", file ), /names no input set "empty"/ );
	writeFileSync( file, JSON.stringify( { format: "other", sets: { client: [ "a" ] } } ) );
	assert.throws( () => inputSet( "client", file ), /is not a opensro-input-sets-v1 document/ );
});

test("run_if_changed with no listed inputs needs a set of its name", () => {
	const name = `input-sets-test-${process.pid}`;
	const result = spawnSync( process.execPath, [ wrapper, name, "--", process.execPath, "-e", "" ], {
		cwd: repository,
		encoding: "utf8",
		env: { ...process.env, SRO_CHECK_FORCE: "1" }
	} );
	assert.notEqual( result.status, 0 );
	assert.match( result.stderr, new RegExp( `names no input set "${name}"` ) );
});

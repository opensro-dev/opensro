/*
===========================================================================

laterOptionalTeleportStep.test.mjs - the build's reverse return table step

The reverse return map's destinations (SRO_REVERSE_RETURN_MAP) are the
later client's refoptionalteleport.txt; without its import the server
infers points from recall gates and unique nests. The step's inputs are
SRO_VSRO_ROOT and the archive's presence, as the map buttons beside it.
These tests pin the plan for each input and the skip line, without the
archive: the run itself stays the importer's, behind its pin.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { laterOptionalTeleportImportPlan, RESOURCE_BUILD_STEPS } from "../../build/resourceBuild.mjs";

test("no archive configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_VSRO_ROOT: "" }, { SRO_VSRO_ROOT: "  " } ] ) {
		const plan = laterOptionalTeleportImportPlan( env, true );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later reverse return table: skipped \(SRO_VSRO_ROOT is not set/ );
	}
});

test("a configured folder without the archive: the plan names it and skips", () => {
	const plan = laterOptionalTeleportImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false );
	assert.equal( plan.run, false );
	assert.match( plan.line, /no Media\.pk2 under SRO_VSRO_ROOT D:\/later-client/ );
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterOptionalTeleportImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "D:/later-client" );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_optional_teleport.py", "--vsro-root", "D:/later-client" ] );
});

test("the step skips without the archive and stays one printed line", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-optional-teleport-step-" ) );
	const saved = process.env.SRO_VSRO_ROOT;
	const printed = [];
	const log = console.log;
	t.after( () => {
		console.log = log;
		if ( saved === undefined ) delete process.env.SRO_VSRO_ROOT;
		else process.env.SRO_VSRO_ROOT = saved;
		fs.rmSync( empty, { recursive: true, force: true } );
	} );
	console.log = ( ...args ) => printed.push( args.join( " " ) );
	process.env.SRO_VSRO_ROOT = empty;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterOptionalTeleport(), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /no Media\.pk2 under SRO_VSRO_ROOT/ );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterOptionalTeleport(), { skipped: true } );
	assert.equal( printed.length, 2 );
	assert.match( printed[1], /SRO_VSRO_ROOT is not set/ );
});

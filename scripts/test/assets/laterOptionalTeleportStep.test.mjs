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
		const plan = laterOptionalTeleportImportPlan( env, true, "" );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later reverse return table: skipped \(SRO_VSRO_ROOT is not set/ );
	}
});

test("a configured folder without the archive fails the build, naming it", () => {
	assert.throws(
		() => laterOptionalTeleportImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false, "" ),
		/^Error: Later reverse return table: SRO_VSRO_ROOT names the later client D:\/later-client, but it holds no Media\.pk2/
	);
});

test("the variant stamp's later client stands in for an unset SRO_VSRO_ROOT", () => {
	const plan = laterOptionalTeleportImportPlan( {}, true, "E:/stamped-client" );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "E:/stamped-client" );
	assert.throws(
		() => laterOptionalTeleportImportPlan( {}, false, "E:/stamped-client" ),
		/the variant stamp names the later client/
	);
	// The shell's root wins over the stamp's.
	assert.equal(
		laterOptionalTeleportImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true, "E:/stamped-client" ).root,
		"D:/later-client"
	);
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterOptionalTeleportImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "D:/later-client" );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_optional_teleport.py", "--vsro-root", "D:/later-client" ] );
});

test("the step fails on a named folder without the archive, and skips in one line with none", async ( t ) => {
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
	await assert.rejects( RESOURCE_BUILD_STEPS.importLaterOptionalTeleport( "" ), /holds no Media\.pk2/ );
	assert.equal( printed.length, 0 );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterOptionalTeleport( "" ), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /SRO_VSRO_ROOT is not set and no variant stamp names a later client/ );
});

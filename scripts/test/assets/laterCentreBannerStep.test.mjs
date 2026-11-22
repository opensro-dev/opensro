/*
===========================================================================

laterCentreBannerStep.test.mjs - the build's later-client centre banner step

The centre banner's plate art (#3260): SRO_V1696_ROOT and the archive's
presence decide whether the importer runs; the public paths are the three
com_center PNGs. These tests pin the plan and the skip line without
the archive.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
	laterCentreBannerImportPlan,
	laterCentreBannerPublicPaths,
	RESOURCE_BUILD_STEPS
} from "../../build/resourceBuild.mjs";

test("no v1.696 client configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_V1696_ROOT: "" }, { SRO_V1696_ROOT: "  " } ] ) {
		const plan = laterCentreBannerImportPlan( env, true );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later centre banner: skipped \(SRO_V1696_ROOT is not set/ );
	}
});

test("a configured folder without the archive: the plan names it and skips", () => {
	const plan = laterCentreBannerImportPlan( { SRO_V1696_ROOT: "D:/v1696" }, false );
	assert.equal( plan.run, false );
	assert.match( plan.line, /no Media\.pk2 under SRO_V1696_ROOT D:\/v1696/ );
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterCentreBannerImportPlan( { SRO_V1696_ROOT: "D:/v1696" }, true );
	assert.equal( plan.run, true );
	assert.deepEqual( plan.args, [ "scripts/import_v1696_centre_banner.py", "--v1696-root", "D:/v1696" ] );
});

test("the public paths are the centre plate's three pieces", () => {
	assert.deepEqual( laterCentreBannerPublicPaths(), [
		"/assets/images/Media_extracted/interface/ifcommon/com_center_corner.png",
		"/assets/images/Media_extracted/interface/ifcommon/com_center_edge.png",
		"/assets/images/Media_extracted/interface/ifcommon/com_center_edge2.png"
	] );
});

test("the step skips without the archive and stays one printed line", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-centre-banner-step-" ) );
	const saved = process.env.SRO_V1696_ROOT;
	const printed = [];
	const log = console.log;
	t.after( () => {
		console.log = log;
		if ( saved === undefined ) delete process.env.SRO_V1696_ROOT;
		else process.env.SRO_V1696_ROOT = saved;
		fs.rmSync( empty, { recursive: true, force: true } );
	} );
	console.log = ( ...args ) => printed.push( args.join( " " ) );
	process.env.SRO_V1696_ROOT = empty;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterCentreBanner(), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /no Media\.pk2 under SRO_V1696_ROOT/ );
});

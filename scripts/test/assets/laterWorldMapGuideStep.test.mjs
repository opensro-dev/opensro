/*
===========================================================================

laterWorldMapGuideStep.test.mjs - the build's later-client map guide step

The step's inputs are SRO_V1696_ROOT and the archive's presence; its
outputs are the guide catalogue, its art and the hunting-ground masks
(issue #3387). These tests pin the plan for each input and the public
paths one import publishes, without the archive: the run itself stays
the importer's, behind its pin.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
	laterWorldMapGuideImportPlan,
	laterWorldMapGuidePublicPaths,
	RESOURCE_BUILD_STEPS
} from "../../build/resourceBuild.mjs";

test("no v1.696 client configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_V1696_ROOT: "" }, { SRO_V1696_ROOT: "  " } ] ) {
		const plan = laterWorldMapGuideImportPlan( env, true );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later world map guide: skipped \(SRO_V1696_ROOT is not set/ );
	}
});

test("a configured folder without the archive: the plan names it and skips", () => {
	const plan = laterWorldMapGuideImportPlan( { SRO_V1696_ROOT: "D:/v1696" }, false );
	assert.equal( plan.run, false );
	assert.match( plan.line, /no Media\.pk2 under SRO_V1696_ROOT D:\/v1696/ );
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterWorldMapGuideImportPlan( { SRO_V1696_ROOT: "D:/v1696" }, true );
	assert.equal( plan.run, true );
	assert.deepEqual( plan.args, [ "scripts/import_v1696_worldmap_guide.py", "--v1696-root", "D:/v1696" ] );
});

test("the public paths are the catalogue, its art and every tile mask it names", () => {
	const paths = laterWorldMapGuidePublicPaths( {
		imageRoot: "/i/",
		art: [ "worldmap/wmap_monster_icon" ],
		monsters: [ { tiles: [ [ 1, 2, "worldmap/guide-mask/m0_1x2" ] ] }, { tiles: [] }, {} ]
	} );
	assert.deepEqual( paths, [
		"/assets/data/worldmap-guide.json",
		"/i/worldmap/wmap_monster_icon.png",
		"/i/worldmap/guide-mask/m0_1x2.png"
	] );
	assert.deepEqual( laterWorldMapGuidePublicPaths( null ), [ "/assets/data/worldmap-guide.json" ] );
});

test("the step skips without the archive and stays one printed line", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-worldmap-guide-step-" ) );
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
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterWorldMapGuide(), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /no Media\.pk2 under SRO_V1696_ROOT/ );
});

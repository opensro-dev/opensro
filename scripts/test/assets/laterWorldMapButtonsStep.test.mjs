/*
===========================================================================

laterWorldMapButtonsStep.test.mjs - the build's later-client map button step

The step's inputs are one environment variable and the archive's
presence, as the dungeon maps beside it; its outputs are the globe,
the follow pin's faces and the floor selector the dungeon pages draw
(issue #3210). These tests pin the plan for each input and the public
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
	laterWorldMapButtonPublicPaths,
	laterWorldMapButtonsImportPlan,
	RESOURCE_BUILD_STEPS
} from "../../build/resourceBuild.mjs";

test("no archive configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_VSRO_ROOT: "" }, { SRO_VSRO_ROOT: "  " } ] ) {
		const plan = laterWorldMapButtonsImportPlan( env, true );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later world map buttons: skipped \(SRO_VSRO_ROOT is not set/ );
	}
});

test("a configured folder without the archive: the plan names it and skips", () => {
	const plan = laterWorldMapButtonsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false );
	assert.equal( plan.run, false );
	assert.match( plan.line, /no Media\.pk2 under SRO_VSRO_ROOT D:\/later-client/ );
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterWorldMapButtonsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "D:/later-client" );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_worldmap_buttons.py", "--vsro-root", "D:/later-client" ] );
});

test("the step skips without the archive and stays one printed line", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-worldmap-buttons-step-" ) );
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
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterWorldMapButtons(), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /no Media\.pk2 under SRO_VSRO_ROOT/ );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterWorldMapButtons(), { skipped: true } );
	assert.equal( printed.length, 2 );
	assert.match( printed[1], /SRO_VSRO_ROOT is not set/ );
});

test("one import publishes exactly the button set", () => {
	assert.deepEqual( laterWorldMapButtonPublicPaths(), [
		"/assets/images/Media_extracted/interface/worldmap/wmap_big_button_world.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_button_automatic.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_button_passivity.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_up_button.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_up_button_disable.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_down_button.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_down_button_disable.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_floor_box.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_floor_choice_box.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_floor_big_box.png"
	] );
});

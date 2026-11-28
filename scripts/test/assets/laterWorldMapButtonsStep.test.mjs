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
		const plan = laterWorldMapButtonsImportPlan( env, true, "" );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later world map buttons: skipped \(SRO_VSRO_ROOT is not set/ );
	}
});

test("a configured folder without the archive fails the build, naming it", () => {
	assert.throws(
		() => laterWorldMapButtonsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false, "" ),
		/^Error: Later world map buttons: SRO_VSRO_ROOT names the later client D:\/later-client, but it holds no Media\.pk2/
	);
});

test("the variant stamp's later client stands in for an unset SRO_VSRO_ROOT", () => {
	const plan = laterWorldMapButtonsImportPlan( {}, true, "E:/stamped-client" );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "E:/stamped-client" );
	assert.throws(
		() => laterWorldMapButtonsImportPlan( {}, false, "E:/stamped-client" ),
		/the variant stamp names the later client/
	);
	// The shell's root wins over the stamp's.
	assert.equal(
		laterWorldMapButtonsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true, "E:/stamped-client" ).root,
		"D:/later-client"
	);
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterWorldMapButtonsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "D:/later-client" );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_worldmap_buttons.py", "--vsro-root", "D:/later-client" ] );
});

test("the step fails on a named folder without the archive, and skips in one line with none", async ( t ) => {
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
	await assert.rejects( RESOURCE_BUILD_STEPS.importLaterWorldMapButtons( "" ), /holds no Media\.pk2/ );
	assert.equal( printed.length, 0 );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterWorldMapButtons( "" ), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /SRO_VSRO_ROOT is not set and no variant stamp names a later client/ );
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
		"/assets/images/Media_extracted/interface/worldmap/wmap_floor_big_box.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_wnd_bottom_frame.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_wnd_corner_02.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_window_edge_1.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_window_edge_2.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_window_edge_3.png",
		"/assets/images/Media_extracted/interface/worldmap/wmap_window_edge_4.png",
		"/assets/images/Media_extracted/interface/guide/gd_area_tab_on.png",
		"/assets/images/Media_extracted/interface/guide/gd_area_tab_off.png",
		"/assets/images/Media_extracted/interface/guide/gd_monster_tab_on.png",
		"/assets/images/Media_extracted/interface/guide/gd_monster_tab_off.png",
		"/assets/images/Media_extracted/interface/guide/gd_long_index_open.png",
		"/assets/images/Media_extracted/interface/guide/gd_long_contents_event.png",
		"/assets/images/Media_extracted/interface/worldmap/map/xy_returnicon.png",
		"/assets/images/Media_extracted/interface/worldmap/map/xy_returnicon_focus.png",
		"/assets/images/Media_extracted/interface/worldmap/map/xy_returnicon_press.png",
		"/assets/images/Media_extracted/interface/worldmap/map/map_return.png",
		"/assets/images/Media_extracted/interface/worldmap/map/map_return_focus.png",
		"/assets/images/Media_extracted/interface/worldmap/map/map_return_press.png"
	] );
});

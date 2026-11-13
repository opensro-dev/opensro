/*
===========================================================================

laterTombMinimapStep.test.mjs - the build's Ch'in Tomb minimap step

The v1.150 archives ship floor minimap art for Donwhang Stone Cave alone;
the tomb's floors come from the pinned later client (minimap_d/jinsi,
import_vsro_tomb_minimap.py). The step's inputs are SRO_VSRO_ROOT and the
archive's presence, as the dungeon maps beside it. These tests pin the plan
for each input and the public paths one catalogue publishes, without the
archive: the run itself stays the importer's, behind its pin.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
	laterTombMinimapImportPlan,
	laterTombMinimapPublicPaths,
	RESOURCE_BUILD_STEPS
} from "../../build/resourceBuild.mjs";

test("no archive configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_VSRO_ROOT: "" }, { SRO_VSRO_ROOT: "  " } ] ) {
		const plan = laterTombMinimapImportPlan( env, true );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later tomb minimap: skipped \(SRO_VSRO_ROOT is not set/ );
	}
});

test("a configured folder without the archive: the plan names it and skips", () => {
	const plan = laterTombMinimapImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false );
	assert.equal( plan.run, false );
	assert.match( plan.line, /no Media\.pk2 under SRO_VSRO_ROOT D:\/later-client/ );
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterTombMinimapImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "D:/later-client" );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_tomb_minimap.py", "--vsro-root", "D:/later-client" ] );
});

test("the step skips without the archive and stays one printed line", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-tomb-minimap-step-" ) );
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
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterTombMinimap(), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /no Media\.pk2 under SRO_VSRO_ROOT/ );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterTombMinimap(), { skipped: true } );
	assert.equal( printed.length, 2 );
	assert.match( printed[1], /SRO_VSRO_ROOT is not set/ );
});

test("one catalogue publishes exactly itself and its tiles", () => {
	const tiles = [
		"/assets/images/Media_extracted/minimap_d/jinsi/qt_a01_floor01_127x128.png",
		"/assets/images/Media_extracted/minimap_d/jinsi/qt_a01_floor01_128x128.png"
	];
	assert.deepEqual( laterTombMinimapPublicPaths( { tilePaths: tiles } ), [
		"/assets/data/later-dungeon-minimap.json",
		...tiles
	] );
	// A catalogue without tiles claims only itself; a stray entry is skipped.
	assert.deepEqual( laterTombMinimapPublicPaths( { tilePaths: [ 7, "" ] } ), [
		"/assets/data/later-dungeon-minimap.json"
	] );
	assert.deepEqual( laterTombMinimapPublicPaths( null ), [ "/assets/data/later-dungeon-minimap.json" ] );
});

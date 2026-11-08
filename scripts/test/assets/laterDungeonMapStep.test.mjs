/*
===========================================================================

laterDungeonMapStep.test.mjs - the build's later-client dungeon map step

The step's inputs are one environment variable and the archive's
presence; its outputs are the catalogue and images the M window asks
for (#2661, #2956). These tests pin the plan for each input and the
public paths one catalogue publishes, without the archive: the run
itself stays the importer's, behind its pin.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
	laterDungeonMapImportPlan,
	laterDungeonMapPublicPaths,
	RESOURCE_BUILD_STEPS
} from "../../build/resourceBuild.mjs";
import { buildKnobLines, ENV_KNOBS } from "../../build/shared/resourceBuildFingerprint.mjs";

test("no archive configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_VSRO_ROOT: "" }, { SRO_VSRO_ROOT: "  " } ] ) {
		const plan = laterDungeonMapImportPlan( env, true );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later dungeon maps: skipped \(SRO_VSRO_ROOT is not set/ );
	}
});

test("a configured folder without the archive: the plan names it and skips", () => {
	const plan = laterDungeonMapImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false );
	assert.equal( plan.run, false );
	assert.match( plan.line, /no Media\.pk2 under SRO_VSRO_ROOT D:\/later-client/ );
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterDungeonMapImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.equal( plan.root, "D:/later-client" );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_dungeon_maps.py", "--vsro-root", "D:/later-client" ] );
});

test("the step skips without the archive and stays one printed line", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-dungeon-step-" ) );
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
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterDungeonMaps(), { skipped: true } );
	assert.equal( printed.length, 1 );
	assert.match( printed[0], /no Media\.pk2 under SRO_VSRO_ROOT/ );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterDungeonMaps(), { skipped: true } );
	assert.equal( printed.length, 2 );
	assert.match( printed[1], /SRO_VSRO_ROOT is not set/ );
});

test("one catalogue publishes exactly its pages and its sign icon", () => {
	const catalog = {
		imageRoot: "/assets/images/Media_extracted/interface/worldmap/dungeon/",
		signIcon: "map_world_iconwf",
		maps: [ { image: "qt_a01" }, { image: "dw_b02" }, { image: "" }, {} ]
	};
	assert.deepEqual( laterDungeonMapPublicPaths( catalog ), [
		"/assets/data/later-dungeon-map.json",
		"/assets/images/Media_extracted/interface/worldmap/dungeon/qt_a01.png",
		"/assets/images/Media_extracted/interface/worldmap/dungeon/dw_b02.png",
		"/assets/images/Media_extracted/interface/worldmap/dungeon/map_world_iconwf.png"
	] );
	assert.deepEqual( laterDungeonMapPublicPaths( null ), [ "/assets/data/later-dungeon-map.json" ] );
});

test("the archive's folder is a fingerprint knob, so the skip cannot hide it", () => {
	assert.ok( ENV_KNOBS.includes( "SRO_VSRO_ROOT" ) );
	const saved = process.env.SRO_VSRO_ROOT;
	try {
		process.env.SRO_VSRO_ROOT = "D:/later-client";
		assert.ok( buildKnobLines().includes( "env SRO_VSRO_ROOT=D:/later-client" ) );
	} finally {
		if ( saved === undefined ) delete process.env.SRO_VSRO_ROOT;
		else process.env.SRO_VSRO_ROOT = saved;
	}
});

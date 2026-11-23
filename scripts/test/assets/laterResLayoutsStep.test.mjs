/*
===========================================================================

laterResLayoutsStep.test.mjs - the build's quest window layout step (#4245)

The step's input is the pinned later client (SRO_VSRO_ROOT or the variant
stamp), as the world map buttons beside it; what it claims is what the
importer's index records. These tests pin both without the archive.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { assertPublishedFiles } from "../../build/laterClientImports.mjs";
import { laterResLayoutPublicPaths, laterResLayoutsImportPlan } from "../../build/resourceBuild.mjs";

test("no archive configured: the plan is the one skip line", () => {
	const plan = laterResLayoutsImportPlan( {}, true, "" );
	assert.equal( plan.run, false );
	assert.match( plan.line, /^Later quest window layouts: skipped \(SRO_VSRO_ROOT is not set/ );
});

test("a configured archive runs the importer against its folder", () => {
	const plan = laterResLayoutsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true, "" );
	assert.equal( plan.run, true );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_res_layouts.py", "--vsro-root", "D:/later-client" ] );
	assert.throws(
		() => laterResLayoutsImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false, "" ),
		/^Error: Later quest window layouts: SRO_VSRO_ROOT names the later client D:\/later-client, but it holds no Media\.pk2/
	);
});

test("the claim is the index and what it records", () => {
	const index = {
		layouts: [ "questlist", "questminilist" ],
		published: [
			"/assets/data/res-ui/questlist.json",
			"/assets/images/Media_extracted/interface/quest/qst_sub_window.png"
		]
	};
	assert.deepEqual( laterResLayoutPublicPaths( index ), [
		"/assets/data/res-ui/index.json",
		"/assets/data/res-ui/questlist.json",
		"/assets/images/Media_extracted/interface/quest/qst_sub_window.png"
	] );
	for ( const broken of [ null, {}, { layouts: [] }, { published: [] } ] ) {
		assert.throws( () => laterResLayoutPublicPaths( broken ), /wrote no usable index/ );
	}
});

test("a step never claims a path its importer did not write", async () => {
	const root = await mkdtemp( path.join( os.tmpdir(), "res-ui-claim-" ) );
	try {
		const written = "/assets/data/res-ui/questlist.json";
		await mkdir( path.join( root, path.dirname( written ) ), { recursive: true } );
		await writeFile( path.join( root, written ), "{}" );
		await assertPublishedFiles( root, [ written ], "quest layout" );
		await assert.rejects(
			assertPublishedFiles( root, [ written, "/assets/data/res-ui/questminilist.json" ], "quest layout" ),
			/^Error: The quest layout import published no \/assets\/data\/res-ui\/questminilist\.json$/
		);
		// A directory at the path is not a published file.
		await assert.rejects(
			assertPublishedFiles( root, [ "/assets/data/res-ui" ], "quest layout" ),
			/published no \/assets\/data\/res-ui$/
		);
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
});

/*
===========================================================================

laterRenewalMallStep.test.mjs - the build's renewal Item Mall step

The v1.150 archives ship the classic mall window alone; the renewal one
(CIFNewItemMall) comes from the pinned later client
(import_vsro_renewal_mall.py), for the client's Experimental row. The
step's inputs are SRO_VSRO_ROOT and the archive's presence, as the
dungeon maps beside it. These tests pin the plan for each input and the
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
	laterRenewalMallImportPlan,
	laterRenewalMallPublicPaths,
	RESOURCE_BUILD_STEPS
} from "../../build/resourceBuild.mjs";

test("no archive configured: the plan is the one skip line", () => {
	for ( const env of [ {}, { SRO_VSRO_ROOT: "" }, { SRO_VSRO_ROOT: "  " } ] ) {
		const plan = laterRenewalMallImportPlan( env, true, "" );
		assert.equal( plan.run, false );
		assert.match( plan.line, /^Later renewal mall: skipped \(SRO_VSRO_ROOT is not set/ );
		assert.match( plan.line, /the Item Mall keeps the classic window\)$/ );
	}
});

test("a configured folder without the archive fails the build, naming it", () => {
	assert.throws(
		() => laterRenewalMallImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, false, "" ),
		/^Error: Later renewal mall: SRO_VSRO_ROOT names the later client D:\/later-client, but it holds no Media\.pk2/
	);
});

test("a configured archive: the plan runs the importer against its folder", () => {
	const plan = laterRenewalMallImportPlan( { SRO_VSRO_ROOT: "D:/later-client" }, true );
	assert.equal( plan.run, true );
	assert.deepEqual( plan.args, [ "scripts/import_vsro_renewal_mall.py", "--vsro-root", "D:/later-client" ] );
	// The variant stamp's later client stands in for an unset root.
	assert.equal( laterRenewalMallImportPlan( {}, true, "E:/stamped-client" ).root, "E:/stamped-client" );
});

test("the claimed paths are the catalogue and every picture it lists", () => {
	const later = "/assets/images/Media_extracted/later/interface/mall/";
	assert.deepEqual(
		laterRenewalMallPublicPaths( {
			images: [
				{ path: later + "mall_back_wnd.png", size: [ 424, 520 ] },
				{ path: "" },
				{ size: [ 1, 1 ] },
				{ path: later + "mall_item_wnd.png", size: [ 200, 84 ] }
			]
		} ),
		[ "/assets/data/renewal-mall.json", later + "mall_back_wnd.png", later + "mall_item_wnd.png" ]
	);
	assert.deepEqual( laterRenewalMallPublicPaths( undefined ), [ "/assets/data/renewal-mall.json" ] );
});

test("the step fails on a named folder without the archive, and skips in one line with none", async ( t ) => {
	const empty = fs.mkdtempSync( path.join( os.tmpdir(), "sro-renewal-mall-step-" ) );
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
	await assert.rejects( RESOURCE_BUILD_STEPS.importLaterRenewalMall( "" ), /holds no Media\.pk2/ );
	assert.equal( printed.length, 0 );
	delete process.env.SRO_VSRO_ROOT;
	assert.deepEqual( await RESOURCE_BUILD_STEPS.importLaterRenewalMall( "" ), { skipped: true } );
	assert.equal( printed.length, 1 );
});

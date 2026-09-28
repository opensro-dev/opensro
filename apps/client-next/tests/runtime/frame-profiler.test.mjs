/*
===========================================================================

frame-profiler.test.mjs - tests for tools/lib/frame-profiler.mjs

The recorder's stage accounting, and that instrumentation binds to the
remaining legacy boundaries, refuses missing ones, and leaves explicit
frame-owner hooks unpatched. UI hook behavior is tested with the real UI
in ui-resource-lifetime.test.mjs.

===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createFrameProfiler, instrumentFrameProfiler } from "../../tools/lib/frame-profiler.mjs";

test("pose counters belong to the main character phase and reset each frame", () => {
	const owner = createFrameProfiler( () => 0 );
	owner.start();
	owner.characterCount( "pose-created", 9 );
	owner.begin( 1 );
	owner.renderBegin();
	owner.renderMark( "world-prepare" );
	owner.characterBegin();
	owner.characterCount( "pose-created", 2 );
	owner.characterCount( "pose-retired" );
	owner.end();
	owner.begin( 2 );
	owner.renderMark( "renderer-setup" );
	owner.characterBegin();
	owner.characterCount( "pose-created", 9 );
	owner.end();
	const capture = owner.stop(), at = capture.columns.indexOf( "pose-created" );
	assert.deepEqual( capture.rows.map( row => row[at] ), [ 2, 0 ] );
});
test("frame recorder separates nested renderer stages, closes CPU spans and reports capacity loss", () => {
	let t = 0;
	const owner = createFrameProfiler( () => t, 2 );
	owner.begin( 0 );
	assert.equal( owner.stop().rows.length, 0 );
	owner.start();
	for ( let id = 10; id < 13; id++ ) {
		owner.begin( id );
		t += 1;
		owner.mark( "input-state-frontend" );
		owner.renderBegin();
		t += 2;
		owner.renderMark( "world-prepare" );
		t += 3;
		owner.renderMark( "character-prepare" );
		owner.mark( "render-preparation-submit" );
		t += 1;
		owner.end();
	}
	/*
	================
	index
	================
	*/
	const capture = owner.stop(), index = name => capture.columns.indexOf( name );
	assert.equal( capture.dropped, 1 );
	assert.equal( capture.rows.length, 2 );
	for ( const row of capture.rows ) {
		assert.equal( row[index( "cpuMs" )], 7 );
		assert.equal( row[index( "world-prepare" )], 2 );
		assert.equal( row[index( "character-prepare" )], 3 );
		assert.equal( row[index( "render-preparation-submit" )], 5 );
	}
	owner.start();
	owner.begin( 20 );
	t++;
	owner.end();
	assert.equal( owner.stop().rows[0][0], 20 );
});
test("instrumentation binds to current production boundaries and refuses missing boundaries", async () => {
	const html = "<!doctype html>\r\n<html></html>";
	assert.equal( instrumentFrameProfiler( html, "index.html" ), html );
	for (
		const file of [
			"src/engine/runtime/characters/characters.ts",
			"src/engine/runtime/renderer/frame/frame.ts",
			"src/engine/runtime/renderer/renderer.ts",
			"src/engine/runtime/renderer/world/world.ts",
			"src/engine/runtime/renderer/characters/characters.ts"
		]
	) {
		const source = await readFile( file, "utf8" ), instrumented = instrumentFrameProfiler( source, file );
		assert.notEqual( source, instrumented );
		assert.match( instrumented, /__worldProbeFrameProfiler/ );
		assert.throws( () => instrumentFrameProfiler( "", file ), /expected one/ );
	}
	// runtime.ts carries explicit profiler hooks and is never patched.
	const runtime = await readFile( "src/engine/runtime/runtime.ts", "utf8" );
	assert.equal( instrumentFrameProfiler( runtime, "src/engine/runtime/runtime.ts" ), runtime );
});

test("terrain detail spans accumulate across groups without changing parent clocks", () => {
	let t = 0;
	const owner = createFrameProfiler( () => t );
	owner.start();
	owner.begin( 1 );
	owner.worldBegin();
	for ( let i = 0; i < 2; i++ ) {
		owner.detailBegin( "terrain-seams" );
		t += 2;
		owner.detailEnd( "terrain-seams" );
	}
	owner.worldMark( "world-selection" );
	owner.end();
	const capture = owner.stop(), row = capture.rows[0];
	assert.equal( row[capture.columns.indexOf( "terrain-seams" )], 4 );
	assert.equal( row[capture.columns.indexOf( "world-selection" )], 4 );
	assert.equal( row[capture.columns.indexOf( "cpuMs" )], 4 );
});

test("draw census counts instancing and separate passes without treating shared portrait meshes as duplicates", () => {
	let t = 0;
	const owner = createFrameProfiler( () => t ),
		draw = { indexCount: 12, instanceCount: 3 },
		empty = { indexCount: 0, instanceCount: 1 };
	owner.start();
	owner.draw(
		7,
		{},
		undefined,
		[ draw, draw, empty ],
		[ { count: 4 }, { count: 2, layer: "world" } ],
		[ draw ],
		{ entries: [ { count: 6 } ] },
		{},
		{ draws: [ draw ] },
		undefined,
		[ { draws: [ draw ] } ]
	);
	owner.draw( 8, undefined, undefined, [], [], [], undefined, undefined, undefined, undefined, [] );
	const rows = owner.stop().drawSamples;
	assert.equal( rows.length, 1 );
	const row = rows[0];
	assert.deepEqual( row.main, { draws: 3, instances: 7, triangles: 24, zeroDraws: 1, duplicateReferences: 1 } );
	assert.equal( row.portraits.triangles, 24 );
	assert.equal( row.portraits.duplicateReferences, 0 );
	assert.equal( row.preview.triangles, 12 );
	assert.equal( row.uiQuads, 6 );
	assert.equal( row.renderPasses, 7 );
	assert.equal( row.computePasses, 1 );
	owner.start();
	t = 501;
	owner.draw( 9, undefined, undefined, [], [], [], undefined, undefined, undefined, undefined, [] );
	assert.equal( owner.stop().drawSamples[0].frameId, 9 );
});

test("world detail sampling is bounded and explicitly marks measured frames", () => {
	let t = 0;
	const owner = createFrameProfiler( () => t );
	owner.start();
	for ( let i = 0; i < 65; i++ ) {
		owner.begin( i );
		if ( owner.sampleDetails() ) {
			owner.detailBegin( "world-instance-loop" );
			t += 2;
			owner.detailEnd( "world-instance-loop" );
		}
		owner.end();
	}
	const capture = owner.stop(),
		sample = capture.columns.indexOf( "world-detail-sampled" ),
		cost = capture.columns.indexOf( "world-instance-loop" );
	assert.deepEqual( capture.rows.filter( row => row[sample] ).map( row => row[0] ), [ 0, 32, 64 ] );
	assert.equal( capture.rows.reduce( ( sum, row ) => sum + row[cost], 0 ), 6 );
});

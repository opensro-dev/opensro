/*
===========================================================================

frame-profiler.test.mjs - tests for tools/lib/frame-profiler.mjs

The recorder's stage accounting, and that the world renderer reports its
stages through the explicit frame-probe hooks (no source is patched). UI hook
behavior is tested with the real UI in ui-resource-lifetime.test.mjs.

===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { createFrameProfiler } from "../../tools/lib/frame-profiler.mjs";

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
test("the world renderer reports its preparation stages through the frame probe", async () => {
	const { createWorldRenderer } = await import(
		sourceFileUrl( "src/engine/runtime/renderer/world/world.ts" ).href
	);
	const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
		world = createWorldRenderer(),
		calls = [];
	const geometry = { upload: () => ({}), release() {}, updateInstances: d => d },
		textures = { upload: () => ({}), release() {} };
	const placed = identity();
	placed[14] = 20;
	world.profile( {
		worldBegin: () => calls.push( "begin" ),
		worldMark: stage => calls.push( stage ),
		sampleDetails: () => true,
		detailBegin: name => calls.push( "+" + name ),
		detailEnd: name => calls.push( "-" + name )
	} );
	world.scene( {
		id: "probe",
		originRegion: 1,
		warnings: [],
		groups: [ {
			id: "piece",
			center: [ 0, 0, 20 ],
			radius: 2,
			instanceRadius: 2,
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				normals: new Float32Array( 9 ),
				uvs: new Float32Array( 6 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				instances: placed,
				transform: identity()
			}
		} ]
	} );
	world.camera( { originRegion: 1, eye: [ 0, 0, 0 ], target: [ 0, 0, 20 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	try {
		world.prepare( geometry, textures, 1, 0 );
		assert.deepEqual(
			calls.filter( c => !c.startsWith( "+" ) && !c.startsWith( "-" ) ),
			[ "begin", "world-camera", "world-environment", "world-selection", "world-finalize" ]
		);
		assert.ok( calls.includes( "+world-instance-setup" ) && calls.includes( "-world-instance-upload" ) );
	} finally {
		world.dispose( geometry, textures );
	}
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
	owner.frameDraw(
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
	owner.frameDraw( 8, undefined, undefined, [], [], [], undefined, undefined, undefined, undefined, [] );
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
	owner.frameDraw( 9, undefined, undefined, [], [], [], undefined, undefined, undefined, undefined, [] );
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

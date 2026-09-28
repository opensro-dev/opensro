/*
===========================================================================

animation-ceiling.test.mjs - bounded diagnostic replay behavior

Exercises capture lifetime and compares real pose output across instrumentation.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { build } from "esbuild";
import {
	createAnimationCeiling,
	instrumentAnimationCeiling,
	instrumentWorldSelectionCeiling
} from "../../tools/lib/animation-ceiling.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
test("ceiling intervention is window-scoped, admits cold poses and excludes other models", () => {
	const probe = createAnimationCeiling( true );
	assert.equal( probe.skip( true, true ), false );
	probe.start();
	assert.equal( probe.skip( false, true ), false );
	assert.equal( probe.skip( true, false ), false );
	assert.equal( probe.skip( true, true ), true );
	assert.equal( probe.stats().skipped, 1 );
	probe.pause();
	assert.equal( probe.skip( true, true ), false );
	const control = createAnimationCeiling( false );
	control.start();
	assert.equal( control.skip( true, true ), false );
});
test("alternating ceiling marks exactly the frames whose warmed evaluations are bypassed", () => {
	const probe = createAnimationCeiling( false, true );
	probe.start();
	for ( let frame = 0; frame < 64; frame++ ) {
		const replay = Math.floor( frame / 16 ) % 2;
		assert.equal( probe.frame(), replay );
		assert.equal( probe.skip( true, true ), Boolean( replay ) );
	}
	assert.equal( probe.stats().skipped, 32 );
	probe.pause();
	assert.equal( probe.frame(), 0 );
	assert.equal( probe.skip( true, true ), false );
	probe.start();
	assert.equal( probe.frame(), 0 );
});
test("frozen poses retain exact warmed palettes and sockets, then resume normal evaluation", async () => {
	const source = await readFile( "src/engine/foundation/animation/animation-pose.ts", "utf8" );
	const result = await build( {
		stdin: {
			contents:
				`export {createCharacterPose} from './src/engine/foundation/animation/animation-pose';export {createModelDecoder} from './src/engine/runtime/assets/worker/model/model';`,
			loader: "ts",
			resolveDir: process.cwd()
		},
		plugins: [ {
			name: "ceiling", /*
================
setup
================
			*/
			setup( b ) {
				b.onLoad(
					{ filter: /animation-pose\.ts$/ },
					args => ({
						contents: instrumentAnimationCeiling( source ),
						loader: "ts",
						resolveDir: path.dirname( args.path )
					})
				);
			}
		} ],
		bundle: true,
		platform: "node",
		format: "esm",
		write: false
	} );
	const { createCharacterPose, createModelDecoder } = await import(
		"data:text/javascript;base64," + Buffer.from( result.outputFiles[0].contents ).toString( "base64" )
	);
	const decoder = createModelDecoder(),
		model = decoder.character(
			decoder.decode(
				readPublishedAssetBytesSync(
					"/assets/npc/mob/china/mangnyang.glb",
					path.resolve( "../../.generated/client-public" )
				)
			)
		);
	const probe = createAnimationCeiling( true );
	globalThis.__worldProbeAnimationCeiling = probe;
	const owner = createCharacterPose( model, { ceiling: probe } );
	const palette = pose =>
		model.primitives.map( p => {
			const out = new Float32Array( p.joints.length * 16 );
			pose.palette( p, out );
			return out;
		} );
	try {
		owner.evaluate( "walk", .1 );
		const initial = palette( owner ), sockets = model.nodes.map( n => owner.socket( n.name ) );
		probe.start();
		assert.equal( owner.evaluate( "walk", .8 ), false );
		assert.deepEqual( palette( owner ), initial );
		assert.deepEqual( model.nodes.map( n => owner.socket( n.name ) ), sockets );
		assert.equal( probe.stats().paletteBuilds, 0 );
		const cold = createCharacterPose( model, { ceiling: probe } );
		assert.equal( cold.evaluate( "walk", .8 ), true );
		assert.ok( probe.stats().cold > 0 );
		probe.pause();
		owner.evaluate( "walk", .8 );
		assert.deepEqual( palette( owner ), palette( cold ) );
		assert.notDeepEqual( palette( owner ), initial );
	} finally {
		delete globalThis.__worldProbeAnimationCeiling;
	}
});

test("combined schedule balances four modes and never replays cold selection", () => {
	const p = createAnimationCeiling( false, false, true );
	p.start();
	const counts = [ 0, 0, 0, 0 ];
	for ( let i = 0; i < 256; i++ ) {
		const a = p.frame(), w = p.worldMode(), mode = a + w * 2;
		assert.equal( mode, [ 0, 1, 3, 2, 2, 3, 1, 0 ][Math.floor( i / 16 ) % 8] );
		counts[mode]++;
		assert.equal( p.skip( true, true ), !!a );
		assert.equal( p.worldReplay( false ), false );
		assert.equal( p.worldReplay( true ), !!w );
	}
	assert.deepEqual( counts, [ 64, 64, 64, 64 ] );
	p.pause();
	assert.equal( p.worldReplay( true ), false );
	assert.equal( p.worldMode(), 0 );
});

test("world replay retains draws but updates camera, then resumes culling and admits recovery", async () => {
	const file = "src/engine/runtime/renderer/world/world.ts", source = await readFile( file, "utf8" );
	assert.throws( () => instrumentWorldSelectionCeiling( "" ), /boundary changed/ );
	const result = await build( {
		stdin: {
			contents: instrumentWorldSelectionCeiling( source ),
			resolveDir: path.resolve( path.dirname( file ) ),
			loader: "ts"
		},
		bundle: true,
		platform: "node",
		format: "esm",
		write: false,
		tsconfig: path.resolve( "tsconfig.json" )
	} );
	const { createWorldRenderer } = await import(
		"data:text/javascript;base64," + Buffer.from( result.outputFiles[0].contents ).toString( "base64" )
	);
	const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
		world = createWorldRenderer(),
		probe = createAnimationCeiling( false, false, true );
	const geometry = {
			upload: () => ({}), /*
================
release
================
			*/
			release() {},
			updateInstances: d => d
		},
		textures = {
			upload: () => ({}), /*
================
release
================
			*/
			release() {}
		};
	const m = identity();
	m[14] = 20;
	const value = {
		id: "ceiling",
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
				instances: m,
				transform: identity()
			}
		} ]
	};
	const camera = z => ({
		originRegion: 1,
		eye: [ 0, 0, 0 ],
		target: [ 0, 0, z ],
		near: 1,
		far: 1000,
		fov: Math.PI / 3
	});
	globalThis.__worldProbeAnimationCeiling = probe;
	try {
		world.scene( value );
		world.camera( camera( 20 ) );
		const normal = world.prepare( geometry, textures, 1, 0 );
		assert.equal( normal.draws.length, 1 );
		probe.start();
		for ( let i = 0; i <= 32; i++ ) probe.frame();
		assert.equal( probe.worldMode(), 1 );
		world.camera( camera( -20 ) );
		const replay = world.prepare( geometry, textures, 1, .1 );
		assert.equal( replay.draws, normal.draws );
		assert.notDeepEqual( replay.matrix, normal.matrix );
		assert.deepEqual( replay.camera.target, [ 0, 0, -20 ] );
		probe.pause();
		assert.equal( world.prepare( geometry, textures, 1, .2 ).draws.length, 0 );
		probe.start();
		for ( let i = 0; i <= 32; i++ ) probe.frame();
		world.invalidate();
		world.prepare( geometry, textures, 1, .3 );
		assert.ok( probe.stats().worldCold > 0 );
	} finally {
		world.dispose( geometry, textures );
		delete globalThis.__worldProbeAnimationCeiling;
	}
});

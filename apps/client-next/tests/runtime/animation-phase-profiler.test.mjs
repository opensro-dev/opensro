/*
===========================================================================

animation-phase-profiler.test.mjs - sampled profiling preserves pose output

Exercises capture lifetime and compares real pose output across instrumentation.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { build } from "esbuild";
import { createAnimationPhaseProfiler, instrumentAnimationPhases } from "../../tools/lib/animation-phase-profiler.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

test("animation sampling has bounded counters and window-owned start/stop", () => {
	let time = 0;
	const probe = createAnimationPhaseProfiler( () => time );
	assert.equal( probe.begin(), null );
	probe.start();
	for ( let i = 0; i < 6400; i++ ) {
		const timer = probe.begin();
		if ( timer ) {
			timer.start( "sampling" );
			time += 2;
			timer.end( "sampling" );
		}
	}
	const result = probe.stats();
	assert.equal( result.eligible, 6400 );
	assert.ok( result.sampled > 60 && result.sampled < 140 );
	assert.equal( result.sums.sampling, result.sampled * 2 );
	probe.pause();
	probe.begin();
	assert.deepEqual( probe.stats(), result );
	probe.start();
	assert.equal( probe.stats().eligible, 0 );
});

test("observed evaluation preserves complete palettes for seeks, loops and layered tracks", async () => {
	const source = await readFile( "src/engine/foundation/animation/animation-pose.ts", "utf8" );
	/*
================
load
================
	*/
	async function load( observed ) {
		const result = await build( {
			stdin: {
				contents:
					`export {createCharacterPose} from './src/engine/foundation/animation/animation-pose';export {createModelDecoder} from './src/engine/runtime/assets/worker/model/model';`,
				loader: "ts",
				resolveDir: process.cwd()
			},
			plugins: [ {
				name: "observe", /*
================
setup
================
				*/
				setup( b ) {
					b.onLoad(
						{ filter: /animation-pose\.ts$/ },
						args => ({
							contents: observed ? instrumentAnimationPhases( source ) : source,
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
		return import(
			"data:text/javascript;base64," + Buffer.from( result.outputFiles[0].contents ).toString( "base64" )
		);
	}
	const baseline = await load( false ), observed = await load( true ), decoder = baseline.createModelDecoder();
	const model = decoder.character(
		decoder.decode(
			readPublishedAssetBytesSync(
				"/assets/npc/mob/china/mangnyang.glb",
				path.resolve( "../../.generated/client-public" )
			)
		)
	);
	const probe = createAnimationPhaseProfiler();
	globalThis.__worldProbeAnimationPhases = probe;
	probe.start();
	const owners = [ baseline.createCharacterPose( model ), observed.createCharacterPose( model, { phases: probe } ) ];
	try {
		for ( let frame = 0; frame < 600; frame++ ) {
			const time = (frame % 2 ? 600 - frame : frame) / 37, loop = frame % 3 !== 0;
			const layers = frame % 4 === 0 ?
				[ { clip: "stand", time, loop, weight: .4, lane: "event" }, {
					clip: "walk",
					time: time + .13,
					loop,
					weight: .6,
					lane: "timed"
				} ] :
				undefined;
			for ( const owner of owners ) {
				owner.evaluate( frame % 2 ? "walk" : "stand", time, loop, layers, frame % 2 === 0 );
			}
			for ( const primitive of model.primitives ) {
				const arrays = owners.map( owner => {
					const out = new Float32Array( primitive.joints.length * 16 );
					owner.palette( primitive, out );
					return out;
				} );
				assert.deepEqual( arrays[0], arrays[1] );
			}
		}
		assert.ok( probe.stats().sampled > 0 );
		for ( const value of Object.values( probe.stats().sums ) ) assert.ok( Number.isFinite( value ) && value >= 0 );
	} finally {
		delete globalThis.__worldProbeAnimationPhases;
	}
});

test("materialization attribution is bounded and independent of admission counts", () => {
	const model = { nodes: [ {} ], primitives: [ { name: "fixture" } ] },
		probe = createAnimationPhaseProfiler( () => 0 );
	probe.tag( model, "fixture/model", { sharedPalette: true } );
	probe.start();
	for ( let i = 0; i < 6400; i++ ) {
		const timer = probe.begin( model, "palette", [ { clip: { name: "walk" } } ] );
		if ( timer && !timer.phases ) {
			timer.start( "materialization" );
			timer.end( "materialization" );
		}
	}
	probe.admission( model, "gpu:accepted", 123 );
	const rows = probe.stats().materializations;
	assert.equal( rows.find( r => r.reason === "palette:walk" ).calls, 6400 );
	assert.ok( rows.find( r => r.reason === "palette:walk" ).sampled > 0 );
	assert.equal( rows.find( r => r.reason === "gpu:accepted" ).calls, 123 );
	assert.equal( rows.find( r => r.reason === "gpu:accepted" ).sampled, 0 );
	probe.start();
	assert.deepEqual( probe.stats().materializations, [] );
});

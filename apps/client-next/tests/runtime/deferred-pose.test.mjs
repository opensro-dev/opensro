/*
===========================================================================

deferred-pose.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createCharacterPose, createPaletteStreams, createGpuClipPlan, createGpuSkeletonPlan } = {
	...(await load( "src/engine/foundation/animation/animation-pose.ts" )),
	...(await load( "src/engine/runtime/renderer/characters/palette-streams.ts" )),
	...(await load( "src/engine/foundation/animation/gpu-animation-plan.ts" ))
};
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
/*
================
model
================
*/
function model() {
	return {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] }, {
			name: "socket",
			parent: 0,
			translation: [ 0, 3, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		} ],
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 0, 10, 0, 0 )
			} ]
		} ],
		primitives: [ { joints: [ 0, 1 ], inverseBind: Float32Array.from( [ ...I(), ...I() ] ) } ]
	};
}
test("deferred poses preserve seeks, long clocks, CPU sockets, layers and failed-request isolation", () => {
	const m = model(),
		a = createCharacterPose( m ),
		b = createCharacterPose( m ),
		out = new Float32Array( 32 ),
		expected = new Float32Array( 32 );
	for ( const time of [ 0, .5, 86400.333333, .1, 1, 0 ] ) {
		a.evaluate( "move", time );
		const changed = b.evaluate( "move", time, true, undefined, true );
		assert.equal( b.gpuSample().time, time % 1 );
		const before = b.cpuEvaluations();
		assert.deepEqual( b.socket( "socket" ), a.socket( "socket" ) );
		assert.equal( b.cpuEvaluations(), before + Number( changed ) );
		b.palette( m.primitives[0], out );
		a.palette( m.primitives[0], expected );
		assert.deepEqual( out, expected );
		assert.equal( b.cpuEvaluations(), before + Number( changed ) );
	}
	const layers = [ { clip: "move", time: .8, weight: .5, lane: "event", loop: true }, {
		clip: "move",
		time: .2,
		weight: .5,
		lane: "timed",
		loop: true
	} ];
	a.evaluate( "", 0, false, layers );
	b.evaluate( "", 0, false, layers, true );
	assert.equal( b.gpuSample(), null );
	assert.deepEqual( b.socket( "socket" ), a.socket( "socket" ) );
	const revision = b.revision();
	assert.throws( () =>
		b.evaluate( "", 0, false, [ ...layers, { clip: "missing", time: 0, weight: 1, lane: "timed" } ], true )
	);
	assert.equal( b.revision(), revision );
	assert.deepEqual( b.socket( "socket" ), a.socket( "socket" ) );
	const p = createCharacterPose( m );
	p.evaluate( "move", .25, true, undefined, true );
	assert.equal( p.cpuEvaluations(), 0 );
	assert.equal( p.evaluate( "move", .25 ), false );
	assert.equal( p.cpuEvaluations(), 1 );
});
test("CPU fallback restores every palette after GPU ownership, including unchanged poses", () => {
	const m = model(),
		a = createCharacterPose( m ),
		b = createCharacterPose( m ),
		bank = createPaletteStreams( m, 2 ),
		expected = new Float32Array( 64 );
	a.evaluate( "move", .2, true, undefined, true );
	b.evaluate( "move", .4, true, undefined, true );
	let requests = 0;
	bank.update( [ a, b ], () => {
		requests++;
		return true;
	} );
	assert.equal( requests, 1 );
	assert.equal( a.cpuEvaluations() + b.cpuEvaluations(), 0 );
	a.evaluate( "move", .6, true, undefined, true );
	bank.update( [ a, b ], () => false );
	a.palette( m.primitives[0], expected );
	b.palette( m.primitives[0], expected, 32 );
	assert.deepEqual( bank.streams[0].data, expected );
	bank.update( [ b, a ] );
	assert.deepEqual( [ ...bank.streams[0].offsets ], [ 0, 2 ] );
	b.palette( m.primitives[0], expected );
	a.palette( m.primitives[0], expected, 32 );
	assert.deepEqual( bank.streams[0].data, expected );
});
test("GPU admission bounds rigs and packs all bindings without changing model inputs", () => {
	const m = model(), snapshot = structuredClone( m ), clips = createGpuClipPlan( m.clips );
	assert.ok( clips );
	const skeleton = createGpuSkeletonPlan( m, clips.nodes );
	assert.ok( skeleton );
	assert.equal( skeleton.configurations.get( m.primitives[0] )[0], 2 );
	assert.equal( skeleton.configurations.get( m.primitives[0] )[9], clips.nodes, "the shader bounds clip tables" );
	assert.deepEqual( m, snapshot );
	assert.equal(
		createGpuSkeletonPlan( { ...m, nodes: Array.from( { length: 129 }, () => m.nodes[0] ) }, clips.nodes ),
		null
	);
	assert.throws(
		() => createGpuSkeletonPlan( { ...m, nodes: [ { ...m.nodes[0], parent: 0 } ] }, clips.nodes ),
		/hierarchy/
	);
});

test("models that share clips share one clip plan; a skeleton is per model", () => {
	const m = model(), assembled = { ...m, nodes: [ ...m.nodes ] }, clips = createGpuClipPlan( m.clips );
	assert.ok( clips );
	// The clip set does not depend on the skeleton: an assembled model built
	// on the same clips reads the same tables.
	assert.deepEqual( createGpuClipPlan( assembled.clips )?.data, clips.data );
	const a = createGpuSkeletonPlan( m, clips.nodes ), b = createGpuSkeletonPlan( assembled, clips.nodes );
	assert.ok( a && b );
	assert.deepEqual( a.data, b.data );
	assert.ok( a.data.length < clips.data.length + a.data.length, "skeleton holds no keyframes" );
});

test("one layered pose cannot force eligible siblings onto CPU", () => {
	const m = model(), a = createCharacterPose( m ), b = createCharacterPose( m ), bank = createPaletteStreams( m, 2 );
	a.evaluate( "move", .2, true, undefined, true );
	b.evaluate( "", 0, true, [ { clip: "move", time: .5, weight: .5, lane: "event", loop: true }, {
		clip: "move",
		time: .3,
		weight: .5,
		lane: "timed",
		loop: true
	} ], true );
	bank.update( [ b, a ], ( source, model, primitive, samples ) => {
		assert.equal( samples[0], null );
		assert.equal( samples[1].clip, m.clips[0] );
		const expected = new Float32Array( 32 );
		b.palette( primitive, expected );
		assert.deepEqual( source.subarray( 0, 32 ), expected );
		return true;
	} );
	assert.equal( a.cpuEvaluations(), 0 );
	assert.equal( b.cpuEvaluations(), 1 );
	a.evaluate( "move", .7, true, undefined, true );
	bank.update( [ a, b ], () => false );
	assert.equal( a.cpuEvaluations(), 1 );
	const expected = new Float32Array( 64 );
	a.palette( m.primitives[0], expected );
	b.palette( m.primitives[0], expected, 32 );
	assert.deepEqual( bank.streams[0].data, expected );
});

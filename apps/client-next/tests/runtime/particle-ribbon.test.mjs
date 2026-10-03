/*
===========================================================================

particle-ribbon.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
async function load( file ) {
	return import( sourceFileUrl( "src/engine/foundation/" + file ).href );
}
const { createRibbonChain, pushRibbonPoint, ribbonPolyline, ribbonSpline, ribbonStrip } = await load(
	"rendering/particle-ribbon.ts"
);
const { particleProgram, initializeParticle, advanceParticle } = await load( "animation/particle-program.ts" );
/*
================
chainOf

A ribbon chain through points on the x axis: [ x, width ] each.
================
*/
const chainOf = points => {
	const chain = createRibbonChain( 1 );
	for ( const [x, width] of points ) pushRibbonPoint( chain, [ x, 0, 0 ], 0, [ 1, .5, 0, 1 ], 0, 1, width );
	return chain;
};
test("linked trails require distinct neighbours and use uniform B-spline endpoint weights", () => {
	const drawn = createRibbonChain( 1 ), work = createRibbonChain( 1 );
	ribbonSpline( chainOf( [ [ 0, 1 ], [ 0, 1 ] ] ), drawn, work );
	assert.equal( drawn.count, 0 );
	ribbonSpline( chainOf( [ [ 0, 1 ], [ 0, 1 ], [ 6, 0 ] ] ), drawn, work );
	assert.equal( drawn.count, 4 );
	assert.equal( drawn.positions[0], 1 );
	assert.equal( drawn.positions[9], 6 );
	assert.ok( Math.abs( drawn.widths[1] - 2 / 3 ) < 1e-12 );
	const strip = {
		positions: new Float32Array( 24 ),
		colors: new Float32Array( 32 ),
		uvs: new Float32Array( 16 ),
		indices: new Uint32Array( 18 )
	};
	ribbonStrip( drawn, new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ), strip, 0, 0, work );
	assert.equal( Math.abs( strip.positions[1] ), 1 );
	assert.equal( strip.uvs.at( -2 ), 1 );
	assert.equal( strip.uvs[4], Math.fround( 1 / 3 ) );
	assert.deepEqual( [ ...strip.indices.subarray( 6, 12 ) ], [ 2, 3, 4, 4, 3, 5 ] );
	assert.ok( strip.positions.every( Number.isFinite ) );
	// The raw chain keeps a point that moved by exactly the threshold.
	ribbonPolyline( chainOf( [ [ 0, 1 ], [ 1e-6, 1 ], [ 1e-6, 1 ] ] ), drawn );
	assert.equal( drawn.count, 2 );
});
test("cone execution uses the converted radian vector and advances independent element velocity", () => {
	const program = particleProgram( [ {
		name: "SetConeVel",
		flags: 0,
		parameter: { kind: "AngleVector1", left: [ 1, 1, 360 ], right: [ 1, 1, Math.PI * 2 ] }
	} ] );
	const { state } = initializeParticle( program, new Float32Array( [ 0, 0, .25, 0 ] ), 0 );
	assert.ok( Math.abs( state.velocity[0] + 1 ) < 1e-6 );
	assert.ok( Math.abs( state.velocity[1] ) < 1e-6 );
	advanceParticle( state, program, 2 );
	assert.ok( Math.abs( state.position[0] + 2 ) < 1e-6 );
});
test("relative cone velocity flags 2 transforms velocity by emitter orientation basis", () => {
	const program = particleProgram( [ {
		name: "SetConeVel",
		flags: 2,
		parameter: { kind: "AngleVector1", left: [ 1, 1, 360 ], right: [ 1, 1, Math.PI * 2 ] }
	} ] );
	const transform = [ 0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1 ];
	const { state } = initializeParticle( program, new Float32Array( [ 0, 0, .25, 0 ] ), 0, transform );
	assert.ok( Math.abs( state.velocity[0] ) < 1e-6 );
	assert.ok( Math.abs( state.velocity[1] ) < 1e-6 );
	assert.ok( Math.abs( state.velocity[2] - 1 ) < 1e-6 );
});
test("published derived rotation and cone parameters use authored inputs rather than serialized scratch", async () => {
	const { particleRotation, particleCone } = await load( "animation/particle-rotation.ts" );
	const catalog = JSON.parse( readFileSync( "../../.generated/client-public/assets/effects/programs.json", "utf8" ) ),
		counts = { AxisVector4: 0, RotVector: 0, AngleVector1: 0 };
	function visit( node ) {
		for ( const op of node.renderProgram ?? [] ) {
			const p = op.parameter;
			if ( !(p?.kind in counts) ) continue;
			counts[p.kind]++;
			const convert = p.kind === "AngleVector1" ? particleCone : particleRotation,
				a = convert( p ),
				b = convert( { ...p, right: Array( p.kind === "AngleVector1" ? 3 : 16 ).fill( 999 ) } );
			assert.deepEqual( a, b, "serialized scratch is not authority" );
			assert.ok( a.every( Number.isFinite ) );
		}
		for ( const child of node.children ?? [] ) visit( child );
	}
	for ( const effect of Object.values( catalog.effects ) ) visit( effect.root );
	assert.ok(
		counts.AxisVector4 > 3000 && counts.RotVector > 900 && counts.AngleVector1 > 1300,
		JSON.stringify( counts )
	);
	assert.notDeepEqual(
		particleRotation( { kind: "AxisVector4", left: [ 1, 0, 0, 15 ] } ),
		particleRotation( { kind: "AxisVector4", left: [ 1, 0, 0, 0 ] } )
	);
	assert.deepEqual( particleCone( { kind: "AngleVector1", left: [ 2, 7, 180 ] } ), [
		2,
		7,
		Math.fround( 3.1415927410125732 )
	] );
});

test("SetShapeRot decodes 16-element orientation and initializes particle rotation without spin", () => {
	const angle = Math.fround( 15 * 3.1415927410125732 / 180 ),
		c = Math.fround( Math.cos( angle ) ),
		s = Math.fround( Math.sin( angle ) );
	const matrix = [ 1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1 ];
	const program = particleProgram( [ {
		name: "SetShapeRot",
		flags: 0,
		start: 0,
		end: 1,
		step: 1,
		parameter: {
			kind: "AxisVector4",
			left: [ 1, 0, 0, 15 ],
			right: [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ]
		}
	} ] );
	assert.deepEqual( program.shape, matrix );
	const { state } = initializeParticle( program, new Float32Array( [ 0 ] ), 0 );
	const expected = Array.from( new Float32Array( matrix ) );
	assert.deepEqual( Array.from( state.rotation ), expected );
	advanceParticle( state, program, 1 );
	assert.deepEqual( Array.from( state.rotation ), expected );
});

test("SetRVelocity assigns angular velocity matrix and continuously rotates particle over frames", () => {
	const c = Math.fround( Math.cos( Math.fround( 90 * 3.1415927410125732 / 180 ) ) ),
		rotY90 = [ c, 0, 1, 0, 0, 1, 0, 0, -1, 0, c, 0, 0, 0, 0, 1 ];
	const program = particleProgram( [ {
		name: "SetRVelocity",
		flags: 0,
		start: 0,
		end: 1,
		step: 0,
		parameter: { kind: "RotVector", left: [ 0, 90, 0 ], right: rotY90 }
	} ] );
	assert.deepEqual( program.rVelocity, rotY90 );
	const { state } = initializeParticle( program, new Float32Array( [ 0 ] ), 0 );
	assert.ok( state.angularVelocity );
	assert.deepEqual( Array.from( state.angularVelocity ), rotY90 );
	advanceParticle( state, program, 1 );
	// After 1 frame: identity * rotY90 = rotY90
	assert.ok( Math.abs( state.rotation[0] ) < 1e-6 );
	assert.ok( Math.abs( state.rotation[2] - 1 ) < 1e-6 );
	assert.ok( Math.abs( state.rotation[8] + 1 ) < 1e-6 );
	advanceParticle( state, program, 2 );
	// After 2 frames: 180 degree rotation around Y (diag: -1, 1, -1)
	assert.ok( Math.abs( state.rotation[0] + 1 ) < 1e-6 );
	assert.ok( Math.abs( state.rotation[5] - 1 ) < 1e-6 );
	assert.ok( Math.abs( state.rotation[10] + 1 ) < 1e-6 );
});

test("ConeForce adds cone force aperture velocity at particle initialization", () => {
	const program = particleProgram( [ {
		name: "ConeForce",
		flags: 0,
		start: 0,
		end: 1,
		step: 1,
		parameter: { kind: "AngleVector1", left: [ 2, 2, 360 ], right: [ 2, 2, Math.PI * 2 ] }
	} ] );
	const { state } = initializeParticle( program, new Float32Array( [ 0, 0, .25, 0 ] ), 0 );
	assert.ok( Math.abs( state.velocity[0] + 2 ) < 1e-6 );
	assert.ok( Math.abs( state.velocity[1] ) < 1e-6 );
});

test("Attraction accelerates velocity toward origin during advanceParticle", () => {
	const program = particleProgram(
		[ {
			name: "Attraction",
			flags: 0,
			byte1: 0,
			start: 0,
			end: 1,
			step: 0,
			parameter: { kind: "float", value: 1 }
		} ],
		[],
		20
	);
	const { state } = initializeParticle( program, new Float32Array( [ 0 ] ), 0 );
	state.position[0] = 10;
	state.position[1] = 0;
	state.position[2] = 0;
	advanceParticle( state, program, 1 );
	// Vector to origin: [-10, 0, 0], dist: 10, unit: [-1, 0, 0], k = 1 / 10 = 0.1
	// Velocity should become [-1, 0, 0]
	assert.ok( Math.abs( state.velocity[0] + 1 ) < 1e-6 );
	assert.ok( state.position[0] < 10 );
	// Advance frame 1 -> 2: single-kick op (step:0) does not fire on frame 1
	const velX = state.velocity[0];
	advanceParticle( state, program, 2 );
	assert.strictEqual( state.velocity[0], velX );
});

test("SetConePos samples conical spawn displacement and offsets initial position", () => {
	const program = particleProgram( [ {
		name: "SetConePos",
		flags: 7,
		start: 0,
		end: 1,
		step: 0,
		parameter: { kind: "AngleVector1", left: [ 5, 5, 360 ], right: [ 5, 5, Math.PI * 2 ] }
	} ] );
	const { state } = initializeParticle( program, new Float32Array( [ 0, 0, .25, 0 ] ), 0 );
	assert.ok( Math.abs( state.position[0] + 5 ) < 1e-6 );
	assert.ok( Math.abs( state.position[1] ) < 1e-6 );
	const identityWithPos = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 20, 30, 1 ] );
	const transformed = initializeParticle( program, new Float32Array( [ 0, 0, .25, 0 ] ), 0, identityWithPos );
	assert.ok( Math.abs( transformed.state.position[0] - (10 - 5) ) < 1e-6 );
	assert.ok( Math.abs( transformed.state.position[1] - 20 ) < 1e-6 );
	assert.ok( Math.abs( transformed.state.position[2] - 30 ) < 1e-6 );
});

test("LinkDPipe keeps the raw chain: no spline points, only exact repeats dropped", () => {
	const chain = createRibbonChain( 1 ), raw = createRibbonChain( 1 ), splined = createRibbonChain( 1 );
	for ( const [x, y] of [ [ 0, 0 ], [ 0, 0 ], [ 10, 5 ], [ 10, 5 + 1e-7 ], [ 20, 0 ] ] ) {
		pushRibbonPoint( chain, [ x, y, 0 ], 0, [ 1, 1, 1, 1 ], 0, 1, 2 );
	}
	ribbonPolyline( chain, raw );
	assert.deepEqual(
		Array.from( { length: raw.count }, ( _, i ) => raw.positions[i * 3] ),
		[ 0, 10, 20 ],
		"AF8E80 drops only sub-1e-6 repeats"
	);
	ribbonSpline( chain, splined, createRibbonChain( 1 ) );
	assert.ok( splined.count > raw.count, "LinkPipe's AF9020 spline adds points" );
	ribbonPolyline( chainOf( [ [ 0, 1 ] ] ), raw );
	assert.equal( raw.count, 0 );
});

/*
===========================================================================

particle-orientation.test.mjs - native EFP orientation and BAN command order

AF4581 composes BAN frames with their parent; AF521B positions relative to
that resulting element orientation. Identity-only fixtures miss both errors.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
const { createParticleGraph, advanceParticleGraph } = await import(
	"../../src/engine/foundation/animation/particle-graph.ts"
);
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

/*
================
emitter
================
*/
function emitter( commands ) {
	return {
		parent: -1,
		parents: [ 0 ],
		births: [ 0 ],
		frames: 4,
		keepMatrix: true,
		keepOrigin: true,
		positionDepth: 0,
		matrixDepth: 0,
		velocityDepth: 0,
		followDepth: 0,
		scales: [],
		positions: [],
		rotations: [],
		commands
	};
}

/*
================
turn

Quarter turn around Z in the port's transposed native storage.
================
*/
function turn() {
	return [ 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ];
}

/*
================
run
================
*/
function run( commands, frame = 0 ) {
	const graph = [ emitter( commands ) ];
	const state = createParticleGraph( graph, 0 );
	advanceParticleGraph( state, graph, frame / 20, Float32Array.from( turn() ), new Float32Array( [ .5 ] ) );
	const element = state.elements[0][0];
	assert.ok( element );
	return element;
}

test("BAN and constant rotations retain the parent's nonidentity orientation", () => {
	for ( const name of [ "SetBANRot", "SetRotation", "SetRotationMat" ] ) {
		const element = run( [ { name, flags: 2, frames: [ 0 ], rotations: [ turn() ], program: {} } ] );
		assert.deepEqual( Array.from( element.matrix ), [ -1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
	}
});

test("BAN position mode 4 uses the preceding rotation while mode 7 uses its parent", () => {
	const rotation = { name: "SetBANRot", flags: 2, frames: [ 0 ], rotations: [ turn() ], program: {} };
	for ( const [flags, expected] of [ [ 4, [ -2, 0, 0 ] ], [ 7, [ 0, 2, 0 ] ] ] ) {
		const position = { name: "SetBANPos", flags, frames: [ 0 ], positions: [ [ 2, 0, 0 ] ], program: {} };
		assert.deepEqual( run( [ rotation, position ] ).state.position, expected );
	}
	const position = { name: "SetBANPos", flags: 4, frames: [ 0 ], positions: [ [ 2, 0, 0 ] ], program: {} };
	assert.deepEqual( run( [ position, rotation ] ).state.position, [ 0, 2, 0 ] );
});

test("scheduled rotation does not run before its authored frame", () => {
	const commands = [ {
		name: "SetBANRot",
		flags: 0,
		frames: [ 2 ],
		rotations: [ Array.from( identity() ) ],
		program: {}
	} ];
	assert.deepEqual( Array.from( run( commands, 1 ).matrix ), turn() );
	assert.deepEqual( Array.from( run( commands, 2 ).matrix ), Array.from( identity() ) );
});

test("every published rare effect preserves ordered BAN and rotation commands", () => {
	const bytes = readPublishedAssetBytesSync( "assets/effects/programs.json", publicRoot );
	const catalog = JSON.parse( new TextDecoder().decode( bytes ) );
	let effects = 0, rotations = 0, positions = 0;
	const compiler = createEffectPrograms();
	for ( const [path, source] of Object.entries( catalog.effects ) ) {
		if ( !path.startsWith( "system/system_rare" ) ) continue;
		const { model } = compiler.decode( bytes, path );
		const nodes = [];
		const visit = node => {
			nodes.push( node );
			for ( const child of node.children ) visit( child );
		};
		visit( source.root );
		assert.ok( model.particleGraph );
		assert.equal( model.particleGraph.length, nodes.length );
		for ( const [index, node] of nodes.entries() ) {
			const names = new Set( [ "SetBANRot", "SetBANPos", "SetRotation", "SetRotationMat" ] );
			const expected = node.renderProgram.filter( op => names.has( op.name ) );
			const commands = model.particleGraph[index].commands;
			assert.ok( commands );
			const actual = commands.filter( op => names.has( op.name ) );
			assert.deepEqual(
				actual.map( op => [ op.name, op.flags ] ),
				expected.map( op => [ op.name, op.flags ] ),
				path
			);
			rotations += actual.filter( op => op.name === "SetBANRot" ).length;
			positions += actual.filter( op => op.name === "SetBANPos" ).length;
		}
		effects++;
	}
	assert.ok( effects > 0 );
	assert.equal( rotations, 49 );
	assert.equal( positions, 49 );
});

test("BAN accumulation and constant rotation preserve their different native operand orders", () => {
	// A nonuniform scale and a quarter turn do not commute.
	const scale = [ 2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ];
	const ban = run( [ { name: "SetBANRot", flags: 1, frames: [ 0 ], rotations: [ scale ], program: {} } ] );
	const constant = run( [ { name: "SetRotation", flags: 1, frames: [ 0 ], rotations: [ scale ], program: {} } ] );
	assert.deepEqual( Array.from( ban.matrix ).slice( 0, 8 ), [ 0, 3, 0, 0, -2, 0, 0, 0 ] );
	assert.deepEqual( Array.from( constant.matrix ).slice( 0, 8 ), [ 0, 2, 0, 0, -3, 0, 0, 0 ] );
});

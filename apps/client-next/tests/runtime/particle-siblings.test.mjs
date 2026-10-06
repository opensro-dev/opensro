/*
===========================================================================

particle-siblings.test.mjs - predecessor identity across reused emitter slots

Sibling commands read evolving state in slot execution order. Parent groups
must remain isolated when birth serials no longer match storage order.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createParticleGraph, advanceParticleGraph } = await import(
	"../../src/engine/foundation/animation/particle-graph.ts"
);
const matrix = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
const table = new Float32Array( [ .25, .5, .75 ] );

/*
================
emitter
================
*/
function emitter( overrides = {} ) {
	return {
		parent: -1,
		parents: [ 0 ],
		births: [ 0 ],
		frames: 6,
		keepMatrix: false,
		keepOrigin: true,
		positionDepth: 0,
		matrixDepth: 0,
		velocityDepth: 0,
		followDepth: 0,
		scales: [],
		positions: [],
		rotations: [],
		...overrides
	};
}

test("sibling commands preserve parent isolation and slot execution order after recycling", () => {
	const frames = Array.from( { length: 6 }, ( _, i ) => i );
	const graph = [
		emitter( { births: [ 0, 0 ], parents: [ 0, 0 ], frames: 100 } ),
		emitter( {
			parent: 0,
			capacity: 12,
			emission: { start: 0, duration: 80, period: 1, limit: 6, rate: 2 },
			program: { vectors: [ { name: "SetPosition", value: [ 1, 0, 0 ], flags: 2, frames } ] }
		} )
	];
	const state = createParticleGraph( graph, 0 );
	let recycled = false;
	for ( let tick = 0; tick < 40; tick++ ) {
		const old = new Map(
			state.elements[1].filter( e => e !== undefined ).map( e => [ e.serial, e.state.position[0] ] )
		);
		advanceParticleGraph( state, graph, tick / 20, matrix, table );
		const rows = state.elements[1].flatMap( e => e?.alive ? [ e ] : [] );
		const executed = new Map();
		for ( const element of rows ) {
			const previous = rows.filter( e => e.parent === element.parent && e.serial < element.serial )
				.sort( ( a, b ) => b.serial - a.serial )[0];
			// A predecessor in an earlier slot has already executed this tick;
			// a predecessor in a later slot still carries last tick's position.
			const predecessorX = previous && (executed.get( previous.serial ) ?? old.get( previous.serial ));
			const expected = previous ? predecessorX + 1 : old.get( element.serial ) ?? 0;
			assert.equal( element.state.position[0], expected );
			executed.set( element.serial, expected );
		}
		if ( rows.some( ( e, i ) => i > 0 && e.serial < rows[i - 1].serial ) ) recycled = true;
		assert.equal( new Set( rows.map( e => e.parent ) ).size, 2 );
	}
	assert.ok( recycled );
});

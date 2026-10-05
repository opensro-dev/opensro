/*
===========================================================================

particle-graph.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const load = async file => {
	return import( sourceFileUrl( file ).href );
};
const { createParticleGraph, advanceParticleGraph, particleElementMatrix } = await load(
	"src/engine/foundation/animation/particle-graph.ts"
);
const { particleProgram } = await load( "src/engine/foundation/animation/particle-program.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
const table = new Float32Array( [ .25, .5, .75 ] );
const emitter = ( overrides = {} ) => ({
	parent: -1,
	parents: [ 0 ],
	births: [ 0 ],
	frames: 10,
	keepMatrix: true,
	keepOrigin: true,
	positionDepth: 0,
	matrixDepth: 0,
	velocityDepth: 0,
	followDepth: 0,
	scales: [],
	positions: [],
	rotations: [],
	...overrides
});
const vector = ( name, value, flags = 0, frames = [ 0 ] ) => ({ name, value, flags, frames });
function run( graph, times, transform = identity() ) {
	const state = createParticleGraph( graph, 0 );
	for ( const time of times ) advanceParticleGraph( state, graph, time, transform, table );
	return state;
}
test("invisible moving parents pass their current position and velocity to each child instance", () => {
	const graph = [
		emitter( {
			births: [ 0, 2 ],
			parents: [ 0, 0 ],
			program: { vectors: [ vector( "SetVelocity", [ 2, 0, 0 ] ) ] }
		} ),
		emitter( { parent: 0, parents: [ 0, 1 ], births: [ 2, 4 ] } )
	];
	const state = run( graph, [ .2 ] );
	assert.deepEqual( state.elements[0].map( e => e.state.position ), [ [ 8, 0, 0 ], [ 4, 0, 0 ] ] );
	assert.deepEqual( state.elements[1].map( e => e.state.position ), [ [ 10, 0, 0 ], [ 6, 0, 0 ] ] );
	assert.deepEqual( state.elements[1][0].state.velocity, [ 2, 0, 0 ] );
});
test("every partition of four simulation frames reproduces a delayed first draw, including children of expired parents", () => {
	const graph = [
		emitter( { frames: 3, program: { vectors: [ vector( "SetVelocity", [ 2, 0, 0 ] ) ] } } ),
		emitter( { parent: 0, births: [ 2 ], frames: 8 } )
	];
	// The graph's scratch is working storage, not state: compare the rest.
	const graphState = ( { scratch, ...state } ) => state;
	const expected = run( graph, [ .2 ] );
	for ( let mask = 0; mask < 16; mask++ ) {
		const times = [];
		for ( let n = 0; n < 4; n++ ) if ( mask & (1 << n) ) times.push( n / 20 );
		times.push( .2 );
		assert.deepEqual( graphState( run( graph, times ) ), graphState( expected ) );
	}
	assert.equal( expected.elements[1][0].state.position[0], 10 );
});
test("follow-position inheritance subtracts parent velocity rather than integrating it twice", () => {
	const graph = [
		emitter( { program: { vectors: [ vector( "SetVelocity", [ 2, 0, 0 ] ) ] } } ),
		emitter( { parent: 0, births: [ 1 ], followDepth: 1 } )
	];
	const state = run( graph, [ .2 ] );
	assert.deepEqual( state.elements[1][0].state.position, [ 8, 0, 0 ] );
	assert.deepEqual( state.elements[1][0].state.velocity, [ 0, 0, 0 ] );
});
test("velocity uses the selected affine basis once, and force honors its frame schedule", () => {
	const matrix = identity();
	matrix.set( [ 0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0 ] );
	matrix[12] = 100;
	const graph = [
		emitter( {
			program: {
				vectors: [ vector( "SetVelocity", [ 2, 0, 0 ], 2 ), vector( "Force", [ 0, 1, 0 ], 0, [ 1, 3 ] ) ]
			}
		} )
	];
	const state = run( graph, [ .2 ], matrix ).elements[0][0].state;
	assert.deepEqual( state.position, [ 100, 4, -8 ] );
	assert.deepEqual( state.velocity, [ 0, 2, -2 ] );
});
test("position modes retain additive, parent and previous-sibling semantics", () => {
	for ( const flag of [ 0, 1, 2, 3, 4, 5, 6, 7 ] ) {
		const graph = [
			emitter( {
				births: [ 0, 0 ],
				parents: [ 0, 0 ],
				program: { vectors: [ vector( "SetPosition", [ 3, 0, 0 ], flag ) ] }
			} )
		];
		const state = run( graph, [ 0 ] );
		assert.equal( state.elements[0][0].state.position[0], flag === 2 || flag === 5 ? 0 : 3 );
		assert.equal( state.elements[0][1].state.position[0], 3 );
	}
});
test("sphere positions land on the element itself, its parent or its previous sibling", () => {
	// Two siblings drift at 1 unit per frame under a root at x 10; a zero-radius
	// SetSpherePos at age 2 moves each onto the base its flag selects.
	const expected = [ [ 14, 13 ], [ 12, 11 ], [ 14, 14 ] ];
	for ( const flags of [ 0, 1, 2 ] ) {
		const sphere = particleProgram( [ {
			name: "SetSpherePos",
			flags,
			parameter: { kind: "Vector", value: [ 0, 0, 0 ] }
		} ] );
		const graph = [
			emitter( {
				births: [ 0, 1 ],
				parents: [ 0, 0 ],
				commands: [
					{
						name: "SetVelocity",
						frames: [ 0 ],
						program: { vectors: [ vector( "SetVelocity", [ 1, 0, 0 ] ) ] }
					},
					{ name: "SetSpherePos", frames: [ 2 ], program: sphere }
				]
			} )
		];
		const root = identity();
		root[12] = 10;
		const state = run( graph, [ .05, .1, .15, .2 ], root );
		assert.deepEqual( state.elements[0].map( e => e.state.position[0] ), expected[flags] );
	}
	assert.throws(
		() =>
			particleProgram( [ {
				name: "SetSpherePos",
				flags: 3,
				parameter: { kind: "Vector", value: [ 1, 1, 1 ] }
			} ] ),
		/sphere position flags/
	);
});
test("region rebasing shifts retained parents and children together without changing velocity", () => {
	const graph = [
		emitter( { program: { vectors: [ vector( "SetVelocity", [ 1, 0, 0 ] ) ] } } ),
		emitter( { parent: 0, births: [ 1 ] } )
	];
	const state = run( graph, [ .1 ] );
	const old = state.elements.map( rows => rows[0].state.position[0] );
	const matrix = identity();
	matrix[12] = -1920;
	advanceParticleGraph( state, graph, .1, matrix, table, Infinity, -1920, 0 );
	assert.deepEqual( state.elements.map( rows => rows[0].state.position[0] ), old.map( x => x - 1920 ) );
});
test("vector commands are decoded with the native first/period/end schedule", () => {
	const program = particleProgram(
		[ {
			name: "Force",
			flags: 0,
			byte1: 0,
			start: 1,
			end: 2,
			step: 5,
			parameter: { kind: "Vector", value: [ 0, -1, 0 ] }
		} ],
		[],
		10
	);
	assert.deepEqual( program.vectors[0].frames, [ 1, 3, 5 ] );
});

test("local angular motion propagates to descendants while shape spin remains visual", () => {
	const quarter = [ 0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1 ];
	for ( const shape of [ false, true ] ) {
		const graph = [
			emitter( {
				localMotion: !shape,
				shapeMotion: shape,
				commands: [ {
					name: shape ? "SetShapeRotVel" : "SetRVelocity",
					frames: [ 0 ],
					program: shape ? { spin: quarter } : { rVelocity: quarter }
				} ]
			} ),
			emitter( { parent: 0, births: [ 1 ], program: { vectors: [ vector( "SetVelocity", [ 1, 0, 0 ], 2 ) ] } } )
		];
		const state = run( graph, [ .05 ] );
		assert.deepEqual( state.elements[1][0].state.velocity, shape ? [ 1, 0, 0 ] : [ 0, 0, -1 ] );
	}
});
test("scheduled commands preserve source order and never fire before their first frame", () => {
	const graph = [
		emitter( {
			commands: [ {
				name: "SetVelocity",
				frames: [ 2 ],
				program: { vectors: [ vector( "SetVelocity", [ 2, 0, 0 ] ) ] }
			}, { name: "Force", frames: [ 2 ], program: { vectors: [ vector( "Force", [ 3, 0, 0 ] ) ] } } ]
		} )
	];
	const state = run( graph, [ .05 ] );
	assert.equal( state.elements[0][0].state.velocity[0], 0 );
	advanceParticleGraph( state, graph, .1, identity(), table );
	assert.equal( state.elements[0][0].state.velocity[0], 5 );
	assert.equal( state.elements[0][0].state.position[0], 0 );
	advanceParticleGraph( state, graph, .15, identity(), table );
	assert.equal( state.elements[0][0].state.position[0], 5 );
});
test("signed attraction executes after integration and positive strength pushes away from source", () => {
	const graph = [
		emitter( {
			commands: [ {
				name: "SetPosition",
				frames: [ 0 ],
				program: { vectors: [ vector( "SetPosition", [ 10, 0, 0 ] ) ] }
			}, { name: "Attraction", frames: [ 0 ], program: { attraction: 1 } } ]
		} )
	];
	const state = run( graph, [ 0 ] );
	assert.deepEqual( state.elements[0][0].state.position, [ 10, 0, 0 ] );
	assert.deepEqual( state.elements[0][0].state.velocity, [ 1, 0, 0 ] );
	advanceParticleGraph( state, graph, .05, identity(), table );
	assert.equal( state.elements[0][0].state.position[0], 11 );
});
test("an expired parent contributes no repeated displacement to a surviving follower", () => {
	const graph = [
		emitter( { frames: 3, program: { vectors: [ vector( "SetVelocity", [ 2, 0, 0 ] ) ] } } ),
		emitter( { parent: 0, births: [ 1 ], followDepth: 1 } )
	];
	const state = run( graph, [ .25 ] );
	assert.deepEqual( state.elements[1][0].state.position, [ 4, 0, 0 ] );
});

test("NormalTimeLoop wraps command age without resetting accumulated motion or child identity", () => {
	const graph = [
		emitter( {
			loop: true,
			frames: 3,
			commands: [ { name: "Force", frames: [ 0 ], program: { vectors: [ vector( "Force", [ 1, 0, 0 ] ) ] } } ]
		} ),
		emitter( { parent: 0, births: [ 1 ], frames: 2 } )
	];
	const state = run( graph, [ 0 ] );
	const parent = state.elements[0][0];
	advanceParticleGraph( state, graph, .4, identity(), table );
	assert.equal( state.elements[0][0], parent );
	assert.equal( parent.state.position[0], 15 );
	assert.equal( parent.state.velocity[0], 3 );
	assert.equal( parent.state.frame, 2 );
	assert.equal( state.elements[1].length, 1 );
	assert.equal( state.elements[1][0].parent, parent );
});

test("retirement returns emitter capacity, and a looping root retains its children across clock wrap", () => {
	const emission = { start: 0, duration: 4, period: 1, limit: 1, rate: 1 };
	const graph = [
		emitter( { frames: 4, emission: { ...emission, duration: 1 }, capacity: 2 } ),
		emitter( { parent: 0, frames: 2, emission, capacity: 2 } )
	];
	const history = createParticleGraph( graph, 0 );
	let root, previous;
	const births = [];
	for ( let tick = 0; tick < 24; tick++ ) {
		advanceParticleGraph( history, graph, tick / 20, identity(), table, Infinity, 0, 0, true );
		root ??= history.elements[0][0];
		assert.equal( history.elements[0][0], root );
		const active = history.elements[1].filter( e => e?.alive );
		assert.equal( active.length, 1 );
		if ( active[0] !== previous ) {
			births.push( active[0].born );
			previous = active[0];
		}
		assert.ok( history.elements[1].length <= 2 );
	}
	assert.deepEqual( births, [ 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22 ] );
	advanceParticleGraph( history, graph, 1.5, identity(), table, 1.2, 0, 0, false );
	assert.equal( history.elements.flat().filter( e => e?.alive ).length, 0 );
});
test("live descendants retain an expired parent while newly emitted parents get distinct identities", () => {
	const graph = [
		emitter( { frames: 1, emission: { start: 0, duration: 10, period: 1, limit: 1, rate: 1 }, capacity: 5 } ),
		emitter( {
			parent: 0,
			frames: 3,
			emission: { start: 0, duration: 1, period: 1, limit: 1, rate: 1 },
			capacity: 5
		} )
	];
	const state = run( graph, [ .1 ] );
	const children = state.elements[1].filter( e => e?.alive );
	assert.equal( children.length, 3 );
	assert.equal( new Set( children.map( e => e.parent ) ).size, 3 );
	assert.deepEqual( children.map( e => e.parent.alive ), [ false, false, true ] );
});

test("emission sibling identity follows birth serial through repeated slot retirement and reuse", () => {
	const graph = [
		emitter( {
			frames: 4,
			keepMatrix: false,
			capacity: 6,
			emission: { start: 0, duration: 80, period: 1, limit: 6, rate: 2 },
			program: { vectors: [ vector( "SetPosition", [ 1, 0, 0 ], 2 ) ] }
		} )
	];
	const state = createParticleGraph( graph, 0 );
	let recycled = false;
	for ( let tick = 0; tick < 40; tick++ ) {
		advanceParticleGraph( state, graph, tick / 20, identity(), table );
		const alive = state.elements[0].filter( e => e?.alive ),
			ordered = [ ...alive ].sort( ( a, b ) => a.serial - b.serial );
		if ( alive.some( ( e, i ) => e !== ordered[i] ) ) recycled = true;
		for ( let i = 0; i < ordered.length; i++ ) {
			if ( ordered[i].born === tick ) {
				assert.equal( ordered[i].state.position[0], i ? ordered[i - 1].state.position[0] + 1 : 0 );
			}
		}
	}
	assert.ok( recycled, "storage order must differ from sibling order" );
});

test("sub-tick drawing predicts motion without changing simulation, births or random state", () => {
	const graph = [ emitter( { program: { vectors: [ vector( "SetVelocity", [ 3, 0, 0 ] ) ] } } ) ];
	const state = run( graph, [ .1 ] ),
		element = state.elements[0][0],
		before = JSON.stringify( {
			position: element.state.position,
			previous: element.previousPosition,
			births: state.births
		} ),
		index = state.index,
		out = identity();
	const xs = [];
	for ( const fraction of [ 0, 1 / 3, 2 / 3 ] ) {
		particleElementMatrix( element, out, 0, fraction );
		xs.push( out[12] );
	}
	assert.deepEqual( xs, [ 6, 7, 8 ] );
	assert.equal(
		JSON.stringify( {
			position: element.state.position,
			previous: element.previousPosition,
			births: state.births
		} ),
		before
	);
	assert.equal( state.index, index );
	advanceParticleGraph( state, graph, .15, identity(), table );
	particleElementMatrix( element, out, 0 );
	assert.equal( out[12], 9 );
	advanceParticleGraph( state, graph, .15, identity(), table, Infinity, 1920, 0 );
	particleElementMatrix( element, out, 0, .5 );
	assert.equal( out[12], 1930.5 );
});

test("a holder turn between ticks reaches the elements linked to it on the next tick", () => {
	// A matrix-linked root emitter (EFP node int1 = 1, CEFElement_ResolveLinkAncestors).
	const graph = [ emitter( { frames: 40, matrixDepth: 1 } ) ];
	const down = new Float32Array( [ 1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1 ] );
	const level = new Float32Array( [ 0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1 ] );
	const state = createParticleGraph( graph, 0 );
	advanceParticleGraph( state, graph, 0, down, table );
	// Render frames outnumber 20 Hz ticks: the holder turns on a call that
	// runs no tick, and the next tick must still carry the whole turn.
	advanceParticleGraph( state, graph, .01, level, table );
	advanceParticleGraph( state, graph, .02, level, table );
	advanceParticleGraph( state, graph, .05, level, table );
	const z = Array.from( state.elements[0][0].matrix.subarray( 8, 11 ), v => Math.round( v * 1e6 ) / 1e6 + 0 );
	assert.deepEqual( z, [ 1, 0, 0 ] );
});

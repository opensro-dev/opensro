/*
===========================================================================

palette-bindings.test.mjs - tests for animation-pose.ts, palette-bindings.ts,
character-budget.ts, character-render-plan.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { paletteBindings } = await import( "../../src/engine/foundation/animation/palette-bindings.ts" );
const { characterBatchBytes, characterPoseBytes, createCharacterPlanCaches } = await import(
	"../../src/engine/foundation/animation/character-budget.ts"
);
const { createCharacterRenderPlan } = await import( "../../src/engine/foundation/animation/character-render-plan.ts" );
const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
test("binding reuse requires identical ordered joints and matrix bits", () => {
	const base = { joints: [ 0, 1 ], inverseBind: Float32Array.of( ...identity(), ...identity() ) };
	const copied = { ...base, inverseBind: base.inverseBind.slice() },
		reordered = { ...base, joints: [ 1, 0 ] },
		shifted = { ...base, inverseBind: base.inverseBind.slice() },
		signed = { ...base, inverseBind: base.inverseBind.slice() };
	shifted.inverseBind[12] = 3;
	signed.inverseBind[1] = -0;
	const model = { primitives: [ base, copied, reordered, shifted, signed ] }, bindings = paletteBindings( model );
	assert.equal( bindings.get( copied ), base );
	for ( const p of [ reordered, shifted, signed ] ) assert.equal( bindings.get( p ), p );
	const independent = paletteBindings( model );
	assert.equal( independent.get( copied ), base );
	assert.equal( independent.get( shifted ), shifted );
});
test("a binding hash collision cannot alias different transforms", () => {
	const base = { joints: [ 0 ], inverseBind: identity() }, collision = { joints: [ 0 ], inverseBind: identity() };
	const a = new Uint32Array( base.inverseBind.buffer ),
		b = new Uint32Array( collision.inverseBind.buffer ),
		p = 16777619,
		afterJoint = Math.imul( 2166136261, p );
	b[0] = 0x40000000;
	b[1] = (Math.imul( afterJoint ^ b[0], p ) ^ Math.imul( afterJoint ^ a[0], p ) ^ a[1]) >>> 0;
	assert.ok( collision.inverseBind.every( Number.isFinite ) );
	const bindings = paletteBindings( { primitives: [ base, collision ] } );
	assert.equal( bindings.get( collision ), collision );
});
test("shared binding palettes equal independent poses through animation, divergent binds and output writes", () => {
	const base = { joints: [ 0, 1 ], inverseBind: Float32Array.of( ...identity(), ...identity() ) };
	const primitives = [ base, { ...base, inverseBind: base.inverseBind.slice() }, { ...base, joints: [ 1, 0 ] }, {
		...base,
		inverseBind: base.inverseBind.slice()
	} ];
	primitives[3].inverseBind[12] = 7;
	const model = {
		nodes: [ 0, 1 ].map( ( _, i ) => ({
			name: String( i ),
			parent: i - 1,
			translation: [ i, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		}) ),
		primitives,
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 1,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 0, 2, 4, 6 )
			} ]
		} ]
	};
	const shared = createCharacterPose( model ),
		other = createCharacterPose( model ),
		oracles = primitives.map( p => createCharacterPose( { ...model, primitives: [ p ] } ) );
	for ( let frame = 0; frame < 120; frame++ ) {
		const time = frame * .013;
		shared.evaluate( "move", time );
		other.evaluate( "move", time + .2 );
		for ( let i = 0; i < primitives.length; i++ ) {
			const p = primitives[(i + frame) % primitives.length], oracle = oracles[primitives.indexOf( p )];
			oracle.evaluate( "move", time );
			const a = new Float32Array( 64 ), b = new Float32Array( 64 );
			shared.palette( p, a, 16 );
			oracle.palette( p, b, 16 );
			assert.deepEqual( a, b );
			a.fill( 99 );
			other.palette( p, a, 16 );
			shared.palette( p, a, 16 );
			assert.deepEqual( a.subarray( 16, 48 ), b.subarray( 16, 48 ) );
		}
	}
});
// A body of five parts on one 44-joint skin, each part with its own copy of
// the inverse binds, as a decoded glTF skin delivers them.
const skinned = ( parts, joints ) => {
	const binds = Float32Array.from( { length: joints * 16 }, ( _, i ) => i % 5 ? 0 : 1 );
	return {
		nodes: Array.from( { length: joints }, ( _, i ) => ({
			name: "n" + i,
			parent: i - 1,
			translation: [ 0, i, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		}) ),
		images: [],
		clips: [],
		primitives: Array.from( { length: parts }, () => ({
			joints: Array.from( { length: joints }, ( _, i ) => i ),
			inverseBind: binds.slice(),
			geometry: { positions: new Float32Array( 9 ), indices: new Uint32Array( 3 ) }
		}) )
	};
};
const paletteCharge = model => characterPoseBytes( model ) - characterPoseBytes( { ...model, primitives: [] } );
test("a pose is charged one palette per distinct binding, not one per primitive", () => {
	const body = skinned( 5, 44 );
	assert.equal( paletteCharge( body ), 44 * 64 );
	new Uint32Array( body.primitives[3].inverseBind.buffer )[7] ^= 1;
	assert.equal( paletteCharge( body ), 2 * 44 * 64 );
	const apart = skinned( 3, 4 );
	apart.primitives.forEach( ( p, i ) => p.joints = p.joints.map( j => (j + i) % 4 ) );
	assert.equal( paletteCharge( apart ), 3 * 4 * 64 );
});
test("the pose charge is the palette bytes a pose holds once every primitive is asked", () => {
	const body = skinned( 5, 44 );
	body.primitives.push( { joints: [ 2, 1 ], inverseBind: Float32Array.of( ...identity(), ...identity() ) } );
	const pose = createCharacterPose( body ), out = new Float32Array( 44 * 16 ), original = globalThis.Float32Array;
	pose.socket( "n0" );
	let held = 0;
	globalThis.Float32Array = new Proxy( original, {
		construct( target, args ) {
			const array = new target( ...args );
			held += array.byteLength;
			return array;
		}
	} );
	try {
		for ( let round = 0; round < 2; round++ ) for ( const p of body.primitives ) pose.palette( p, out );
	} finally {
		globalThis.Float32Array = original;
	}
	assert.equal( held, paletteCharge( body ) );
});
test("re-planning a body, or the body extended by a clip, computes its bindings once", () => {
	const caches = createCharacterPlanCaches(), store = caches.bindings.set.bind( caches.bindings );
	let computed = 0;
	caches.bindings.set = ( primitives, bindings ) => {
		computed++;
		return store( primitives, bindings );
	};
	const body = skinned( 5, 44 ), clip = { name: "stand", duration: 1, channels: [] };
	assert.equal( createCharacterRenderPlan( body, caches ).poseBytes, characterPoseBytes( body ) );
	assert.equal( createCharacterRenderPlan( body, caches ).poseBytes, characterPoseBytes( body ) );
	assert.equal( computed, 1 );
	// Admitting a clip re-plans { ...model, clips }: the same primitive list.
	const extended = { ...body, clips: [ clip ] };
	assert.equal( createCharacterRenderPlan( extended, caches ).poseBytes, characterPoseBytes( extended ) );
	assert.equal( computed, 1 );
});
test("re-planning a body, a second wearer's assembly, or the body extended by a clip, computes its cloth slots once", () => {
	const caches = createCharacterPlanCaches(), store = caches.clothSlots.set.bind( caches.clothSlots );
	let computed = 0;
	caches.clothSlots.set = ( cloth, slots ) => {
		computed++;
		return store( cloth, slots );
	};
	// A three-vertex cloth piece: one free vertex, one read pin, one unread pin.
	const piece = {
		name: "part:EQ0",
		node: 0,
		image: -1,
		joints: [ 0 ],
		inverseBind: identity(),
		geometry: {
			positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
			indices: Uint32Array.of( 0, 1, 2 ),
			transform: identity(),
			joints: new Uint32Array( 12 ),
			weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
		},
		cloth: {
			mobility: [ 1, 0, 0 ],
			pins: [ 0, 1, 1 ],
			constraints: [ [ 0, 1, 1 ] ],
			order: [ 0 ],
			force: null,
			gravity: 1,
			gravityMobility: 0,
			windMobility: 0,
			damping: .9,
			windPeriod: 1
		}
	};
	const nodes = [ {
		name: "n0",
		parent: -1,
		translation: [ 0, 0, 0 ],
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	} ];
	const body = { nodes, images: [], clips: [], primitives: [ piece ] };
	const charged = model => createCharacterRenderPlan( model, caches ).batchBytes( 1 );
	assert.equal( charged( body ), characterBatchBytes( body, 1 ) );
	assert.equal( charged( body ), characterBatchBytes( body, 1 ) );
	// Another wearer's assembly spreads the part's primitive: the same cloth data.
	const wearer = { ...body, primitives: [ { ...piece, joints: [ 0 ] } ] };
	assert.equal( charged( wearer ), characterBatchBytes( wearer, 1 ) );
	// Admitting a clip re-plans { ...model, clips }: the same primitive list.
	const extended = { ...body, clips: [ { name: "stand", duration: 1, channels: [] } ] };
	assert.equal( charged( extended ), characterBatchBytes( extended, 1 ) );
	assert.equal( computed, 1 );
});

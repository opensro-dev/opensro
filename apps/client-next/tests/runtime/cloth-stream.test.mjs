/*
===========================================================================

cloth-stream.test.mjs - a GPU-skinned cloth draws what the CPU stream drew

createClothVertices skins every pin on the CPU each frame and rewrites the
whole vertex stream. createClothStream leaves the skinning to the GPU and
writes only what the solver owns. Composed the vertex shader's way (the
stream where its flags say so, else the vertex skinned), with the CPU's own
skinning arithmetic, every frame's positions and normals must equal the
CPU stream's bit for bit, through option flips, restarts, frame debt,
stalls, gusts and every pin kind, with the same random calls.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { clothVertexCase } from "../helpers/cloth-vertex-cases.mjs";
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );
const { createClothStream, solverPins, CLOTH_STREAM_WORDS, STREAM_POSITION, STREAM_NORMAL } = await import(
	"../../src/engine/foundation/animation/cloth-stream.ts"
);
const { skinClothVertex } = await import( "../../src/engine/foundation/animation/cloth-skin.ts" );

/*
================
drawn

Vertex i's position and normal as the skinned vertex shader composes them.
================
*/
function drawn( geometry, palette, stream, i ) {
	const floats = new Float32Array( stream.buffer ), entry = i * CLOTH_STREAM_WORDS, flags = stream[entry + 3];
	const skin = new Float64Array( 6 );
	skinClothVertex( geometry, palette, i, skin );
	return Float32Array.from( { length: 6 }, ( _, k ) =>
		k < 3 ?
			flags & STREAM_POSITION ? floats[entry + k] : skin[k] :
			flags & STREAM_NORMAL ?
			floats[entry + 4 + k - 3] :
			skin[k] );
}

for ( const normals of [ false, true ] ) {
	test(`the stream skinned the CPU's way draws every frame bit for bit (authored normals ${normals})`, () => {
		const reference = clothVertexCase( true, normals ), mirror = clothVertexCase( true, normals );
		const cpu = createClothVertices( reference.primitive, reference.random );
		const gpu = createClothStream( mirror.primitive, mirror.random );
		const geometry = mirror.primitive.geometry, count = geometry.positions.length / 3;
		const stream = new Uint32Array( count * CLOTH_STREAM_WORDS );
		let idle = 0, restarts = 0, wasEnabled = true;
		for ( let frame = 0; frame < reference.frames; frame++ ) {
			const a = reference.input( frame ), b = mirror.input( frame );
			const expected = cpu.update( a.palette, a.seconds, a.enabled, a.motion );
			let asked = 0;
			const write = gpu.update( () => (asked++, b.palette), b.seconds, b.enabled, b.motion );
			assert.ok( asked <= 1, "one palette a frame at most" );
			if ( write ) stream.set( write.entries, write.first * CLOTH_STREAM_WORDS );
			else if ( b.enabled ) {
				idle++;
				// The GPU samples the skeleton: between steps the CPU builds none.
				assert.equal( asked, 0, `frame ${frame}: no step, no CPU palette` );
			}
			if ( b.enabled && !wasEnabled ) restarts++;
			wasEnabled = b.enabled;
			for ( let i = 0; i < count; i++ ) {
				const actual = drawn( geometry, b.palette, stream, i );
				for ( let k = 0; k < 6; k++ ) {
					assert.ok(
						Object.is( actual[k], expected[i * 14 + k] ),
						`frame ${frame}, vertex ${i}, ${k < 3 ? "position" : "normal"} ${k % 3}`
					);
				}
			}
		}
		assert.equal( mirror.calls(), reference.calls(), "the same random calls" );
		assert.ok( idle > 0, "frames without a due step write nothing" );
		assert.ok( restarts > 0, "the case restarts the simulation" );
	});
}

test("a cloth stream refuses a mesh the GPU cannot skin", () => {
	const unskinned = clothVertexCase( false, false );
	assert.throws( () => createClothStream( unskinned.primitive, unskinned.random ), /skinned mesh/ );
});

// A joint only the unread pins of withUnreadPins hang from.
const UNREAD_JOINT = 9;
/*
================
withUnreadPins

The case with every twelfth vertex (a pin 1) taken off every constraint and
hung from UNREAD_JOINT alone, so no solver step reads it; the palette grows
to that joint.
================
*/
function withUnreadPins( base ) {
	const { geometry, cloth } = base.primitive, count = geometry.positions.length / 3;
	const unread = i => i % 12 === 0;
	const kept = cloth.constraints.map( ( constraint, index ) => [ constraint, index ] ).filter( ( [constraint] ) =>
		!unread( constraint[0] ) && !unread( constraint[1] )
	);
	const renumbered = new Map( kept.map( ( [, index], at ) => [ index, at ] ) );
	const data = {
		...cloth,
		constraints: kept.map( ( [constraint] ) => constraint ),
		order: cloth.order.filter( index => renumbered.has( index ) ).map( index => renumbered.get( index ) )
	};
	const joints = geometry.joints.slice(), weights = geometry.weights.slice();
	for ( let i = 0; i < count; i++ ) {
		if ( !unread( i ) ) continue;
		joints.set( [ UNREAD_JOINT, 0, 0, 0 ], i * 4 );
		weights.set( [ 1, 0, 0, 0 ], i * 4 );
	}
	return {
		...base,
		primitive: { geometry: { ...geometry, joints, weights }, cloth: data },
		input( frame ) {
			const source = base.input( frame );
			const palette = Float32Array.from(
				{ length: (UNREAD_JOINT + 1) * 16 },
				( _, i ) => Math.sin( i * .17 + frame * .031 )
			);
			return { ...source, palette };
		}
	};
}

test("a step re-anchors only the pins the solver reads, and the stream still draws the CPU's way", () => {
	const reference = withUnreadPins( clothVertexCase( true, true ) ),
		mirror = withUnreadPins( clothVertexCase( true, true ) );
	const read = new Set( solverPins( mirror.primitive.cloth ) );
	assert.ok( [ ...read ].every( i => i % 12 !== 0 ), "no unread pin is listed" );
	assert.ok( read.size > 0 && read.size < mirror.primitive.cloth.pins.filter( pin => pin !== 0 ).length );
	const cpu = createClothVertices( reference.primitive, reference.random );
	const gpu = createClothStream( mirror.primitive, mirror.random );
	const geometry = mirror.primitive.geometry, count = geometry.positions.length / 3;
	const stream = new Uint32Array( count * CLOTH_STREAM_WORDS );
	let readsUnread = 0;
	for ( let frame = 0; frame < reference.frames; frame++ ) {
		const a = reference.input( frame ), b = mirror.input( frame );
		const expected = cpu.update( a.palette, a.seconds, a.enabled, a.motion );
		// Count the reads of the joint only unread pins use, past the first frame.
		const watched = new Proxy( b.palette, {
			get( target, key ) {
				const index = typeof key === "string" ? Number( key ) : NaN;
				if ( frame > 0 && Math.floor( index / 16 ) === UNREAD_JOINT ) readsUnread++;
				const value = Reflect.get( target, key );
				return typeof value === "function" ? value.bind( target ) : value;
			}
		} );
		const write = gpu.update( () => watched, b.seconds, b.enabled, b.motion );
		if ( write ) stream.set( write.entries, write.first * CLOTH_STREAM_WORDS );
		for ( let i = 0; i < count; i++ ) {
			const actual = drawn( geometry, b.palette, stream, i );
			for ( let k = 0; k < 6; k++ ) {
				assert.ok( Object.is( actual[k], expected[i * 14 + k] ), `frame ${frame}, vertex ${i}, ${k}` );
			}
		}
	}
	assert.equal( mirror.calls(), reference.calls(), "the same random calls" );
	assert.equal( readsUnread, 0, "the CPU never skins a pin no step reads" );
});

/*
===========================================================================

cloth-stream.test.mjs - a GPU-skinned cloth draws what the CPU stream drew

createClothVertices skins every pin on the CPU each frame and rewrites the
whole vertex stream. createClothStream leaves the skinning to the GPU and
writes only what the solver owns, one entry a slot. Composed the vertex
shader's way (a vertex's slot, then the stream where its flags say so, else
the vertex skinned), with the CPU's own skinning arithmetic, every frame's
positions and normals must equal the CPU stream's bit for bit, through
option flips, off stretches and frames out of view, restarts (which anchor
from the palette the last off frame kept), frame debt, stalls, gusts and
every pin kind, with the same random calls.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { clothVertexCase, UNREAD_JOINT, withOffStretches, withUnreadPins } from "../helpers/cloth-vertex-cases.mjs";
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );
const { createClothStream, solverPins, CLOTH_STREAM_WORDS, STREAM_POSITION, STREAM_NORMAL, CLOTH_NO_SLOT } =
	await import(
		"../../src/engine/foundation/animation/cloth-stream.ts"
	);
const { skinClothVertex } = await import( "../../src/engine/foundation/animation/cloth-skin.ts" );

/*
================
slotMap

Each vertex's stream slot, or CLOTH_NO_SLOT, as the device's slot map holds it.
================
*/
function slotMap( stream, count ) {
	const map = new Uint32Array( count ).fill( CLOTH_NO_SLOT );
	stream.slots.forEach( ( vertex, slot ) => map[vertex] = slot );
	return map;
}

/*
================
drawn

Vertex i's position and normal as the skinned vertex shader composes them:
its slot from the map, then its stream entry.
================
*/
function drawn( geometry, palette, stream, map, i ) {
	const floats = new Float32Array( stream.buffer ),
		slot = map[i],
		entry = slot * CLOTH_STREAM_WORDS,
		flags = slot === CLOTH_NO_SLOT ? 0 : stream[entry + 3];
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
		const reference = withOffStretches( clothVertexCase( true, normals ) ),
			mirror = withOffStretches( clothVertexCase( true, normals ) );
		const cpu = createClothVertices( reference.primitive, reference.random );
		const gpu = createClothStream( mirror.primitive, mirror.random );
		const geometry = mirror.primitive.geometry, count = geometry.positions.length / 3;
		const stream = new Uint32Array( gpu.slots.length * CLOTH_STREAM_WORDS ), map = slotMap( gpu, count );
		let idle = 0, restarts = 0, rebuilds = 0, wasEnabled = true, kept;
		for ( let frame = 0; frame < reference.frames; frame++ ) {
			const a = reference.input( frame ), b = mirror.input( frame );
			if ( b.absent ) continue;
			const expected = cpu.update( a.palette, a.seconds, a.enabled, a.motion );
			let asked = 0, rebuilt = 0;
			const write = gpu.update(
				{
					current: () => (asked++, b.palette),
					keep: () => (kept = b.palette, true),
					kept: () => (rebuilt++, kept)
				},
				b.seconds,
				b.enabled,
				b.motion
			);
			assert.ok( asked <= 1 && rebuilt <= 1, "one palette of this frame and one kept a frame at most" );
			if ( !b.enabled && !wasEnabled ) assert.equal( asked + rebuilt, 0, `frame ${frame}: a later off frame` );
			if ( rebuilt ) assert.ok( b.enabled && !wasEnabled, `frame ${frame}: only a restart rebuilds` );
			rebuilds += rebuilt;
			if ( write ) stream.set( write.entries, write.first * CLOTH_STREAM_WORDS );
			else if ( b.enabled ) {
				idle++;
				// The GPU samples the skeleton: between steps the CPU builds none.
				assert.equal( asked, 0, `frame ${frame}: no step, no CPU palette` );
			}
			if ( b.enabled && !wasEnabled ) restarts++;
			wasEnabled = b.enabled;
			for ( let i = 0; i < count; i++ ) {
				const actual = drawn( geometry, b.palette, stream, map, i );
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
		assert.ok( rebuilds > 0, "a restart rebuilds a kept palette" );
	});
}

test("a cloth stream refuses a mesh the GPU cannot skin", () => {
	const unskinned = clothVertexCase( false, false );
	assert.throws( () => createClothStream( unskinned.primitive, unskinned.random ), /skinned mesh/ );
});

test("a step re-anchors only the pins the solver reads, and the stream still draws the CPU's way", () => {
	const reference = withUnreadPins( clothVertexCase( true, true ) ),
		mirror = withUnreadPins( clothVertexCase( true, true ) );
	const read = new Set( solverPins( mirror.primitive.cloth ) );
	assert.ok( [ ...read ].every( i => i % 12 !== 0 ), "no unread pin is listed" );
	assert.ok( read.size > 0 && read.size < mirror.primitive.cloth.pins.filter( pin => pin !== 0 ).length );
	const cpu = createClothVertices( reference.primitive, reference.random );
	const gpu = createClothStream( mirror.primitive, mirror.random );
	const geometry = mirror.primitive.geometry, count = geometry.positions.length / 3;
	const stream = new Uint32Array( gpu.slots.length * CLOTH_STREAM_WORDS ), map = slotMap( gpu, count );
	let readsUnread = 0, kept;
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
		const write = gpu.update(
			{
				current: () => watched,
				keep: () => (kept = watched, true),
				kept: () => kept
			},
			b.seconds,
			b.enabled,
			b.motion
		);
		if ( write ) stream.set( write.entries, write.first * CLOTH_STREAM_WORDS );
		for ( let i = 0; i < count; i++ ) {
			const actual = drawn( geometry, b.palette, stream, map, i );
			for ( let k = 0; k < 6; k++ ) {
				assert.ok( Object.is( actual[k], expected[i * 14 + k] ), `frame ${frame}, vertex ${i}, ${k}` );
			}
		}
	}
	assert.equal( mirror.calls(), reference.calls(), "the same random calls" );
	assert.equal( readsUnread, 0, "the CPU never skins a pin no step reads" );
});

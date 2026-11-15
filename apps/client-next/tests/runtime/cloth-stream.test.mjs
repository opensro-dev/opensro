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
const { createClothStream, CLOTH_STREAM_WORDS, STREAM_POSITION, STREAM_NORMAL } = await import(
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
			const write = gpu.update( b.palette, b.seconds, b.enabled, b.motion );
			if ( write ) stream.set( write.entries, write.first * CLOTH_STREAM_WORDS );
			else if ( b.enabled ) idle++;
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

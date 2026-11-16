/*
===========================================================================

cloth-stream-table.test.mjs - real dresses drawn through the device's skin table

A cloth draw's skin table holds the skin entries, a slot map of one entry a
vertex and the cloth stream of one entry a slot (device/geometry.ts). Two
shipped avatar dresses and a synthetic piece whose pins move step through
option flips and restarts; every frame,
each vertex resolved from the table's bytes the vertex shader's way (its
slot from the map, then the stream entry, else the vertex skinned) must
draw what createClothVertices drew, bit for bit, and no write may cover
more than the stream's slots.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" );
const { createGeometryResources } = await import( "../../src/engine/runtime/renderer/device/geometry.ts" );
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );
const { createClothStream, CLOTH_STREAM_WORDS, STREAM_POSITION, STREAM_NORMAL, CLOTH_NO_SLOT } = await import(
	"../../src/engine/foundation/animation/cloth-stream.ts"
);
const { skinClothVertex } = await import( "../../src/engine/foundation/animation/cloth-skin.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
import { clothVertexCase, withUnreadPins } from "../helpers/cloth-vertex-cases.mjs";
globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, STORAGE: 4, VERTEX: 8, INDEX: 16 };
globalThis.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2 };

// The widest span between movable vertices and the most read pins.
const DRESSES = [ "ITEM_MALL_AVATAR_M_DEVIL_WING_DRESS", "ITEM_MALL_AVATAR_M_ANGEL_WING_DRESS" ];
const FRAMES = 720;
const HZ = 60;
const RANDOM_SEED = 0x1188;

/*
================
device

Geometry resources on a fake GPU whose buffers hold the bytes written to
them, with every write to a buffer listed.
================
*/
function device() {
	const writes = [],
		gpu = {
			createBuffer( { size, label } ) {
				return { label, size, bytes: new Uint8Array( size ), destroy() {} };
			},
			createTexture() {
				return {
					createView() {
						return {};
					},
					destroy() {}
				};
			},
			createSampler() {
				return {};
			},
			createBindGroup( { entries } ) {
				return entries;
			},
			pushErrorScope() {},
			popErrorScope() {
				return Promise.resolve( null );
			},
			queue: {
				writeTexture() {},
				writeBuffer( buffer, offset, data, start, length ) {
					buffer.bytes.set( new Uint8Array( data, start, length ), offset );
					writes.push( { buffer, length } );
				}
			}
		};
	// A fake of the few device calls geometry makes, not a whole GPUDevice.
	const resources = /** @type {any} */ (createGeometryResources)(
		gpu,
		() => gpu,
		e => {
			throw e;
		},
		() => ({
			getBindGroupLayout() {
				return {};
			}
		}),
		{ texture() {}, acquire() {}, drop() {} },
		{},
		{},
		{},
		() => ({})
	);
	return { resources, writes };
}

/*
================
dressCase

A shipped dress's cloth piece, as the model decoder admits it, stepped at
60 Hz on a seeded presentation RNG and switched off for a stretch twice, so
the stream restarts the simulation. Each joint swings about y and drifts,
as a pose moves the bones a dress hangs from.
================
*/
function dressCase( code ) {
	const read = path => readPublishedAssetBytesSync( path, CLIENT_PUBLIC_ROOT );
	const roster = JSON.parse( new TextDecoder().decode( read( "/assets/char/roster.json" ) ) );
	const record = Object.values( roster.dress.equipment ).find( r => r.code === code );
	const decoder = createModelDecoder();
	const model = decoder.character( decoder.decode( read( record.bodies.CH_M.glb ) ) );
	const primitive = model.primitives.find( p => p.cloth );
	assert.ok( primitive?.geometry.joints && primitive.geometry.weights, `${code} has a skinned cloth piece` );
	const random = createPresentationRandom( RANDOM_SEED );
	let calls = 0;
	return {
		primitive,
		frames: FRAMES,
		random: () => (calls++, random.range( 0, 32768 )),
		calls: () => calls,
		/*
		================
		input
		================
		*/
		input( frame ) {
			const palette = new Float32Array( primitive.joints.length * 16 );
			for ( let j = 0; j < primitive.joints.length; j++ ) {
				const angle = .25 * Math.sin( frame * .05 + j * .3 ), c = Math.cos( angle ), s = Math.sin( angle );
				palette.set(
					[
						c,
						0,
						-s,
						0,
						0,
						1,
						0,
						0,
						s,
						0,
						c,
						0,
						.1 * Math.sin( frame * .07 ),
						.05 * Math.cos( frame * .03 + j ),
						0,
						1
					],
					j * 16
				);
			}
			return {
				palette,
				seconds: 1000.123 + frame / HZ,
				enabled: frame % 300 < 240,
				motion: { direction: [ 0, 0, 1 ], speed: 0 }
			};
		}
	};
}

/*
================
drawn

Vertex i's position and normal from the draw's skin table bytes, as the
skinned vertex shader composes them: word 0 of its slot map entry (V + i)
names its slot, whose stream entry (2V + slot) says what the solver owns.
================
*/
function drawn( table, geometry, bones, i, skin ) {
	const words = new Uint32Array( table.buffer ), floats = new Float32Array( table.buffer );
	const count = geometry.positions.length / 3, slot = words[(count + i) * CLOTH_STREAM_WORDS];
	const entry = (2 * count + slot) * CLOTH_STREAM_WORDS, flags = slot === CLOTH_NO_SLOT ? 0 : words[entry + 3];
	skinClothVertex( geometry, bones, i, skin );
	return Float32Array.from( { length: 6 }, ( _, k ) =>
		k < 3 ?
			flags & STREAM_POSITION ? floats[entry + k] : skin[k] :
			flags & STREAM_NORMAL ?
			floats[entry + 1 + k] :
			skin[k] );
}

/*
================
drawsThroughTable

Steps reference on createClothVertices and mirror on createClothStream,
writing the stream into the device's table, and checks every frame's every
vertex and every write.
================
*/
function drawsThroughTable( reference, mirror ) {
	const geometry = mirror.primitive.geometry, count = geometry.positions.length / 3;
	const cpu = createClothVertices( reference.primitive, reference.random );
	const gpu = createClothStream( mirror.primitive, mirror.random );
	const slots = gpu.slots.length;
	assert.ok( slots > 0 && slots < count, `${slots} of ${count} vertices have a slot` );
	const { resources, writes } = device();
	const draw = resources.commands.upload( {
		...geometry,
		material: undefined,
		bones: mirror.input( 0 ).palette,
		clothStream: gpu.slots
	} );
	// The fake device's bind group is its entries; binding 6 is the skin table.
	const table = /** @type {any} */ (draw.binding).find( e => e.binding === 6 ).resource.buffer;
	assert.equal( table.size, (2 * count + slots) * CLOTH_STREAM_WORDS * 4 );
	const skin = new Float64Array( 6 );
	let steps = 0, restarts = 0, wasEnabled = true;
	for ( let frame = 0; frame < reference.frames; frame++ ) {
		const a = reference.input( frame ), b = mirror.input( frame );
		const expected = cpu.update( a.palette, a.seconds, a.enabled, a.motion );
		const before = mirror.calls(), written = writes.length;
		const write = gpu.update( () => b.palette, b.seconds, b.enabled, b.motion );
		if ( write ) resources.commands.writeClothStream( draw, write.first, write.entries );
		const own = writes.slice( written ).filter( w => w.buffer === table );
		assert.equal( own.length, write ? 1 : 0, `frame ${frame}: one write a changed stream` );
		for ( const w of own ) {
			assert.ok( w.length <= slots * CLOTH_STREAM_WORDS * 4, `frame ${frame}: ${w.length} bytes` );
		}
		if ( mirror.calls() > before ) steps++;
		if ( b.enabled && !wasEnabled ) restarts++;
		wasEnabled = b.enabled;
		for ( let i = 0; i < count; i++ ) {
			const actual = drawn( table.bytes, geometry, b.palette, i, skin );
			for ( let k = 0; k < 6; k++ ) {
				assert.ok(
					Object.is( actual[k], expected[i * 14 + k] ),
					`frame ${frame}, vertex ${i}, ${k < 3 ? "position" : "normal"} ${k % 3}`
				);
			}
		}
	}
	assert.equal( mirror.calls(), reference.calls(), "the same random calls" );
	assert.ok( steps > 0 && restarts > 0, `${steps} steps, ${restarts} restarts` );
	resources.commands.release( draw );
	resources.dispose();
}

for ( const code of DRESSES ) {
	test(`${code} draws through its slot map and stream as the CPU drew it, writing only its slots`, () => {
		drawsThroughTable( dressCase( code ), dressCase( code ) );
	});
}

test("pins that move, and pins no step reads, draw through the slot map as the CPU drew them", () => {
	// Every twelfth vertex has no slot; pin 2 and mobile pins move off their anchors.
	drawsThroughTable(
		withUnreadPins( clothVertexCase( true, true ) ),
		withUnreadPins( clothVertexCase( true, true ) )
	);
});

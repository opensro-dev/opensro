/*
===========================================================================

cloth-stream-table.test.mjs - real dresses drawn through the device's skin table

A cloth draw's skin table holds the skin entries, a slot map of one entry a
vertex and the cloth stream of one entry a slot (device/geometry.ts). Two
shipped avatar dresses and a synthetic piece whose pins move step through
option flips, off stretches, frames out of view and restarts; every frame,
each vertex resolved from the table's bytes the vertex shader's way (its
slot from the map, then the stream entry, else the vertex skinned) must
draw what createClothVertices drew, bit for bit, no write may cover more
than the stream's slots, and the table must hold every byte, and the
stream make every random call, of the stream as it was before off frames
kept their pose instead of anchoring the cloth.

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
import { clothVertexCase, withOffStretches, withUnreadPins } from "../helpers/cloth-vertex-cases.mjs";
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
60 Hz on a seeded presentation RNG. It goes off for one frame, for 60, for
two, and for 120 of which it is out of view (not drawn) for 40, and comes
back; two frames are 70 ms longer, so it restarts both on a solver step
(frames 260 and 520) and between steps. Each joint swings about y and
drifts, as a pose moves the bones a dress hangs from.
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
			const long = (frame >= 260 ? 1 : 0) + (frame >= 520 ? 1 : 0);
			return {
				palette,
				seconds: 1000.123 + frame / HZ + long * .07,
				enabled: !(frame === 100 || frame >= 200 && frame < 260 || frame >= 300 && frame < 302 ||
					frame >= 400 && frame < 520 || frame === 600),
				absent: frame >= 430 && frame < 470,
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
stream

A cloth stream on its own device table, with palettes that count what
the stream asks for. keeping false runs the stream as it ran before off
frames kept their pose: every off frame anchors from this frame's palette.
================
*/
function stream( source, keeping ) {
	const gpu = createClothStream( source.primitive, source.random );
	const { resources, writes } = device();
	const draw = resources.commands.upload( {
		...source.primitive.geometry,
		material: undefined,
		bones: source.input( 0 ).palette,
		clothStream: gpu.slots
	} );
	// The fake device's bind group is its entries; binding 6 is the skin table.
	const table = /** @type {any} */ (draw.binding).find( e => e.binding === 6 ).resource.buffer;
	const asked = { current: 0, kept: 0 };
	let kept;
	return {
		gpu,
		resources,
		writes,
		draw,
		table,
		asked,
		/*
		================
		update
		================
		*/
		update( input ) {
			const write = gpu.update(
				{
					current: () => (asked.current++, input.palette),
					keep: () => keeping && (kept = input.palette, true),
					kept: () => (asked.kept++, kept)
				},
				input.seconds,
				input.enabled,
				input.motion
			);
			if ( write ) resources.commands.writeClothStream( draw, write.first, write.entries );
			return write;
		}
	};
}

/*
================
drawsThroughTable

Steps reference on createClothVertices, mirror on createClothStream and
before on the stream as it was before off frames kept their pose, each
stream writing into its own device table, and checks every frame: every
vertex mirror draws, every write, and every byte of the two tables. An off
frame after the first asks mirror's palettes for nothing.
================
*/
function drawsThroughTable( reference, mirror, before ) {
	const geometry = mirror.primitive.geometry, count = geometry.positions.length / 3;
	const cpu = createClothVertices( reference.primitive, reference.random );
	const now = stream( mirror, true ), then = stream( before, false );
	const slots = now.gpu.slots.length, { table, writes } = now;
	assert.ok( slots > 0 && slots < count, `${slots} of ${count} vertices have a slot` );
	assert.equal( table.size, (2 * count + slots) * CLOTH_STREAM_WORDS * 4 );
	const skin = new Float64Array( 6 );
	let steps = 0, restarts = 0, kept = 0, wasEnabled = true;
	for ( let frame = 0; frame < reference.frames; frame++ ) {
		const a = reference.input( frame ), b = mirror.input( frame );
		if ( b.absent ) continue;
		const expected = cpu.update( a.palette, a.seconds, a.enabled, a.motion );
		const calls = mirror.calls(), written = writes.length, asked = { ...now.asked };
		const write = now.update( b );
		then.update( before.input( frame ) );
		const own = writes.slice( written ).filter( w => w.buffer === table );
		assert.equal( own.length, write ? 1 : 0, `frame ${frame}: one write a changed stream` );
		for ( const w of own ) {
			assert.ok( w.length <= slots * CLOTH_STREAM_WORDS * 4, `frame ${frame}: ${w.length} bytes` );
		}
		assert.ok(
			now.table.bytes.every( ( byte, i ) => byte === then.table.bytes[i] ),
			`frame ${frame}: the table holds what it held before`
		);
		assert.equal( mirror.calls(), before.calls(), `frame ${frame}: the random calls of before` );
		if ( !b.enabled && !wasEnabled ) {
			assert.deepEqual( now.asked, asked, `frame ${frame}: an off frame after the first asks for no palette` );
		}
		if ( now.asked.kept > asked.kept ) kept++;
		if ( mirror.calls() > calls ) steps++;
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
	assert.ok( steps > 0 && restarts > 0 && kept > 0, `${steps} steps, ${restarts} restarts, ${kept} kept` );
	for ( const s of [ now, then ] ) {
		s.resources.commands.release( s.draw );
		s.resources.dispose();
	}
}

for ( const code of DRESSES ) {
	test(`${code} draws through its slot map and stream as the CPU drew it, writing only its slots`, () => {
		drawsThroughTable( dressCase( code ), dressCase( code ), dressCase( code ) );
	});
}

test("pins that move, and pins no step reads, draw through the slot map as the CPU drew them", () => {
	// Every twelfth vertex has no slot; pin 2 and mobile pins move off their anchors.
	const piece = () => withOffStretches( withUnreadPins( clothVertexCase( true, true ) ) );
	drawsThroughTable( piece(), piece(), piece() );
});

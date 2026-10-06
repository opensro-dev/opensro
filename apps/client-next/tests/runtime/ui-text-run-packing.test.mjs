/*
===========================================================================

ui-text-run-packing.test.mjs - text runs pack the bytes their glyphs would

The UI publishes a label as one text run; the GPU UI packer writes one
record per glyph. Fed runs, it must write exactly the bytes and draw
ranges it writes for the same scene expanded into glyph quads
(expandTextRuns), across retained, replaced and shifted frames.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const { createUiResources } = await import( "../../src/engine/runtime/renderer/device/ui.ts" );
const { createUiText } = await import( "../../src/engine/runtime/ui/text/text.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { resolveTextOverlaps } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );

const FONT = "/assets/fonts/native-ui-font-atlas.png";
const RECORD_FLOATS = 28;

/*
================
fakeDevice

A device whose buffers keep the bytes written to them, so two packers can
be compared byte for byte.
================
*/
function fakeDevice() {
	/** @type {{ bytes: Uint8Array; destroy(): void; }[]} */
	const buffers = [];
	/** @type {{ id: number; }[]} */
	const groups = [];
	/** @type {any} */
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		/*
		================
		createBuffer
		================
		*/
		createBuffer( options ) {
			const buffer = { bytes: new Uint8Array( options.size ), destroy() {} };
			buffers.push( buffer );
			return buffer;
		},
		createSampler: () => ({}),
		createTexture: () => ({ createView: () => ({}), destroy() {} }),
		/*
		================
		createBindGroup
		================
		*/
		createBindGroup() {
			const group = { id: groups.length };
			groups.push( group );
			return group;
		},
		queue: {
			writeTexture() {},
			copyExternalImageToTexture() {},
			/*
			================
			writeBuffer
			================
			*/
			writeBuffer( buffer, offset, data ) {
				buffer.bytes.set( new Uint8Array( data.buffer, data.byteOffset, data.byteLength ), offset );
			}
		}
	};
	return { buffers, groups, device };
}

/*
================
drawShape

Draw ranges with bindings named by creation order, comparable across devices.
================
*/
function drawShape( draws, groups ) {
	return draws.map( d => ({ first: d.first, count: d.count, layer: d.layer, binding: groups.indexOf( d.binding ) }) );
}

test("text runs pack byte-identical records and draws to their expanded glyph quads", async t => {
	// Node has no WebGPU: the packer reads only these usage flags.
	/** @type {any} */
	const gpu = globalThis;
	const saved = [ gpu.GPUBufferUsage, gpu.GPUShaderStage, gpu.GPUTextureUsage ];
	gpu.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	gpu.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
	gpu.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	t.after( () => {
		[gpu.GPUBufferUsage, gpu.GPUShaderStage, gpu.GPUTextureUsage] = saved;
	} );
	const a = fakeDevice(), b = fakeDevice();
	const runs = createUiResources( a.device, "rgba8unorm" ), glyphs = createUiResources( b.device, "rgba8unorm" );
	await runs.ready;
	await glyphs.ready;
	for ( const owner of [ runs, glyphs ] ) {
		for ( const id of [ FONT, "sprite" ] ) {
			owner.texture( id, { data: new Uint8ClampedArray( 16 ), width: 2, height: 2, colorSpace: "srgb" } );
		}
	}
	const atlas = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json" );
	const text = createUiText( {
		available: () => 1,
		request: () => 1,
		take: () => ({
			kind: "bytes",
			id: 1,
			buffer: atlas.buffer.slice( atlas.byteOffset, atlas.byteOffset + atlas.length )
		}),
		cancel() {}
	}, "http://fixture.invalid/" );
	for ( let i = 0; i < 3; i++ ) text.step();
	let seed = 23;
	const random = () => ((seed = (seed * 1103515245 + 12345) >>> 0) / 4294967296);
	const words = [ "Jangan", "Hotan Bazaar", "", "Ox", "Western China Donwhang Gate", "123" ];
	let previous = null, glyphRecords = 0;
	for ( let frame = 0; frame < 600; frame++ ) {
		/** @type {any[]} */
		let quads;
		if ( previous && random() < .3 ) quads = previous;
		else {
			quads = [];
			const count = 1 + Math.floor( random() * 12 );
			for ( let n = 0; n < count; n++ ) {
				if ( random() < .55 ) {
					const x = Math.floor( random() * 400 ) + (random() < .5 ? .5 : 0);
					quads.push( ...text.quads(
						words[Math.floor( random() * words.length )],
						[ x, Math.floor( random() * 300 ), 20 + Math.floor( random() * 200 ), 14 ],
						random() < .5 ? [ 0, 0, 800, 600 ] : [ x - 5, 0, 60 + random() * 100, 600 ],
						[ 1, random(), 1, 1 ],
						{ hAlign: Math.floor( random() * 3 ), overflow: random() < .2 ? "clip" : undefined }
					) );
				} else {
					quads.push( {
						texture: random() < .1 ? "absent" : random() < .5 ? "sprite" : "",
						rect: [ random() * 500, random() * 400, 5 + random() * 50, 5 + random() * 20 ],
						uv: [ 0, 0, 1, 1 ],
						clip: [ 0, 0, 800, 600 ],
						color: [ random(), 1, 1, 1 ],
						...(random() < .2 ? { depth: random() } : {})
					} );
				}
			}
			quads = resolveTextOverlaps( quads );
		}
		previous = quads;
		const expanded = expandTextRuns( quads );
		const drawsA = runs.prepare( { revision: frame, width: 800, height: 600, quads } ),
			drawsB = glyphs.prepare( { revision: frame, width: 800, height: 600, quads: expanded } );
		const used = expanded.length * RECORD_FLOATS * 4;
		glyphRecords += expanded.length;
		assert.deepStrictEqual( drawShape( drawsA, a.groups ), drawShape( drawsB, b.groups ), `draws frame ${frame}` );
		assert.deepStrictEqual(
			a.buffers[0].bytes.subarray( 0, used ),
			b.buffers[0].bytes.subarray( 0, used ),
			`bytes frame ${frame}`
		);
	}
	assert.ok( glyphRecords > 10000, "the random scenes must exercise many glyph records" );
	// The same run object one record later: record r held glyph r of the run and
	// must now hold glyph r - 1. A cache keyed only by quad identity misses this.
	const run = text.quads( "Hotan Bazaar", [ 10, 10, 200, 14 ], [ 0, 0, 800, 600 ], [ 1, 1, 1, 1 ] )[0];
	const plain = {
		texture: "",
		rect: [ 1, 2, 3, 4 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 800, 600 ],
		color: [ 1, 1, 1, 1 ]
	};
	for ( const quads of /** @type {any[][]} */ ([ [ run ], [ plain, run ] ]) ) {
		runs.prepare( { revision: 9000 + quads.length, width: 800, height: 600, quads } );
		const expanded = expandTextRuns( quads );
		glyphs.prepare( { revision: 9000 + quads.length, width: 800, height: 600, quads: expanded } );
		const used = expanded.length * RECORD_FLOATS * 4;
		assert.deepStrictEqual( a.buffers[0].bytes.subarray( 0, used ), b.buffers[0].bytes.subarray( 0, used ) );
	}
});

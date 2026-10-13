/*
===========================================================================

geometry-stage-policy.test.mjs - an unblended draw's pixels are opaque

Native draws into an X8R8G8B8 back buffer, which keeps no alpha: an
alpha-tested surface that survives ALPHAREF 0x80 shows solid. The port's
viewer renders dolls into a transparent target, so the material uniform
tells the shader which draws blend nothing (stagePolicy.y = 1, alpha 1)
and which keep their alpha (blended, faded, decal, sky, lightmap: 0).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";

const { createGeometryResources } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/geometry.ts" ).href
);
const { createRetirement } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/retirement.ts" ).href );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

// struct Material in pipelines.ts: stagePolicy is the sixteenth vec4.
const MATERIAL_BINDING = 2;
const STAGE_POLICY_OPAQUE = 15 * 4 + 1;
const VERTEX_FLOATS = 14;
const INSTANCE_FLOATS = 16;

/*
================
packedOpacity

Uploads one triangle with `material` through a fake device and returns the
stagePolicy.y its material uniform carries.
================
*/
function packedOpacity( material ) {
	const retirement = createRetirement();
	const device = {
		limits: { maxBufferSize: 256 * 1024 * 1024 },
		createBuffer( descriptor ) {
			return { ...descriptor, bytes: new Uint8Array( descriptor.size ), destroy() {} };
		},
		createTexture() {
			return { createView: () => ({}), destroy() {} };
		},
		createSampler() {
			return {};
		},
		createBindGroup( { entries } ) {
			return entries;
		},
		pushErrorScope() {},
		popErrorScope: async () => null,
		queue: {
			writeTexture() {},
			writeBuffer( buffer, offset, data, start, size ) {
				buffer.bytes.set( new Uint8Array( data, start, size ), offset );
			},
			submit() {}
		}
	};
	const resources = createGeometryResources(
		device,
		() => device,
		error => {
			throw error;
		},
		() => ({ getBindGroupLayout: () => ({}) }),
		{ texture: () => ({}), acquire() {}, drop() {} },
		{},
		{},
		{},
		undefined,
		undefined,
		"rgba8unorm",
		undefined,
		retirement.retire
	);
	const draw = resources.commands.upload( {
		positions: new Float32Array( 9 ),
		vertices: new Float32Array( VERTEX_FLOATS * 3 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: new Float32Array( INSTANCE_FLOATS ),
		material
	} );
	const buffer = draw.binding.find( entry => entry.binding === MATERIAL_BINDING ).resource.buffer;
	return new Float32Array( buffer.bytes.buffer )[STAGE_POLICY_OPAQUE];
}

test("an alpha-tested unblended material packs opaque output", () => {
	assert.equal( packedOpacity( { alphaCutoff: 0.5 } ), 1 );
	assert.equal( packedOpacity( {} ), 1 );
});

test("blended, faded, decal, sky and lightmap materials keep their alpha", () => {
	assert.equal( packedOpacity( { blend: true, alphaCutoff: 0.5 } ), 0 );
	assert.equal( packedOpacity( { objectFade: true } ), 0 );
	assert.equal( packedOpacity( { instanceFade: true } ), 0 );
	assert.equal( packedOpacity( { decal: true } ), 0 );
	assert.equal( packedOpacity( { sky: 1 } ), 0 );
	assert.equal( packedOpacity( { lightmap: true } ), 0 );
});

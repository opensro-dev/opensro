/*
===========================================================================

msaa-transient.test.mjs - the MSAA colour's transient usage bit

Chrome 146+ exposes GPUTextureUsage.TRANSIENT_ATTACHMENT; older browsers do
not. The 4x colour asks for it only where it exists, so a browser without it
still gets a valid plain render attachment. One pass clears and discards
the colour, so it is always transient where the browser allows it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { transientUsage, createMsaa } = await import( "../../src/engine/runtime/renderer/device/msaa.ts" );

test("a browser with transient attachments gets the bit", () => {
	assert.equal( transientUsage( { RENDER_ATTACHMENT: 0x10, TRANSIENT_ATTACHMENT: 0x20 } ), 0x20 );
});

test("a browser without them gets no extra usage", () => {
	assert.equal( transientUsage( { RENDER_ATTACHMENT: 0x10 } ), 0 );
});

const USAGE = { RENDER_ATTACHMENT: 0x10, TEXTURE_BINDING: 0x04, TRANSIENT_ATTACHMENT: 0x20 };

/*
================
fakeDevice

Records every texture the MSAA owner creates; the rest is inert.
================
*/
function fakeDevice() {
	const textures = [];
	return {
		textures,
		createShaderModule: () => ({}),
		createRenderPipeline: () => ({ getBindGroupLayout: () => ({}) }),
		createBindGroup: () => ({}),
		createTexture( descriptor ) {
			textures.push( descriptor );
			return { createView: () => ({}), destroy() {} };
		}
	};
}

test("the 4x colour carries the transient bit and is created once per size", () => {
	const previous = globalThis.GPUTextureUsage;
	globalThis.GPUTextureUsage = /** @type {any} */ (USAGE);
	try {
		const device = fakeDevice(), stage = createMsaa( /** @type {any} */ (device) );
		const colours = () => device.textures.filter( texture => texture.label === "msaa-scene-colour" );
		stage.prepare( 64, 64, "rgba8unorm", true );
		stage.prepare( 64, 64, "rgba8unorm", true );
		assert.deepEqual( colours().map( texture => texture.usage ), [ 0x30 ] );
		stage.prepare( 32, 64, "rgba8unorm", true );
		assert.deepEqual( colours().map( texture => texture.usage ), [ 0x30, 0x30 ] );
	} finally {
		globalThis.GPUTextureUsage = previous;
	}
});

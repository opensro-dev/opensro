/*
===========================================================================

msaa-transient.test.mjs - the MSAA colour's transient usage bit

Chrome 146+ exposes GPUTextureUsage.TRANSIENT_ATTACHMENT; older browsers do
not. The 4x colour asks for it only where it exists, so a browser without it
still gets a valid plain render attachment.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { transientUsage } = await import( "../../src/engine/runtime/renderer/device/msaa.ts" );

test("a browser with transient attachments gets the bit", () => {
	assert.equal( transientUsage( { RENDER_ATTACHMENT: 0x10, TRANSIENT_ATTACHMENT: 0x20 } ), 0x20 );
});

test("a browser without them gets no extra usage", () => {
	assert.equal( transientUsage( { RENDER_ATTACHMENT: 0x10 } ), 0 );
});

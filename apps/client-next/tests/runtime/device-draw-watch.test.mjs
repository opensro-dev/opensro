/*
===========================================================================

device-draw-watch.test.mjs - the static runs' state revision is scoped

DeviceDraw.revision keys the frame's static run recordings (frame.ts
recordPersistent). Only a watched draw (object-cull.ts adopts the static
world draws) may move it: a character, particle or UI draw rebinding every
frame must never re-record the static world.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { DeviceDraw } = await import( "../../src/engine/runtime/renderer/device/device-draw.ts" );

/*
================
make
================
*/
function make() {
	return new DeviceDraw( { pipeline: {}, vertices: {}, indices: {}, count: 3 }, {}, 1, 1 );
}

test("only a watched draw's binding, pipeline or arguments move the revision", () => {
	const watched = make(), other = make();
	DeviceDraw.watch( watched );
	const start = DeviceDraw.revision;
	DeviceDraw.rebind( other, {} );
	DeviceDraw.repipeline( other, {}, {} );
	DeviceDraw.drawIndirect( other, {}, 0 );
	DeviceDraw.select( watched, 3, 0 );
	assert.equal( DeviceDraw.revision, start, "unwatched changes and counts leave it" );
	DeviceDraw.rebind( watched, {} );
	assert.equal( DeviceDraw.revision, start + 1 );
	DeviceDraw.repipeline( watched, {}, {} );
	DeviceDraw.drawIndirect( watched, {}, 20 );
	assert.equal( DeviceDraw.revision, start + 3 );
	DeviceDraw.unwatch( watched );
	const after = DeviceDraw.revision;
	DeviceDraw.rebind( watched, {} );
	assert.equal( DeviceDraw.revision, after, "an unwatched draw no longer counts" );
});

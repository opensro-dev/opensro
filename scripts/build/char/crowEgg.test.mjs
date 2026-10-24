/*
===========================================================================
crowEgg.test.mjs - owned egg geometry survives the normal runtime GLB contract.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createCrowEgg } from "./crowEgg.mjs";

/*
================
owned egg export
================
*/
test("owned egg exports a bounded grounded shell with an embedded texture", () => {
	const glb = createCrowEgg();
	assert.equal( glb.readUInt32LE( 0 ), 0x46546c67 );
	assert.equal( glb.readUInt32LE( 8 ), glb.length );
	const json = JSON.parse( glb.subarray( 20, 20 + glb.readUInt32LE( 12 ) ).toString( "utf8" ) );
	assert.equal( json.meshes.length, 1 );
	assert.equal( json.images.length, 1 );
	const pos = json.accessors[json.meshes[0].primitives[0].attributes.POSITION];
	assert.equal( pos.count, 273 );
	assert.deepEqual( [ pos.min[1], pos.max[1] ], [ 0, 9 ] );
	assert.equal( json.animations?.length ?? 0, 0 );
});

/*
===========================================================================

environment-stages.test.mjs - the Experimental rows' stage words

environment-block.ts owns which row sits in which stage word. All off is
the native frame (both stages vec4s zero); a frame's overrides (no
cascade in a preview, the viewer's studio light) never change the saved
words the next world frame starts from.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

const { frameStages, packStages } = await import( "../../src/engine/runtime/renderer/device/environment-block.ts" );

const OFF = {
	heightFog: false,
	dynamicSun: false,
	waterRefraction: false,
	skyDetail: false,
	hdrToneMap: false,
	sunShadow: false,
	perPixelLighting: false,
	ambientOcclusion: false
};

test("all rows off packs the native zero words; each row owns one word", () => {
	const saved = new Float32Array( 8 );
	packStages( saved, OFF );
	assert.deepEqual( [ ...saved ], [ 0, 0, 0, 0, 0, 0, 0, 0 ] );
	packStages( saved, { ...OFF, waterRefraction: true } );
	assert.deepEqual( [ ...saved ], [ 0, 0, 1, 0, 0, 0, 0, 0 ] );
	packStages( saved, { ...OFF, ambientOcclusion: true, sunShadow: true } );
	assert.deepEqual( [ ...saved ], [ 0, 0, 0, 0, 0, 1, 0, 1 ] );
});

test("a frame's overrides leave the saved words as the rows set them", () => {
	const saved = new Float32Array( 8 ), out = new Float32Array( 8 );
	packStages( saved, { ...OFF, sunShadow: true } );
	assert.deepEqual( [ ...frameStages( saved, out, false, true ) ], [ 0, 1, 0, 0, 0, 0, 0, 0 ] );
	assert.deepEqual( [ ...saved ], [ 0, 0, 0, 0, 0, 1, 0, 0 ], "the saved choice is kept for world re-entry" );
	assert.deepEqual( [ ...frameStages( saved, out, true, false ) ], [ 0, 0, 0, 0, 0, 1, 0, 0 ] );
});

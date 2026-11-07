/*
===========================================================================

dungeon-camera.test.mjs - the background distance never limits a dungeon

8B17E0 draws dungeons through a camera that never takes the Video background
distance, so only outdoor scenes project with it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { DUNGEON_CAMERA_FAR, sceneCameraFar } = await import(
	"../../src/engine/foundation/rendering/dungeon-camera.ts"
);

test("the background distance limits outdoor scenes and never a dungeon", () => {
	assert.equal( sceneCameraFar( false, 1500 ), 1500 );
	assert.equal( sceneCameraFar( false, undefined ), undefined );
	assert.equal( sceneCameraFar( true, 1500 ), DUNGEON_CAMERA_FAR );
	assert.equal( sceneCameraFar( true, undefined ), DUNGEON_CAMERA_FAR );
});

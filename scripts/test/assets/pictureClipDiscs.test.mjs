/*
===========================================================================

pictureClipDiscs.test.mjs - every portrait disc the client masks with is published

The client draws a CIFStaticWithPictureClip portrait through the face disc
its window sets (+0x380): pmi_face for the player (6B48B2), pmi_pet_face
for a pet (6B3828), qpt_face for a quick party slot (5BA64B) and pt_face
for the party windows (5B8E6B). The UI device skips a quad whose mask is
not loaded, so a disc the build does not publish leaves its portrait out
and the slot shows only its black backing (the quick party slots after
#3699).

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { runtimeCifImageReferences } from "../../build/shared/cifRuntimeImageCatalog.mjs";

const PICTURE_CLIP_DISCS = [
	"interface/playerminiinfo/pmi_face.ddj",
	"interface/playerminiinfo/pmi_pet_face.ddj",
	"interface/quickparty/qpt_face.ddj",
	"interface/party/pt_face.ddj"
];

test("every portrait disc the client masks with is a published runtime image", () => {
	const published = new Set( runtimeCifImageReferences );
	for ( const disc of PICTURE_CLIP_DISCS ) {
		assert.ok( published.has( disc ), `the build does not publish ${disc}` );
	}
});

/*
===========================================================================

objectTextureRecord.test.mjs - a published texture names no build location

Two builds of the same sources into differently named generated roots must
publish the same bytes, so a texture's image source is named relative to
the tree that holds it: the licensed game tree for a block source, the
generated tree's intermediate images for a converted PNG.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";

import { objectTextureRecord } from "../../build/world/objects/buildTitleSectorObjectResources.mjs";
import { extractedRoot } from "../../build/world/paths.mjs";

const textureSourcePath = "prim/mtrl/particle/pokpo1.ddj";

/*
================
recordFor

One texture's published record, built for a generated root of the given
name.
================
*/
function recordFor( generatedRootName, blockFormat = null ) {
	return objectTextureRecord( {
		area: "outdoor",
		textureSourcePath,
		blockFormat,
		extractedRoot,
		imageRoot: path.join( "/proof", "o3-native-proof", generatedRootName, "intermediate", "images" )
	} );
}

test("a converted image keeps its name when the build root's name changes", () => {
	const fix = recordFor( "fix" );
	const branch = recordFor( "branch" );

	assert.deepEqual( fix, branch );
	assert.equal( fix.imageSourcePath, "Data_extracted/prim/mtrl/particle/pokpo1.png" );
	assert.equal( fix.imagePublicPath, "/assets/world/outdoor/object-textures/prim/mtrl/particle/pokpo1.png" );
});

test("a block source keeps its game-relative name", () => {
	const record = recordFor( "fix", "dxt5" );

	assert.equal( record.imageSourcePath, "extracted/Data_extracted/prim/mtrl/particle/pokpo1.ddj" );
	assert.equal( record.imagePublicPath, "/assets/world/outdoor/object-textures/prim/mtrl/particle/pokpo1.texture" );
	assert.equal( record.blockFormat, "dxt5" );
});

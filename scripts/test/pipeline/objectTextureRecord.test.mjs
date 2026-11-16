/*
===========================================================================

objectTextureRecord.test.mjs - a published texture names no build location

Two builds of the same sources into differently named generated roots must
publish the same bytes, so a texture's image source is named relative to
the tree that holds it: the licensed game tree for a block source, the
generated tree's intermediate images for a converted PNG. A material whose
texture source is absent names no texture at all.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";

import {
	materialTexturePublicPath,
	objectTextureRecord,
	objectTextureSource
} from "../../build/world/objects/buildTitleSectorObjectResources.mjs";
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

test("a material whose texture source is absent names no texture", () => {
	// A named path the build never writes would be re-claimed when the region
	// is reused, and the claim audit would refuse the build.
	assert.equal(
		materialTexturePublicPath( { area: "outdoor", textureSourcePath, blockFormat: null, sourceThere: false } ),
		null
	);
	assert.equal(
		materialTexturePublicPath( {
			area: "outdoor",
			textureSourcePath: null,
			blockFormat: null,
			sourceThere: false
		} ),
		null
	);
});

test("a material whose texture source is present names its published file", () => {
	assert.equal(
		materialTexturePublicPath( { area: "outdoor", textureSourcePath, blockFormat: null, sourceThere: true } ),
		"/assets/world/outdoor/object-textures/prim/mtrl/particle/pokpo1.png"
	);
	assert.equal(
		materialTexturePublicPath( { area: "outdoor", textureSourcePath, blockFormat: "dxt1", sourceThere: true } ),
		"/assets/world/outdoor/object-textures/prim/mtrl/particle/pokpo1.texture"
	);
});

test("a material's texture is judged by the file the copy pass publishes from", () => {
	// A present .ddj whose PNG conversion is missing must name no texture:
	// the copy pass reads the PNG, so the PNG is the file whose presence counts.
	const imageRoot = path.join( "/proof", "images" );
	const options = { textureSourcePath, extractedRoot, imageRoot };
	assert.equal(
		objectTextureSource( { ...options, blockFormat: null } ),
		path.join( imageRoot, "Data_extracted", "prim", "mtrl", "particle", "pokpo1.png" )
	);
	assert.equal( path.extname( objectTextureSource( { ...options, blockFormat: "dxt5" } ) ), ".ddj" );
});

test("a block source keeps its game-relative name", () => {
	const record = recordFor( "fix", "dxt5" );

	assert.equal( record.imageSourcePath, "extracted/Data_extracted/prim/mtrl/particle/pokpo1.ddj" );
	assert.equal( record.imagePublicPath, "/assets/world/outdoor/object-textures/prim/mtrl/particle/pokpo1.texture" );
	assert.equal( record.blockFormat, "dxt5" );
});

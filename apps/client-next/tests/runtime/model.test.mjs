/*
===========================================================================

model.test.mjs - tests for model.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";

const { createModelDecoder } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/model/model.ts" ) ).href
);
function glb( json, binary = new Uint8Array( 12 ) ) {
	const text = Buffer.from( JSON.stringify( json ) );
	const length = Math.ceil( text.length / 4 ) * 4;
	const bytes = new Uint8Array( 28 + length + binary.length );
	const view = new DataView( bytes.buffer );
	view.setUint32( 0, 0x46546c67, true );
	view.setUint32( 4, 2, true );
	view.setUint32( 8, bytes.length, true );
	view.setUint32( 12, length, true );
	view.setUint32( 16, 0x4e4f534a, true );
	bytes.fill( 32, 20, 20 + length );
	bytes.set( text, 20 );
	view.setUint32( 20 + length, binary.length, true );
	view.setUint32( 24 + length, 0x004e4942, true );
	bytes.set( binary, 28 + length );
	return bytes;
}
const schema = () => ({
	asset: { version: "2.0" },
	buffers: [ { byteLength: 12 } ],
	bufferViews: [ { buffer: 0, byteLength: 12 } ],
	accessors: [ { bufferView: 0, componentType: 5126, count: 1, type: "VEC3" } ]
});
test("every published customization preview shares its mission material contract", () => {
	const publicRoot = path.join( root, "../../.generated/client-public" ),
		roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot ),
		decoder = createModelDecoder();
	let checked = 0;
	for ( const row of roster.models ) {
		if ( !row.previewGlb ) continue;
		const preview = decoder.decode( readPublishedAssetBytesSync( row.previewGlb, publicRoot ) ),
			mission = decoder.decode( readPublishedAssetBytesSync( row.glb, publicRoot ) );
		assert.deepEqual(
			preview.json.materials,
			mission.json.materials,
			row.codename + " preview material publication drift"
		);
		checked++;
	}
	assert.equal( checked, 52 );
});
test("GLB container preserves installed character skins and animations", () => {
	const bytes = readPublishedAssetBytesSync(
		"/assets/char/china/chinaman_adventurer.glb",
		path.join( root, "../../.generated/client-public" )
	);
	const decoded = createModelDecoder().decode( bytes );
	assert.ok( decoded.binary.byteLength > 0 );
	assert.ok( decoded.json.meshes.length > 0 );
	assert.ok( decoded.json.skins.length > 0 );
	assert.ok( Object.isFrozen( decoded.json.meshes ) );
	const view = new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength );
	const original = JSON.parse( new TextDecoder().decode( bytes.subarray( 20, 20 + view.getUint32( 12, true ) ) ) );
	assert.deepEqual( JSON.parse( JSON.stringify( decoded.json ) ), original );
});
test("GLB rejects truncated chunks and accessor ranges before geometry admission", () => {
	const decoder = createModelDecoder(), bytes = glb( schema() );
	assert.equal( decoder.decode( bytes ).binary.byteLength, 12 );
	assert.throws( () => decoder.decode( bytes.subarray( 0, bytes.length - 1 ) ), /header/ );
	const bad = schema();
	bad.accessors[0].count = 2;
	assert.throws( () => decoder.decode( glb( bad ) ), /accessor exceeds/ );
	const external = schema();
	external.buffers[0].uri = "external.bin";
	assert.throws( () => decoder.decode( glb( external ) ), /buffer/ );
});
test("GLB output owns binary bytes independently of the source pack", () => {
	const bytes = glb( schema(), Uint8Array.of( 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12 ) );
	const decoded = createModelDecoder().decode( bytes );
	bytes.fill( 0 );
	assert.equal( new Uint8Array( decoded.binary )[0], 1 );
});

test("character admission preserves opaque sheen alpha and cutout coverage through instance fades", () => {
	const decoder = createModelDecoder(),
		document = decoder.decode(
			readPublishedAssetBytesSync(
				"/assets/char/china/chinaman_adventurer.glb",
				path.join( root, "../../.generated/client-public" )
			)
		);
	for ( const alphaMode of [ undefined, "OPAQUE", "MASK", "BLEND" ] ) {
		const json = JSON.parse( JSON.stringify( document.json ) );
		for ( const material of json.materials ) {
			material.alphaMode = alphaMode;
			delete material.extras?.sroFadeAlphaOnly;
		}
		const model = decoder.character( { ...document, json } ), alpha = alphaMode === "MASK" || alphaMode === "BLEND";
		assert.ok( model.primitives.length > 0 );
		for ( const primitive of model.primitives ) {
			assert.equal( primitive.geometry.material.textureAlpha, alpha );
			assert.equal( primitive.geometry.material.fadeAlphaOnly, !alpha );
		}
	}
});

test("native environment coverage retains texture cutouts but fades with instance alpha alone", () => {
	const decoder = createModelDecoder(),
		document = decoder.decode(
			readPublishedAssetBytesSync(
				"/assets/char/china/chinaman_adventurer.glb",
				path.join( root, "../../.generated/client-public" )
			)
		);
	const json = JSON.parse( JSON.stringify( document.json ) );
	for ( const material of json.materials ) {
		material.alphaMode = "MASK";
		material.alphaCutoff = 1 / 255;
		material.extras = { ...material.extras, sroFadeAlphaOnly: true };
	}
	const model = decoder.character( { ...document, json } );
	assert.ok( model.primitives.length );
	for ( const primitive of model.primitives ) {
		assert.equal( primitive.geometry.material.textureAlpha, true );
		assert.equal( primitive.geometry.material.fadeAlphaOnly, true );
	}
});

test("authored NaN texture coordinates read as 0; a non-finite position still refuses the model", () => {
	const decoder = createModelDecoder();
	for (
		const asset of [
			"/assets/world/animated-objects/artifact-china-dunhuang-w_cd_ani_boat.glb",
			"/assets/world/animated-objects/artifact-china-dunhuang-w_cd_boat.glb",
			"/assets/world/animated-objects/npc-npc-chinasystem_boatman2.glb"
		]
	) {
		const bytes = readPublishedAssetBytesSync( asset, path.join( root, "../../.generated/client-public" ) );
		const model = decoder.character( decoder.decode( bytes ) );
		assert.ok( model.primitives.length > 0, asset );
		for ( const primitive of model.primitives ) {
			assert.ok( primitive.geometry.uvs.every( Number.isFinite ), asset );
		}
		// The same model with one NaN position is refused, as before.
		const copy = Uint8Array.from( bytes ), view = new DataView( copy.buffer );
		const length = view.getUint32( 12, true ), json = JSON.parse( Buffer.from( copy.subarray( 20, 20 + length ) ) );
		const accessor = json.accessors[json.meshes[0].primitives[0].attributes.POSITION];
		const at = 28 + length + (json.bufferViews[accessor.bufferView].byteOffset ?? 0) + (accessor.byteOffset ?? 0);
		view.setFloat32( at, NaN, true );
		assert.throws( () => decoder.character( decoder.decode( copy ) ), /Non-finite character accessor/, asset );
	}
});

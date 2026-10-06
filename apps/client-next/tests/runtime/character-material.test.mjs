import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { characterMaterialState } from "../../../../scripts/build/shared/characterMaterialState.mjs";
import { characterEnvironment } from "../../../../scripts/build/shared/characterEnvironment.mjs";
import {
	characterMaterialBindings,
	resolveCharacterMaterialBinding
} from "../../../../scripts/build/char/characterMaterialBindings.mjs";
import { loadDataAsset } from "../../../../scripts/build/shared/jmxAssetIO.mjs";
import { parseJmxResourceBsr } from "../../../../scripts/build/world/objects/formats.mjs";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";

test("retail axe BSR preserves the environment modifier that overrides BMT coverage", async () => {
	const source = "res/item/europe/weapon/axe_01_r.bsr";
	const bsr = parseJmxResourceBsr( await loadDataAsset( source ), source );
	assert.equal( defined( bsr.modifiers ).environmentModifiers.length, 1 );
	const modifier = defined( bsr.modifiers ).environmentModifiers[0];
	assert.deepEqual( modifier.baseWords, [ 1056964608, 1, 256, 4294967295, 0, 3 ] );
	assert.deepEqual( modifier.words24, [ 1, 65536, 0, 2080374784 ] );
	assert.deepEqual( characterEnvironment( 0x340, [ modifier ] ), { textureId: 1, mode: 1 } );
	assert.equal( characterEnvironment( 0x10340, [ modifier ] ), undefined );
	assert.throws(
		() =>
			characterEnvironment( 0x340, [ { ...modifier, words24: [ 0xffffffff, ...modifier.words24.slice( 1 ) ] } ] ),
		/null environment texture/
	);
	assert.throws(
		() => characterEnvironment( 0x340, [ { ...modifier, baseWords: [ ...modifier.baseWords.slice( 0, 5 ), 2 ] } ] ),
		/blend variant/
	);
	assert.deepEqual( characterMaterialState( 0x340, [ modifier ] ), {
		alphaCutoff: 1 / 255,
		fadeAlphaOnly: true,
		doubleSided: false
	} );
	assert.equal( characterMaterialState( 0x10340, [ modifier ] ).alphaCutoff, 128 / 255 );
	assert.equal(
		characterMaterialState( 0x341, [ { ...modifier, baseWords: [ ...modifier.baseWords.slice( 0, 5 ), 1 ] } ] )
			.alphaCutoff,
		0
	);
	assert.throws( () => characterMaterialState( 0x340, [ { ...modifier, stateId: 1 } ] ), /runtime ownership/ );
});

test("native baseline alpha bypasses and culling remain independent", () => {
	for ( const flags of [ 0, 1, 0x200, 0x201, 0x20000200, 0x10000201, 0x40000200 ] ) {
		const state = characterMaterialState( flags );
		assert.equal( state.doubleSided, !!(flags & 1) );
		assert.equal( state.alphaCutoff, (flags & 0x200) && !(flags & 0x30000000) ? 128 / 255 : 0 );
	}
});

test("all published character parts retain their own native material and modifier contract", async () => {
	const publicRoot = CLIENT_PUBLIC_ROOT;
	const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot );
	const bindings = await characterMaterialBindings( roster );
	// Collected independently of the binder: every GLB path the roster names.
	const expectedAssets = new Set();
	JSON.stringify( roster, ( key, value ) => {
		if ( (key === "glb" || key === "previewGlb") && typeof value === "string" ) expectedAssets.add( value );
		return value;
	} );
	assert.deepEqual(
		[ ...bindings.keys() ].sort(),
		[ ...expectedAssets ].sort(),
		"every published character GLB is bound to its native source"
	);
	const seen = new Set();
	let modified = 0;
	for ( const [asset, sources] of bindings ) {
		const bytes = readPublishedAssetBytesSync( asset, publicRoot ),
			json = JSON.parse( bytes.subarray( 20, 20 + bytes.readUInt32LE( 12 ) ) );
		for ( const mesh of json.meshes ) {
			for ( const primitive of mesh.primitives ) {
				const material = json.materials[primitive.material];
				const native = resolveCharacterMaterialBinding( sources, mesh.name, material.name );
				const expected = characterMaterialState( native.flags, native.environmentModifiers );
				assert.equal( material.alphaCutoff ?? 0, expected.alphaCutoff, asset + ":" + material.name );
				assert.equal( material.doubleSided, expected.doubleSided, asset + ":" + material.name );
				assert.equal( material.extras.sroFadeAlphaOnly, expected.fadeAlphaOnly, asset + ":" + material.name );
				const reflection = characterEnvironment( native.flags, native.environmentModifiers );
				assert.deepEqual( material.extras.sroEnvironment, reflection, asset + ": reflection descriptor" );
				if ( reflection && reflection.textureId !== 0xffffffff ) {
					const texture = json.textures[material.extras.sroEnvironmentTexture];
					assert.ok( texture, asset );
					assert.equal( json.images[texture.source].name, "sro-environment-" + reflection.textureId );
				}
				if ( native.environmentModifiers.length ) {
					modified++;
					seen.add( expected.alphaCutoff );
				}
			}
		}
	}
	assert.ok( modified > 100 );
	assert.deepEqual( [ ...seen ].sort(), [ 0, 1 / 255 ] );
});

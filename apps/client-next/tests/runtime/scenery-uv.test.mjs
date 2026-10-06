/*
===========================================================================
scenery-uv.test.mjs - missing retail UVs must not reject valid world geometry
Exercises publication, legacy GLB admission and static JSON scenery together.
===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { existsSync } from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { avatarToGlb } from "../../../../scripts/build/char/exportGlb.mjs";
import { readPackedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" );
const { createWorldDecoder } = await import( "../../src/engine/runtime/assets/worker/world/world.ts" );
const { textureCoordinate } = await import( "../../src/engine/foundation/rendering/geometry-validation.ts" );

/*
================
fixtureAvatar
================
*/
function fixtureAvatar() {
	return {
		name: "undefined-uv-fixture",
		materials: new Map( [ [ "surface", {
			flags: 0,
			colors: { diffuse: [ 1, 1, 1, 1 ], ambient: [ 1, 1, 1, 1 ] }
		} ] ] ),
		clips: [],
		skeleton: {
			boneCount: 1,
			byName: new Map( [ [ "root", 0 ] ] ),
			bones: [ { name: "root", parentIndex: -1, local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] } } ]
		},
		parts: [ {
			localToGlobal: [ 0 ],
			mesh: {
				materialName: "surface",
				rigid: true,
				vertexCount: 3,
				positions: Float32Array.of( 0, 0, 0, 10, 0, 0, 0, 10, 0 ),
				normals: Float32Array.of( 0, 0, 1, 0, 0, 1, 0, 0, 1 ),
				uvs: Float32Array.of( NaN, 0.25, 0.75, Infinity, -0.5, 2 ),
				indices: Uint16Array.of( 0, 1, 2 ),
				triangleCount: 1
			}
		} ]
	};
}

/*
================
replaceFirstComponent

Mutate fixture bytes, never source code. The container remains structurally valid.
================
*/
function replaceFirstComponent( document, semantic, value ) {
	const index = document.json.meshes[0].primitives[0].attributes[semantic];
	const accessor = document.json.accessors[index];
	const view = document.json.bufferViews[accessor.bufferView];
	const binary = document.binary.slice( 0 );
	new DataView( binary ).setFloat32( (view.byteOffset ?? 0) + (accessor.byteOffset ?? 0), value, true );
	return { json: document.json, binary };
}

test("publication emits finite UVs and retains each valid component", () => {
	const decoder = createModelDecoder();
	const document = decoder.decode( avatarToGlb( fixtureAvatar() ) );
	const result = decoder.character( document );
	assert.ok( result.primitives[0].geometry.uvs );
	assert.deepEqual( Array.from( result.primitives[0].geometry.uvs ), [ 0, 0.25, 0.75, 0, -0.5, 2 ] );
});

test("legacy GLB UVs recover without weakening geometry validation", () => {
	const decoder = createModelDecoder();
	const document = decoder.decode( avatarToGlb( fixtureAvatar() ) );
	for ( const value of [ NaN, Infinity, -Infinity ] ) {
		const legacy = replaceFirstComponent( document, "TEXCOORD_0", value );
		const recoveredUvs = decoder.character( legacy ).primitives[0].geometry.uvs;
		assert.ok( recoveredUvs );
		assert.deepEqual( Array.from( recoveredUvs ), [
			0,
			0.25,
			0.75,
			0,
			-0.5,
			2
		] );
		for ( const semantic of [ "POSITION", "NORMAL" ] ) {
			assert.throws(
				() => decoder.character( replaceFirstComponent( document, semantic, value ) ),
				/Non-finite character accessor/
			);
		}
	}
	assert.throws( () => textureCoordinate( /** @type {any} */ ("invalid") ), /Invalid texture coordinate/ );
});

test("static JSON and animated GLB scenery share the same missing UV policy", () => {
	const mesh = {
		sourcePath: "fixture.bms",
		headerOffsets: [],
		metadata: { materialName: "surface" },
		positions: [ 0, 0, 0, 10, 0, 0, 0, 10, 0 ],
		normals: [ 0, 0, 1, 0, 0, 1, 0, 0, 1 ],
		uvs: [ null, 0.25, 0.75, null, -0.5, 2 ],
		indices: [ 0, 1, 2 ],
		bounds: { min: [ 0, 0, 0 ], max: [ 10, 10, 0 ] }
	};
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: {
			placements: [ { objectId: 1, uid: 1, regionId: "257", position: { x: 0, y: 0, z: 0 }, yaw: 0 } ],
			resources: {
				meshes: [ mesh ],
				bsr: [ {
					objectId: 1,
					sourcePath: "fixture.bsr",
					meshPaths: [ "fixture.bms" ],
					materialPaths: [ "surface.bmt" ]
				} ],
				materialSets: [ {
					sourcePath: "surface.bmt",
					materials: [ { name: "surface", flags: 0, colors: { diffuse: [ 1, 1, 1, 1 ] } } ]
				} ]
			}
		}
	};
	const scene = createWorldDecoder().decode( /** @type {any} */ (bundle) );
	assert.equal( scene.groups.length, 1, "valid geometry must not disappear because of optional UVs" );
	assert.ok( scene.groups[0].geometry.uvs );
	assert.deepEqual( Array.from( scene.groups[0].geometry.uvs ), [ 0, 0.25, 0.75, 0, -0.5, 2 ] );
	assert.deepEqual( mesh.uvs, [ null, 0.25, 0.75, null, -0.5, 2 ], "source resource remains immutable" );
});

const publicRoot = CLIENT_PUBLIC_ROOT;
test( "all three published boat resources decode from unchanged verified packs", {
	skip: !existsSync( path.join( publicRoot, "assets/packs/manifest.json" ) )
}, () => {
	const decoder = createModelDecoder();
	for (
		const name of [
			"artifact-china-dunhuang-w_cd_boat",
			"artifact-china-dunhuang-w_cd_ani_boat",
			"npc-npc-chinasystem_boatman2"
		]
	) {
		const bytes = readPackedAssetBytesSync( `/assets/world/animated-objects/${name}.glb`, publicRoot );
		const model = decoder.character( decoder.decode( bytes ) );
		assert.ok( model.primitives.length > 0 );
		for ( const primitive of model.primitives ) {
			assert.ok( primitive.geometry.uvs );
			assert.ok( primitive.geometry.uvs.every( Number.isFinite ), name );
		}
	}
} );

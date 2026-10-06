/*
===========================================================================

world-surfaces.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";
const publicRoot = CLIENT_PUBLIC_ROOT;
const bytes = asset => readPublishedAssetBytesSync( asset, publicRoot );
const json = asset => readPublishedAssetJsonSync( asset, publicRoot );
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, "src/engine", file ) ).href );
}
const { decodeDxt1 } = await load( "foundation/assets/dds.ts" ),
	{ skyGroups } = await load( "foundation/rendering/sky-geometry.ts" ),
	{ createWorldDecoder } = await load( "runtime/assets/worker/world/world.ts" );
test("DDS decoder handles BC1 transparent mode, nonzero offsets, edges and rejected surfaces", () => {
	const d = new Uint8Array( 136 ), v = new DataView( d.buffer );
	for (
		const [offset, value] of [ [ 0, 0x20534444 ], [ 4, 124 ], [ 12, 3 ], [ 16, 2 ], [ 76, 32 ], [ 80, 4 ], [
			84,
			0x31545844
		], [ 132, 0xffffffff ] ]
	) v.setUint32( offset, value, true );
	assert.equal( decodeDxt1( d ).pixels.length, 24 );
	assert.ok( decodeDxt1( d ).pixels.every( v => v === 0 ) );
	v.setUint16( 128, 0xf800, true );
	v.setUint32( 132, 0, true );
	const padded = new Uint8Array( 140 );
	padded.set( d, 4 );
	assert.deepEqual( [ ...decodeDxt1( padded.subarray( 4 ) ).pixels.slice( 0, 4 ) ], [ 255, 0, 0, 255 ] );
	assert.throws( () => decodeDxt1( d.subarray( 0, 135 ) ), /payload/ );
	assert.throws( () => decodeDxt1( d, 8 ), /budget/ );
	v.setUint32( 112, 0x200, true );
	assert.throws( () => decodeDxt1( d ), /surface/ );
});
test("installed MAPT lightmaps reach every sector and retain all four selected surfaces", () => {
	const bundle = json( "/assets/world/china/region-62a8.json" ),
		scene = createWorldDecoder().decode( new TextEncoder().encode( JSON.stringify( bundle ) ) );
	const lights = scene.groups.filter( g => g.material.lightmap );
	assert.equal( lights.length, bundle.terrain.sectors.length );
	for ( const group of lights ) {
		const decoded = decodeDxt1( bytes( group.material.texture ) );
		assert.equal( decoded.width, 512 );
		assert.equal( decoded.height, 512 );
		assert.deepEqual( [ ...new Set( group.ranges.map( r => r.lod ) ) ].sort(), [ 0, 1, 2, 3 ] );
		assert.ok( group.geometry.uvs.every( v => v >= 0 && v <= 1 ) );
		assert.equal( group.material.terrain, undefined );
	}
});
test("sky hemisphere and veil match native counts and use the authored star field", () => {
	const sky = json( "/assets/world/outdoor/shared-render-resources.json" ).sky,
		groups = skyGroups( sky ),
		dome = groups.find( g => g.material.sky === 1 ),
		veil = groups.find( g => g.material.sky === 6 );
	assert.equal( dome.geometry.positions.length / 3, 545 );
	assert.equal( dome.geometry.indices.length / 3, 1024 );
	assert.equal( veil.geometry.positions.length / 3, 605 );
	assert.equal( veil.geometry.indices.length / 3, 1148 );
	assert.equal( groups.find( g => g.material.sky === 2 ).geometry.indices.length, 18000 );
	assert.equal( groups.find( g => g.material.sky === 5 ).geometry.positions.length, 75 );
	assert.equal( groups.find( g => g.material.sky === 4 ).material.frames.length, 29 );
});
test("authored animated meshes replace their static submissions and share one palette across instances", async () => {
	const { createModelDecoder } = await load( "runtime/assets/worker/model/model.ts" ),
		decoder = createModelDecoder(),
		manifest = json( "/assets/world/china/animated-objects.json" ),
		bundle = json( "/assets/world/china/region-62a8.json" );
	const [sourcePath, entry] = Object.entries( manifest.objects ).find( ( [source] ) =>
		bundle.objects.resources.bsr.some( r => r.sourcePath === source )
	);
	const model = decoder.character( decoder.decode( bytes( entry.glbPublicPath ) ) );
	bundle.animated = [ { ...entry, sourcePath, model: { ...model, images: [] } } ];
	const scene = createWorldDecoder().decode( bundle ), animated = scene.groups.filter( g => g.animation );
	for ( const [index, primitive] of model.primitives.entries() ) {
		const draws = animated.filter( group => group.animation.primitive === index );
		assert.ok( draws.length > 0 );
		if ( primitive.cloth ) {
			assert.ok( draws.every( group => group.geometry.instances.length === 16 ) );
			assert.equal( new Set( draws.map( group => group.geometry.bones ) ).size, 1 );
		} else assert.equal( draws.length, 1 );
	}
	for ( const group of animated ) {
		const primitive = model.primitives[group.animation.primitive];
		const sources = entry.skinnedMeshPaths.map( path =>
			bundle.objects.resources.meshes.find( m => m.sourcePath.toLowerCase() === path.toLowerCase() )
		).filter( m => m.metadata.materialName === primitive.name );
		let offset = 0;
		const expected = [];
		for ( const mesh of sources ) {
			if ( mesh.headerOffsets[7] ) expected.push( [ offset, mesh.indices.length ] );
			offset += mesh.indices.length;
		}
		assert.equal( group.material.sharedPose, true );
		assert.equal( group.geometry.indices.length, offset );
		for ( let instance = 0; instance < group.geometry.instances.length / 16; instance++ ) {
			const parts = group.collision.filter( c => c.instance === instance );
			assert.deepEqual( parts.map( c => [ c.indexStart, c.indexCount ] ), expected );
			assert.equal( new Set( parts.map( c => c.object ) ).size, expected.length ? 1 : 0 );
		}
		assert.equal( group.geometry.bones.length, primitive.joints.length * 16 );
		assert.ok( group.geometry.instances.length >= 16 );
	}
	assert.ok( !scene.groups.some( g => entry.skinnedMeshPaths.some( p => g.id.startsWith( "object:" + p + ":" ) ) ) );
});

test("sky applies native half-bias transforms before shader uniforms", async () => {
	const { worldEnvironment } = await load( "foundation/rendering/world-environment.ts" );
	const e = worldEnvironment(
		{
			startTimeOfDay: .5,
			ratePerSecond: 0,
			tracks: {
				scalar0x25c: [ { t: 0, value: 0 } ],
				scalar0x288: [ { t: 0, value: -1 } ],
				cloudAlpha: [ { t: 0, value: 0 } ]
			}
		},
		{ eye: [ 0, 0, 2 ], target: [ 0, 0, 0 ], fov: Math.PI / 3, near: 1, far: 100 },
		1,
		0
	);
	assert.equal( e[47], 40000 );
	assert.equal( e[50], .5 );
	assert.equal( e[55], 1 );
});

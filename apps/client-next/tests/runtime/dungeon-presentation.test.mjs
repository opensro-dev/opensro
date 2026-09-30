/*
===========================================================================

dungeon-presentation.test.mjs - tests for parseDof.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { join } from "node:path";
import { dataExtractedRoot } from "../../../../scripts/build/world/paths.mjs";
import { readDofPresentation } from "../../../../scripts/build/world/assets/buildDungeonResources.mjs";
import { dungeonWaterVertices, resolveDungeonWater } from "../../../../scripts/build/world/assets/dungeonWater.mjs";
import { parseJmxResourceBsr, parseJmxBmsStaticMesh } from "../../../../scripts/build/world/objects/formats.mjs";

test("dungeon water fan follows shared-face order and the provider keeps the last tint", async () => {
	const mesh = { positions: [ 0, 0, 0, 10, 0, 0, 10, 0, 20, 0, 0, 20 ], indices: [ 0, 1, 2, 0, 2, 3 ] };
	assert.deepEqual( dungeonWaterVertices( mesh, [ 100, 5, 200 ] ), [
		100,
		5,
		200,
		110,
		5,
		200,
		110,
		5,
		220,
		100,
		5,
		220
	] );
	const cyclic = { ...mesh, indices: [ 1, 2, 0, 2, 3, 0 ] };
	assert.deepEqual( dungeonWaterVertices( cyclic, [ 0, 0, 0 ] ), [ 10, 0, 20, 10, 0, 0, 0, 0, 0, 0, 0, 20 ] );
	assert.throws( () => dungeonWaterVertices( { ...mesh, indices: [ 0, 1, 2, 0, 1, 2 ] }, [ 0, 0, 0 ] ) );
	const objects = [ { index: 0, path: "first", position: [ 0, 0, 0 ], color: 0xff112233 }, {
		index: 1,
		path: "last",
		position: [ 100, 0, 0 ],
		color: 0xff445566
	} ];
	const actual = await resolveDungeonWater(
		{ blocks: [ { index: 4, fog: { color: 1 }, waterObjects: objects } ] },
		async path => ({ renderMeshSection: { paths: [ path ] } }),
		async () => mesh
	);
	assert.equal( actual[0].vertices.length, 24 );
	assert.equal( actual[0].color, 0xb4445566 );
	assert.deepEqual( actual[0].vertices.slice( 0, 12 ), dungeonWaterVertices( mesh, [ 0, 0, 0 ] ) );
	assert.equal( actual[0].sources.length, 2 );
});

test( "all shipped water objects resolve their real BSR/BMS provider geometry", { timeout: 30000 }, async () => {
	const read = path => fs.readFileSync( join( dataExtractedRoot, path.replaceAll( "\\", "/" ).toLowerCase() ) );
	let count = 0, blocks = 0;
	for ( const dir of [ "china", "wchina" ] ) {
		for (
			const name of fs.readdirSync( join( dataExtractedRoot, "dungeon", dir ) ).filter( n =>
				n.endsWith( ".dof" ) && n !== "dunhwang_cv1.dof"
			)
		) {
			const projection = readDofPresentation( fs.readFileSync( join( dataExtractedRoot, "dungeon", dir, name ) ), name );
			const surfaces = await resolveDungeonWater(
				projection,
				async path => parseJmxResourceBsr( read( path ), path ),
				async path => parseJmxBmsStaticMesh( read( path ), path )
			);
			count += surfaces.reduce( ( n, s ) => n + new Set( s.sources.map( p => p.objectIndex ) ).size, 0 );
			blocks += surfaces.length;
			for ( const surface of surfaces ) {
				assert.ok( surface.vertices.length >= 12 );
				assert.ok( surface.vertices.every( Number.isFinite ) );
				assert.equal( surface.color >>> 24, 180 );
			}
		}
	}
	assert.equal( count, 36 );
	assert.ok( blocks > 0 );
} );

const { parseDof } = await import(
	sourceFileUrl( "tests/oracles/legacy/packages/runtime/src/world/dungeon/parseDof.ts" ).href
);
test("all twelve well-formed retail DOFs retain exact water ownership, transforms, tint and fog", () => {
	let count = 0, water = 0;
	for ( const dir of [ "china", "wchina" ] ) {
		for (
			const name of fs.readdirSync( join( dataExtractedRoot, "dungeon", dir ) ).filter( n =>
				n.endsWith( ".dof" )
			)
		) {
			const file = join( dataExtractedRoot, "dungeon", dir, name ),
				bytes = fs.readFileSync( file );
			if ( name === "dunhwang_cv1.dof" ) {
				assert.throws( () => readDofPresentation( bytes, file ) );
				continue;
			}
			const reference = parseDof( bytes ), actual = readDofPresentation( bytes, file );
			count++;
			assert.equal( actual.blocks.length, reference.blocks.length );
			reference.blocks.forEach( ( block, index ) => {
				const expected = block.objects.flatMap( ( o, i ) =>
					o.flag & 4 ?
						[ {
							index: i,
							name: o.name,
							path: o.path,
							position: o.position,
							rotation: o.rotation,
							scale: o.scale,
							flags: o.flag,
							color: o.waterColor
						} ] :
						[]
				);
				assert.deepEqual( actual.blocks[index], {
					index,
					path: block.path,
					name: block.name,
					position: block.position,
					yaw: block.yaw,
					fog: block.fog,
					waterObjects: expected,
					connectedBlocks: block.connectedBlocks,
					visibleBlocks: block.visibleBlocks
				} );
				water += expected.length;
			} );
			const header = bytes.slice();
			header[0] = 0;
			assert.throws( () => readDofPresentation( header, file ) );
			const grid = bytes.readUInt32LE( 20 );
			assert.throws( () => readDofPresentation( bytes.subarray( 0, grid - 1 ), file ) );
		}
	}
	assert.equal( count, 12 );
	assert.equal( water, 36 );
});

import { resolveSkyTextures } from "../../../../scripts/build/world/assets/copySkyImages.mjs";
test("flare publication retains native mip resources and the shared original sun source", () => {
	const sky = resolveSkyTextures();
	assert.deepEqual(
		sky.flareTexturePublicPaths,
		Array.from( { length: 8 }, ( _, i ) => `/assets/images/Map_extracted/sun/lens${i + 1}.texture` )
	);
	assert.equal( new Set( sky.textures.map( t => t.sourcePath ) ).size, sky.textures.length );
	for ( const publicPath of sky.flareTexturePublicPaths ) {
		assert.ok( sky.textures.some( t => t.publicPath === publicPath.replace( /\.texture$/, ".png" ) ) );
	}
	assert.equal( sky.sunTexturePublicPath, sky.flareTexturePublicPaths[1].replace( /\.texture$/, ".png" ) );
});

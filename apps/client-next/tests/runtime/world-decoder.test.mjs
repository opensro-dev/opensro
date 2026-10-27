/*
===========================================================================

world-decoder.test.mjs - tests for world.ts, follow-camera.ts,
TerrainAssociationMask.ts, world-scene.ts

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
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

const { createWorldDecoder } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/world/world.ts" ) ).href
);

test("camera eligibility follows the BSR per-mesh collision flag without removing decorative draws", async () => {
	const { cameraCollisionParts, cameraSegmentHit } = await import(
		sourceFileUrl( path.join( root, "src/engine/foundation/rendering/follow-camera.ts" ) ).href
	);
	// Only the middle part carries an object-nav payload, on the decorative
	// first part: native A40044 reads CPrimMesh +0x1E8, never the nav offset.
	const meshes = [ 100, 0, 0 ].map( ( nav, i ) => ({
		sourcePath: `part${i}.bms`,
		headerOffsets: [ 0, 0, 0, 0, 0, 0, 0, nav ],
		metadata: { materialName: "shared" },
		positions: [ -10, -10, 30 + i * 30, 10, -10, 30 + i * 30, 0, 10, 30 + i * 30 ],
		normals: [ 0, 0, 1, 0, 0, 1, 0, 0, 1 ],
		uvs: [ 0, 0, 1, 0, 0, 1 ],
		indices: [ 0, 1, 2 ],
		bounds: { min: [ -10, -10, 30 + i * 30 ], max: [ 10, 10, 30 + i * 30 ] }
	}) );
	/** @type {{ paths: string[]; cameraCollision?: boolean[]; }} */
	const renderMeshSection = { paths: meshes.map( m => m.sourcePath ), cameraCollision: [ false, true, false ] };
	const branch = {
		objectId: 1,
		sourcePath: "tree.bsr",
		meshPaths: meshes.map( m => m.sourcePath ),
		renderMeshSection,
		materialPaths: [ "shared.bmt" ]
	};
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: {
			placements: [ 0, 100 ].map( ( x, i ) => ({
				objectId: 1,
				uid: i,
				regionId: "257",
				position: { x, y: 0, z: 0 },
				yaw: 0
			}) ),
			resources: {
				meshes,
				bsr: [ branch ],
				materialSets: [ {
					sourcePath: "shared.bmt",
					materials: [ { name: "shared", flags: 0, colors: { diffuse: [ 1, 1, 1, 1 ] } } ]
				} ]
			}
		}
	};
	const scene = createWorldDecoder().decode( bundle );
	assert.equal( scene.groups.length, 3 );
	assert.ok( scene.groups.every( g => g.geometry.instances.length === 32 ) );
	assert.deepEqual( scene.groups.map( g => g.collision.length ), [ 0, 2, 0 ] );
	const parts = cameraCollisionParts( scene );
	assert.equal( parts.length, 2 );
	// Decorative first/last parts remain visible and do not claim first-part collision precedence.
	assert.equal( cameraSegmentHit( parts, { start: [ 0, 0, 0 ], delta: [ 0, 0, 100 ] } ), .6 );
	assert.equal( cameraSegmentHit( parts, { start: [ 100, 0, 0 ], delta: [ 0, 0, 100 ] } ), .6 );
	// Merged animated primitives must advance index offsets across excluded parts.
	const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const geometry = {
		positions: Float32Array.from( meshes.flatMap( m => m.positions ) ),
		normals: new Float32Array( 27 ),
		uvs: new Float32Array( 18 ),
		indices: Uint32Array.from( [ 0, 1, 2, 3, 4, 5, 6, 7, 8 ] ),
		joints: new Uint32Array( 36 ),
		weights: Float32Array.from( Array.from( { length: 36 }, ( _, i ) => i % 4 === 0 ? 1 : 0 ) ),
		transform: identity()
	};
	bundle.animated = [ {
		sourcePath: "tree.bsr",
		glbPublicPath: "tree.glb",
		clipName: "stand",
		skinnedMeshPaths: branch.meshPaths,
		model: {
			images: [],
			nodes: [ { parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
			primitives: [ { name: "shared", joints: [ 0 ], inverseBind: identity(), geometry } ],
			clips: [ { name: "stand", duration: 1, channels: [] } ]
		}
	} ];
	const animated = createWorldDecoder().decode( bundle );
	assert.equal( animated.groups.length, 1 );
	assert.equal( animated.groups[0].geometry.indices.length, 9 );
	assert.deepEqual( animated.groups[0].collision.map( c => [ c.instance, c.order, c.indexStart, c.indexCount ] ), [ [
		0,
		1,
		3,
		3
	], [ 1, 1, 3, 3 ] ] );
	assert.equal(
		cameraSegmentHit( cameraCollisionParts( animated ), { start: [ 0, 0, 0 ], delta: [ 0, 0, -100 ] } ),
		.6
	);
	branch.renderMeshSection.cameraCollision = [ false, false, false ];
	const foliageOnly = createWorldDecoder().decode( bundle );
	assert.equal( foliageOnly.groups.length, 1 );
	assert.equal( cameraCollisionParts( foliageOnly ).length, 0 );
	// A BSR without per-mesh flags keeps the constructor default (A859B0): every part collides.
	delete renderMeshSection.cameraCollision;
	assert.equal( cameraCollisionParts( createWorldDecoder().decode( bundle ) ).length, 6 );
});

test("a lightmapped mesh carries its lightmap texture and second UV set; a broken UV set drops the mesh", () => {
	const mesh = ( lightmapUvs ) => ({
		sourcePath: "room.bms",
		metadata: { materialName: "shared" },
		positions: [ -10, 0, 0, 10, 0, 0, 0, 10, 0 ],
		normals: [ 0, 0, 1, 0, 0, 1, 0, 0, 1 ],
		uvs: [ 0, 0, 1, 0, 0, 1 ],
		indices: [ 0, 1, 2 ],
		bounds: { min: [ -10, 0, 0 ], max: [ 10, 10, 0 ] },
		lightmap: { texturePublicPath: "/assets/world/dungeon/object-textures/room-lightmap.png", uvs: lightmapUvs }
	});
	// A dungeon is a region with bit 0x8000 set (sectorY 128).
	const decode = ( lightmapUvs, outdoor = false ) =>
		createWorldDecoder().decode( {
			source: { sectorX: 1, sectorY: outdoor ? 1 : 128 },
			terrain: { blocks: [] },
			terrainTextures: { tileCatalog: { referencedTiles: [] } },
			objects: {
				placements: [ {
					objectId: 1,
					uid: 0,
					regionId: "257",
					position: { x: 0, y: 0, z: 0 },
					yaw: 0,
					...(outdoor ? {} : { dungeonBlock: 0 })
				} ],
				resources: {
					meshes: [ mesh( lightmapUvs ) ],
					bsr: [ {
						objectId: 1,
						sourcePath: "room.bsr",
						meshPaths: [ "room.bms" ],
						materialPaths: [ "shared.bmt" ]
					} ],
					materialSets: [ {
						sourcePath: "shared.bmt",
						materials: [ { name: "shared", flags: 0, colors: { diffuse: [ 1, 1, 1, 1 ] } } ]
					} ]
				}
			},
			...(outdoor ? {} : { dungeonBlocks: [ { index: 0, visibleBlocks: [], ambientColor: 0xff674961 } ] })
		} );
	const scene = decode( [ .25, .25, .75, .25, .25, .75 ] );
	assert.equal( scene.groups.length, 1 );
	const [group] = scene.groups;
	assert.equal( group.material.objectLightmap, "/assets/world/dungeon/object-textures/room-lightmap.png" );
	assert.deepEqual( [ ...group.geometry.maskUVs ], [ .25, .25, .75, .25, .25, .75 ] );
	assert.deepEqual( [ ...group.geometry.uvs ], [ 0, 0, 1, 0, 0, 1 ] );
	assert.equal( decode( [ .25, .25 ] ).groups.length, 0 );
	// Outdoors the lightmap is not drawn: no texture and no second UV set.
	const field = decode( [ .25, .25, .75, .25, .25, .75 ], true ).groups;
	assert.equal( field.length, 1 );
	assert.equal( field[0].material.objectLightmap, undefined );
	assert.equal( field[0].geometry.maskUVs, undefined );
});

test("terrain association masks match the reference at each sampled grid resolution", async () => {
	// Test-only reference: no legacy module enters the replacement runtime.

	const { buildNativeTerrainAssociationPasses } = await import(
		sourceFileUrl(
			path.join(
				root,
				"tests/oracles/legacy/apps/client/src/domains/world/babylon/terrain/TerrainAssociationMask.ts"
			)
		).href
	);
	const words = Array.from(
		{ length: 289 },
		( _, i ) => (Math.floor( i / 17 / 3 ) + Math.floor( i % 17 / 4 )) % 5 | ((i % 3) << 13)
	);
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [ { blockX: 0, blockZ: 0, heights: Array( 289 ).fill( 0 ), textureData: words } ] },
		terrainTextures: {
			tileCatalog: {
				referencedTiles: Array.from(
					{ length: 5 },
					( _, textureId ) => ({ textureId, imagePublicPath: `/assets/${textureId}.png` })
				)
			}
		},
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	const scene = createWorldDecoder().decode( new TextEncoder().encode( JSON.stringify( bundle ) ) );
	for ( let lod = 0; lod < 4; lod++ ) {
		const step = 1 << lod,
			axis = 16 / step + 1,
			sampled = Array.from(
				{ length: axis * axis },
				( _, i ) => words[Math.floor( i / axis ) * step * 17 + i % axis * step]
			);
		const expected = buildNativeTerrainAssociationPasses( {
			verticesPerBlockAxis: axis,
			tilesPerBlockAxis: axis - 1,
			textureData: sampled
		} ).map( p => [ p.tileX * step, p.tileZ * step, p.associationKey, p.cornerMask, p.opaque ].join( ":" ) ).sort();
		const actual = [];
		for ( const group of scene.groups ) {
			for ( const range of group.ranges ?? [] ) {
				if ( range.lod === lod ) {
					for ( let v = range.vertexStart; v < range.vertexStart + range.vertexCount; v += 4 ) {
						const g = group.geometry,
							mask = g.colors[v * 4] | g.colors[v * 4 + 1] << 1 | g.colors[v * 4 + 2] << 2 |
								g.colors[v * 4 + 3] << 3;
						actual.push(
							[
								g.positions[v * 3] / 20,
								g.positions[v * 3 + 2] / 20,
								group.material.order,
								mask,
								!group.material.blend
							].join( ":" )
						);
					}
				}
			}
		}
		assert.deepEqual( actual.sort(), expected, `LOD ${lod}` );
	}
});
test("installed city bundle fits the decoded scene admission budget", () => {
	const bytes = readPublishedAssetBytesSync(
		"/assets/world/china/region-62a8.json",
		CLIENT_PUBLIC_ROOT
	);
	const scene = createWorldDecoder().decode( bytes );
	assert.ok( scene.groups.length > 0 );
	assert.equal(
		scene.starRandomState,
		3749729837,
		"Inline published sky carries the same construction continuation"
	);
});

test("published Constantinople sidewalk placements preserve native rotation and visible bounds", async () => {
	const bytes = readPublishedAssetBytesSync(
		"/assets/world/constantinople/region-694e.json",
		CLIENT_PUBLIC_ROOT
	);
	const bundle = JSON.parse( new TextDecoder().decode( bytes ) );
	const resources = bundle.objects.resources;
	const refs = resources.bsr.filter( row => /euro_constan_st\d/.test( row.sourcePath ) );
	const ids = new Set( refs.map( row => row.objectId ) );
	bundle.objects.placements = bundle.objects.placements.filter( row => ids.has( row.objectId ) );
	bundle.objects.resources = { ...resources, bsr: refs };
	bundle.terrain = { blocks: [] };
	delete bundle.sky;
	const scene = createWorldDecoder().decode( bundle, true );
	const key = p => [ Number( p.regionId ), p.objectId, p.uid, p.position.x, p.position.y, p.position.z ].join( ":" );
	const memberships = new Map();
	for ( const p of bundle.objects.placements ) {
		const region = Number( p.sourceSector?.sectorId ?? p.regionId );
		const cell = [
			((region & 255) - bundle.source.sectorX) * 6 + (p.blockX ?? Math.floor( p.position.x / 320 )),
			((region >>> 8) - bundle.source.sectorY) * 6 + (p.blockZ ?? Math.floor( p.position.z / 320 ))
		];
		const cells = memberships.get( key( p ) ) ?? new Set();
		cells.add( cell.join( ":" ) );
		memberships.set( key( p ), cells );
	}
	assert.ok(
		[ ...memberships.values() ].some( cells => cells.size > 1 ),
		"Fixture includes placements shared by multiple cells"
	);
	for ( const group of scene.groups ) {
		assert.equal( group.visibility.length, group.geometry.instances.length / 16 );
		assert.equal(
			new Set( group.visibility.map( v => v.id ) ).size,
			group.visibility.length,
			"Shared cells must not duplicate draws"
		);
		for ( const v of group.visibility ) {
			assert.deepEqual( new Set( v.cells.map( cell => cell.join( ":" ) ) ), memberships.get( v.id ) );
		}
	}
	const positions = new Map(
		bundle.objects.placements.map(
			p => [
				[ Number( p.regionId ), p.objectId, p.uid, p.position.x, p.position.y, p.position.z ].join( ":" ),
				p
			]
		)
	);
	let checked = 0;
	for ( const p of positions.values() ) {
		const ref = refs.find( r => r.objectId === p.objectId ), region = Number( p.regionId );
		const x = p.position.x + ((region & 255) - bundle.source.sectorX) * 1920,
			z = p.position.z + ((region >>> 8) - bundle.source.sectorY) * 1920;
		for ( const meshPath of ref.renderMeshSection.paths ) {
			const group = scene.groups.find( g => g.id.startsWith( "object:" + meshPath + ":" ) );
			assert.ok( group, meshPath );
			const instances = group.geometry.instances;
			let matrix;
			for ( let i = 0; i < instances.length; i += 16 ) {
				if (
					Math.abs( instances[i + 12] - x ) < .01 && Math.abs( instances[i + 13] - p.position.y ) < .01 &&
					Math.abs( instances[i + 14] - z ) < .01
				) {
					matrix = instances.subarray( i, i + 16 );
					break;
				}
			}
			assert.ok( matrix, `Missing placement ${p.uid}` );
			const v = group.geometry.positions, c = Math.cos( p.yaw ), s = Math.sin( p.yaw );
			for ( let i = 0; i < v.length; i += 3 ) {
				assert.ok( Math.abs( matrix[0] * v[i] + matrix[8] * v[i + 2] - (c * v[i] - s * v[i + 2]) ) < .01 );
				assert.ok( Math.abs( matrix[2] * v[i] + matrix[10] * v[i + 2] - (s * v[i] + c * v[i + 2]) ) < .01 );
				assert.ok(
					Math.hypot( v[i], v[i + 1], v[i + 2] ) <= group.instanceRadius + .01,
					"Culling bound must contain every sidewalk vertex"
				);
			}
			checked++;
		}
	}
	assert.ok( checked > 100, `Only ${checked} published sidewalk instances exercised` );
});

test("streamed outdoor region resolves its neighborhood and only referenced meshes", async () => {
	const decoder = createWorldDecoder(),
		read = path => readPublishedAssetBytesSync( path, CLIENT_PUBLIC_ROOT );
	const resolved = await decoder.resolve(
		read( "/assets/world/outdoor/regions/region-62a9.json" ),
		async path => read( path )
	);
	assert.equal( resolved.terrain.sectors.length, 9 );
	assert.ok( resolved.objects.resources.meshes.length > 0 );
	assert.ok( resolved.objects.resources.meshes.length < 5813 );
	const scene = decoder.decode( resolved );
	assert.equal( scene.originRegion, 0x62a9 );
	assert.ok( scene.environment );
	assert.equal(
		scene.starRandomState,
		3749729837,
		"Packed shared sky carries construction RNG continuation through the worker"
	);
	assert.ok( scene.groups.some( group => group.material.frames?.length === 30 ) );
	assert.ok( scene.groups.some( group => group.instanceRadius !== undefined ) );
});
test("terrain decoding refuses expansion beyond its budget before building output arrays", () => {
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: {
			blocks: [ { blockX: 0, blockZ: 0, heights: Array( 289 ).fill( 0 ), textureData: Array( 289 ).fill( 0 ) } ]
		},
		terrainTextures: { tileCatalog: { referencedTiles: [ { textureId: 0, imagePublicPath: "/assets/t.png" } ] } },
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	const bytes = new TextEncoder().encode( JSON.stringify( bundle ) );
	assert.throws(
		() => createWorldDecoder( 1024 ).decode( bytes ),
		/decode scratch budget exceeded: \d+ bytes > 1024 bytes/
	);
	assert.ok( createWorldDecoder().decode( bytes ).groups.length > 0 );
});

test("normal water is the native depth mask times the 180/255 factor; special water is 200/255 opaque", () => {
	const block = {
		blockX: 0,
		blockZ: 0,
		heights: Array( 289 ).fill( 0 ),
		textureData: Array( 289 ).fill( 0 ),
		water: { type: 0, waveType: 0, height: 20 }
	};
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [ block ] },
		terrainTextures: { tileCatalog: { referencedTiles: [ { textureId: 0, imagePublicPath: "tile" } ] } },
		water: { specialTexturePublicPath: "special", normalFramePublicPaths: [ "water0", "water1" ] },
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	const water = () =>
		createWorldDecoder().decode( new TextEncoder().encode( JSON.stringify( bundle ) ) ).groups.find( group =>
			group.material.frames
		);
	const draw = water();
	assert.equal( draw.geometry.positions.length, 289 * 3 );
	// The vertex carries the depth mask alone: (20 - 0) * 0.5 = 10, over 15
	// (SWorldCell_BuildWaterMaskTexture 8872B0). The 180/255 texture factor
	// (8AF0BA) rides on the group material colour and multiplies it in the
	// shader, so the two terms stay separately observable here.
	for ( let i = 3; i < draw.geometry.colors.length; i += 4 ) {
		assert.ok( Math.abs( draw.geometry.colors[i] - 10 / 15 ) < 1e-6, `vertex alpha ${draw.geometry.colors[i]}` );
	}
	assert.deepEqual( draw.material.color, [ 1, 1, 1, 180 / 255 ] );
	assert.equal( draw.material.depthWrite, undefined );
	assert.equal( Math.max( ...draw.geometry.uvs ), 4 );
	// A sheet at or under the terrain has an all-zero mask: the cell is dry
	// and emits no sheet at all (the native skip at 8AE761).
	block.water.height = 0.5;
	assert.equal( water(), undefined );
	block.water.height = -1;
	assert.equal( water(), undefined );
	block.water.type = 1;
	assert.equal( water(), undefined );
	block.water.waveType = 1;
	assert.equal( water(), undefined );
	const special = createWorldDecoder().decode( new TextEncoder().encode( JSON.stringify( bundle ) ) ).groups.find(
		g => g.material.texture === "special"
	);
	assert.equal( special.geometry.positions.length, 12 );
	assert.equal( special.material.frames, undefined );
	assert.equal( special.material.water, false );
	// Special water carries its 200/255 factor in the vertex colour alone;
	// the material colour stays untinted.
	assert.deepEqual( special.material.color, [ 1, 1, 1, 1 ] );
	// The ice is 200/255 opaque (TFACTOR 0xC8000000, 8BADC2), blended over
	// what lies beneath it, not a solid sheet.
	assert.equal( special.material.blend, true );
	assert.equal( special.material.depthWrite, true );
	for ( let i = 3; i < special.geometry.colors.length; i += 4 ) {
		assert.ok(
			Math.abs( special.geometry.colors[i] - 200 / 255 ) < 1e-6,
			`vertex alpha ${special.geometry.colors[i]}`
		);
	}
	bundle.source.sectorY = 128;
	assert.ok(
		!createWorldDecoder().decode( new TextEncoder().encode( JSON.stringify( bundle ) ) ).groups.some( g =>
			g.id.startsWith( "water:" )
		)
	);
});

test("complete mission Constantinople fits unchanged admission with shared cell heights and lean lightmaps", async () => {
	const decoder = createWorldDecoder(),
		read = p => readPublishedAssetBytesSync( p, CLIENT_PUBLIC_ROOT );
	const bundle = await decoder.resolve(
		read( "/assets/world/outdoor/regions/region-694f.json" ),
		async p => read( p )
	);
	assert.equal( bundle.objects.placements.length, 3324 );
	const scene = decoder.decode( bundle );

	const { worldSceneBytes, copyWorldScene, prepareWorldScene, WORLD_SCENE_BYTES } = await import(
		sourceFileUrl( path.join( root, "src/engine/foundation/rendering/world-scene.ts" ) ).href
	);
	assert.equal( WORLD_SCENE_BYTES, 192 * 1024 * 1024 );
	assert.ok( worldSceneBytes( scene ) < WORLD_SCENE_BYTES );
	// Authored cloth needs independent simulation per placement: 44 formerly
	// instanced draws split without removing scenery or raising admission.
	assert.equal( scene.groups.length, 961, "All scenery and independent cloth draws fit the budget" );
	const cloth = scene.groups.filter( g => g.geometry.cloth );
	assert.ok( cloth.length );
	assert.ok( cloth.every( g => g.geometry.instances.length === 16 ) );
	const lightmaps = scene.groups.filter( g => g.material.lightmap );
	assert.ok( lightmaps.length );
	assert.ok(
		lightmaps.every( g => !g.geometry.colors && !g.geometry.maskUVs ),
		"Lightmaps do not allocate terrain mask attributes"
	);
	const copy = copyWorldScene( scene ), grids = new Map();
	for ( const group of copy.groups ) {
		for ( const range of group.ranges ?? [] ) {
			const key = range.cell.join( ":" );
			if ( grids.has( key ) ) assert.equal( range.heights, grids.get( key ) );
			else grids.set( key, range.heights );
		}
	}
	assert.equal( grids.size, 324 );
	assert.equal( worldSceneBytes( copy ), worldSceneBytes( scene ) );
	// Terrain is held as its packed stream and positions; the split copies of
	// the other attributes are left behind (world-scene.ts prepareWorldScene).
	let split = 0;
	for ( const group of scene.groups ) {
		if ( !group.ranges ) continue;
		const g = group.geometry;
		split += [ g.normals, g.uvs, g.colors, g.maskUVs ].reduce( ( sum, s ) => sum + (s?.byteLength ?? 0), 0 );
	}
	assert.ok( split > 0 );
	assert.equal( prepareWorldScene( scene ).bytes, worldSceneBytes( scene ) - split );
});

test("a cave region's bundle draws no sky or lens flares, an outdoor one does (#1793 plane feature sky)", () => {
	const bundle = JSON.parse( new TextDecoder().decode( readPublishedAssetBytesSync(
		"/assets/world/china/region-62a8.json",
		CLIENT_PUBLIC_ROOT
	) ) );
	bundle.terrain = { blocks: [] };
	bundle.objects.placements = [];
	assert.ok( bundle.sky?.flareTexturePublicPaths?.length, "fixture: the city bundle carries a sky with flares" );
	const outdoor = createWorldDecoder().decode( structuredClone( bundle ), true );
	assert.ok( outdoor.groups.length > 0, "outdoors the sky draws" );
	assert.deepEqual( outdoor.flareTextures, bundle.sky.flareTexturePublicPaths );
	const cave = structuredClone( bundle );
	cave.source.sectorY |= 0x80;
	const indoor = createWorldDecoder().decode( cave, true );
	assert.equal( indoor.groups.length, 0, "a cave draws no sky groups" );
	assert.equal( indoor.flareTextures, undefined, "a cave has no lens flares" );
});

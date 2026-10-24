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
import { NATIVE_ONLY } from "../helpers/expanded-build.mjs";

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
			const projection = readDofPresentation(
				fs.readFileSync( join( dataExtractedRoot, "dungeon", dir, name ) ),
				name
			);
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
test( "all twelve well-formed retail DOFs retain exact water ownership, transforms, tint and fog", NATIVE_ONLY, () => {
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
					ambientColor: block.unk1,
					fog: block.fog,
					waterObjects: expected,
					// Every other object the block places (torches, cobwebs, doors, boulders).
					blockObjects: block.objects.flatMap( ( o, i ) =>
						o.flag & 4 ?
							[] :
							[ {
								index: i,
								name: o.name,
								path: o.path,
								position: o.position,
								rotation: o.rotation,
								scale: o.scale,
								flags: o.flag
							} ]
					),
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
} );

import { resolveSkyTextures } from "../../../../scripts/build/world/assets/copySkyImages.mjs";
test("flare publication retains native mip resources and the shared original sun source", () => {
	const sky = resolveSkyTextures();
	assert.deepEqual(
		sky.flareTexturePublicPaths,
		Array.from( { length: 8 }, ( _, i ) => `/assets/images/Map_extracted/sun/lens${i + 1}.texture` )
	);
	assert.equal( new Set( sky.textures.map( t => t.sourcePath ) ).size, sky.textures.length );
	// The sun disc (lens2) ships its PNG beside its container; the other flares
	// ship only their containers, and their rows name exactly those.
	for ( const [index, publicPath] of sky.flareTexturePublicPaths.entries() ) {
		const shipped = index === 1 ? publicPath.replace( /\.texture$/, ".png" ) : publicPath;
		assert.ok( sky.textures.some( t => t.publicPath === shipped ), shipped );
	}
	assert.equal( sky.sunTexturePublicPath, sky.flareTexturePublicPaths[1].replace( /\.texture$/, ".png" ) );
});

import { tmpdir } from "node:os";
import { extractedRoot, imageSourceRoot, normalizeAssetPath } from "../../../../scripts/build/world/paths.mjs";
import { buildDungeonResourceManifest } from "../../../../scripts/build/world/assets/buildDungeonResources.mjs";
import { buildDungeonWorlds } from "../../../../scripts/build/world/assets/buildDungeonWorlds.mjs";
import { collectDungeonParticleReferences } from "../../../../scripts/build/effects/buildEffectPrograms.mjs";
import {
	parseJmxVisualEffect,
	collectJmxEffectTexturePaths
} from "../../../../scripts/build/effects/parseJmxVisualEffect.mjs";

const { createWorldDecoder } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/world/world.ts" ).href
);
const { createWorldStream } = await import(
	sourceFileUrl( "src/engine/runtime/world/world.ts" ).href
);
const { createNavigation } = await import(
	sourceFileUrl(
		"src/engine/foundation/navigation/clipper.ts"
	).href
);
const { createNavigationResources } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/navigation/navigation.ts" ).href
);
const { createNavigationStream } = await import(
	sourceFileUrl( "src/engine/runtime/navigation/navigation.ts" ).href
);
const { WORLD_SCENE_BYTES, worldSceneBytes } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/world-scene.ts" ).href
);
const { createEffectPrograms } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/effects/program/program.ts" ).href
);

/*
================
portalInterior
================
*/
function portalInterior( p, e, regionId ) {
	const m = p.mesh,
		v = m.vertices,
		at = m.edges[e * 6] * 3,
		bt = m.edges[e * 6 + 1] * 3,
		cell = m.edges[e * 6 + 2] * 3;
	let x = (v[at] + v[bt]) / 2, y = (v[at + 1] + v[bt + 1]) / 2, z = (v[at + 2] + v[bt + 2]) / 2, cx = 0, cz = 0;
	for ( let k = 0; k < 3; k++ ) {
		cx += v[m.cells[cell + k] * 3] / 3;
		cz += v[m.cells[cell + k] * 3 + 2] / 3;
	}
	const d = Math.hypot( cx - x, cz - z );
	x += (cx - x) / d * .5;
	z += (cz - z) / d * .5;
	return {
		regionId,
		x: Math.cos( p.yaw ) * x - Math.sin( p.yaw ) * z + p.x,
		y: y + p.y,
		z: Math.sin( p.yaw ) * x + Math.cos( p.yaw ) * z + p.z,
		angle: 0
	};
}

/*
================
loadDungeonFixture

Builds the dungeon resource manifest and every dungeon world bundle once
for the region and effects tests below. The build takes seconds solo and
far longer under parallel gate load, so it runs at module load, outside
every test's clock: each test then carries only its own region's
decode/stream/clip work against the unchanged 60 s cap, instead of the
shared build riding the first test until it times out (the parallel-run
flake this split removes). The temp manifest is deleted as soon as its
bytes are in memory; nothing below reads it back.
================
*/
async function loadDungeonFixture() {
	const tempRoot = fs.mkdtempSync( join( tmpdir(), "sro-dungeon-e2e-" ) );
	try {
		const manifestPath = join( tempRoot, "dungeon-resources.json" );
		await buildDungeonResourceManifest( { extractedRoot, targetPath: manifestPath } );
		const manifestBytes = new Uint8Array( fs.readFileSync( manifestPath ) );
		const manifest = JSON.parse( Buffer.from( manifestBytes ).toString( "utf8" ) );
		const { bundles } = await buildDungeonWorlds( manifest, { publishFiles: false } );
		return { manifestBytes, manifest, bundles };
	} finally {
		fs.rmSync( tempRoot, { recursive: true, force: true } );
	}
}

const dungeonFixture = await loadDungeonFixture();

// Portal seams crossed per region; the eight counts sum to the 2798 the
// single combined test asserted before the split.
const dungeonRegionPortals = {
	32769: 300,
	32770: 0,
	32771: 264,
	32772: 1128,
	32773: 1098,
	32774: 0,
	32775: 0,
	32777: 8
};

test("Donwhang's torches, doors and boulders decode as drawable, block-culled groups", () => {
	const bundle = dungeonFixture.bundles.get( 32769 );
	const decoded = createWorldDecoder().decode(
		new TextEncoder().encode( JSON.stringify( bundle ) ),
		false,
		{ origin: 32769, part: "all" }
	);
	assert.deepEqual( decoded.warnings.filter( w => /Object resource absent|Mesh absent/.test( w ) ), [] );
	const instancesOf = name => {
		const groups = decoded.groups.filter( g => g.id.toLowerCase().includes( name ) );
		return { groups, count: groups.reduce( ( n, g ) => n + (g.geometry.instances?.length ?? 0) / 16, 0 ) };
	};
	// The cave file places 4 sealed doors, 10 boulder piles and 364 wall torches.
	assert.equal( instancesOf( "out_obj_door" ).count, 4 );
	assert.equal( instancesOf( "out_obj_stone" ).count, 10 );
	assert.ok( instancesOf( "flame_a" ).count >= 364 );
	for ( const name of [ "out_obj_door", "out_obj_stone", "flame_a", "cobweb" ] ) {
		const { groups } = instancesOf( name );
		assert.ok( groups.length > 0, `${name}: no groups` );
		assert.ok( groups.every( g => g.dungeonBlock !== undefined ), `${name}: groups follow the room culling` );
	}
	// Their particle emitters follow the same rooms.
	assert.ok( (decoded.scenery?.length ?? 0) > 0 );
	assert.ok( decoded.scenery.every( e => Number.isInteger( e.dungeonBlock ) ), "every cave emitter names its room" );
});

for ( const regionId of [ 32769, 32770, 32771, 32772, 32773, 32774, 32775, 32777 ] ) {
	test(
		`0x8000-bit dungeon region ${regionId} streams, decodes mesh/collision/water and crosses every room portal seam`,
		{ timeout: 60000 },
		async () => {
			const { manifestBytes, bundles } = dungeonFixture;
			const bundle = bundles.get( regionId );
			assert.ok( bundle, `missing dungeon world bundle for ${regionId}` );

			const raw = new TextEncoder().encode( JSON.stringify( bundle ) );
			const worldDecoder = createWorldDecoder();
			const decoded = worldDecoder.decode( raw, false, { origin: regionId, part: "all" } );
			assert.equal( decoded.originRegion, regionId );
			assert.equal( decoded.id, `region:${regionId}:${regionId}:all` );
			assert.ok(
				worldSceneBytes( decoded ) <= WORLD_SCENE_BYTES,
				`region ${regionId} exceeds WORLD_SCENE_BYTES`
			);
			assert.ok( decoded.groups.length > 0, `region ${regionId} has no scene groups` );
			assert.ok( (decoded.dungeonVisibility?.length ?? 0) > 0, `region ${regionId} has no dungeonVisibility` );
			assert.deepEqual(
				decoded.dungeonAmbient,
				bundle.dungeonBlocks.map( block => block.ambientColor )
			);

			// Verify 0x8000-bit world streaming requests only the single dungeon bundle per floor
			// (never a 3x3 outdoor neighborhood) and admits each floor cleanly.
			const worldRequests = [];
			const worldReady = new Map();
			let nextWorldId = 0;
			const worldStream = createWorldStream(
				{
					available: () => 4,
					request( url, maxByteLength, decode ) {
						const id = ++nextWorldId;
						worldRequests.push( { url, maxByteLength, decode } );
						return id;
					},
					take: id => worldReady.get( id ) ?? null,
					cancel: () => {}
				},
				{
					cancelWorldUpdate: () => {},
					setWorld: () => {},
					adoptWorld: () => {},
					setWorldTexture: () => {},
					neededWorldTextures: () => [],
					worldStats: () => ({ pendingTextures: 0, visibleGroups: 1 }),
					setWorldCamera: () => {}
				},
				"https://assets.test"
			);
			worldStream.step( { regionId, x: 0, y: 0, z: 0, angle: 0 } );
			assert.equal( worldRequests.length, 1 );
			assert.equal(
				worldRequests[0].url,
				`https://assets.test/assets/world/dungeon/regions/0x${regionId.toString( 16 ).padStart( 4, "0" )}.json`
			);
			assert.equal( worldRequests[0].decode, "world" );

			// Verify 0x8000-bit navigation streaming and room portal seam crossing.
			const navResources = createNavigationResources();
			const nav = createNavigation();
			const navProduct = await navResources.resolve( manifestBytes, regionId, async () => manifestBytes );
			assert.equal( navProduct.regionId, regionId );
			assert.ok( navProduct.objects.length > 0 );
			nav.install( regionId, navProduct );

			const navRequests = [];
			const navPending = new Map();
			const navSent = [];
			let nextNavId = 0;
			const navStream = createNavigationStream(
				{
					available: () => 4,
					request( url, maxByteLength, decode ) {
						const id = ++nextNavId;
						navRequests.push( { url, maxByteLength, decode } );
						navPending.set( id, { kind: "navigation", product: navProduct } );
						return id;
					},
					take: id => {
						const r = navPending.get( id );
						navPending.delete( id );
						return r ?? null;
					},
					cancel: () => {}
				},
				c => navSent.push( c ),
				"https://assets.test"
			);
			navStream.step( { regionId, x: 0, y: 0, z: 0, angle: 0 } );
			assert.equal( navRequests.length, 1 );
			assert.equal(
				navRequests[0].url,
				`https://assets.test/assets/world/dungeon/dungeon-resources.json#${regionId}`
			);
			assert.equal( navRequests[0].decode, "navigation" );
			navStream.step( { regionId, x: 0, y: 0, z: 0, angle: 0 } );
			assert.equal( navSent.length, 1 );
			assert.equal( navSent[0].regionId, regionId );
			assert.equal( navSent[0].bundle.regionId, regionId );

			let portals = 0;
			for ( let i = 0; i < navProduct.objects.length; i++ ) {
				for ( const link of navProduct.objects[i].links ?? [] ) {
					const a = portalInterior( navProduct.objects[i], link.edge, regionId ),
						b = portalInterior( navProduct.objects[link.target], link.targetEdge, regionId ),
						q = nav.clip( a, b );
					portals++;
					assert.ok(
						q && Math.hypot( q.x - b.x, q.z - b.z ) <= .1,
						`portal seam failed in region 0x${regionId.toString( 16 )} room ${i} edge ${link.edge}`
					);
				}
			}
			assert.equal( portals, dungeonRegionPortals[regionId] );
		}
	);
}

test(
	"dungeon room particle references resolve and compile every room EFP end to end",
	{ timeout: 60000 },
	async () => {
		const { manifest } = dungeonFixture;

		// Verify every dungeon room BSR particle reference resolves and compiles in program.ts.
		const dungeonRefs = collectDungeonParticleReferences( manifest );
		const uniqueEfps = [ ...new Set( dungeonRefs.map( r => r.effectPath ) ) ].sort();
		assert.equal( uniqueEfps.length, 70 );
		assert.ok( uniqueEfps.includes( "system/system_dungeonpotal_02.efp" ) );
		// Objects placed in rooms (blockObjects) carry their own programs: the
		// Donwhang torches and sunbeams reported missing in game.
		assert.ok( uniqueEfps.includes( "map/frame5.efp" ) );
		assert.ok( uniqueEfps.includes( "map/404_sunbeams.efp" ) );

		const particlesRoot = join( extractedRoot, "Particles_extracted" );
		const particleImageRoot = join( imageSourceRoot, "Particles_extracted" );
		const effects = {};
		const meshes = {};
		const textures = {};
		for ( const effectPath of uniqueEfps ) {
			const abs = join( particlesRoot, ...effectPath.split( "/" ) );
			const parsed = parseJmxVisualEffect( fs.readFileSync( abs ), effectPath );
			effects[effectPath] = parsed;
			const visitResource = r => {
				for ( const m of r?.meshes ?? [] ) {
					const mp = normalizeAssetPath( m.path ?? "" );
					if ( mp.endsWith( ".bms" ) && !meshes[mp] ) {
						const bmsAbs = join( particlesRoot, ...mp.split( "/" ) );
						const decoded = parseJmxBmsStaticMesh( fs.readFileSync( bmsAbs ), bmsAbs );
						meshes[mp] = {
							sourcePath: mp,
							positions: decoded.positions,
							normals: decoded.normals,
							uvs: decoded.uvs,
							indices: decoded.indices,
							bounds: decoded.bounds
						};
					}
				}
			};
			const visit = obj => {
				visitResource( obj.resource );
				for ( const c of obj.controllers ?? [] ) visitResource( c.resource );
				for ( const ch of obj.children ?? [] ) visit( ch );
			};
			visit( parsed.root );
			for ( const tex of collectJmxEffectTexturePaths( parsed ) ) {
				const png = normalizeAssetPath( tex ).replace( /\.ddj$/i, ".png" );
				textures[png] = `/assets/images/Particles_extracted/${png}`;
				assert.ok( fs.existsSync( join( particleImageRoot, ...png.split( "/" ) ) ), png );
			}
		}
		const catalogBytes = new TextEncoder().encode(
			JSON.stringify( {
				format: "sro-jmx-easyfx-programs",
				version: 3,
				framesPerSecond: 20,
				effects,
				ambiguousEffectAliases: {},
				missingEffects: [],
				meshes,
				textures
			} )
		);
		const effectDecoder = createEffectPrograms();
		for ( const effectPath of uniqueEfps ) {
			const res = effectDecoder.decode( catalogBytes, effectPath );
			assert.ok( res.model.primitives.length > 0, effectPath );
		}
	}
);

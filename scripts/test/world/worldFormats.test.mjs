/*
===========================================================================

worldFormats.test.mjs - parsers for the retail world formats

Terrain, object, navmesh and dungeon parsers against real extracted map
data. Roots come from build/world/paths.mjs.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { parseJmxMapObjectPlacementO2, parseJmxMapTerrain, readJmxMapObjectInfo } from "../../build/world/index.mjs";
import {
	NAVMESH_HEIGHT_AXIS_VERTICES,
	NAVMESH_TILES_PER_AXIS,
	parseNavmesh
} from "../../build/world/navmesh/parseNavmesh.mjs";
import { buildDungeonResourceManifest } from "../../build/world/assets/buildDungeonResources.mjs";
import { parseJmxBmsStaticMesh } from "../../build/world/objects/formats.mjs";
import { deriveTitleTerrainSectorCoverage } from "../../build/world/sworld/deriveTitleTerrainSectorCoverage.mjs";
import { readPublishedAssetJson } from "../../lib/publishedAsset.mjs";
import { extractedRoot, rebuildRoot } from "../../build/world/paths.mjs";

const sectorMapRoot = path.join( extractedRoot, "Map_extracted", "105" );
const terrainPath = path.join( sectorMapRoot, "78.m" );
const objects2Path = path.join( sectorMapRoot, "78.o2" );
const objectInfoPath = path.join( extractedRoot, "Map_extracted", "object.ifo" );
const navmeshPath = path.join( extractedRoot, "Data_extracted", "navmesh", "nv_694e.nvm" );
const harborBridgeMeshSourcePath = "prim/mesh/bldg/europe/constantinople/harbor/euro_esteuro_port01.bms";
const harborBridgeMeshPath = path.join( extractedRoot, "Data_extracted", ...harborBridgeMeshSourcePath.split( "/" ) );
const navmeshTileCount = NAVMESH_TILES_PER_AXIS * NAVMESH_TILES_PER_AXIS;

test("title terrain coverage follows normalized camera sectors and expands their native halo", () => {
	const coverage = deriveTitleTerrainSectorCoverage(
		{
			camera: [
				{ sectorX: 78, sectorY: 105, position: { x: 3839, z: -1 } },
				{ sectorX: 78, sectorY: 105, position: { x: 3840, z: 1920 } }
			]
		},
		{ sectorX: 78, sectorY: 105, terrainSectorMargin: 1 }
	);

	assert.deepEqual(
		coverage.sectorGrid.cameraPathSectors.map( (
			{ sectorId, sectorX, sectorY }
		) => [ sectorId, sectorX, sectorY ] ),
		[
			[ "0x684f", 79, 104 ],
			[ "0x694e", 78, 105 ],
			[ "0x6a50", 80, 106 ]
		]
	);
	assert.equal( coverage.sectors.length, 20 );
	assert.deepEqual(
		{
			minSectorX: coverage.sectorGrid.minSectorX,
			maxSectorX: coverage.sectorGrid.maxSectorX,
			minSectorY: coverage.sectorGrid.minSectorY,
			maxSectorY: coverage.sectorGrid.maxSectorY,
			width: coverage.sectorGrid.width,
			height: coverage.sectorGrid.height
		},
		{ minSectorX: 77, maxSectorX: 81, minSectorY: 103, maxSectorY: 107, width: 5, height: 5 }
	);
});

test("title terrain coverage rejects malformed camera and margin contracts", () => {
	assert.throws(
		() => deriveTitleTerrainSectorCoverage( null, { sectorX: 78, sectorY: 105, terrainSectorMargin: -1 } ),
		/non-negative integer/
	);
	assert.throws(
		() => deriveTitleTerrainSectorCoverage( { camera: [ {} ] }, { sectorX: 78, sectorY: 105 } ),
		/camera\.sectorX/
	);
});

test("JMXVMAPO1001 105/78.o2 parses the known title sector placement counts", async () => {
	const parsed = parseJmxMapObjectPlacementO2( await readFile( objects2Path ), objects2Path );

	assert.equal( parsed.signature, "JMXVMAPO1001" );
	assert.equal( parsed.placements.length, 455 );
	assert.equal( parsed.uniqueObjectIds.length, 48 );
	assert.deepEqual( parsed.slotCounts, [ 0, 0, 196, 259 ] );
	assert.equal( parsed.consumedBytes, parsed.byteLength );
});

test("JMXVOBJI1000 object.ifo resolves every referenced 105/78.o2 object id", async () => {
	const objects = parseJmxMapObjectPlacementO2( await readFile( objects2Path ), objects2Path );
	const objectInfo = await readJmxMapObjectInfo( objectInfoPath );

	assert.equal( objectInfo.signature, "JMXVOBJI1000" );
	assert.equal( objectInfo.declaredCount, 1966 );
	assert.equal( objectInfo.entries.length, 1966 );

	for ( const objectId of objects.uniqueObjectIds ) {
		assert.ok( objectInfo.entriesById[String( objectId )], `object.ifo is missing object id ${objectId}` );
	}

	assert.equal(
		objectInfo.entriesById["1609"].sourcePath,
		"res/bldg/europe/constantinople/euro_constan_bl_01.bsr"
	);
	assert.equal(
		objectInfo.entriesById["1610"].sourcePath,
		"res/bldg/europe/constantinople/euro_constan_bl_03.bsr"
	);
	assert.equal(
		objectInfo.entriesById["1612"].sourcePath,
		"res/bldg/europe/constantinople/euro_constan_bl_04.bsr"
	);
	assert.equal(
		objectInfo.entriesById["1677"].sourcePath,
		"res/bldg/europe/constantinople/euro_constan_streetlight04.bsr"
	);
	assert.equal(
		objectInfo.entriesById["1679"].sourcePath,
		"res/bldg/europe/constantinople/minga/euro_constan_tree02_01.bsr"
	);
	assert.equal(
		objectInfo.entriesById["1550"].sourcePath,
		"res/nature/europe/east eurpoe/garden/euro_grs_weed01.bsr"
	);
});

test("JMXVMAPM1000 105/78.m consumes all 36 terrain blocks", async () => {
	const terrain = parseJmxMapTerrain( await readFile( terrainPath ), terrainPath );

	assert.equal( terrain.signature, "JMXVMAPM1000" );
	assert.equal( terrain.blocks.length, 36 );
	assert.equal( terrain.blockSizeBytes, 2575 );
	assert.equal( terrain.consumedBytes, terrain.byteLength );
});

test("JMXVNVM 1000 0x694e parses walkability and height maps", async () => {
	const buffer = await readFile( navmeshPath );
	const navmesh = parseNavmesh( buffer );

	assert.equal( navmesh.objects.length, navmesh.objectCount );
	assert.ok( navmesh.objects.every( ( object ) => Number.isInteger( object.linkEdgeCount ) ) );
	assert.ok(
		navmesh.objects.every( ( object ) =>
			object.linkEdgeCount === 0 || Buffer.from( object.linkEdges, "base64" ).length === object.linkEdgeCount * 6
		)
	);
	assert.equal( navmesh.cells.length, navmesh.totalCellCount );
	assert.equal( navmesh.openCells.length, navmesh.openCellCount );
	assert.ok( navmesh.cells.some( ( cell ) => cell.objectIndices.length > 0 ) );
	assert.equal( navmesh.globalEdges.count, navmesh.globalEdgeCount );
	assert.equal( navmesh.globalEdges.lines.length, navmesh.globalEdgeCount * 4 );
	assert.equal( navmesh.globalEdges.flags.length, navmesh.globalEdgeCount );
	assert.equal( navmesh.globalEdges.assocDirections.length, navmesh.globalEdgeCount * 2 );
	assert.equal( navmesh.globalEdges.assocCells.length, navmesh.globalEdgeCount * 2 );
	// Cast: readEdgeBlock carries assocRegions only for global edges, and
	// these ARE the global edges - the format guarantees the field here.
	assert.equal( /** @type {Uint16Array} */ (navmesh.globalEdges.assocRegions).length, navmesh.globalEdgeCount * 2 );
	assert.equal( navmesh.internalEdges.count, navmesh.internalEdgeCount );
	assert.equal( navmesh.internalEdges.lines.length, navmesh.internalEdgeCount * 4 );
	assert.equal( navmesh.internalEdges.flags.length, navmesh.internalEdgeCount );
	assert.equal( navmesh.internalEdges.assocDirections.length, navmesh.internalEdgeCount * 2 );
	assert.equal( navmesh.internalEdges.assocCells.length, navmesh.internalEdgeCount * 2 );
	assert.equal( navmesh.tileCellIds.length, navmeshTileCount );
	assert.equal( navmesh.tileFlags.length, navmeshTileCount );
	assert.equal( navmesh.tileTextureIds.length, navmeshTileCount );
	assert.equal( navmesh.blockedTiles.length, navmeshTileCount );
	assert.equal( navmesh.heightMap.length, NAVMESH_HEIGHT_AXIS_VERTICES * NAVMESH_HEIGHT_AXIS_VERTICES );
	assert.equal( navmesh.planeType.length, 36 );
	assert.equal( navmesh.planeHeight.length, 36 );
	assert.equal( navmesh.bytesConsumed, buffer.length );
	assert.ok( Number.isFinite( navmesh.heightMap[0] ) );
});

test("JMXVNVM 1000 object link rows decode as reciprocal {neighborObj, neighborEdge, myEdge} u16 triples", async () => {
	// Native consumer: CRTNavMeshTerrain_StepPlacedObject @0x0040429c walks NavMeshInstance+0xa0 rows
	// (read by CRTNavMeshTerrain_LoadFromNvm CRTNavMeshTerrain_Load) to hand a mover off to
	// a NEIGHBOR object mesh (multi-object bridge chains). Row layout verified
	// against the ASM and the full 3863-file corpus: u16[0] neighbor object
	// index in the SAME region (0xffff = one-sided), u16[1] neighbor outline
	// edge index (CRTNavMesh_EnterCellThroughEdge entry), u16[2] own outline edge index (the scan
	// key from CRTNavMeshObj_FindOutlineEdgeIndex). nv_17ab is a known multi-object chain region.
	const linked = parseNavmesh(
		await readFile( path.join( extractedRoot, "Data_extracted", "navmesh", "nv_17ab.nvm" ) )
	);
	const rowsByObject = linked.objects.map( ( object ) =>
		object.linkEdgeCount > 0 ?
			decodeLinkRows( Buffer.from( object.linkEdges, "base64" ), object.linkEdgeCount ) :
			[]
	);

	const allRows = rowsByObject.flat();
	assert.ok( allRows.length > 0, "nv_17ab should carry object link rows" );
	for ( const [objectIndex, rows] of rowsByObject.entries() ) {
		for ( const [neighborObj, neighborEdge, myEdge] of rows ) {
			if ( neighborObj === 0xffff ) continue;
			assert.ok( neighborObj < linked.objectCount, "neighbor index stays inside the region object vector" );
			assert.ok(
				rowsByObject[neighborObj].some(
					( [backObj, backEdge, backMyEdge] ) =>
						backObj === objectIndex && backEdge === myEdge && backMyEdge === neighborEdge
				),
				`object ${objectIndex} row -> ${neighborObj} must have the reciprocal row`
			);
		}
	}
});

/*
================
decodeLinkRows
================
*/
function decodeLinkRows( buffer, count ) {
	const rows = [];
	for ( let i = 0; i < count; i += 1 ) {
		rows.push( [
			buffer.readUInt16LE( i * 6 ),
			buffer.readUInt16LE( i * 6 + 2 ),
			buffer.readUInt16LE( i * 6 + 4 )
		] );
	}
	return rows;
}

test("JMXVBMS 0110 preserves native object-nav tail bytes for harbor bridge mesh", async () => {
	const mesh = parseJmxBmsStaticMesh( await readFile( harborBridgeMeshPath ), harborBridgeMeshSourcePath );
	const payload = mesh.nativePayloads?.[0];

	assert.ok( payload, "bridge mesh should expose its unresolved native payload tail" );
	assert.equal( payload.kind, "bms-offset7-post-payload-tail" );
	assert.equal( payload.headerOffsetIndex, 7 );
	assert.equal( payload.byteOffset, mesh.headerOffsets[7] );
	assert.equal( payload.byteLength, mesh.byteLength - payload.byteOffset );
	assert.equal( payload.countHint, 410 );
	assert.match( payload.sha256, /^[0-9a-f]{64}$/ );
	assert.equal( Buffer.from( payload.rawBase64, "base64" ).byteLength, payload.byteLength );
});

test("JMXVBMS 0110 0x400 meshes carry their lightmap path and second UV set", async () => {
	const sourcePath = "prim/mesh/dun/china/jinsi_cv/floor_01/jinsi_floor1_dragon01.bms";
	const buffer = await readFile( path.join( extractedRoot, "Data_extracted", ...sourcePath.split( "/" ) ) );
	const mesh = parseJmxBmsStaticMesh( buffer, sourcePath );
	assert.ok( mesh.metadata.unknown1 & 0x400 );
	assert.equal(
		mesh.lightmap?.sourcePath,
		"prim/lightmap/dun/china/jinsi_cv/floor_01/jinsi_floor1_dragon01lightingmap.ddj"
	);
	assert.equal( mesh.lightmap.uvs.length, mesh.uvs.length );
	assert.ok( mesh.lightmap.uvs.every( Number.isFinite ) );
	// CPrimMesh_ReadBmsBody (A85F80): 52-byte vertices, then the counted path,
	// which ends exactly where the triangle section begins.
	const pathOffset = mesh.headerOffsets[0] + 4 + mesh.vertexCount * 52;
	assert.equal( pathOffset + 4 + buffer.readUInt32LE( pathOffset ), mesh.headerOffsets[1] );

	const bridge = parseJmxBmsStaticMesh( await readFile( harborBridgeMeshPath ), harborBridgeMeshSourcePath );
	assert.equal( bridge.lightmap !== undefined, (bridge.metadata.unknown1 & 0x400) !== 0 );
});

test("dungeon resource manifest preserves dungeoninfo rows and exact JMXVDOF payload bytes", async ( t ) => {
	const tempDir = await mkdtemp( path.join( tmpdir(), "sro-dungeon-resources-" ) );
	t.after( () => rm( tempDir, { recursive: true, force: true } ) );
	const targetPath = path.join( tempDir, "dungeon-resources.json" );

	const summary = await buildDungeonResourceManifest( {
		extractedRoot,
		targetPath
	} );
	const manifest = JSON.parse( await readFile( targetPath, "utf8" ) );

	assert.equal( summary.dungeonCount, 8 );
	assert.equal( summary.resourceCount, 8 );
	assert.equal( manifest.format, "sro-dungeon-resources" );
	assert.equal( manifest.version, 3 );
	assert.deepEqual(
		manifest.entries.map( ( entry ) => [ entry.regionId, entry.sectorId, entry.dofName ] ),
		[
			[ 1, 0x8001, "Dungeon\\wchina\\Dunhwang_Cv.dof" ],
			[ 9, 0x8009, "Dungeon\\wchina\\event.dof" ],
			[ 7, 0x8007, "Dungeon\\china\\jinsi_floor01.dof" ],
			[ 6, 0x8006, "Dungeon\\china\\jinsi_floor02.dof" ],
			[ 5, 0x8005, "Dungeon\\china\\jinsi_floor03.dof" ],
			[ 4, 0x8004, "Dungeon\\china\\jinsi_floor04.dof" ],
			[ 3, 0x8003, "Dungeon\\china\\jinsi_floor05.dof" ],
			[ 2, 0x8002, "Dungeon\\china\\jinsi_floor06.dof" ]
		]
	);

	const payloadsByName = new Map( manifest.resources.map( ( resource ) => [ resource.normalizedName, resource ] ) );
	for (
		const expectedName of [
			"dungeon/wchina/dunhwang_cv.dof",
			"dungeon/wchina/event.dof",
			"dungeon/china/jinsi_floor01.dof",
			"dungeon/china/jinsi_floor02.dof",
			"dungeon/china/jinsi_floor03.dof",
			"dungeon/china/jinsi_floor04.dof",
			"dungeon/china/jinsi_floor05.dof",
			"dungeon/china/jinsi_floor06.dof"
		]
	) {
		const payload = payloadsByName.get( expectedName );
		assert.ok( payload, `manifest should carry ${expectedName}` );
		const bytes = Buffer.from( payload.rawBase64, "base64" );
		assert.equal( bytes.byteLength, payload.byteLength );
		assert.equal( bytes.toString( "ascii", 0, 12 ), "JMXVDOF 0101" );
		assert.match( payload.sha256, /^[0-9a-f]{64}$/ );
	}
	assert.equal( summary.residentBsrCount, 173 );
	assert.equal( summary.residentNavMeshCount, 173 );
	assert.equal( manifest.navResources.format, "sro-world-nav-object-resources" );
	assert.equal( manifest.navResources.bsr.length, 173 );
	assert.equal( manifest.navResources.meshes.length, 173 );
	assert.ok(
		manifest.navResources.bsr.some(
			( resource ) => resource.sourcePath === "res/dun/wchina/donhwang_cv/floor_1/passage01_01.bsr"
		),
		"the Donwhang DOF's first resident BSR must be indexed by exact resource path"
	);
	for ( const mesh of manifest.navResources.meshes ) {
		assert.ok( mesh.nativePayloads?.length > 0, `${mesh.sourcePath} must carry native nav bytes` );
	}
});

test("sector 0x694e region bundle carries parsed terrain, objects, and object definitions", async () => {
	const bundle = await readPublishedAssetJson( "/assets/world/constantinople/region-694e.json" );

	assert.equal( bundle.source.sectorId, "0x694e" );
	assert.equal( bundle.terrain.blockCount, 36 );
	const seedObjectSector = bundle.objects.sectors.find( ( sector ) => sector.sectorId === "0x694e" );
	assert.ok( seedObjectSector, "the title bundle must retain the authored seed-sector object plane" );
	assert.equal( seedObjectSector.placementCount, 455 );
	assert.equal( seedObjectSector.uniqueObjectCount, 48 );
	assert.deepEqual( seedObjectSector.slotCounts, [ 0, 0, 196, 259 ] );
	const publishedDefinitionIds = new Set(
		bundle.objectInfo.referencedDefinitions.map( ( entry ) => entry.objectId )
	);
	for ( const objectId of seedObjectSector.uniqueObjectIds ) {
		assert.ok(
			publishedDefinitionIds.has( objectId ),
			`the published bundle omitted seed object definition ${objectId}`
		);
	}
	// The installed title bundle owns the merged preload halo and its shared
	// resource projection; validate the resource plane without republishing it.
	assert.ok(
		/** @type {{ resources: { meshes: Array<{ nativePayloads?: unknown[] }> } }} */ (bundle.objects)
			.resources.meshes.some( ( mesh ) => /** @type {number} */ (mesh.nativePayloads?.length) > 0 ),
		"object resource bundle should preserve BMS native payload tails"
	);
	assert.equal( bundle.navmesh.version, 2 );
	assert.equal( bundle.navmesh.heightMapAxisVertices, NAVMESH_HEIGHT_AXIS_VERTICES );
	const seedNavmeshRegion = bundle.navmesh.regions.find( ( region ) => region.regionId === 0x694e );
	assert.ok( seedNavmeshRegion?.blockedTiles, "seed navmesh region should carry base64 blocked tiles" );
	assert.ok( seedNavmeshRegion?.heightMap, "seed navmesh region should carry a base64 height map" );
	assert.ok( seedNavmeshRegion?.objects?.length > 0, "seed navmesh region should carry native objects" );
	assert.ok( seedNavmeshRegion?.cells?.count > 0, "seed navmesh region should carry native cells" );
	assert.equal( base64ByteLength( seedNavmeshRegion.tileCellIds ), navmeshTileCount * 4 );
	assert.equal( base64ByteLength( seedNavmeshRegion.tileFlags ), navmeshTileCount * 2 );
	assert.equal( base64ByteLength( seedNavmeshRegion.tileTextureIds ), navmeshTileCount * 2 );
	assert.ok( seedNavmeshRegion.globalEdges?.count > 0, "seed navmesh region should carry global edges" );
	assert.ok( seedNavmeshRegion.internalEdges?.count > 0, "seed navmesh region should carry internal edges" );
	assert.equal( base64ByteLength( seedNavmeshRegion.planeType ), 36 );
	assert.equal( base64ByteLength( seedNavmeshRegion.planeHeight ), 36 * 4 );
});

/*
================
base64ByteLength
================
*/
function base64ByteLength( base64 ) {
	return Buffer.from( base64, "base64" ).byteLength;
}

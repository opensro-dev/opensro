/*
===========================================================================

buildDungeonResources.mjs - build the shared JMXVDOF 0101 dungeon manifest

Extracts per-room block presentation (transforms, room ambient D3DCOLOR,
fog, water objects, visibility) and navigation meshes from the retail .dof
files listed in dungeoninfo.txt.

===========================================================================
*/
import { resolveDungeonWater } from "./dungeonWater.mjs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../../generatedManifestSidecars.mjs";
import { sha256Hex } from "../../shared/hash.mjs";
import { BinaryReader } from "../../shared/jmxBinaryReader.mjs";
import { writeJsonIfChanged } from "../../shared/jsonOut.mjs";
import { exists } from "../io.mjs";
import { parseJmxBmsStaticMesh, parseJmxResourceBsr } from "../objects/formats.mjs";
import { extractedRoot, normalizeAssetPath, publicRoot, toHex16 } from "../paths.mjs";
import { readDungeonInfoRows } from "./dungeonInfo.mjs";

const DUNGEON_RESOURCE_FORMAT = "sro-dungeon-resources";
const DUNGEON_RESOURCE_VERSION = 3;
export const DUNGEON_RESOURCE_PUBLIC_PATH = "/assets/world/dungeon/dungeon-resources.json";

/*
================
buildDungeonResourceManifest
================
*/
export async function buildDungeonResourceManifest( options = {} ) {
	const sourceExtractedRoot = options.extractedRoot ?? extractedRoot;
	const dungeonInfoPath = options.dungeonInfoPath ??
		path.join( sourceExtractedRoot, "Data_extracted", "dungeon", "dungeoninfo.txt" );
	const dofRoot = options.dofRoot ?? path.join( sourceExtractedRoot, "Data_extracted" );
	const targetPath = options.targetPath ?? path.join( publicRoot, DUNGEON_RESOURCE_PUBLIC_PATH.replace( /^\//, "" ) );

	const entries = await readDungeonInfoRows( dungeonInfoPath );
	const resources = [];
	const residentPaths = new Set();
	const seenDofNames = new Set();
	for ( const entry of entries ) {
		const normalizedName = normalizeDofName( entry.dofName );
		if ( seenDofNames.has( normalizedName ) ) {
			continue;
		}
		seenDofNames.add( normalizedName );
		const sourcePath = resolveDofPath( dofRoot, entry.dofName );
		const bytes = await readFile( sourcePath );
		const presentation = readDofPresentation( bytes, sourcePath );
		const waterSurfaces = await resolveDungeonWater(
			presentation,
			async name => parseJmxResourceBsr( await readFile( dataAssetPath( sourceExtractedRoot, name ) ), name ),
			async name => parseJmxBmsStaticMesh( await readFile( dataAssetPath( sourceExtractedRoot, name ) ), name )
		);
		for ( const residentPath of presentation.blocks.map( block => block.path ) ) {
			residentPaths.add( normalizeAssetPath( residentPath ) );
		}
		resources.push( {
			dofName: entry.dofName,
			normalizedName,
			sourcePath: toExtractedRelative( sourcePath, sourceExtractedRoot ),
			byteLength: bytes.byteLength,
			sha256: sha256Hex( bytes ),
			rawBase64: bytes.toString( "base64" ),
			presentation,
			waterSurfaces
		} );
	}
	const navResources = await buildDungeonNavResources( sourceExtractedRoot, residentPaths );

	const manifest = {
		format: DUNGEON_RESOURCE_FORMAT,
		version: DUNGEON_RESOURCE_VERSION,
		publicPath: DUNGEON_RESOURCE_PUBLIC_PATH,
		source: {
			dungeonInfoPath: toExtractedRelative( dungeonInfoPath, sourceExtractedRoot ),
			dofRoot: toExtractedRelative( dofRoot, sourceExtractedRoot )
		},
		entries: entries.map( entry => ({
			regionId: entry.regionId,
			sectorId: entry.regionId | 0x8000,
			regionHex: toHex16( entry.regionId ),
			sectorHex: toHex16( entry.regionId | 0x8000 ),
			dofName: entry.dofName,
			normalizedName: normalizeDofName( entry.dofName )
		}) ),
		resources,
		navResources
	};

	await mkdir( path.dirname( targetPath ), { recursive: true } );
	await writeJsonIfChanged( targetPath, manifest );
	await refreshPrecompressedSidecars( [ targetPath ], { onlyWhenStale: true } );
	return {
		publicPath: DUNGEON_RESOURCE_PUBLIC_PATH,
		dungeonCount: manifest.entries.length,
		resourceCount: manifest.resources.length,
		totalBytes: resources.reduce( ( sum, resource ) => sum + resource.byteLength, 0 ),
		residentBsrCount: navResources.bsr.length,
		residentNavMeshCount: navResources.meshes.length
	};
}

/*
================
resolveDofPath
================
*/
function resolveDofPath( dofRoot, dofName ) {
	return path.join( dofRoot, ...dofName.split( /[\\/]+/ ) );
}

/*
================
normalizeDofName
================
*/
function normalizeDofName( dofName ) {
	return normalizeAssetPath( dofName );
}

/*
================
toExtractedRelative
================
*/
function toExtractedRelative( filePath, sourceExtractedRoot ) {
	return path.relative( sourceExtractedRoot, filePath ).replaceAll( "\\", "/" );
}

/*
================
buildDungeonNavResources
================
*/
async function buildDungeonNavResources( sourceExtractedRoot, residentPaths ) {
	const bsr = [];
	const meshPaths = new Set();
	let objectId = 0x80000000;

	for ( const sourcePath of [ ...residentPaths ].sort() ) {
		const absolutePath = dataAssetPath( sourceExtractedRoot, sourcePath );
		if ( !(await exists( absolutePath )) ) {
			throw new Error( `DOF resident BSR is missing: ${sourcePath}` );
		}
		const resource = parseJmxResourceBsr( await readFile( absolutePath ), sourcePath );
		bsr.push( {
			objectId,
			sourcePath,
			sourceGamePath: toExtractedRelative( absolutePath, sourceExtractedRoot ),
			renderMeshSection: resource.renderMeshSection,
			meshPaths: resource.meshPaths
		} );
		objectId += 1;
		for ( const meshPath of resource.meshPaths ) {
			meshPaths.add( normalizeAssetPath( meshPath ) );
		}
	}

	const meshes = [];
	for ( const sourcePath of [ ...meshPaths ].sort() ) {
		const absolutePath = dataAssetPath( sourceExtractedRoot, sourcePath );
		if ( !(await exists( absolutePath )) ) {
			throw new Error( `DOF resident BSR references missing BMS: ${sourcePath}` );
		}
		const mesh = parseJmxBmsStaticMesh( await readFile( absolutePath ), sourcePath );
		if ( !mesh.nativePayloads?.length ) {
			continue;
		}
		meshes.push( {
			sourcePath,
			byteLength: mesh.byteLength,
			headerOffsets: mesh.headerOffsets,
			nativePayloads: mesh.nativePayloads
		} );
	}

	const navMeshPaths = new Set( meshes.map( mesh => mesh.sourcePath ) );
	const residentsWithoutNav = bsr.filter( resource =>
		!(resource.renderMeshSection?.paths?.length ? resource.renderMeshSection.paths : resource.meshPaths)
			.some( meshPath => navMeshPaths.has( normalizeAssetPath( meshPath ) ) )
	);
	if ( residentsWithoutNav.length > 0 ) {
		throw new Error(
			`DOF resident BSRs have no decoded object-nav payload: ` +
				residentsWithoutNav.map( resource => resource.sourcePath ).join( ", " )
		);
	}

	return {
		format: "sro-world-nav-object-resources",
		version: 1,
		bsr,
		meshes
	};
}

/*
================
dataAssetPath
================
*/
function dataAssetPath( sourceExtractedRoot, sourcePath ) {
	return path.join(
		sourceExtractedRoot,
		"Data_extracted",
		...normalizeAssetPath( sourcePath ).split( "/" )
	);
}

/*
================
readDofPresentation

JMXVDOF 0101 block projection. Navigation retains resident block BSRs;
room ambient D3DCOLOR (CRTDunBlock_ReadFromArchive 0x00AD8181 -> +0x6c),
water placement and tint belong to the block presentation, not navigation.
================
*/
export function readDofPresentation( bytes, sourcePath ) {
	const cursor = new BinaryReader( bytes, sourcePath );
	if ( cursor.text( 12, "signature" ) !== "JMXVDOF 0101" ) {
		throw new Error( `${sourcePath}: invalid JMXVDOF signature` );
	}
	const offsets = Array.from( { length: 8 }, () => cursor.u32( "header offset" ) );

	cursor.u32( "general type" );
	cursor.str( "general name" );
	cursor.u32( "general unknown 0" );
	cursor.u32( "general unknown 1" );
	cursor.u16( "general region id" );
	cursor.seek( offsets[7], "bounding box" );
	cursor.skip( 12 * 4, "bounding boxes" );
	cursor.seek( offsets[0], "block section" );

	const blockCount = cursor.count( "block count", 4096 );
	const blocks = [];
	const vector = label =>
		Array.from( { length: 3 }, ( _, i ) => {
			const value = cursor.f32( `${label} ${i}` );
			if ( !Number.isFinite( value ) ) throw Error( `${sourcePath}: non-finite ${label}` );
			return value;
		} );
	for ( let blockIndex = 0; blockIndex < blockCount; blockIndex += 1 ) {
		const blockPath = cursor.str( `block ${blockIndex} path` ),
			name = cursor.str( `block ${blockIndex} name` );
		cursor.u32( "block unknown" );
		const position = vector( "block position" ), yaw = cursor.f32( "block yaw" );
		if ( !Number.isFinite( yaw ) ) throw Error( `${sourcePath}: non-finite block yaw` );
		cursor.skip( 4 + 6 * 4, "block entrance/bounds" );
		const ambientColor = cursor.u32( "ambient color" );
		const fog = {
			color: cursor.u32( "fog color" ),
			nearPlane: cursor.f32( "fog near" ),
			farPlane: cursor.f32( "fog far" ),
			intensity: cursor.f32( "fog intensity" ),
			heightFog: /** @type {{unk3:number,unk4:number,unk5:number,unk6:number}|null} */ (null)
		};
		if ( ![ fog.nearPlane, fog.farPlane, fog.intensity ].every( Number.isFinite ) ) {
			throw Error( `${sourcePath}: non-finite fog` );
		}
		const waterObjects = [], blockObjects = [];
		if ( cursor.u8( `block ${blockIndex} height-fog flag` ) !== 0 ) {
			const values = Array.from( { length: 4 }, () => cursor.f32( "height fog" ) );
			if ( !values.every( Number.isFinite ) ) throw Error( `${sourcePath}: non-finite height fog` );
			fog.heightFog = { unk3: values[0], unk4: values[1], unk5: values[2], unk6: values[3] };
		}
		if ( cursor.u8( `block ${blockIndex} optional-vector flag` ) !== 0 ) {
			cursor.skip( 7 * 4, `block ${blockIndex} optional vectors` );
		}
		cursor.str( `block ${blockIndex} unknown string` );
		cursor.skip( 2 * 4, `block ${blockIndex} room/floor` );
		const connectedBlocks = Array.from(
			{ length: cursor.count( "connected blocks", 4096 ) },
			() => cursor.u32( "connected block" )
		);
		const visibleBlocks = Array.from(
			{ length: cursor.count( "visible blocks", 4096 ) },
			() => cursor.u32( "visible block" )
		);
		if ( [ ...connectedBlocks, ...visibleBlocks ].some( i => i >= blockCount ) ) {
			throw Error( `${sourcePath}: invalid block visibility/link` );
		}

		const objectCount = cursor.count( `block ${blockIndex} object count`, 1 << 20 );
		cursor.u32( `block ${blockIndex} collision object count` );
		for ( let objectIndex = 0; objectIndex < objectCount; objectIndex += 1 ) {
			const objectName = cursor.str( `block ${blockIndex} object ${objectIndex} name` );
			const objectPath = cursor.str( `block ${blockIndex} object ${objectIndex} path` );
			const objectPosition = vector( "object position" ),
				rotation = vector( "object rotation" ),
				scale = vector( "object scale" );
			const flags = cursor.u32( `block ${blockIndex} object ${objectIndex} flags` );
			cursor.skip( 2 * 4, `block ${blockIndex} object ${objectIndex} tail` );
			if ( (flags & 4) !== 0 ) {
				const color = cursor.u32( `block ${blockIndex} object ${objectIndex} water color` );
				waterObjects.push( {
					index: objectIndex,
					name: objectName,
					path: objectPath,
					position: objectPosition,
					rotation,
					scale,
					flags,
					color
				} );
			} else {
				// 0xAD1B70 draws every non-water object a room block places
				// (torches, cobwebs, sealed doors, cave-in boulders).
				blockObjects.push( {
					index: objectIndex,
					name: objectName,
					path: objectPath,
					position: objectPosition,
					rotation,
					scale,
					flags
				} );
			}
		}

		blocks.push( {
			index: blockIndex,
			path: blockPath,
			name,
			position,
			yaw,
			ambientColor,
			fog,
			waterObjects,
			blockObjects,
			connectedBlocks,
			visibleBlocks
		} );
		const lightCount = cursor.count( `block ${blockIndex} light count`, 1 << 20 );
		for ( let lightIndex = 0; lightIndex < lightCount; lightIndex += 1 ) {
			cursor.str( `block ${blockIndex} light ${lightIndex} name` );
			cursor.skip( 15 * 4, `block ${blockIndex} light ${lightIndex} fields` );
		}
	}
	if ( cursor.offset !== offsets[2] ) {
		throw new Error(
			`${sourcePath}: block section consumed ${cursor.offset}, expected grid offset ${offsets[2]}`
		);
	}
	return { version: 1, blocks };
}

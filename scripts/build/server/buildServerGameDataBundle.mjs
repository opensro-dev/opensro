/*
===========================================================================

buildServerGameDataBundle.mjs - the verified server game-data projection

Builds the immutable projection GameWorld reads: the retail textdata, the
character authority, authored areas and movement authority, with a manifest
that pins every file's size and digest, plus its lossless release archive.
It is written inside the Go module (paths.mjs serverGameDataRoot), where the
server's tests and Go's test cache see it.

===========================================================================
*/
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { copyFile, mkdir, readdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { withGeneratedAssetsLock } from "../../rebuildLock.mjs";
import { loadCharacterDataRows } from "../char/resolveCharRoster.mjs";
import { dataExtractedRoot, publicRoot, rebuildRoot, retailTextdataRoot, serverGameDataRoot } from "../world/paths.mjs";
import { buildStructureZoneProjection } from "./structureZones.mjs";
import { buildServerGameDataArchive } from "./serverGameDataArchive.mjs";
import { completeItemTextProjection, completeItemReferenceProjection } from "../shared/itemTextCompletions.mjs";
import { completeEnglishTextProjection, ENGLISH_COMPLETION_FILES } from "../shared/englishCompletions.mjs";
import { assertItemNameCoverage } from "../shared/itemNameCoverage.mjs";
import { readLocalizedTextDataRowsSync } from "../shared/textDataIo.mjs";

const MANIFEST_FORMAT = "sro-game-data-bundle";
const SCHEMA_VERSION = 1;
const GAME_VERSION = "1.150";
const PROJECTION = "server";
const PROTOCOL_VERSION = 2;
const WORLD_PUBLIC_ROOT = path.join( publicRoot, "assets", "world" );
// CObjectStringIfo_Load (98C7F0) reads this Data.pk2 file.
const OBJECT_STRING_FILE = path.join( dataExtractedRoot, "navmesh", "objectstring.ifo" );

export const defaultServerGameDataRoot = serverGameDataRoot;

/*
================
buildServerGameDataBundle
================
*/
export async function buildServerGameDataBundle( options ) {
	const outputRoot = path.resolve( options.output ?? defaultServerGameDataRoot );
	const textdataRoot = path.resolve( options.textdataRoot ?? retailTextdataRoot );
	const worldPublicRoot = path.resolve( options.worldPublicRoot ?? WORLD_PUBLIC_ROOT );
	const dataVersion = options.dataVersion ?? "dev";
	const sourceRevision = options.sourceRevision ?? "workspace";
	assertSafeOutputRoot( outputRoot );
	const temporaryRoot = `${outputRoot}.tmp-${process.pid}`;
	await rm( temporaryRoot, { recursive: true, force: true } );
	await mkdir( temporaryRoot, { recursive: true } );

	try {
		await copyTextdataProjection( temporaryRoot, textdataRoot );
		await buildCharacterAuthorityProjection( temporaryRoot, textdataRoot );
		await buildAreaProjection( temporaryRoot, worldPublicRoot );
		await buildMovementProjection( temporaryRoot, worldPublicRoot );
		await buildStructureZoneProjection( temporaryRoot, options.objectStringFile ?? OBJECT_STRING_FILE );

		const files = await describeProjectionFiles( temporaryRoot );
		const manifest = {
			format: MANIFEST_FORMAT,
			schemaVersion: SCHEMA_VERSION,
			gameVersion: GAME_VERSION,
			dataVersion,
			sourceRevision,
			projection: PROJECTION,
			protocolVersion: PROTOCOL_VERSION,
			contentDigest: contentDigest( files ),
			files
		};
		const manifestBytes = Buffer.from( `${JSON.stringify( manifest, null, 2 )}\n` );
		await writeFile( path.join( temporaryRoot, "manifest.json" ), manifestBytes );
		await publishDirectory( temporaryRoot, outputRoot );
		const archive = await buildServerGameDataArchive( outputRoot );

		const manifestDigest = digestBytes( manifestBytes );
		const totalBytes = files.reduce( ( sum, file ) => sum + file.size, 0 );
		console.log( `Built ${PROJECTION} game-data ${dataVersion}` );
		console.log( `  root: ${outputRoot}` );
		console.log( `  files: ${files.length}` );
		console.log( `  bytes: ${totalBytes}` );
		console.log( `  manifest: ${manifestDigest}` );
		console.log( `  archive: ${archive.bytes} bytes (${archive.fileCount} files)` );
	} catch ( error ) {
		await rm( temporaryRoot, { recursive: true, force: true } );
		throw error;
	}
}

/*
================
buildCharacterAuthorityProjection

Build the server's semantic playable-character identity catalogue directly
from the retail RefObjChar table. This is intentionally not copied from the
browser roster: GLB paths, dress meshes, and weapon meshes are client-owned
presentation, while refObjId/codename identity is shared game authority.
================
*/
export async function buildCharacterAuthorityProjection( bundleRoot, textdataRoot ) {
	const rows = loadCharacterDataRows( textdataRoot, {
		codenamePattern: /^CHAR_(?:CH|EU)_(?:MAN|WOMAN)_/
	} );
	const models = [];
	const seenRefObjIds = new Map();
	for ( const [codename, columns] of rows ) {
		if ( columns[0] !== "1" ) continue;
		const refObjId = Number( columns[1] );
		const charBit = Number( columns[8] );
		const tid1 = Number( columns[9] );
		const tid2 = Number( columns[10] );
		const tid3 = Number( columns[11] );
		const tidWord = ((charBit !== 0 ? 0x0002 : 0) |
			((tid1 & 7) << 2) |
			((tid2 & 3) << 5) |
			((tid3 & 15) << 7)) >>> 0;
		if ( !Number.isSafeInteger( refObjId ) || refObjId <= 0 || tidWord !== 0x0026 ) continue;
		const previous = seenRefObjIds.get( refObjId );
		if ( previous ) {
			throw new Error( `Duplicate playable-character refObjId ${refObjId}: ${previous} and ${codename}` );
		}
		const bodyRadius = Number( columns[50] );
		if ( !Number.isFinite( bodyRadius ) || bodyRadius <= 0 ) {
			throw new Error( `Playable-character ${codename} has invalid RefObjChar BCRadius ${columns[50]}` );
		}
		seenRefObjIds.set( refObjId, codename );
		models.push( { codename, refObjId, bodyRadius } );
	}
	models.sort( ( left, right ) => left.refObjId - right.refObjId || left.codename.localeCompare( right.codename ) );
	if ( models.length < 4 ) {
		throw new Error( `Playable-character authority catalogue is incomplete (${models.length} rows)` );
	}
	await writeJson(
		path.join( bundleRoot, "character-authority", "catalog.json" ),
		{ format: "sro-server-character-authority", version: 2, models }
	);
}

/*
================
copyTextdataProjection
================
*/
async function copyTextdataProjection( bundleRoot, sourceRoot ) {
	const entries = await readdir( sourceRoot, { withFileTypes: true } );
	const sourceFiles = entries
		.filter( ( entry ) => entry.isFile() && entry.name.toLowerCase().endsWith( ".txt" ) )
		.sort( ( left, right ) => left.name.localeCompare( right.name ) );
	if ( sourceFiles.length === 0 ) {
		throw new Error( `No textdata files found under ${sourceRoot}` );
	}
	const targetRoot = path.join( bundleRoot, "textdata" );
	await mkdir( targetRoot, { recursive: true } );
	for ( const entry of sourceFiles ) {
		const source = path.join( sourceRoot, entry.name ), target = path.join( targetRoot, entry.name.toLowerCase() );
		const name = entry.name.toLowerCase();
		if ( ENGLISH_COMPLETION_FILES.includes( name ) ) {
			// Same English policy as the client catalogs: item names first, then the
			// authored completions for every other untranslated row.
			const bytes = await readFile( source );
			await writeFile(
				target,
				completeEnglishTextProjection(
					name,
					name === "textdataname.txt" ? completeItemTextProjection( bytes ) : bytes
				)
			);
		} else if ( /^itemdata.*\.txt$/i.test( entry.name ) ) {
			await writeFile( target, completeItemReferenceProjection( await readFile( source ) ) );
		} else {
			await copyFile( source, target );
		}
	}
	assertItemNameCoverage(
		targetRoot,
		Object.fromEntries(
			readLocalizedTextDataRowsSync( path.join( targetRoot, "textdataname.txt" ) ).map(
				row => [ row[1], row[8] ]
			)
		)
	);
}

/*
================
buildAreaProjection
================
*/
async function buildAreaProjection( bundleRoot, worldRoot ) {
	const source = await readJson( path.join( worldRoot, "authored-areas.json" ) );
	if ( source.format !== "sro-authored-world-area-catalog" || source.version !== 1 ) {
		throw new Error( `Unsupported authored-area source ${source.format} v${source.version}` );
	}
	const areas = [ ...(source.areas ?? []) ]
		.map( ( { slug, regionId, access, entry, population } ) => ({
			slug,
			regionId,
			access,
			entry,
			population: population ?? []
		}) )
		.sort( ( left, right ) => left.slug.localeCompare( right.slug ) );
	await writeJson(
		path.join( bundleRoot, "world-authority", "areas", "catalog.json" ),
		{ format: "sro-server-world-area-catalog", version: 1, areas }
	);
}

/*
================
buildMovementProjection
================
*/
async function buildMovementProjection( bundleRoot, worldRoot ) {
	const sourceCatalog = await readJson( path.join( worldRoot, "world-region-catalog.json" ) );
	if ( sourceCatalog.format !== "sro-world-region-catalog" || sourceCatalog.version !== 1 ) {
		throw new Error( `Unsupported world-region source ${sourceCatalog.format} v${sourceCatalog.version}` );
	}

	const authorityRoot = path.join( bundleRoot, "world-authority", "movement" );
	const indexPaths = new Set();
	const bundlePaths = new Set();
	const referencedObjectNav = new Map();
	const regionsById = {};
	for ( const regionId of Object.keys( sourceCatalog.regionsById ?? {} ).sort() ) {
		regionsById[regionId] = [ ...sourceCatalog.regionsById[regionId] ]
			.map( ( entry ) => {
				const worldRegionsPath = worldAuthorityPath( entry.worldRegionsPublicPath );
				const bundlePath = worldAuthorityPath( entry.bundlePublicPath );
				indexPaths.add( worldRegionsPath );
				bundlePaths.add( bundlePath );
				return {
					id: entry.id,
					area: entry.area,
					seedRegionId: entry.seedRegionId,
					seedSector: entry.seedSector,
					sectorX: entry.sectorX,
					sectorY: entry.sectorY,
					worldRegionsPath,
					bundlePath,
					source: entry.source
				};
			} )
			.sort( compareCatalogEntries );
	}

	for ( const relativePath of [ ...indexPaths ].sort() ) {
		const source = await readJson( path.join( worldRoot, relativePath ) );
		const projected = {
			format: "sro-server-world-region-index",
			version: 1,
			regionSize: source.regionSize,
			seedRegionId: source.seedRegionId,
			regions: [ ...(source.regions ?? []) ]
				.map( ( entry ) => ({
					id: entry.id,
					bundlePath: worldAuthorityPath( entry.bundlePublicPath )
				}) )
				.sort( ( left, right ) => left.id.localeCompare( right.id ) )
		};
		for ( const entry of projected.regions ) {
			bundlePaths.add( entry.bundlePath );
		}
		await writeJson( path.join( authorityRoot, relativePath ), projected );
	}

	for ( const relativePath of [ ...bundlePaths ].sort() ) {
		const source = await readJson( path.join( worldRoot, relativePath ) );
		const objectProjection = await buildObjectNavProjection(
			authorityRoot,
			relativePath,
			source,
			worldRoot,
			referencedObjectNav
		);
		await writeJson( path.join( authorityRoot, relativePath ), projectRegionBundle( source, objectProjection ) );
	}

	const dungeonSource = path.join( worldRoot, "dungeon", "dungeon-resources.json" );
	if ( await isRegularFile( dungeonSource ) ) {
		await writeJson(
			path.join( authorityRoot, "dungeon", "dungeon-resources.json" ),
			projectDungeonResources( await readJson( dungeonSource ) )
		);
	}

	await writeJson( path.join( authorityRoot, "catalog.json" ), {
		format: "sro-server-movement-catalog",
		version: 1,
		regionsById
	} );
}

/*
================
projectRegionBundle
================
*/
function projectRegionBundle( source, resourceIndexPath ) {
	const projectBlock = ( block ) => ({
		blockX: block.blockX,
		blockZ: block.blockZ,
		...(block.water ? { water: block.water } : {})
	});
	const terrain = {
		sectors: (source.terrain?.sectors ?? []).map( ( sector ) => ({
			sectorX: sector.sectorX,
			sectorY: sector.sectorY,
			blocks: (sector.blocks ?? []).map( projectBlock )
		}) ),
		blocks: (source.terrain?.blocks ?? []).map( projectBlock )
	};
	const navmesh = {
		regionSize: source.navmesh?.regionSize,
		tileSize: source.navmesh?.tileSize,
		tilesPerAxis: source.navmesh?.tilesPerAxis,
		heightMapAxisVertices: source.navmesh?.heightMapAxisVertices,
		regions: (source.navmesh?.regions ?? []).map( ( region ) => ({
			dx: region.dx,
			dz: region.dz,
			heightMap: region.heightMap,
			planeType: region.planeType,
			planeHeight: region.planeHeight,
			blockedTiles: region.blockedTiles,
			tileCellIds: region.tileCellIds,
			cells: region.cells ?? { count: 0 },
			objects: region.objects ?? []
		}) )
	};
	return {
		format: "sro-server-movement-region",
		version: 1,
		source: {
			sectorId: source.source?.sectorId,
			sectorX: source.source?.sectorX,
			sectorY: source.source?.sectorY
		},
		terrain,
		navmesh,
		objects: resourceIndexPath ? { resourceIndexPath } : {}
	};
}

/*
================
buildObjectNavProjection
================
*/
async function buildObjectNavProjection(
	authorityRoot,
	bundlePath,
	sourceBundle,
	worldRoot,
	referencedObjectNav
) {
	const resources = sourceBundle.objects?.resources;
	if ( !resources || !Array.isArray( resources.bsr ) || !Array.isArray( resources.meshes ) ) {
		const referencedIndex = sourceBundle.objects?.resourceIndexPublicPath;
		if ( !referencedIndex ) {
			return "";
		}
		return buildReferencedObjectNavProjection(
			authorityRoot,
			worldRoot,
			referencedIndex,
			referencedObjectNav
		);
	}
	const usedObjectIds = new Set();
	for ( const region of sourceBundle.navmesh?.regions ?? [] ) {
		for ( const object of region.objects ?? [] ) {
			if ( Number.isInteger( object.assetId ) && object.assetId > 0 ) {
				usedObjectIds.add( object.assetId );
			}
		}
	}
	const meshBySource = new Map(
		resources.meshes
			.filter( hasObjectNavPayload )
			.map( ( mesh ) => [ normalizeGamePath( mesh.sourcePath ), mesh ] )
	);
	const areaRoot = path.posix.dirname( bundlePath );
	const bundleStem = path.posix.basename( bundlePath, ".json" );
	const meshFiles = [];
	const emittedMeshes = new Map();
	const bsr = [];

	for ( const row of [ ...resources.bsr ].sort( ( left, right ) => left.objectId - right.objectId ) ) {
		if ( !usedObjectIds.has( row.objectId ) ) {
			continue;
		}
		const sourcePaths = row.renderMeshSection?.paths?.length ?
			row.renderMeshSection.paths :
			(row.meshPaths ?? []);
		const resolved = sourcePaths
			.map( normalizeGamePath )
			.filter( ( sourcePath ) => meshBySource.has( sourcePath ) );
		if ( resolved.length === 0 ) {
			continue;
		}
		bsr.push( {
			objectId: row.objectId,
			renderMeshSection: { paths: resolved },
			meshPaths: resolved
		} );
		for ( const sourcePath of resolved ) {
			if ( emittedMeshes.has( sourcePath ) ) {
				continue;
			}
			const mesh = meshBySource.get( sourcePath );
			const meshPath = path.posix.join(
				areaRoot,
				"object-nav",
				`${bundleStem}-${digestText( sourcePath ).slice( 0, 16 )}.json`
			);
			emittedMeshes.set( sourcePath, meshPath );
			meshFiles.push( { sourcePath, path: meshPath } );
			await writeJson( path.join( authorityRoot, meshPath ), {
				format: "sro-server-object-nav-mesh",
				version: 1,
				mesh: {
					byteLength: mesh.byteLength,
					headerOffsets: mesh.headerOffsets,
					nativePayloads: mesh.nativePayloads
				}
			} );
		}
	}
	if ( bsr.length === 0 ) {
		return "";
	}
	meshFiles.sort( ( left, right ) => left.sourcePath.localeCompare( right.sourcePath ) );
	const indexPath = path.posix.join( areaRoot, `${bundleStem}-object-nav.json` );
	await writeJson( path.join( authorityRoot, indexPath ), {
		format: "sro-server-object-nav-index",
		version: 1,
		bsr,
		meshFiles
	} );
	return indexPath;
}

/*
================
buildReferencedObjectNavProjection
================
*/
async function buildReferencedObjectNavProjection(
	authorityRoot,
	worldRoot,
	publicIndexPath,
	cache
) {
	const relativeIndexPath = worldAuthorityPath( publicIndexPath );
	if ( cache.has( relativeIndexPath ) ) {
		return cache.get( relativeIndexPath );
	}

	const sourceIndex = await readJson( path.join( worldRoot, relativeIndexPath ) );
	const referencedMeshSources = new Set();
	for ( const row of sourceIndex.bsr ?? [] ) {
		const paths = row.renderMeshSection?.paths?.length ?
			row.renderMeshSection.paths :
			(row.meshPaths ?? []);
		for ( const sourcePath of paths ) {
			referencedMeshSources.add( normalizeGamePath( sourcePath ) );
		}
	}

	const meshPathBySource = new Map();
	const meshFiles = [];
	for ( const descriptor of sourceIndex.meshFiles ?? [] ) {
		const sourcePath = normalizeGamePath( descriptor.sourcePath );
		if ( !referencedMeshSources.has( sourcePath ) ) {
			continue;
		}
		const relativeMeshPath = worldAuthorityPath( descriptor.publicPath );
		const document = await readJson( path.join( worldRoot, relativeMeshPath ) );
		const mesh = document.mesh ?? document;
		if ( !hasObjectNavPayload( mesh ) ) {
			continue;
		}
		const projectedPath = path.posix.join(
			path.posix.dirname( relativeIndexPath ),
			"object-nav",
			`${digestText( sourcePath ).slice( 0, 16 )}.json`
		);
		await writeJson( path.join( authorityRoot, projectedPath ), {
			format: "sro-server-object-nav-mesh",
			version: 1,
			mesh: {
				byteLength: mesh.byteLength,
				headerOffsets: mesh.headerOffsets,
				nativePayloads: mesh.nativePayloads
			}
		} );
		meshPathBySource.set( sourcePath, projectedPath );
		meshFiles.push( { sourcePath, path: projectedPath } );
	}

	const bsr = [];
	for ( const row of sourceIndex.bsr ?? [] ) {
		const sourcePaths = (row.renderMeshSection?.paths?.length ?
			row.renderMeshSection.paths :
			(row.meshPaths ?? [])).map( normalizeGamePath );
		const resolved = sourcePaths.filter( ( sourcePath ) => meshPathBySource.has( sourcePath ) );
		if ( resolved.length === 0 ) {
			continue;
		}
		bsr.push( {
			objectId: row.objectId,
			renderMeshSection: { paths: resolved },
			meshPaths: resolved
		} );
	}
	meshFiles.sort( ( left, right ) => left.sourcePath.localeCompare( right.sourcePath ) );
	bsr.sort( ( left, right ) => left.objectId - right.objectId );

	const projectedIndexPath = path.posix.join(
		path.posix.dirname( relativeIndexPath ),
		`${path.posix.basename( relativeIndexPath, ".json" )}-object-nav.json`
	);
	await writeJson( path.join( authorityRoot, projectedIndexPath ), {
		format: "sro-server-object-nav-index",
		version: 1,
		bsr,
		meshFiles
	} );
	cache.set( relativeIndexPath, projectedIndexPath );
	return projectedIndexPath;
}

/*
================
hasObjectNavPayload
================
*/
function hasObjectNavPayload( mesh ) {
	return Array.isArray( mesh?.nativePayloads ) &&
		mesh.nativePayloads.some( ( payload ) => payload.kind === "bms-offset7-post-payload-tail" );
}

/*
================
projectDungeonResources
================
*/
function projectDungeonResources( source ) {
	return {
		format: "sro-server-dungeon-authority",
		version: 1,
		entries: (source.entries ?? []).map( ( { sectorId, normalizedName } ) => ({ sectorId, normalizedName }) ),
		resources: (source.resources ?? []).map( ( { normalizedName, byteLength, rawBase64 } ) => ({
			normalizedName,
			byteLength,
			rawBase64
		}) ),
		navResources: {
			bsr: (source.navResources?.bsr ?? []).map( ( row ) => ({
				sourcePath: row.sourcePath,
				renderMeshSection: { paths: row.renderMeshSection?.paths ?? [] },
				meshPaths: row.meshPaths ?? []
			}) ),
			meshes: (source.navResources?.meshes ?? [])
				.filter( hasObjectNavPayload )
				.map( ( mesh ) => ({
					sourcePath: mesh.sourcePath,
					byteLength: mesh.byteLength,
					headerOffsets: mesh.headerOffsets,
					nativePayloads: mesh.nativePayloads
				}) )
		}
	};
}

/*
================
describeProjectionFiles
================
*/
async function describeProjectionFiles( root ) {
	const files = [];
	await walkFiles( root, async ( absolutePath ) => {
		const relativePath = path.relative( root, absolutePath ).replaceAll( "\\", "/" );
		const bytes = await readFile( absolutePath );
		files.push( {
			path: relativePath,
			mediaType: relativePath.endsWith( ".json" ) ? "application/json" : "text/plain; charset=utf-8",
			size: bytes.length,
			digest: digestBytes( bytes )
		} );
	} );
	files.sort( ( left, right ) => compareUtf8Bytes( left.path, right.path ) );
	return files;
}

/*
================
compareUtf8Bytes
================
*/
function compareUtf8Bytes( left, right ) {
	return Buffer.compare( Buffer.from( left, "utf8" ), Buffer.from( right, "utf8" ) );
}

/*
================
walkFiles
================
*/
async function walkFiles( root, visit ) {
	const entries = await readdir( root, { withFileTypes: true } );
	entries.sort( ( left, right ) => left.name.localeCompare( right.name ) );
	for ( const entry of entries ) {
		const absolutePath = path.join( root, entry.name );
		if ( entry.isSymbolicLink() ) {
			throw new Error( `Projection contains a symlink: ${absolutePath}` );
		}
		if ( entry.isDirectory() ) {
			await walkFiles( absolutePath, visit );
		} else if ( entry.isFile() ) {
			await visit( absolutePath );
		} else {
			throw new Error( `Projection contains a non-regular entry: ${absolutePath}` );
		}
	}
}

/*
================
contentDigest
================
*/
function contentDigest( files ) {
	const digest = createHash( "sha256" );
	for ( const file of files ) {
		writeFramedString( digest, file.path );
		writeFramedString( digest, file.mediaType );
		const size = Buffer.alloc( 8 );
		size.writeBigUInt64BE( BigInt( file.size ) );
		digest.update( size );
		writeFramedString( digest, file.digest );
	}
	return `sha256:${digest.digest( "hex" )}`;
}

/*
================
writeFramedString
================
*/
function writeFramedString( digest, value ) {
	const bytes = Buffer.from( value );
	const length = Buffer.alloc( 8 );
	length.writeBigUInt64BE( BigInt( bytes.length ) );
	digest.update( length );
	digest.update( bytes );
}

/*
================
digestBytes
================
*/
function digestBytes( bytes ) {
	return `sha256:${createHash( "sha256" ).update( bytes ).digest( "hex" )}`;
}

/*
================
digestText
================
*/
function digestText( value ) {
	return createHash( "sha256" ).update( value ).digest( "hex" );
}

/*
================
writeJson
================
*/
async function writeJson( filename, value ) {
	await mkdir( path.dirname( filename ), { recursive: true } );
	await writeFile( filename, `${JSON.stringify( value, null, 2 )}\n` );
}

/*
================
readJson
================
*/
async function readJson( filename ) {
	return JSON.parse( await readFile( filename, "utf8" ) );
}

/*
================
isRegularFile
================
*/
async function isRegularFile( filename ) {
	try {
		return (await stat( filename )).isFile();
	} catch ( error ) {
		if ( error.code === "ENOENT" ) {
			return false;
		}
		throw error;
	}
}

/*
================
worldAuthorityPath
================
*/
function worldAuthorityPath( publicPath ) {
	const prefix = "/assets/world/";
	if ( typeof publicPath !== "string" || !publicPath.startsWith( prefix ) ) {
		throw new Error( `World asset is outside ${prefix}: ${JSON.stringify( publicPath )}` );
	}
	const relativePath = publicPath.slice( prefix.length );
	if ( !relativePath || path.posix.normalize( relativePath ) !== relativePath || relativePath.startsWith( "../" ) ) {
		throw new Error( `Unsafe world asset path: ${JSON.stringify( publicPath )}` );
	}
	return relativePath;
}

/*
================
normalizeGamePath
================
*/
function normalizeGamePath( value ) {
	return String( value ?? "" ).trim().replaceAll( "\\", "/" ).replace( /^\/+/, "" ).toLowerCase();
}

/*
================
compareCatalogEntries
================
*/
function compareCatalogEntries( left, right ) {
	return left.bundlePath.localeCompare( right.bundlePath ) || left.id.localeCompare( right.id );
}

/*
================
publishDirectory
================
*/
async function publishDirectory( temporaryRoot, outputRoot ) {
	const backupRoot = `${outputRoot}.old-${process.pid}`;
	await rm( backupRoot, { recursive: true, force: true } );
	let movedOld = false;
	try {
		if ( await directoryExists( outputRoot ) ) {
			await rename( outputRoot, backupRoot );
			movedOld = true;
		}
		await mkdir( path.dirname( outputRoot ), { recursive: true } );
		await rename( temporaryRoot, outputRoot );
		await rm( backupRoot, { recursive: true, force: true } );
	} catch ( error ) {
		if ( movedOld && !(await directoryExists( outputRoot )) ) {
			await rename( backupRoot, outputRoot );
		}
		throw error;
	}
}

/*
================
directoryExists
================
*/
async function directoryExists( filename ) {
	try {
		return (await stat( filename )).isDirectory();
	} catch ( error ) {
		if ( error.code === "ENOENT" ) {
			return false;
		}
		throw error;
	}
}

/*
================
assertSafeOutputRoot
================
*/
function assertSafeOutputRoot( outputRoot ) {
	const forbidden = [
		path.parse( outputRoot ).root,
		rebuildRoot,
		publicRoot,
		retailTextdataRoot,
		WORLD_PUBLIC_ROOT
	].map( ( value ) => path.resolve( value ).toLowerCase() );
	if ( forbidden.includes( outputRoot.toLowerCase() ) ) {
		throw new Error( `Refusing unsafe game-data output root ${outputRoot}` );
	}
}

/*
================
parseArgs
================
*/
function parseArgs( argv ) {
	argv = argv.filter( ( value ) => value !== "--" );
	const values = new Map();
	for ( let index = 0; index < argv.length; index += 2 ) {
		const name = argv[index];
		const value = argv[index + 1];
		if ( !name?.startsWith( "--" ) || value === undefined ) {
			throw usage();
		}
		values.set( name.slice( 2 ), value );
	}
	const output = values.get( "output" ) ?? defaultServerGameDataRoot;
	const dataVersion = values.get( "data-version" ) ?? process.env.SRO_GAME_DATA_VERSION ?? "dev";
	const sourceRevision = values.get( "source-revision" ) ?? gitRevision();
	if ( !output || !dataVersion || !validLabel( dataVersion ) || !validLabel( sourceRevision ) ) {
		throw usage();
	}
	return {
		output,
		dataVersion,
		sourceRevision,
		textdataRoot: path.resolve( values.get( "textdata-root" ) ?? retailTextdataRoot ),
		worldPublicRoot: path.resolve( values.get( "world-public-root" ) ?? WORLD_PUBLIC_ROOT )
	};
}

/*
================
validLabel
================
*/
function validLabel( value ) {
	return typeof value === "string" && value.length > 0 && value.length <= 128 && /^[!-~]+$/.test( value );
}

/*
================
gitRevision
================
*/
function gitRevision() {
	try {
		const revision = execFileSync( "git", [ "rev-parse", "HEAD" ], {
			cwd: rebuildRoot,
			encoding: "utf8",
			stdio: [ "ignore", "pipe", "ignore" ]
		} ).trim();
		return revision || "unknown";
	} catch {
		return "unknown";
	}
}

/*
================
usage
================
*/
function usage() {
	return new Error(
		"Usage: node scripts/build/server/buildServerGameDataBundle.mjs " +
			"--output <directory> --data-version <version> [--source-revision <revision>] " +
			"[--textdata-root <directory>] " +
			"[--world-public-root <directory>]"
	);
}

if ( process.argv[1] && import.meta.url === pathToFileURL( path.resolve( process.argv[1] ) ).href ) {
	const args = parseArgs( process.argv.slice( 2 ) );
	await withGeneratedAssetsLock( "server game-data projection", async () => {
		await buildServerGameDataBundle( args );
	} );
}

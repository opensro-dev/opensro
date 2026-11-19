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
import { VARIANT_MANIFEST } from "../../lib/expandedBuild.mjs";
import {
	dataExtractedRoot,
	extractedRoot,
	gameRoot,
	publicRoot,
	rebuildRoot,
	retailTextdataRoot,
	serverGameDataRoot
} from "../world/paths.mjs";
import { buildStructureZoneProjection, portOnlyObjectStringFiles } from "./structureZones.mjs";
import { buildAINavProjection } from "./aiNavProjection.mjs";
import { buildServerGameDataArchive } from "./serverGameDataArchive.mjs";
import { completeItemTextProjection, completeItemReferenceProjection } from "../shared/itemTextCompletions.mjs";
import { completeEnglishTextProjection, ENGLISH_COMPLETION_FILES } from "../shared/englishCompletions.mjs";
import { assertItemNameCoverage } from "../shared/itemNameCoverage.mjs";
import { readLocalizedTextDataRowsSync } from "../shared/textDataIo.mjs";
import { compareText } from "../shared/textOrder.mjs";
import { buildObjectNavProjection, hasObjectNavPayload } from "./objectNavProjection.mjs";
import { readJson, worldAuthorityPath, writeJson } from "./projectionFiles.mjs";
import { sha256Hex } from "../shared/hash.mjs";

const MANIFEST_FORMAT = "sro-game-data-bundle";
const SCHEMA_VERSION = 1;
const GAME_VERSION = "1.150";
const PROJECTION = "server";
const PROTOCOL_VERSION = 2;
const WORLD_PUBLIC_ROOT = path.join( publicRoot, "assets", "world" );
// CObjectStringIfo_Load (98C7F0) reads this Data.pk2 file.
const OBJECT_STRING_FILE = path.join( dataExtractedRoot, "navmesh", "objectstring.ifo" );
// Port-only, not native: the later fortresses' placements
// (structureZones.portOnlyObjectStringFiles), merged when they exist.
const OBJECT_STRING_ROOT = path.join( dataExtractedRoot, "navmesh" );
// The dungeon route tables (aiNavProjection.mjs) sit beside objectstring.ifo.
const AINAV_SOURCE_ROOT = path.join( dataExtractedRoot, "navmesh" );
// Read by apps/server/internal/game/world/alexandria (OverlayFile).
const ALEXANDRIA_OVERLAY_FILE = path.join( "textdata", "alexandria-overlay.json" );
// Written by scripts/import_vsro_optional_teleport.py; read by
// apps/server/internal/game/world/optionalteleport (File).
const OPTIONAL_TELEPORT_NAME = "vsro-optional-teleport.json";
const OPTIONAL_TELEPORT_SOURCE = path.join( extractedRoot, "VsroOptionalTeleport_extracted", OPTIONAL_TELEPORT_NAME );
const OPTIONAL_TELEPORT_FORMAT = "opensro-vsro-optional-teleport-v1";

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
		await buildAlexandriaOverlayProjection(
			temporaryRoot,
			options.variantManifest ?? path.join( gameRoot, VARIANT_MANIFEST )
		);
		await buildOptionalTeleportProjection(
			temporaryRoot,
			options.optionalTeleportSource ?? OPTIONAL_TELEPORT_SOURCE
		);
		await buildCharacterAuthorityProjection( temporaryRoot, textdataRoot );
		await buildAreaProjection( temporaryRoot, worldPublicRoot );
		await buildMovementProjection( temporaryRoot, worldPublicRoot );
		await buildAINavProjection( temporaryRoot, options.aiNavSourceRoot ?? AINAV_SOURCE_ROOT );
		await buildStructureZoneProjection(
			temporaryRoot,
			options.objectStringFile ?? OBJECT_STRING_FILE,
			options.objectStringPortOnlyFiles ?? await portOnlyObjectStringFiles( OBJECT_STRING_ROOT )
		);

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
		// RefObjChar columns 87/88: the displacement flags SkillCombat_AllowsDisplacementOutcome
		// (58E520, bit 0 knockdown) and 58FF7A (bit 1 knockback) read, and the knockdown recovery.
		const knockdown = Number( columns[87] ), koRecoverMs = Number( columns[88] );
		if ( !Number.isInteger( knockdown ) || knockdown < 0 || knockdown > 0xffffffff ) {
			throw new Error( `Playable-character ${codename} has invalid RefObjChar knockdown ${columns[87]}` );
		}
		if ( !Number.isInteger( koRecoverMs ) || koRecoverMs < 0 || koRecoverMs > 0xffffffff ) {
			throw new Error( `Playable-character ${codename} has invalid RefObjChar KORecover ${columns[88]}` );
		}
		seenRefObjIds.set( refObjId, codename );
		models.push( { codename, refObjId, bodyRadius, knockdown, koRecoverMs } );
	}
	models.sort( ( left, right ) => left.refObjId - right.refObjId || compareText( left.codename, right.codename ) );
	if ( models.length < 4 ) {
		throw new Error( `Playable-character authority catalogue is incomplete (${models.length} rows)` );
	}
	await writeJson(
		path.join( bundleRoot, "character-authority", "catalog.json" ),
		{ format: "sro-server-character-authority", version: 3, models }
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
		.sort( ( left, right ) => compareText( left.name, right.name ) );
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
buildAlexandriaOverlayProjection

Port-only, not native. A game root made by build_alexandria_variant.py adds
Alexandria's regions, gates and NPC placements to the textdata; the server
shows them only while SRO_ALEXANDRIA is on. This projects what it must hide
otherwise: the added regions, teleport codes and gate building codenames. A
native game root has no variant manifest and the projection gains no file,
so its manifest stays byte-identical.
================
*/
export async function buildAlexandriaOverlayProjection( bundleRoot, variantManifestPath ) {
	if ( !(await isRegularFile( variantManifestPath )) ) {
		return;
	}
	const variant = await readJson( variantManifestPath );
	if ( variant.format !== "opensro-alexandria-variant-v1" ) {
		throw new Error( `Unsupported Alexandria variant manifest ${variant.format}` );
	}
	const regions = (variant.regions ?? []).map( ( { sectorX, sectorY } ) => (sectorY << 8) | sectorX );
	const teleports = variant.gates?.gates ?? [];
	const buildings = variant.gates?.buildingCodes;
	if ( regions.length === 0 || teleports.length === 0 || !Array.isArray( buildings ) ) {
		throw new Error( `Alexandria variant manifest ${variantManifestPath} lacks regions, gates or buildingCodes` );
	}
	await writeJson( path.join( bundleRoot, ALEXANDRIA_OVERLAY_FILE ), {
		format: "sro-server-alexandria-overlay",
		version: 1,
		regions: [ ...new Set( regions ) ].sort( ( left, right ) => left - right ),
		teleports: [ ...teleports ].sort(),
		buildings: [ ...buildings ].sort()
	} );
}

/*
================
buildOptionalTeleportProjection

Port-only, not native. The later client's reverse return map table, when
the operator imported it (import_vsro_optional_teleport.py), joins the
textdata unchanged; the server serves it only while SRO_REVERSE_RETURN_MAP
is on. A game root without the import gains no file, so its manifest stays
byte-identical.
================
*/
export async function buildOptionalTeleportProjection( bundleRoot, sourcePath ) {
	if ( !(await isRegularFile( sourcePath )) ) {
		return;
	}
	const bytes = await readFile( sourcePath );
	const document = JSON.parse( bytes.toString( "utf8" ) );
	if (
		document.format !== OPTIONAL_TELEPORT_FORMAT || !Array.isArray( document.rows ) || document.rows.length === 0
	) {
		throw new Error( `Unsupported optional-teleport import ${sourcePath} (${document.format})` );
	}
	const targetRoot = path.join( bundleRoot, "textdata" );
	await mkdir( targetRoot, { recursive: true } );
	await writeFile( path.join( targetRoot, OPTIONAL_TELEPORT_NAME ), bytes );
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
		.sort( ( left, right ) => compareText( left.slug, right.slug ) );
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
				.sort( ( left, right ) => compareText( left.id, right.id ) )
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
	entries.sort( ( left, right ) => compareText( left.name, right.name ) );
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
	return `sha256:${sha256Hex( bytes )}`;
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
compareCatalogEntries
================
*/
function compareCatalogEntries( left, right ) {
	return compareText( left.bundlePath, right.bundlePath ) || compareText( left.id, right.id );
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

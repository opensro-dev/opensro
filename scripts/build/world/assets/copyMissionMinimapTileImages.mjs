import { copyFile, mkdir, readFile, stat, writeFile } from "node:fs/promises";
import { copyIntoPublicTree } from "../../shared/publicWrite.mjs";
import { claimPublicFile } from "../../shared/publicationLedger.mjs";
import path from "node:path";
import { extractedRoot, imagePublicRoot, imageSourceRoot, publicRoot, toHex16 } from "../paths.mjs";
import { uniqueStrings } from "../../shared/collections.mjs";
import { mapWithConcurrency } from "../../shared/asyncUtils.mjs";
import { listFiles } from "../../shared/fsUtils.mjs";
import { decodeJmxText } from "../../shared/jmxBinaryReader.mjs";
import { writeJsonIfChanged } from "../../shared/jsonOut.mjs";
import { readDungeonInfoRows } from "./dungeonInfo.mjs";

const MISSION_MINIMAP_SOURCE_DIRECTORIES = [ "minimap", "minimap_d" ];
const MISSION_DUNGEON_MINIMAP_MANIFEST_FORMAT = "sro-mission-dungeon-minimap-manifest";
const MISSION_DUNGEON_MINIMAP_MANIFEST_VERSION = 1;
const MISSION_DUNGEON_MINIMAP_MANIFEST_PUBLIC_PATH = "/assets/data/mission-dungeon-minimap.json";

export async function copyMissionMinimapTileImages( options = {} ) {
	const sourceDirectories = options.sourceDirectories ?? MISSION_MINIMAP_SOURCE_DIRECTORIES;
	const tiles = (
		await Promise.all(
			sourceDirectories.map( ( directoryName ) => listConvertedMinimapImages( directoryName ) )
		)
	)
		.flat()
		.sort( ( left, right ) => left.publicPath.localeCompare( right.publicPath ) );

	// Copy only tiles whose target is missing or stale: unconditionally re-copying all
	// ~3.3k tiles refreshed their mtimes every run, defeating downstream hash caches.
	await mapWithConcurrency( tiles, 16, async ( tile ) => {
		if ( await copyTargetIsFresh( tile.sourcePath, tile.targetPath ) ) {
			// A fresh tile is kept, and still this build's output.
			claimPublicFile( tile.targetPath );
			return;
		}
		await copyIntoPublicTree( tile.sourcePath, tile.targetPath );
	} );

	await buildMissionDungeonMinimapManifest( tiles, options );

	return tiles;
}

// The retail archive, not successful downloads or converted output, defines
// intentional absence. Refuse incomplete conversion before publishing coverage.
export async function retailMinimapArt( tiles, sourceRoot = path.join( extractedRoot, "Media_extracted" ) ) {
	const paths = [];
	for ( const directory of MISSION_MINIMAP_SOURCE_DIRECTORIES ) {
		const root = path.join( sourceRoot, directory );
		const files = await listFiles( root, { extensions: [ ".ddj" ], missing: "throw", sort: true } );
		for ( const file of files ) {
			paths.push(
				`/assets/images/Media_extracted/${directory}/${
					path.relative( root, file ).replaceAll( "\\", "/" ).replace( /\.ddj$/i, ".png" )
				}`
			);
		}
	}
	const converted = new Set( tiles.map( tile => tile.publicPath.toLowerCase() ) ),
		native = new Set( paths.map( p => p.toLowerCase() ) );
	if ( !native.size || native.size !== paths.length ) throw Error( "Invalid retail minimap artwork inventory" );
	for ( const file of paths ) {
		if ( !converted.has( file.toLowerCase() ) ) throw Error( "Retail minimap conversion missing: " + file );
	}
	for ( const file of converted ) {
		if ( !native.has( file ) ) throw Error( "Minimap conversion has no retail source: " + file );
	}
	return paths.sort();
}

async function copyTargetIsFresh( sourcePath, targetPath ) {
	try {
		const [sourceStat, targetStat] = await Promise.all( [ stat( sourcePath ), stat( targetPath ) ] );
		return targetStat.size === sourceStat.size && targetStat.mtimeMs >= sourceStat.mtimeMs;
	} catch {
		return false;
	}
}

export function missionMinimapTilePublicPath( sectorX, sectorY ) {
	return `/assets/images/Media_extracted/minimap/${missionMinimapTileFileName( sectorX, sectorY )}`;
}

export function missionDungeonMinimapTilePublicPath( directory, prefix, sectorX, sectorY ) {
	return `/assets/images/Media_extracted/minimap_d/${directory}/${prefix}_${sectorX}x${sectorY}.png`;
}

export function buildMissionMinimapTileGrid( regionId, radius = 1 ) {
	const centerX = regionId & 0xff;
	const centerY = (regionId >> 8) & 0xff;
	const tiles = [];

	for ( let offsetY = -radius; offsetY <= radius; offsetY += 1 ) {
		for ( let offsetX = -radius; offsetX <= radius; offsetX += 1 ) {
			const sectorX = centerX + offsetX;
			const sectorY = centerY + offsetY;
			tiles.push( {
				sectorX,
				sectorY,
				offsetX,
				offsetY,
				fileName: missionMinimapTileFileName( sectorX, sectorY ),
				publicPath: missionMinimapTilePublicPath( sectorX, sectorY )
			} );
		}
	}

	return tiles;
}

export function buildMissionDungeonMinimapTileGrid( selection, position, radius = 1 ) {
	const gridX = Math.floor( position.x / 1920 );
	const gridY = Math.floor( position.z / 1920 );
	const centerX = gridX + 0x80;
	const centerY = gridY + 0x80;
	const availableTiles = selection?.tiles ? new Set( selection.tiles ) : null;
	const tiles = [];

	for ( let offsetY = -radius; offsetY <= radius; offsetY += 1 ) {
		for ( let offsetX = -radius; offsetX <= radius; offsetX += 1 ) {
			const sectorX = centerX + offsetX;
			const sectorY = centerY + offsetY;
			const key = `${sectorX}x${sectorY}`;
			const hasTile = Boolean( selection?.directory && selection?.prefix ) &&
				(!availableTiles || availableTiles.has( key ));
			tiles.push( {
				sectorX,
				sectorY,
				offsetX,
				offsetY,
				fileName: hasTile ? `${selection.prefix}_${key}.png` : undefined,
				publicPath: hasTile ?
					missionDungeonMinimapTilePublicPath( selection.directory, selection.prefix, sectorX, sectorY ) :
					undefined
			} );
		}
	}

	return tiles;
}

function missionMinimapTileFileName( sectorX, sectorY ) {
	return `${sectorX}x${sectorY}.png`;
}

async function listConvertedMinimapImages( directoryName ) {
	const sourceRoot = path.join( imageSourceRoot, "Media_extracted", directoryName );
	const publicDirectory = `/assets/images/Media_extracted/${directoryName}`;
	const entries = await listFiles( sourceRoot, {
		extensions: [ ".png" ],
		missing: "empty",
		sort: true
	} );

	if ( entries.length === 0 && directoryName === "minimap" ) {
		throw new Error( `Missing converted minimap images under ${sourceRoot}; run the DDJ image conversion first.` );
	}

	return entries.map( ( sourcePath ) => {
		const relativePath = path.relative( sourceRoot, sourcePath ).replaceAll( "\\", "/" );
		const publicPath = `${publicDirectory}/${relativePath}`;
		const parsedNormalTile = directoryName === "minimap" ? parseMissionMinimapTileName( relativePath ) : undefined;
		const parsedDungeonTile = directoryName === "minimap_d" ?
			parseMissionDungeonMinimapTileName( relativePath ) :
			undefined;
		return {
			directoryName,
			sourcePath,
			targetPath: path.join( imagePublicRoot, "Media_extracted", directoryName, relativePath ),
			fileName: path.basename( relativePath ),
			relativePath,
			publicPath,
			...parsedNormalTile,
			...parsedDungeonTile
		};
	} );
}

async function buildMissionDungeonMinimapManifest( tiles, options = {} ) {
	const manifestPath = options.dungeonMinimapManifestPath ??
		path.join( publicRoot, MISSION_DUNGEON_MINIMAP_MANIFEST_PUBLIC_PATH.replace( /^\//, "" ) );
	const dungeonInfoPath = options.dungeonInfoPath ??
		path.join( extractedRoot, "Data_extracted", "dungeon", "dungeoninfo.txt" );
	const dofRoot = options.dofRoot ?? path.join( extractedRoot, "Data_extracted" );
	const artPrefixes = buildDungeonArtPrefixIndex( tiles );
	const dungeonInfoRows = await readDungeonInfoRows( dungeonInfoPath );
	const dungeons = [];

	for ( const row of dungeonInfoRows ) {
		const dofPath = resolveDofPath( dofRoot, row.dofName );
		const floorLabels = dofPath ? await readDofFloorLabels( dofPath ) : [];
		const floors = floorLabels
			.map( ( label, floorIndex ) => {
				const normalizedLabel = label.trim().toLowerCase();
				const art = artPrefixes.get( normalizedLabel );
				if ( !normalizedLabel || !art ) {
					return null;
				}
				return {
					floorIndex,
					floorLabel: label,
					directory: art.directory,
					prefix: art.prefix,
					tileCount: art.tiles.length,
					bounds: art.bounds,
					tiles: art.tiles
				};
			} )
			.filter( Boolean );

		dungeons.push( {
			regionId: row.regionId,
			sectorId: row.regionId | 0x8000,
			regionHex: toHex16( row.regionId ),
			sectorHex: toHex16( row.regionId | 0x8000 ),
			dofName: row.dofName,
			floors
		} );
	}

	const manifest = {
		format: MISSION_DUNGEON_MINIMAP_MANIFEST_FORMAT,
		version: MISSION_DUNGEON_MINIMAP_MANIFEST_VERSION,
		publicPath: MISSION_DUNGEON_MINIMAP_MANIFEST_PUBLIC_PATH,
		source: {
			dungeonInfoPath: path.relative( extractedRoot, dungeonInfoPath ).replaceAll( "\\", "/" ),
			minimapDirectory: "Media_extracted/minimap_d"
		},
		dungeons,
		tilePaths: await retailMinimapArt( tiles, options.minimapSourceRoot ),
		artSets: [ ...artPrefixes.values() ].map( ( art ) => ({
			directory: art.directory,
			prefix: art.prefix,
			tileCount: art.tiles.length,
			bounds: art.bounds,
			tiles: art.tiles
		}) )
	};

	await writeJsonIfChanged( manifestPath, manifest );
	return manifest;
}

function buildDungeonArtPrefixIndex( tiles ) {
	const groups = new Map();
	for ( const tile of tiles ) {
		if ( tile.directoryName !== "minimap_d" || !tile.dungeonDirectory || !tile.dungeonPrefix ) {
			continue;
		}
		const key = tile.dungeonPrefix.toLowerCase();
		const group = groups.get( key ) ?? {
			directory: tile.dungeonDirectory.toLowerCase(),
			prefix: tile.dungeonPrefix.toLowerCase(),
			coordinates: []
		};
		group.coordinates.push( { x: tile.dungeonTileX, y: tile.dungeonTileY } );
		groups.set( key, group );
	}

	const output = new Map();
	for ( const [key, group] of groups ) {
		const tiles = uniqueStrings( group.coordinates.map( ( coord ) => `${coord.x}x${coord.y}` ) ).sort(
			compareTileKeys
		);
		output.set( key, {
			directory: group.directory,
			prefix: group.prefix,
			tiles,
			bounds: boundsForTileKeys( tiles )
		} );
	}
	return output;
}

function resolveDofPath( root, dofName ) {
	const normalized = dofName.replaceAll( "\\", path.sep ).replaceAll( "/", path.sep );
	return path.join( root, normalized );
}

async function readDofFloorLabels( filePath ) {
	let bytes;
	try {
		bytes = await readFile( filePath );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) {
			return [];
		}
		throw error;
	}

	if ( bytes.subarray( 0, 12 ).toString( "latin1" ) !== "JMXVDOF 0101" ) {
		return [];
	}

	const labelOffset = bytes.readUInt32LE( 0x1c );
	if ( labelOffset <= 0 || labelOffset >= bytes.byteLength ) {
		return [];
	}

	let offset = labelOffset;
	const readU32 = () => {
		const value = bytes.readUInt32LE( offset );
		offset += 4;
		return value;
	};
	const readString = () => {
		const length = readU32();
		const value = decodeJmxText( bytes.subarray( offset, offset + length ) );
		offset += length;
		return value;
	};

	const roomCount = readU32();
	for ( let index = 0; index < roomCount; index += 1 ) {
		readString();
	}
	const floorCount = readU32();
	const floors = [];
	for ( let index = 0; index < floorCount; index += 1 ) {
		floors.push( readString() );
	}
	return floors;
}

function parseMissionMinimapTileName( relativePath ) {
	const match = relativePath.match( /^(\d+)x(\d+)\.png$/i );
	if ( !match ) {
		return undefined;
	}

	return {
		sectorX: Number.parseInt( match[1], 10 ),
		sectorY: Number.parseInt( match[2], 10 )
	};
}

function parseMissionDungeonMinimapTileName( relativePath ) {
	const match = relativePath.match( /^([^/]+)\/(.+)_(-?\d+)x(-?\d+)\.png$/i );
	if ( !match ) {
		return undefined;
	}

	return {
		dungeonDirectory: match[1],
		dungeonPrefix: match[2],
		dungeonTileX: Number.parseInt( match[3], 10 ),
		dungeonTileY: Number.parseInt( match[4], 10 )
	};
}

function boundsForTileKeys( tileKeys ) {
	const coords = tileKeys.map( ( key ) => {
		const [x, y] = key.split( "x" ).map( ( part ) => Number.parseInt( part, 10 ) );
		return { x, y };
	} );
	return {
		minX: Math.min( ...coords.map( ( coord ) => coord.x ) ),
		maxX: Math.max( ...coords.map( ( coord ) => coord.x ) ),
		minY: Math.min( ...coords.map( ( coord ) => coord.y ) ),
		maxY: Math.max( ...coords.map( ( coord ) => coord.y ) )
	};
}

function compareTileKeys( left, right ) {
	const [leftX, leftY] = left.split( "x" ).map( ( part ) => Number.parseInt( part, 10 ) );
	const [rightX, rightY] = right.split( "x" ).map( ( part ) => Number.parseInt( part, 10 ) );
	return leftX - rightX || leftY - rightY;
}

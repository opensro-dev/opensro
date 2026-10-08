import path from "node:path";

import { normalizeAssetPath, readText, splitIndexedRowCells, textDataDir } from "./resourceIo.mjs";

const WORLD_MAP_INFO_FILE = "worldmap_mapinfo.txt";
const WORLD_MAP_LOCAL_INFO_FILE = "worldmap_localinfo.txt";
const WORLD_MAP_TILE_REGION_STEP = 4;

/**
 * Recover the complete native world-map image dependency closure from the two
 * shipped data tables. The world page is tiled, so its DDJ names are derived
 * from the page dimensions and region bounds instead of another allowlist.
 */
export function collectWorldMapImageReferencesFromRows( mapInfoRows, localInfoRows ) {
	const mapRows = parseTableRows( mapInfoRows );
	const localRows = parseTableRows( localInfoRows );
	const references = new Set();

	for ( const columns of mapRows ) {
		const ddjPath = columns[3] ?? "";
		if ( isDdjPath( ddjPath ) ) references.add( normalizeAssetPath( ddjPath ) );
	}
	for ( const columns of localRows ) {
		const kind = Number.parseInt( columns[1] ?? "", 10 );
		const ddjPath = columns[2] ?? "";
		if ( kind === 2 && isDdjPath( ddjPath ) ) references.add( normalizeAssetPath( ddjPath ) );
	}

	const worldRow = mapRows.find(
		( columns ) => Number.parseInt( columns[0] ?? "", 10 ) === 0 && Number.parseInt( columns[1] ?? "", 10 ) === 0
	);
	if ( !worldRow ) throw new Error( `${WORLD_MAP_INFO_FILE} has no world-page row (id=0, kind=0).` );

	const tileReferences = deriveWorldTileReferences( worldRow );
	for ( const ddjPath of tileReferences ) references.add( ddjPath );

	return {
		references: [ ...references ].sort(),
		tileReferences,
		mapPageReferences: uniqueSorted(
			mapRows.map( ( columns ) => columns[3] ?? "" ).filter( isDdjPath ).map( normalizeAssetPath )
		),
		overlayReferences: uniqueSorted(
			localRows
				.filter( ( columns ) => Number.parseInt( columns[1] ?? "", 10 ) === 2 )
				.map( ( columns ) => columns[2] ?? "" )
				.filter( isDdjPath )
				.map( normalizeAssetPath )
		)
	};
}

export async function collectWorldMapImageReferences() {
	const [mapInfoText, localInfoText] = await Promise.all( [
		readText( path.join( textDataDir, WORLD_MAP_INFO_FILE ) ),
		readText( path.join( textDataDir, WORLD_MAP_LOCAL_INFO_FILE ) )
	] );
	return collectWorldMapImageReferencesFromRows(
		mapInfoText.split( /\r?\n/ ),
		localInfoText.split( /\r?\n/ )
	);
}

function deriveWorldTileReferences( columns ) {
	const textureWidth = requirePositiveInteger( columns[4], "world texture width" );
	const textureHeight = requirePositiveInteger( columns[5], "world texture height" );
	const drawWidth = requirePositiveInteger( columns[8], "world draw width" );
	const drawHeight = requirePositiveInteger( columns[9], "world draw height" );
	const regionLeft = requireInteger( columns[10], "world left region" );
	const regionTop = requireInteger( columns[11], "world top region" );
	if ( drawWidth % textureWidth !== 0 || drawHeight % textureHeight !== 0 ) {
		throw new Error(
			`${WORLD_MAP_INFO_FILE} world-page dimensions do not form a whole tile grid: ` +
				`${drawWidth}x${drawHeight} / ${textureWidth}x${textureHeight}.`
		);
	}

	const references = [];
	for ( let column = 0; column < drawWidth / textureWidth; column += 1 ) {
		for ( let row = 0; row < drawHeight / textureHeight; row += 1 ) {
			const tileA = regionLeft + WORLD_MAP_TILE_REGION_STEP * column;
			const tileB = regionTop - WORLD_MAP_TILE_REGION_STEP * row;
			references.push( `interface/worldmap/map/map_world_${tileA}x${tileB}.ddj` );
		}
	}
	return references;
}

function parseTableRows( lines ) {
	return lines
		.map( ( line ) => line.trim() )
		.filter( ( line ) => line.length > 0 && !line.startsWith( "//" ) )
		.map( splitIndexedRowCells )
		.filter( ( columns ) => /^\d+$/.test( columns[0] ?? "" ) );
}

function isDdjPath( value ) {
	return typeof value === "string" && /\.ddj$/i.test( value.trim() );
}

function requirePositiveInteger( value, label ) {
	const parsed = requireInteger( value, label );
	if ( parsed <= 0 ) {
		throw new Error( `${WORLD_MAP_INFO_FILE} ${label} must be positive; got ${JSON.stringify( value )}.` );
	}
	return parsed;
}

function requireInteger( value, label ) {
	const parsed = Number( value );
	if ( !Number.isInteger( parsed ) ) {
		throw new Error( `${WORLD_MAP_INFO_FILE} ${label} must be an integer; got ${JSON.stringify( value )}.` );
	}
	return parsed;
}

function uniqueSorted( values ) {
	return [ ...new Set( values ) ].sort();
}

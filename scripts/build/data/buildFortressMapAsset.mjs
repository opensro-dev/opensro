/*
===========================================================================

buildFortressMapAsset.mjs - the battle aide's fortress map: pages, zones and
structures

CIFFortressMap (65E330) embeds a world map page (worldmap_mapinfo.txt ids
1000 and up name their fortress world in the last column) and draws one
marker per event zone of that world (CIFWorldMap_RefreshFortressOverlays
57E250). The zones are eventzonedata.txt rows; their positions come from the
Map.pk2 ObjectString.ifo the client world loads (SWorld_LoadMapInfo 8B63F2),
which is why a zone that file does not place has no marker. Each marker and
the status panel (65D970) read the standing structure's RefObjChar fields:

	column  5 -> +0x060 name symbol (the zone record's +0x24)
	column 27 -> +0x0B4 repair cost   column 28 -> +0x0B8 revive cost
	column 54 -> +0x154 icon; "_d" is drawn at 0 HP, "_r" below full
	column 57 -> +0x1A8 level (the panel's rank)
	column 59 -> +0x1B0 max HP        column 77 -> +0x1E0 physical defence
	column 89 -> +0x210 first default skill (the panel's feature line)
	column 110/111 -> +0x264/+0x268 the gold and guild points a construction
	                  or upgrade of this structure costs

siegestructupgradedata.txt (loader kind 0x3C) maps a structure to its
targets, read until the first "xxx"; a target whose codename has no
reference is 0, which the panel shows as the top rank.

Output: /assets/data/fortressMap.json, { format, version, pages, zones,
structures }.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { parseObjectStringIfo } from "../server/structureZones.mjs";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";
import { mapExtractedRoot, publicRoot, retailTextdataRoot } from "../world/paths.mjs";

const FORMAT = "sro-fortress-map";
const VERSION = 1;

// worldmap_mapinfo.txt: ids 1000 and up are fortress and arena pages.
const MAP_PAGE_FIRST = 1000;
const MAP_COLUMN_WORLD = 18;
const MAP_COLUMNS = 19;

// eventzonedata.txt
const ZONE_COLUMN_ID = 1;
const ZONE_COLUMN_CODENAME = 2;
const ZONE_COLUMN_TYPE = 3;
const ZONE_COLUMN_DEFAULT = 9;
const ZONE_COLUMN_WORLD = 10;

// characterdata*.txt
const CHAR_COLUMN_ID = 1;
const CHAR_COLUMN_CODENAME = 2;
const CHAR_COLUMN_NAME = 5;
const CHAR_COLUMN_COST_REPAIR = 27;
const CHAR_COLUMN_COST_REVIVE = 28;
const CHAR_COLUMN_ICON = 54;
const CHAR_COLUMN_LEVEL = 57;
const CHAR_COLUMN_MAX_HP = 59;
const CHAR_COLUMN_DEFENCE = 77;
const CHAR_COLUMN_SKILL = 89;
const CHAR_COLUMN_UPGRADE_GOLD = 110;
const CHAR_COLUMN_UPGRADE_POINTS = 111;
const STRUCTURE_PREFIX = "STRUCTURE_";

// siegestructupgradedata.txt
const UPGRADE_COLUMN_CODENAME = 1;
const UPGRADE_COLUMN_FIRST_TARGET = 3;
const UPGRADE_NONE = "xxx";

/*
================
integer
================
*/
function integer( value, label ) {
	const n = Number( String( value ?? "" ).trim() );
	if ( !Number.isSafeInteger( n ) ) throw new Error( `[fortress-map] ${label}: ${JSON.stringify( value )}` );
	return n;
}

/*
================
fortressMapPages

The fortress pages: id, the world they show, the picture and its geometry
in the overview's field order (texture, picture, draw size, region box and
the left-top / right-bottom offsets).
================
*/
export function fortressMapPages( rows ) {
	const pages = [];
	for ( const cols of rows ) {
		const id = Number( cols[0] );
		if ( !Number.isInteger( id ) || id < MAP_PAGE_FIRST || cols.length < MAP_COLUMNS ) continue;
		const world = cols[MAP_COLUMN_WORLD].trim();
		if ( !world || world === UPGRADE_NONE ) continue;
		const numbers = [];
		for ( let i = 4; i < 18; i++ ) numbers.push( integer( cols[i], `map page ${id} column ${i}` ) );
		pages.push( {
			id,
			world,
			image: cols[3].trim().replaceAll( "\\", "/" ).toLowerCase(),
			texture: numbers.slice( 0, 2 ),
			picture: numbers.slice( 2, 4 ),
			size: numbers.slice( 4, 6 ),
			bounds: numbers.slice( 6, 14 )
		} );
	}
	return pages;
}

/*
================
fortressMapZones

The structure zones of every page's world that the client world places:
id, codename, type (DEFENSIVE_POSITION, GUARD_TOWER, BARRICADE, ...), the
default structure and the placement's region and position.
================
*/
export function fortressMapZones( rows, placements, worlds ) {
	const byName = new Map( placements.map( row => [ row.name.toUpperCase(), row ] ) );
	const zones = [];
	for ( const cols of rows ) {
		if ( cols[0]?.trim() !== "1" || cols.length <= ZONE_COLUMN_WORLD ) continue;
		const world = cols[ZONE_COLUMN_WORLD].trim();
		if ( !worlds.has( world ) ) continue;
		const codeName = cols[ZONE_COLUMN_CODENAME].trim();
		const placed = byName.get( codeName.toUpperCase() );
		if ( !placed ) continue;
		zones.push( {
			id: integer( cols[ZONE_COLUMN_ID], `zone ${codeName}` ),
			codeName,
			type: cols[ZONE_COLUMN_TYPE].trim(),
			world,
			defaultStructure: cols[ZONE_COLUMN_DEFAULT].trim(),
			regionId: placed.regionId,
			x: placed.x,
			y: placed.y,
			z: placed.z
		} );
	}
	return zones.sort( ( a, b ) => a.id - b.id );
}

/*
================
fortressStructures

Every in-service structure reference, keyed by RefObj id, with the fields
the map reads and its upgrade targets (ids, 0 for a codename with no
reference).
================
*/
export function fortressStructures( characterRows, upgradeRows ) {
	const byCode = new Map(), structures = {};
	for ( const cols of characterRows ) {
		if ( cols[0]?.trim() !== "1" || cols.length <= CHAR_COLUMN_UPGRADE_POINTS ) continue;
		const codeName = cols[CHAR_COLUMN_CODENAME].trim();
		if ( !codeName.startsWith( STRUCTURE_PREFIX ) ) continue;
		const id = integer( cols[CHAR_COLUMN_ID], codeName );
		const icon = cols[CHAR_COLUMN_ICON].trim().replaceAll( "\\", "/" ).toLowerCase();
		byCode.set( codeName.toUpperCase(), id );
		structures[String( id )] = {
			codeName,
			name: cols[CHAR_COLUMN_NAME].trim(),
			icon: /\.ddj$/.test( icon ) ? icon : "",
			level: integer( cols[CHAR_COLUMN_LEVEL], `${codeName} level` ),
			maxHp: integer( cols[CHAR_COLUMN_MAX_HP], `${codeName} max HP` ),
			defence: integer( cols[CHAR_COLUMN_DEFENCE], `${codeName} defence` ),
			skill: integer( cols[CHAR_COLUMN_SKILL], `${codeName} skill` ),
			costRepair: integer( cols[CHAR_COLUMN_COST_REPAIR], `${codeName} repair cost` ),
			costRevive: integer( cols[CHAR_COLUMN_COST_REVIVE], `${codeName} revive cost` ),
			upgradeGold: integer( cols[CHAR_COLUMN_UPGRADE_GOLD], `${codeName} upgrade gold` ),
			upgradeGuildPoints: integer( cols[CHAR_COLUMN_UPGRADE_POINTS], `${codeName} upgrade points` ),
			upgrades: null
		};
	}
	for ( const cols of upgradeRows ) {
		if ( cols[0]?.trim() !== "1" ) continue;
		const id = byCode.get( cols[UPGRADE_COLUMN_CODENAME]?.trim().toUpperCase() ?? "" );
		if ( id === undefined ) continue;
		const targets = [];
		for ( let i = UPGRADE_COLUMN_FIRST_TARGET; i < cols.length; i++ ) {
			const code = cols[i].trim();
			if ( !code || code.toLowerCase() === UPGRADE_NONE ) break;
			targets.push( byCode.get( code.toUpperCase() ) ?? 0 );
		}
		structures[String( id )].upgrades = targets;
	}
	return structures;
}

/*
================
fortressMapImageReferences

The DDJ paths the map draws: each structure icon with its damaged and
destroyed variants, and the barricade zones' gate marker.
================
*/
export function fortressMapImageReferences( structures ) {
	const references = new Set( [ "interface/worldmap/map/gate.ddj" ] );
	for ( const row of Object.values( structures ) ) {
		if ( !row.icon ) continue;
		const stem = row.icon.slice( 0, -".ddj".length );
		for ( const ddj of [ row.icon, stem + "_d.ddj", stem + "_r.ddj" ] ) references.add( "icon/" + ddj );
	}
	return [ ...references ].sort();
}

/*
================
readFortressMapSources
================
*/
export function readFortressMapSources( textdataRoot = retailTextdataRoot ) {
	const characterRows = [];
	for ( const shard of listTextDataShardNamesSync( textdataRoot, /^characterdata.*\.txt$/i ) ) {
		characterRows.push( ...readTextDataRowsSync( path.join( textdataRoot, shard ) ) );
	}
	return {
		mapRows: readTextDataRowsSync( path.join( textdataRoot, "worldmap_mapinfo.txt" ) ),
		zoneRows: readTextDataRowsSync( path.join( textdataRoot, "eventzonedata.txt" ) ),
		upgradeRows: readTextDataRowsSync( path.join( textdataRoot, "siegestructupgradedata.txt" ) ),
		characterRows,
		placements: parseObjectStringIfo( readFileSync( path.join( mapExtractedRoot, "ObjectString.ifo" ), "latin1" ) )
	};
}

/*
================
buildFortressMapAsset
================
*/
export async function buildFortressMapAsset( options = {} ) {
	const sources = options.sources ?? readFortressMapSources();
	const pages = fortressMapPages( sources.mapRows );
	const zones = fortressMapZones( sources.zoneRows, sources.placements, new Set( pages.map( page => page.world ) ) );
	const structures = fortressStructures( sources.characterRows, sources.upgradeRows );
	const { outPath } = exportDataAsset( {
		publicRoot: options.publicRoot ?? publicRoot,
		outputFileName: "fortressMap.json",
		value: { format: FORMAT, version: VERSION, pages, zones, structures }
	} );
	await refreshPrecompressedSidecars( [ outPath ], { onlyWhenStale: true } );
	console.log(
		`[fortress-map] wrote ${pages.length} page(s), ${zones.length} zone(s), ` +
			`${Object.keys( structures ).length} structure reference(s)`
	);
	return {
		outPath,
		pages: pages.length,
		zones: zones.length,
		imageReferences: fortressMapImageReferences( structures )
	};
}

if ( isMainScript( import.meta.url ) ) await buildFortressMapAsset();

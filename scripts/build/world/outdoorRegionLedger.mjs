/*
===========================================================================

outdoorRegionLedger.mjs - what each outdoor region bundle was built from

The outdoor builder's build cache, never published: per region, the terrain
tiles its bundle names, every public path it names, and the digest of the
inputs it was built from. The builder reuses a bundle only while that digest
matches (buildOutdoorWorldRegionResources.mjs), and the resource build
refuses bundles it does not (findStaleOutdoorRegions). It exists because
reuse keyed on code alone kept Baghdad's 11-15 bundle after an 11-16 import
renumbered its objects ("Missing navigation BSR 2631").

===========================================================================
*/
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { publicPathToFile } from "../shared/assetPaths.mjs";
import { mapWithConcurrency } from "../shared/asyncUtils.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { compareText } from "../shared/textOrder.mjs";
import { OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH, OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH } from "./constants.mjs";
import { WORLD_LAYER_TABLE } from "./maploader/worldLayers.mjs";
import { extractedRoot, generatedRoot, publicRoot } from "./paths.mjs";

// Build cache, never published: the terrain tiles each region bundle
// references. A reused region re-publishes its tiles from here, so a bundle
// can never be "current" while the images it names are missing; a region
// this ledger does not know yet is read from its bundle once.
const TERRAIN_TILE_LEDGER_PATH = path.join( generatedRoot, "intermediate", "outdoor-terrain-tiles.json" );
// v4: each ledger tile records the reference its bundle names
// (imagePublicPath), so a hit is trusted only while every reference still
// matches the probe; an older ledger cannot tell and is discarded.
// v5: each region also records the public paths its bundle names, so a
// reused region claims them without re-reading the bundle (2.4 GB in all).
// v6: each region also records the digest of the inputs it was built from
// (outdoorRegionInputDigest); a v5 ledger cannot vouch for any bundle.
const TERRAIN_TILE_LEDGER_VERSION = 6;

/*
================
readTerrainTileLedger
================
*/
export async function readTerrainTileLedger() {
	return (await readRegionLedger()).tiles;
}

/*
================
readRegionLedger

The ledger's three maps by region id: the terrain tiles each bundle names,
every public path it names (claimKeptOutput's input on reuse), and the
digest of the inputs it was built from.
================
*/
export async function readRegionLedger() {
	try {
		const ledger = JSON.parse( await readFile( TERRAIN_TILE_LEDGER_PATH, "utf8" ) );
		if (
			ledger.version === TERRAIN_TILE_LEDGER_VERSION && ledger.regions && ledger.references && ledger.inputs
		) {
			return {
				tiles: new Map( Object.entries( ledger.regions ) ),
				references: new Map( Object.entries( ledger.references ) ),
				inputs: new Map( Object.entries( ledger.inputs ) )
			};
		}
	} catch ( error ) {
		if ( error.code !== "ENOENT" ) throw error;
	}
	return { tiles: new Map(), references: new Map(), inputs: new Map() };
}

/*
================
writeTerrainTileLedger
================
*/
export async function writeTerrainTileLedger( tilesByRegion, referencesByRegion, inputsByRegion ) {
	await mkdir( path.dirname( TERRAIN_TILE_LEDGER_PATH ), { recursive: true } );
	const sorted = ( map ) => Object.fromEntries( [ ...map ].sort( ( [a], [b] ) => compareText( a, b ) ) );
	await writeFile(
		TERRAIN_TILE_LEDGER_PATH,
		JSON.stringify( {
			version: TERRAIN_TILE_LEDGER_VERSION,
			regions: sorted( tilesByRegion ),
			references: sorted( referencesByRegion ),
			inputs: sorted( inputsByRegion )
		} )
	);
}

/*
================
outdoorSharedInputDigest

The digest of what every region bundle resolves through: the map object and
tile catalogues, the seasonal layer table, and the shared object and render
stores as published. Any change rebuilds every region, because a bundle
embeds object ids and collision rows from them.
================
*/
export async function outdoorSharedInputDigest( extractedRootPath, publicRootPath = publicRoot ) {
	const mapRoot = path.join( extractedRootPath, "Map_extracted" );
	const files = [
		path.join( mapRoot, "object.ifo" ),
		path.join( mapRoot, "tile2d.ifo" ),
		path.join( mapRoot, WORLD_LAYER_TABLE ),
		publicPathToFile( OUTDOOR_WORLD_OBJECT_INDEX_PUBLIC_PATH, publicRootPath ),
		publicPathToFile( OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH, publicRootPath )
	];
	return sha256Hex( (await Promise.all( files.map( fileDigestOrAbsent ) )).join( ":" ) );
}

/*
================
outdoorRegionInputDigest

The digest of one sector's own source files (outdoorRegionInputPaths) under
the shared digest. Equal digests mean the builder would read the same bytes.
================
*/
export async function outdoorRegionInputDigest( sector, sharedDigest ) {
	const parts = await Promise.all( outdoorRegionInputPaths( sector ).map( fileDigestOrAbsent ) );
	return sha256Hex( `${sharedDigest}:${parts.join( ":" )}` );
}

/*
================
outdoorRegionInputPaths

The files one sector's bundle is built from: discovery requires all four.
================
*/
function outdoorRegionInputPaths( sector ) {
	return [ `${sector.mapBasePath}.m`, `${sector.mapBasePath}.t`, `${sector.mapBasePath}.o2`, sector.navmeshPath ];
}

/*
================
fileDigestOrAbsent

A file's SHA-256, or a fixed marker when it does not exist (the layer table
is optional), so adding or removing it changes the digest.
================
*/
async function fileDigestOrAbsent( file ) {
	try {
		return sha256Hex( await readFile( file ) );
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return "absent";
		throw error;
	}
}

/*
================
findStaleOutdoorRegions

The sectors whose recorded input digest differs from their inputs now, or
that the ledger does not vouch for at all. The resource build refuses them:
a stale bundle loads, but names objects the published index may not hold.
================
*/
export async function findStaleOutdoorRegions( options = {} ) {
	const sourceExtractedRoot = options.extractedRoot ?? extractedRoot;
	const sectors = options.sectors;
	const { inputs } = await readRegionLedger();
	const sharedDigest = await outdoorSharedInputDigest( sourceExtractedRoot, options.publicRoot );
	const stale = [];
	await mapWithConcurrency( sectors, options.jobs ?? 1, async ( sector ) => {
		const recorded = inputs.get( String( sector.id ) );
		if ( !recorded || recorded !== (await outdoorRegionInputDigest( sector, sharedDigest )) ) {
			stale.push( sector.id );
		}
	} );
	return stale.sort( compareText );
}

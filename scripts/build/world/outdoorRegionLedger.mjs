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

Beside the digests, each region records a stat signature of its inputs and
the ledger one whole-set key over them: an unchanged input set is
recognised from stats alone, without re-reading or re-hashing a single
region file (#4337 item 35).

===========================================================================
*/
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
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
// v7: each region also records a stat signature of its inputs, and the
// ledger the shared digest those digests were computed under plus one
// whole-set key over the signatures, so an unchanged input set skips the
// per-region pass entirely; a v6 ledger records no signatures.
const TERRAIN_TILE_LEDGER_VERSION = 7;

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

The ledger's maps by region id: the terrain tiles each bundle names, every
public path it names (claimKeptOutput's input on reuse), the digest of the
inputs it was built from, and the stat signature of those inputs. Plus the
shared digest every recorded input digest was computed under (null when a
partial run left the set mixed) and the whole-set key over the signatures
(null when no full run vouched for the set). A ledgerPath names another
ledger (a test's fixture tree); the build's own ledger is the default.
================
*/
export async function readRegionLedger( ledgerPath = TERRAIN_TILE_LEDGER_PATH ) {
	try {
		const ledger = JSON.parse( await readFile( ledgerPath, "utf8" ) );
		if (
			ledger.version === TERRAIN_TILE_LEDGER_VERSION && ledger.regions && ledger.references && ledger.inputs &&
			ledger.inputStats
		) {
			return {
				tiles: new Map( Object.entries( ledger.regions ) ),
				references: new Map( Object.entries( ledger.references ) ),
				inputs: new Map( Object.entries( ledger.inputs ) ),
				inputStats: new Map( Object.entries( ledger.inputStats ) ),
				setKey: ledger.setKey ?? null,
				sharedDigest: ledger.sharedDigest ?? null
			};
		}
	} catch ( error ) {
		if ( error.code !== "ENOENT" ) throw error;
	}
	return {
		tiles: new Map(),
		references: new Map(),
		inputs: new Map(),
		inputStats: new Map(),
		setKey: null,
		sharedDigest: null
	};
}

/*
================
writeTerrainTileLedger

cache carries the reuse vouchers: inputStats (each region's stat
signature), sharedDigest and setKey (outdoorInputSetKey) as readRegionLedger
returns them. The defaults write an unvouched ledger: correct, just slower
to re-check.
================
*/
export async function writeTerrainTileLedger( tilesByRegion, referencesByRegion, inputsByRegion, cache = {} ) {
	const ledgerPath = cache.ledgerPath ?? TERRAIN_TILE_LEDGER_PATH;
	await mkdir( path.dirname( ledgerPath ), { recursive: true } );
	const sorted = ( map ) => Object.fromEntries( [ ...map ].sort( ( [a], [b] ) => compareText( a, b ) ) );
	await writeFile(
		ledgerPath,
		JSON.stringify( {
			version: TERRAIN_TILE_LEDGER_VERSION,
			regions: sorted( tilesByRegion ),
			references: sorted( referencesByRegion ),
			inputs: sorted( inputsByRegion ),
			inputStats: sorted( cache.inputStats ?? new Map() ),
			setKey: cache.setKey ?? null,
			sharedDigest: cache.sharedDigest ?? null
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
outdoorRegionInputSignature

One sector's input files as stats alone: each file's size and mtime, or
absent. Equal signatures mean the files the recorded digest was computed
from still stand as they were, so the digest still describes them and the
files are never read. The trust is the one fileHashCache.mjs documents: a
content change preserving both size and mtime is invisible, and none of
this pipeline's writers do that (extractors and builders always rewrite
with fresh mtimes).
================
*/
export async function outdoorRegionInputSignature( sector ) {
	const parts = [];
	for ( const file of outdoorRegionInputPaths( sector ) ) {
		try {
			const info = await stat( file );
			parts.push( `${info.size}:${info.mtimeMs}` );
		} catch ( error ) {
			if ( error.code !== "ENOENT" ) throw error;
			parts.push( "absent" );
		}
	}
	return parts.join( ":" );
}

/*
================
outdoorInputSetKey

One key for the whole input set: the shared digest over every region's
stat signature, regions in id order. While it matches the ledger's, the
recorded digests vouch for every bundle and the per-region pass never
runs; on a miss it names the regions whose signatures moved.
================
*/
export function outdoorInputSetKey( sharedDigest, signaturesByRegion ) {
	const lines = [ ...signaturesByRegion ].sort( ( [a], [b] ) => compareText( a, b ) )
		.map( ( [regionId, signature] ) => `${regionId}=${signature}` );
	return sha256Hex( `${sharedDigest}:${lines.join( "\n" )}` );
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

The unchanged case never hashes a region: one stat probe per input file
rebuilds the whole-set key, and while it matches the ledger's every
recorded digest stands. On a miss, only regions whose signature moved (or
that the ledger does not know) are re-hashed — unless the shared digest
moved, which is folded into every region's digest and moves them all.
options.regionDigest replaces the digest computation (a test counts the
calls to prove the unchanged case makes none).
================
*/
export async function findStaleOutdoorRegions( options = {} ) {
	const sourceExtractedRoot = options.extractedRoot ?? extractedRoot;
	const sectors = options.sectors;
	const regionDigest = options.regionDigest ?? outdoorRegionInputDigest;
	const ledger = await readRegionLedger( options.ledgerPath );
	const sharedDigest = await outdoorSharedInputDigest( sourceExtractedRoot, options.publicRoot );
	const signatures = new Map();
	await mapWithConcurrency( sectors, options.jobs ?? 1, async ( sector ) => {
		signatures.set( String( sector.id ), await outdoorRegionInputSignature( sector ) );
	} );
	if ( ledger.setKey !== null && outdoorInputSetKey( sharedDigest, signatures ) === ledger.setKey ) {
		return [];
	}
	const sharedMoved = ledger.sharedDigest === null || ledger.sharedDigest !== sharedDigest;
	const stale = [];
	await mapWithConcurrency( sectors, options.jobs ?? 1, async ( sector ) => {
		const recorded = ledger.inputs.get( String( sector.id ) );
		if ( !recorded ) {
			stale.push( sector.id );
			return;
		}
		// The signature stands where the digest was recorded and the shared
		// digest stands with it: these bytes are the digested bytes.
		if ( !sharedMoved && signatures.get( String( sector.id ) ) === ledger.inputStats.get( String( sector.id ) ) ) {
			return;
		}
		if ( recorded !== (await regionDigest( sector, sharedDigest )) ) {
			stale.push( sector.id );
		}
	} );
	return stale.sort( compareText );
}

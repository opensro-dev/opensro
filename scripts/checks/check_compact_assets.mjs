/*
===========================================================================

check_compact_assets.mjs - a compacted tree still holds a complete release

After compaction the loose server projection is gone and its archive, the
packed client assets and the compact-state marker must agree: sizes, file
counts, required outdoor regions and referenced world-map images.

===========================================================================
*/
import { readFile, stat } from "node:fs/promises";
import path from "node:path";

import { listFiles } from "../build/shared/fsUtils.mjs";
import { toPublicImagePath } from "../build/shared/assetPaths.mjs";
import { collectWorldMapImageReferences } from "../build/shared/worldMapImageReferences.mjs";
import { validateServerGameDataArchive } from "../build/server/serverGameDataArchive.mjs";
import { generatedRoot, publicAssetsRoot, rebuildRoot, serverGameDataRoot } from "../build/world/paths.mjs";

const generatedAssetsRoot = path.join( generatedRoot, "intermediate" );
const serverGameDataArchivePath = `${serverGameDataRoot}.srogz`;
const statePath = path.join( rebuildRoot, ".state", "compact-assets.json" );
const manifestPath = path.join( publicAssetsRoot, "packs", "manifest.json" );
const originalPk2Bytes = 2_356_998_144;
const maximumCompactBytes = Math.floor( originalPk2Bytes * 0.8 );
const expectedOutdoorRegionCount = 2_123;
const requiredOutdoorPaths = [
	"/assets/world/outdoor/world-regions.json.gz",
	"/assets/world/outdoor/world-region-catalog.json.gz",
	"/assets/world/outdoor/regions/region-6a48.json.gz"
];

const [state, manifest, worldMapClosure] = await Promise.all( [
	readJson( statePath ),
	readJson( manifestPath ),
	collectWorldMapImageReferences()
] );
if ( state.format !== "sro-compact-assets" || state.version !== 2 ) {
	fail( `Invalid compact-state marker: ${statePath}` );
}

assertOutdoorReleaseProjection( manifest );
assertWorldMapReleaseProjection( manifest, worldMapClosure );

let compressedBytes = 0;
let packCount = 0;
for ( const group of manifest.groups ?? [] ) {
	for ( const pack of group.packs ?? [] ) {
		const identityPath = resolvePublicPath( pack.path );
		const identityStats = await stat( identityPath ).catch( () => undefined );
		if ( identityStats ) {
			fail( `Compact tree still contains identity pack ${pack.path}` );
		}
		const sidecarPath = resolvePublicPath( pack.zstdPath );
		const sidecarStats = await stat( sidecarPath ).catch( () => undefined );
		if ( !sidecarStats?.isFile() || sidecarStats.size !== pack.zstdBytes ) {
			fail( `Missing or wrong-size zstd pack ${pack.zstdPath}` );
		}
		compressedBytes += sidecarStats.size;
		packCount += 1;
	}
}

const generatedStats = await stat( generatedAssetsRoot ).catch( () => undefined );
if ( generatedStats ) {
	fail( `Reproducible staging cache still exists: ${generatedAssetsRoot}` );
}
const looseServerStats = await stat( serverGameDataRoot ).catch( () => undefined );
if ( looseServerStats ) {
	fail( `Loose server projection still exists: ${serverGameDataRoot}` );
}
const serverArchive = await validateServerGameDataArchive( serverGameDataArchivePath );
const serverArchiveBytes = (await stat( serverGameDataArchivePath )).size;
if ( serverArchiveBytes !== state.serverArchive?.bytes || serverArchive.fileCount !== state.serverArchive?.fileCount ) {
	fail( "Server game-data archive no longer matches the compact-state marker." );
}

const publicFiles = await listFiles( publicAssetsRoot );
let footprintBytes = 0;
for ( const filePath of publicFiles ) {
	footprintBytes += (await stat( filePath )).size;
}
const releaseFootprintBytes = footprintBytes + serverArchiveBytes;
if ( releaseFootprintBytes > maximumCompactBytes ) {
	fail(
		`Compact release footprint ${formatBytes( releaseFootprintBytes )} exceeds the 80%-of-PK2 ceiling ` +
			`${formatBytes( maximumCompactBytes )}.`
	);
}
if ( packCount !== state.packCount || compressedBytes !== state.compressedPackBytes ) {
	fail( "Compact-state pack totals no longer match the installed pack tree." );
}

console.log(
	`Compact asset check OK: ${packCount} zstd-only packs, ${publicFiles.length} installed files, ` +
		`${formatBytes( releaseFootprintBytes )} including the server projection archive.`
);
console.log(
	`The installed release footprint is ${((releaseFootprintBytes / originalPk2Bytes) * 100).toFixed( 1 )}% ` +
		`of the original ${formatBytes( originalPk2Bytes )} PK2 payload.`
);

/*
================
readJson
================
*/
async function readJson( filePath ) {
	return JSON.parse( await readFile( filePath, "utf8" ) );
}

/*
================
assertOutdoorReleaseProjection
================
*/
function assertOutdoorReleaseProjection( manifest ) {
	const outdoorGroup = manifest.groups?.find( ( group ) => group.name === "outdoor-world" );
	if ( !outdoorGroup || outdoorGroup.assetCount === 0 || outdoorGroup.packs?.length === 0 ) {
		fail( "outdoor-world is empty; a compact release would reproduce the walking-in-air boundary regression." );
	}

	const outdoorAssets = new Set(
		(manifest.assets ?? [])
			.filter( ( asset ) => asset.group === "outdoor-world" )
			.map( ( asset ) => asset.path )
	);
	const regionCount =
		[ ...outdoorAssets ].filter( ( publicPath ) =>
			/^\/assets\/world\/outdoor\/regions\/region-[0-9a-f]{4}\.json\.gz$/i.test( publicPath )
		).length;
	if ( regionCount !== expectedOutdoorRegionCount ) {
		fail(
			`outdoor-world contains ${regionCount} independently streamed regions; expected ${expectedOutdoorRegionCount}.`
		);
	}
	for ( const publicPath of requiredOutdoorPaths ) {
		if ( !outdoorAssets.has( publicPath ) ) {
			fail( `outdoor-world is missing required routing asset ${publicPath}` );
		}
	}
}

/*
================
assertWorldMapReleaseProjection
================
*/
function assertWorldMapReleaseProjection( manifest, closure ) {
	if (
		closure.tileReferences.length !== 224 ||
		closure.mapPageReferences.length !== 7 ||
		closure.overlayReferences.length !== 29 ||
		closure.references.length !== 260
	) {
		fail(
			`world-map source closure drifted: ${closure.tileReferences.length} tiles, ` +
				`${closure.mapPageReferences.length} pages, ${closure.overlayReferences.length} overlays, ` +
				`${closure.references.length} total.`
		);
	}

	const group = manifest.groups?.find( ( candidate ) => candidate.name === "game-images" );
	if ( !group || group.load !== "startup" || group.assetCount === 0 || group.packs?.length === 0 ) {
		fail( "world-map assets require a populated startup-resident game-images group." );
	}
	const assets = new Map( (manifest.assets ?? []).map( ( asset ) => [ asset.path.toLowerCase(), asset ] ) );
	for ( const ddjPath of closure.references ) {
		const publicPath = toPublicImagePath( "Media_extracted", ddjPath );
		if ( assets.get( publicPath.toLowerCase() )?.group !== "game-images" ) {
			fail( `world-map dependency is absent from startup packs: ${publicPath}` );
		}
	}
}

/*
================
resolvePublicPath
================
*/
function resolvePublicPath( publicPath ) {
	if ( typeof publicPath !== "string" || !publicPath.startsWith( "/assets/" ) ) {
		fail( `Invalid public path in pack manifest: ${String( publicPath )}` );
	}
	const absolutePath = path.resolve( path.dirname( publicAssetsRoot ), publicPath.replace( /^\/+/, "" ) );
	const relative = path.relative( path.dirname( publicAssetsRoot ), absolutePath );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		fail( `Public path escapes the public root: ${publicPath}` );
	}
	return absolutePath;
}

/*
================
formatBytes
================
*/
function formatBytes( bytes ) {
	return `${(bytes / (1024 ** 3)).toFixed( 3 )} GiB (${bytes.toLocaleString( "en-US" )} bytes)`;
}

/*
================
fail
================
*/
function fail( message ) {
	throw new Error( `Compact asset check failed: ${message}` );
}

/*
===========================================================================

compact_sro_assets.mjs - compact the generated tree and the server game-data archive

Compacts the published client assets and the server game-data archive,
verifies the archive, and refuses to run without the regeneration sources
(the client PK2 archives and their extracted trees). Roots come from
build/world/paths.mjs.

===========================================================================
*/
// First: it sizes libuv's thread pool, which runs the zstd compression below.
import { buildJobs } from "./build/shared/buildParallelism.mjs";
import { assertInsideRoot, containedPublicFile, normalizePublicAssetPath } from "./build/shared/assetPaths.mjs";
import { createLimiter } from "./build/shared/asyncUtils.mjs";
import { ASSET_PACK_ZSTD_LEVEL, ASSET_PACK_ZSTD_WINDOW_LOG, compressAssetPackZstd } from "./build/assetPacks.mjs";
import { publishAssetPackManifest } from "./build/assetPackPublication.mjs";
import { createHash } from "node:crypto";
import { mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import * as zlib from "node:zlib";

import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { compressBrotliSync } from "./build/shared/compressionUtils.mjs";
import { listFiles } from "./build/shared/fsUtils.mjs";
import { validateServerGameDataArchive } from "./build/server/serverGameDataArchive.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";
import {
	dataExtractedRoot,
	gameRoot,
	generatedRoot,
	mapExtractedRoot,
	mediaExtractedRoot,
	publicAssetsRoot,
	publicRoot,
	rebuildRoot,
	serverGameDataRoot
} from "./build/world/paths.mjs";

const generatedAssetsRoot = path.join( generatedRoot, "intermediate" );
const serverGameDataArchivePath = `${serverGameDataRoot}.srogz`;
const serverGameDataCacheRoot = path.join( path.dirname( serverGameDataRoot ), ".game-data-cache" );
const packManifestPath = path.join( publicAssetsRoot, "packs", "manifest.json" );
const compactStatePath = path.join( rebuildRoot, ".state", "compact-assets.json" );
const dropGeneratedCache = process.argv.includes( "--drop-generated-cache" );

const BOOTSTRAP_PUBLIC_PATHS = new Set(
	[
		"/assets/images/Media_extracted/interface/loading/gauge_loading.png",
		"/assets/images/Media_extracted/interface/loading/loading_form.png",
		"/assets/images/Media_extracted/interface/loading/nowloading.png",
		"/assets/images/Media_extracted/interface/outer/logo-big.png",
		...Array.from(
			{ length: 10 },
			( _, index ) =>
				`/assets/images/Media_extracted/interface/loading/start_loading_${
					String( index + 1 ).padStart( 2, "0" )
				}.png`
		),
		// buildWebAssetManifest reads this version record after the packed loose
		// tree has been removed.
		"/assets/launcher/manifest.json"
	].map( ( value ) => value.toLowerCase() )
);
const SIDECAR_SUFFIXES = [ ".br", ".zst", ".gz" ];

await withGeneratedAssetsLock( "compact browser asset release", async () => {
	const before = await measureAssetFootprint();
	const manifest = JSON.parse( await readFile( packManifestPath, "utf8" ) );
	const packs = manifest.groups.flatMap( ( group ) => group.packs );

	if ( manifest.format !== "sro-asset-pack-index" || manifest.version !== 1 || packs.length === 0 ) {
		throw new Error( `Cannot compact an invalid or empty asset-pack manifest: ${packManifestPath}` );
	}
	if ( typeof zlib.zstdDecompressSync !== "function" ) {
		throw new Error( "Compact asset validation requires Node.js zstd decompression support." );
	}

	// The build writes identity packs only; the zstd-19 copies this release
	// keeps instead are made here, once, for every pack that lacks one.
	await ensurePackZstdCopies( manifest, packs );

	// Iteration uses fast lossless encodings. Shipping is the deliberate cold
	// path: regenerate the retained pack index at maximum compression before
	// choosing the smallest negotiated representation below.
	await refreshPrecompressedSidecars( [ packManifestPath ] );

	let compressedPackBytes = 0;
	let identityPackBytes = 0;
	for ( const pack of packs ) {
		const identityPath = containedPublicFile( publicRoot, pack.path );
		const zstdPath = containedPublicFile( publicRoot, pack.zstdPath );
		const zstdStats = await stat( zstdPath );
		if ( !zstdStats.isFile() || zstdStats.size !== pack.zstdBytes ) {
			throw new Error( `Pack sidecar size mismatch: ${pack.zstdPath}` );
		}
		const compressed = await readFile( zstdPath );
		const identity = zlib.zstdDecompressSync( compressed );
		const digest = createHash( "sha256" ).update( identity ).digest( "hex" );
		if ( identity.byteLength !== pack.bytes || digest !== pack.sha256.toLowerCase() ) {
			throw new Error( `Pack sidecar does not reproduce the manifest identity bytes: ${pack.zstdPath}` );
		}
		assertInsideRoot( publicAssetsRoot, identityPath, `pack identity ${pack.path}` );
		compressedPackBytes += compressed.byteLength;
		identityPackBytes += identity.byteLength;
	}

	const packedLogicalPaths = new Set();
	for ( const asset of manifest.assets ) {
		const normalized = normalizePublicAssetPath( asset.path ).toLowerCase();
		packedLogicalPaths.add( normalized );
		if ( normalized.endsWith( ".json.gz" ) ) {
			packedLogicalPaths.add( normalized.slice( 0, -".gz".length ) );
		}
	}

	const allPublicFiles = await listFiles( publicAssetsRoot );
	const keepPaths = new Set();
	keepPaths.add( "/assets/packs/manifest.json" );
	for ( const pack of packs ) {
		if ( typeof pack.zstdPath !== "string" ) {
			throw new Error( `Pack has no zstd representation: ${pack.path}` );
		}
		keepPaths.add( normalizePublicAssetPath( pack.zstdPath ).toLowerCase() );
	}

	for ( const filePath of allPublicFiles ) {
		const publicPath = toPublicPath( filePath );
		const lowerPath = publicPath.toLowerCase();
		if ( lowerPath.startsWith( "/assets/packs/" ) || isSidecarPath( lowerPath ) ) {
			continue;
		}
		if ( !packedLogicalPaths.has( lowerPath ) || BOOTSTRAP_PUBLIC_PATHS.has( lowerPath ) ) {
			keepPaths.add( lowerPath );
		}
	}

	for ( const bootstrapPath of BOOTSTRAP_PUBLIC_PATHS ) {
		await requireFile( containedPublicFile( publicRoot, bootstrapPath ), `bootstrap asset ${bootstrapPath}` );
		keepPaths.add( bootstrapPath );
	}

	// Retain only the smallest fresh negotiated representation for the few JSON
	// files that remain loose (principally the two manifests).
	for ( const publicPath of [ ...keepPaths ] ) {
		const basePath = containedPublicFile( publicRoot, publicPath );
		const baseStats = await stat( basePath ).catch( () => undefined );
		if ( !baseStats?.isFile() ) {
			continue;
		}
		const candidates = [];
		for ( const suffix of SIDECAR_SUFFIXES ) {
			const candidatePath = `${basePath}${suffix}`;
			const candidateStats = await stat( candidatePath ).catch( () => undefined );
			if ( candidateStats?.isFile() && candidateStats.mtimeMs >= baseStats.mtimeMs ) {
				candidates.push( { suffix, bytes: candidateStats.size } );
			}
		}
		candidates.sort( ( left, right ) => left.bytes - right.bytes );
		if ( candidates[0] ) {
			keepPaths.add( `${publicPath}${candidates[0].suffix}`.toLowerCase() );
		}
	}

	const removePaths = allPublicFiles.filter( ( filePath ) =>
		!keepPaths.has( toPublicPath( filePath ).toLowerCase() )
	);
	for ( const filePath of removePaths ) {
		assertInsideRoot( publicAssetsRoot, filePath, "compact asset removal" );
		await rm( filePath, { force: true } );
	}

	const webManifest = await buildWebAssetManifest();
	const webManifestPath = path.join( publicAssetsRoot, "manifest.json" );
	const webManifestBytes = await readFile( webManifestPath );
	const webManifestBrotliPath = `${webManifestPath}.br`;
	await writeFile( webManifestBrotliPath, compressBrotliSync( webManifestBytes ) );
	for ( const suffix of [ ".gz", ".zst" ] ) {
		await rm( `${webManifestPath}${suffix}`, { force: true } );
	}

	let droppedGeneratedCache = { files: 0, bytes: 0 };
	if ( dropGeneratedCache ) {
		await requireRegenerationSources();
		droppedGeneratedCache = await measureTree( generatedAssetsRoot );
		// The staging cache belongs to the generated root, which a worktree may
		// share from another checkout (SRO_GENERATED_ROOT).
		assertInsideRoot( generatedRoot, generatedAssetsRoot, "generated image staging cache" );
		await rm( generatedAssetsRoot, { recursive: true, force: true } );
	}

	const serverArchive = await validateServerGameDataArchive( serverGameDataArchivePath );
	const droppedServerProjection = await measureTree( serverGameDataRoot );
	assertGeneratedPath( serverGameDataRoot, "loose server game-data projection" );
	await rm( serverGameDataRoot, { recursive: true, force: true } );
	assertGeneratedPath( serverGameDataCacheRoot, "server game-data extraction cache" );
	await rm( serverGameDataCacheRoot, { recursive: true, force: true } );
	const after = await measureAssetFootprint();
	const publicFiles = (await listFiles( publicAssetsRoot )).map( toPublicPath ).sort();
	const state = {
		format: "sro-compact-assets",
		version: 2,
		generatedAt: new Date().toISOString(),
		packCount: packs.length,
		assetCount: manifest.assets.length,
		identityPackBytes,
		compressedPackBytes,
		before,
		after,
		removedPublicFiles: removePaths.length,
		droppedGeneratedCache,
		serverArchive: {
			path: path.relative( rebuildRoot, serverGameDataArchivePath ).replaceAll( "\\", "/" ),
			bytes: (await stat( serverGameDataArchivePath )).size,
			fileCount: serverArchive.fileCount,
			expandedBytes: serverArchive.expandedBytes
		},
		droppedServerProjection,
		webManifestFiles: webManifest.files.length,
		publicFiles
	};
	await mkdir( path.dirname( compactStatePath ), { recursive: true } );
	await writeFile( compactStatePath, `${JSON.stringify( state, null, 2 )}\n`, "utf8" );

	console.log(
		`Compact assets OK: ${packs.length} zstd-only packs preserve ${formatBytes( identityPackBytes )} ` +
			`of identity data in ${formatBytes( compressedPackBytes )}.`
	);
	console.log(
		`Asset footprint: ${formatBytes( before.bytes )} -> ${formatBytes( after.bytes )} ` +
			`(${((1 - after.bytes / before.bytes) * 100).toFixed( 1 )}% removed).`
	);
	console.log(
		`Removed ${removePaths.length} duplicate public files` +
			(dropGeneratedCache ?
				` and ${droppedGeneratedCache.files} reproducible staging files (${
					formatBytes( droppedGeneratedCache.bytes )
				}).` :
				".")
	);
	console.log(
		`Server projection: ${formatBytes( droppedServerProjection.bytes )} loose -> ` +
			`${formatBytes( (await stat( serverGameDataArchivePath )).size )} lossless release archive.`
	);
} );

/*
================
requireRegenerationSources
================
*/
async function requireRegenerationSources() {
	const required = [
		path.join( gameRoot, "Data.pk2" ),
		path.join( gameRoot, "Map.pk2" ),
		path.join( gameRoot, "Media.pk2" ),
		dataExtractedRoot,
		mapExtractedRoot,
		mediaExtractedRoot
	];
	for ( const sourcePath of required ) {
		const sourceStats = await stat( sourcePath ).catch( () => undefined );
		if ( !sourceStats ) {
			throw new Error(
				`Refusing to drop the generated staging cache; regeneration source is missing: ${sourcePath}`
			);
		}
	}
}

/*
================
requireFile
================
*/
async function requireFile( filePath, label ) {
	const fileStats = await stat( filePath ).catch( () => undefined );
	if ( !fileStats?.isFile() ) {
		throw new Error( `Missing ${label}: ${filePath}` );
	}
}

/*
================
measureAssetFootprint
================
*/
async function measureAssetFootprint() {
	const [published, generated, serverGameData] = await Promise.all( [
		measureTree( publicAssetsRoot ),
		measureTree( generatedAssetsRoot ),
		measureTree( generatedRoot )
	] );
	return {
		files: published.files + generated.files + serverGameData.files,
		bytes: published.bytes + generated.bytes + serverGameData.bytes,
		published,
		generated,
		serverGameData
	};
}

/*
================
measureTree
================
*/
async function measureTree( root ) {
	const files = await listFiles( root ).catch( () => [] );
	let bytes = 0;
	for ( const filePath of files ) {
		bytes += (await stat( filePath )).size;
	}
	return { files: files.length, bytes };
}

/*
================
toPublicPath
================
*/
function toPublicPath( filePath ) {
	assertInsideRoot( publicRoot, filePath, "public asset" );
	return `/${path.relative( publicRoot, filePath ).split( path.sep ).join( "/" )}`;
}

/*
================
isSidecarPath
================
*/
function isSidecarPath( publicPath ) {
	return SIDECAR_SUFFIXES.some( ( suffix ) => publicPath.endsWith( suffix ) );
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
assertGeneratedPath

The server game-data projection may live in another checkout's Go module
(SRO_SERVER_GAME_DATA_ROOT), so its removal is bounded by the one thing
every location shares: it sits below a .generated folder.
================
*/
function assertGeneratedPath( target, label ) {
	const parts = path.resolve( target ).split( path.sep );
	const marker = parts.lastIndexOf( ".generated" );
	if ( marker === -1 || marker === parts.length - 1 ) {
		throw new Error( `${label} must stay below a .generated folder, got ${target}` );
	}
}

/*
================
ensurePackZstdCopies

Writes the zstd-19 copy of every pack that has none (or a wrong-size one),
records it in the pack index and publishes the index. Packs compress in
parallel under the build's SRO_BUILD_JOBS budget.
================
*/
async function ensurePackZstdCopies( manifest, packs ) {
	const slots = createLimiter( buildJobs() );
	let written = 0;
	await Promise.all( packs.map( pack =>
		slots( async () => {
			const zstdPublicPath = `${pack.path}.zst`;
			const zstdPath = containedPublicFile( publicRoot, zstdPublicPath );
			const existing = await stat( zstdPath ).catch( () => undefined );
			if ( pack.zstdPath === zstdPublicPath && existing?.isFile() && existing.size === pack.zstdBytes ) return;
			const compressed = await compressAssetPackZstd(
				await readFile( containedPublicFile( publicRoot, pack.path ) )
			);
			await writeFile( zstdPath, compressed );
			Object.assign( pack, {
				zstdPath: zstdPublicPath,
				zstdBytes: compressed.length,
				zstdLevel: ASSET_PACK_ZSTD_LEVEL,
				zstdWindowLog: ASSET_PACK_ZSTD_WINDOW_LOG
			} );
			written++;
		} )
	) );
	if ( written === 0 ) return;
	await publishAssetPackManifest( publicRoot, packManifestPath, Buffer.from( JSON.stringify( manifest ) ) );
	console.log( `[compact] wrote ${written} zstd pack copies` );
}

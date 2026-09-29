/*
===========================================================================

assetPacks.mjs - build the asset packs and publish their index

Groups the public assets into packs of about the target size, reuses any
pack whose members are unchanged, compresses each with zstd, and publishes
the pack index (assets/packs/manifest.json) atomically. The same builder
writes the incremental slot packs, each with its own local index.

After the main index is published, every file under the packs root that it
no longer uses is soft-archived through the shared pack garbage rule
(assetPackGarbage.mjs), so superseded slots and transport files do not pile
up between full builds.

===========================================================================
*/

import { ASSET_SCHEMA } from "./assetSchema.mjs";
import { prepareAssetDelivery } from "./assetDelivery.mjs";
import { validatePackedFontAtlases } from "./assetPackPublication.mjs";
import { validateAssetPackIndex } from "./assetPackIndexValidation.mjs";
export { validateAssetPackIndex };
import { createHash } from "node:crypto";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
	normalizePublicAssetPath as normalizePublicPath,
	toPublicPath as toPublicAssetPath
} from "./shared/assetPaths.mjs";
import { publishBytesAtomically } from "./shared/atomicPublish.mjs";
import { mapWithConcurrency } from "./shared/asyncUtils.mjs";
import { compressZstd, DEFAULT_ZSTD_LEVEL, DEFAULT_ZSTD_WINDOW_LOG } from "./shared/compressionUtils.mjs";
import { openFileHashCache } from "./shared/fileHashCache.mjs";
import { listFiles } from "./shared/fsUtils.mjs";
import { sha256Hex } from "./shared/hash.mjs";
import { archiveGeneratedArtifact } from "./artifacts/generatedArtifactArchive.mjs";
import { livePackFiles } from "./assetPackLiveSet.mjs";
import { collectPackGarbage } from "./assetPackGarbage.mjs";
import { readJsonOrUndefined } from "./shared/jsonOut.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = path.join( rebuildRoot, ".generated", "client-public" );
const packsRoot = path.join( publicRoot, "assets", "packs" );

export const DEFAULT_ASSET_PACK_TARGET_BYTES = 50 * 1024 * 1024;
export const ASSET_PACK_MAGIC = "SROPACK1";
export const ASSET_PACK_ZSTD_LEVEL = DEFAULT_ZSTD_LEVEL;
export const ASSET_PACK_ZSTD_WINDOW_LOG = DEFAULT_ZSTD_WINDOW_LOG;

const FILE_HASH_CONCURRENCY = 8;
/** stat() sweeps are cheap syscalls; high fan-out matters on Windows where each is slow. */
const FILE_STAT_CONCURRENCY = 64;
/** Packs building in parallel; each holds one pack buffer (+ zstd output) in memory. */
const PACK_BUILD_CONCURRENCY = 3;

/**
 * zstd fields are optional: reused packs come from a previous manifest, and older
 * manifests may predate the sidecar fields (indexReusablePacks filters on them anyway).
 * @typedef {{ path: string, bytes: number, sha256: string, assetCount: number, zstdPath?: string, zstdBytes?: number, zstdLevel?: number, zstdWindowLog?: number }} AssetPackEntry
 * @typedef {{ path: string, packPath: string, offset: number, length: number, mime: string, sha256: string, group: string }} AssetPackAssetRow
 * @typedef {{ name: string, load: string, targetBytes: number, assetCount: number, totalBytes: number, packs: AssetPackEntry[] }} AssetPackGroupIndex
 */

/*
================
listPublicAssetFiles
================
*/
export async function listPublicAssetFiles( options = {} ) {
	const root = path.resolve( options.publicRoot ?? publicRoot );
	const roots = options.roots ?? [ "/assets" ];
	const extensions = new Set( (options.extensions ?? []).map( ( extension ) => normalizeExtension( extension ) ) );
	const exclude = new Set(
		(options.exclude ?? []).map( ( publicPath ) => normalizePublicPath( publicPath ).toLowerCase() )
	);
	const files = [];

	for ( const rootPublicPath of roots ) {
		const absoluteRoot = resolvePublicAssetFile( root, rootPublicPath );
		const rootStat = await stat( absoluteRoot );
		if ( rootStat.isFile() ) {
			const publicPath = normalizePublicPath( rootPublicPath );
			if ( matchesPublicAsset( publicPath, extensions, exclude ) ) {
				files.push( publicPath );
			}
			continue;
		}

		for ( const absolutePath of await listFiles( absoluteRoot ) ) {
			const publicPath = toPublicAssetPath( absolutePath, root );
			if ( matchesPublicAsset( publicPath, extensions, exclude ) ) {
				files.push( publicPath );
			}
		}
	}

	return uniquePublicPaths( files ).sort( ( left, right ) => left.localeCompare( right ) );
}

/*
================
buildAssetPacks
================
*/
export async function buildAssetPacks( options = {} ) {
	const root = path.resolve( options.publicRoot ?? publicRoot );
	const outputRoot = path.resolve( options.outputRoot ?? packsRoot );
	const indexPath = path.join( outputRoot, "manifest.json" );
	const defaultTargetBytes = options.targetBytes ?? DEFAULT_ASSET_PACK_TARGET_BYTES;
	const groups = options.groups ?? [];

	assertInside( root, outputRoot, "asset pack output root" );
	await mkdir( outputRoot, { recursive: true } );

	// Incremental reuse: a pack is fully determined by its ordered member contents plus the
	// format/zstd constants, so packs from the previous manifest whose members are unchanged
	// are reused as-is - no re-read, no rewrite, no zstd-19 re-compression. (The old behavior
	// wiped the directory and re-compressed every pack on every run, which dominated the
	// build; it also left the directory manifest-less if the run was interrupted.)
	const reuseEnabled = process.env.SRO_ASSET_PACKS_NO_CACHE !== "1";
	const { reusablePacks, previousIndex } = reuseEnabled ?
		await indexReusablePacks( indexPath ) :
		{ reusablePacks: new Map(), previousIndex: undefined };
	// hashCachePath must be injectable: tests build throwaway trees in temp dirs, and
	// without it they write fixture hashes into the production .state cache (and, with
	// SRO_BUILD_HASH_CACHE=0, used to truncate it - see fileHashCache.save()'s guard).
	const hashCache = await openFileHashCache( options.hashCachePath );
	const counters = { built: 0, reused: 0 };

	/** @type {{ format: string, version: number, assetSchema: number, generatedAt: string, targetPackBytes: number, groups: AssetPackGroupIndex[], assets: AssetPackAssetRow[] }} */
	const index = {
		format: "sro-asset-pack-index",
		version: 1,
		// The format of the data this index serves (assetSchema.mjs).
		assetSchema: ASSET_SCHEMA,
		generatedAt: new Date().toISOString(),
		targetPackBytes: defaultTargetBytes,
		groups: [],
		assets: []
	};

	for ( const group of groups ) {
		const groupResult = await buildAssetPackGroup( {
			publicRoot: root,
			outputRoot,
			group,
			targetBytes: group.targetBytes ?? defaultTargetBytes,
			reusablePacks,
			hashCache,
			counters
		} );
		index.groups.push( groupResult.groupIndex );
		index.assets.push( ...groupResult.assets );
	}

	index.groups.sort( ( left, right ) => left.name.localeCompare( right.name ) );
	index.assets.sort( ( left, right ) => left.path.localeCompare( right.path ) );
	validateAssetPackIndex( index );
	await prepareAssetDelivery( index, root, path.join( outputRoot, "delivery.json" ) );
	// Sparse builders publish partial indexes; the final merged publication
	// requires every descriptor dependency before it replaces the live index.
	await validatePackedFontAtlases( index, root, {
		partial: path.resolve( outputRoot ) !== path.join( root, "assets", "packs" )
	} );

	// Reuse the previous timestamp when nothing else changed: the written bytes then stay
	// identical, the write is skipped, and every downstream mtime cache (JSON sidecars, web
	// manifest hashing) holds across no-op rebuilds. Written minified - that is the form the
	// JSON optimizer would rewrite it to anyway.
	let skipIndexWrite = false;
	if ( previousIndex?.generatedAt ) {
		const candidate = Buffer.from( JSON.stringify( { ...index, generatedAt: previousIndex.generatedAt } ), "utf8" );
		const existing = await readFile( indexPath ).catch( () => undefined );
		if ( existing && existing.equals( candidate ) ) {
			index.generatedAt = previousIndex.generatedAt;
			skipIndexWrite = true;
		}
	}

	if ( !skipIndexWrite ) {
		await publishIndex( indexPath, Buffer.from( JSON.stringify( index ), "utf8" ) );
	}
	await archiveStaleOutputs( root, outputRoot, index, indexPath );
	// The main index is the delivery authority: retire every file under the
	// packs root it no longer uses, including superseded slots and transport
	// files outside this build's own output root.
	if ( path.resolve( outputRoot ) === path.resolve( root, "assets", "packs" ) ) {
		await collectPackGarbage( { publicRoot: root, apply: true, index } );
	}
	await hashCache.save();

	return {
		publicPath: toPublicAssetPath( indexPath, root ),
		outputPath: indexPath,
		groupCount: index.groups.length,
		packCount: index.groups.reduce( ( sum, group ) => sum + group.packs.length, 0 ),
		assetCount: index.assets.length,
		totalBytes: index.assets.reduce( ( sum, asset ) => sum + asset.length, 0 ),
		zstdSidecarCount: index.groups.reduce(
			( sum, group ) => sum + group.packs.filter( ( pack ) => typeof pack.zstdBytes === "number" ).length,
			0
		),
		zstdBytes: index.groups.reduce(
			( sum, group ) => sum + group.packs.reduce( ( packSum, pack ) => packSum + (pack.zstdBytes ?? 0), 0 ),
			0
		),
		builtPackCount: counters.built,
		reusedPackCount: counters.reused,
		groups: index.groups,
		assets: index.assets
	};
}

/*
================
buildAssetPackGroup
================
*/
async function buildAssetPackGroup(
	{ publicRoot, outputRoot, group, targetBytes, reusablePacks, hashCache, counters }
) {
	const name = normalizeGroupName( group.name );
	const publicPaths = uniquePublicPaths( group.files ?? [] );
	const files = await mapWithConcurrency( publicPaths, FILE_STAT_CONCURRENCY, async ( publicPath ) => {
		const absolutePath = resolvePublicAssetFile( publicRoot, publicPath );
		const fileStat = await stat( absolutePath );
		if ( !fileStat.isFile() ) {
			throw new Error( `Asset pack input is not a file: ${publicPath}` );
		}
		return {
			publicPath,
			absolutePath,
			stat: fileStat,
			bytes: fileStat.size,
			mime: mimeTypeForPath( publicPath )
		};
	} );

	files.sort( ( left, right ) => left.publicPath.localeCompare( right.publicPath ) );
	await mapWithConcurrency( files, FILE_HASH_CONCURRENCY, async ( file ) => {
		file.sha256 = await hashCache.hashFile( file.absolutePath, file.stat );
	} );

	const chunks = chunkFilesByTargetBytes( files, targetBytes );
	/** @type {AssetPackGroupIndex} */
	const groupIndex = {
		name,
		load: group.load ?? "manual",
		targetBytes,
		assetCount: files.length,
		totalBytes: files.reduce( ( sum, file ) => sum + file.bytes, 0 ),
		packs: []
	};
	/** @type {AssetPackAssetRow[]} */
	const assets = [];

	const results = await mapWithConcurrency(
		chunks.map( ( chunk, chunkIndex ) => ({ chunk, chunkIndex }) ),
		PACK_BUILD_CONCURRENCY,
		( { chunk, chunkIndex } ) =>
			buildOrReusePack( { name, chunk, chunkIndex, outputRoot, publicRoot, reusablePacks, hashCache, counters } )
	);

	for ( const result of results ) {
		groupIndex.packs.push( result.packEntry );
		assets.push( ...result.assetRows );
	}

	return { groupIndex, assets };
}

/*
================
packContentKey

Content key over a pack's ordered members; identical keys mean byte-identical packs.
================
*/
function packContentKey( groupName, members ) {
	const hash = createHash( "sha256" );
	hash.update(
		JSON.stringify( {
			format: ASSET_PACK_MAGIC,
			version: 1,
			zstd: [ ASSET_PACK_ZSTD_LEVEL, ASSET_PACK_ZSTD_WINDOW_LOG ],
			group: groupName,
			files: members.map( ( member ) => [ member.publicPath, member.sha256, member.length, member.mime ] )
		} )
	);
	return hash.digest( "hex" );
}

/*
================
indexReusablePacks

Map packContentKey -> previous manifest pack (+ its asset rows) for unchanged-pack reuse.
================
*/
async function indexReusablePacks( indexPath ) {
	const none = { reusablePacks: new Map(), previousIndex: undefined };
	const previous = await readJsonOrUndefined( indexPath );
	if (
		previous?.format !== "sro-asset-pack-index" ||
		previous.version !== 1 ||
		!Array.isArray( previous.groups ) ||
		!Array.isArray( previous.assets )
	) {
		return none;
	}

	const packsByPath = new Map();
	for ( const group of previous.groups ) {
		for ( const pack of group.packs ?? [] ) {
			if ( typeof pack?.path === "string" && typeof pack.zstdBytes === "number" ) {
				packsByPath.set( pack.path, { groupName: group.name, pack } );
			}
		}
	}

	const membersByPackPath = new Map();
	for ( const asset of previous.assets ) {
		const members = membersByPackPath.get( asset.packPath ) ?? [];
		members.push( asset );
		membersByPackPath.set( asset.packPath, members );
	}

	const reusable = new Map();
	for ( const [packPath, members] of membersByPackPath ) {
		const owner = packsByPath.get( packPath );
		if ( !owner ) continue;
		members.sort( ( left, right ) => left.offset - right.offset );
		const key = packContentKey(
			owner.groupName,
			members.map( ( member ) => ({
				publicPath: member.path,
				sha256: member.sha256,
				length: member.length,
				mime: member.mime
			}) )
		);
		reusable.set( key, { groupName: owner.groupName, pack: owner.pack, members } );
	}
	return { reusablePacks: reusable, previousIndex: previous };
}

/*
================
packOutputsIntact
================
*/
async function packOutputsIntact( publicRoot, pack ) {
	try {
		const binPath = resolvePublicAssetFile( publicRoot, pack.path );
		const zstdPath = resolvePublicAssetFile( publicRoot, pack.zstdPath ?? `${pack.path}.zst` );
		const [binStat, zstdStat] = await Promise.all( [
			stat( binPath ).catch( () => undefined ),
			stat( zstdPath ).catch( () => undefined )
		] );
		const identityIntact = binStat?.isFile() && binStat.size === pack.bytes;
		const zstdIntact = zstdStat?.isFile() && zstdStat.size === pack.zstdBytes;

		// Compact releases intentionally retain only the zstd sidecar. Reuse an
		// unchanged pack without inflating and recompressing its deleted identity.
		return Boolean( zstdIntact && (identityIntact || !binStat) );
	} catch {
		return false;
	}
}

/*
================
buildOrReusePack
================
*/
async function buildOrReusePack(
	{ name, chunk, chunkIndex, outputRoot, publicRoot, reusablePacks, hashCache, counters }
) {
	const plannedKey = packContentKey(
		name,
		chunk.map( ( file ) => ({
			publicPath: file.publicPath,
			sha256: file.sha256,
			length: file.bytes,
			mime: file.mime
		}) )
	);
	const reusable = reusablePacks.get( plannedKey );
	if ( reusable && reusable.groupName === name && (await packOutputsIntact( publicRoot, reusable.pack )) ) {
		counters.reused += 1;
		return {
			packEntry: { ...reusable.pack },
			assetRows: reusable.members.map( ( member ) => ({ ...member }) )
		};
	}

	const payloadParts = [];
	const entries = [];
	let offset = 0;
	for ( const file of chunk ) {
		const bytes = await readFile( file.absolutePath );
		const sha256 = hashCache.noteFileBytes( file.absolutePath, file.stat, bytes );
		entries.push( {
			path: file.publicPath,
			offset,
			length: bytes.length,
			mime: file.mime,
			sha256
		} );
		payloadParts.push( bytes );
		offset += bytes.length;
	}

	const headerJson = Buffer.from(
		JSON.stringify( {
			format: "sro-asset-pack",
			version: 1,
			files: entries
		} ),
		"utf8"
	);
	const header = Buffer.alloc( 12 );
	header.write( ASSET_PACK_MAGIC, 0, "ascii" );
	header.writeUInt32LE( headerJson.length, 8 );
	const buffer = Buffer.concat( [ header, headerJson, ...payloadParts ] );

	const packHash = sha256Hex( buffer );
	const packFileName = `${name}-${String( chunkIndex + 1 ).padStart( 3, "0" )}-${packHash.slice( 0, 12 )}.bin`;
	const packPath = path.join( outputRoot, packFileName );
	const packPublicPath = toPublicAssetPath( packPath, publicRoot );

	// zstd runs on the libuv threadpool; overlapping it with the pack write keeps the
	// (rare, changed-pack-only) compression off the critical path as much as possible.
	const [zstdSidecar] = await Promise.all( [ compressAssetPackZstd( buffer ), writeFile( packPath, buffer ) ] );
	await writeFile( `${packPath}.zst`, zstdSidecar );
	counters.built += 1;

	const packEntry = {
		path: packPublicPath,
		bytes: buffer.length,
		sha256: packHash,
		assetCount: entries.length,
		zstdPath: `${packPublicPath}.zst`,
		zstdBytes: zstdSidecar.length,
		zstdLevel: ASSET_PACK_ZSTD_LEVEL,
		zstdWindowLog: ASSET_PACK_ZSTD_WINDOW_LOG
	};
	const assetRows = entries.map( ( entry ) => ({
		path: entry.path,
		packPath: packPublicPath,
		offset: entry.offset,
		length: entry.length,
		mime: entry.mime,
		sha256: entry.sha256,
		group: name
	}) );
	return { packEntry, assetRows };
}

/*
================
publishIndex

Publish the pack index: write-then-rename, so the previous manifest stays live until the
rename lands and an interrupted run cannot leave packs without an index.

On Windows, rename() over an existing file fails with EPERM while ANY other process holds
the destination open without delete sharing - and manifest.json always has readers here
(dev servers, the Go gateway, the pack integrity check, other agents' test runs), plus
antivirus briefly pins the freshly written .tmp. Losing a multi-minute build to a read
lock on its very last step is the worst outcome, so this retries with backoff and, if the
file stays pinned, falls back to writing the destination in place. The fallback gives up
crash-atomicity for that one write; a torn read by a concurrent reader was already
possible with rename-over on the readers that hit this path.
================
*/
async function publishIndex( indexPath, bytes ) {
	await publishBytesAtomically( indexPath, bytes, { logLabel: "asset-packs" } );
}

/*
================
archiveStaleOutputs

Soft-archive files under the packs root that the freshly published manifest does not reference.
================
*/
async function archiveStaleOutputs( publicRoot, outputRoot, index, indexPath ) {
	// The live set includes the index's precompressed sidecars; deleting them
	// forced a pointless brotli/gzip/zstd recompression of the manifest every build.
	const keep = livePackFiles( publicRoot, indexPath, index );

	for ( const filename of await listFiles( outputRoot ) ) {
		if ( keep.has( path.resolve( filename ).toLowerCase() ) ) continue;
		const relative = path.relative( outputRoot, filename );
		const directOutput = !relative.includes( path.sep );
		const nestedPackArtifact = /\.bin(?:\.zst)?$/iu.test( filename );
		if ( !directOutput && !nestedPackArtifact ) continue;
		await archiveGeneratedArtifact( filename, {
			scopeRoot: publicRoot,
			reason: "superseded-asset-pack-output"
		} );
	}
}

/*
================
compressAssetPackZstd
================
*/
function compressAssetPackZstd( bytes ) {
	return compressZstd( bytes, {
		level: ASSET_PACK_ZSTD_LEVEL,
		windowLog: ASSET_PACK_ZSTD_WINDOW_LOG
	} );
}

/*
================
chunkFilesByTargetBytes
================
*/
function chunkFilesByTargetBytes( files, targetBytes ) {
	const chunks = [];
	let current = [];
	let currentBytes = 0;

	for ( const file of files ) {
		if ( current.length > 0 && currentBytes + file.bytes > targetBytes ) {
			chunks.push( current );
			current = [];
			currentBytes = 0;
		}

		current.push( file );
		currentBytes += file.bytes;
	}

	if ( current.length > 0 ) {
		chunks.push( current );
	}

	return chunks;
}

/*
================
resolvePublicAssetFile
================
*/
function resolvePublicAssetFile( root, publicPath ) {
	const relative = publicPath.replace( /^\/+/, "" );
	const absolutePath = path.resolve( root, relative );
	assertInside( root, absolutePath, `asset ${publicPath}` );
	return absolutePath;
}

/*
================
uniquePublicPaths
================
*/
function uniquePublicPaths( paths ) {
	const seen = new Set();
	const output = [];

	for ( const pathLike of paths ) {
		const publicPath = normalizePublicPath( pathLike );
		if ( !seen.has( publicPath.toLowerCase() ) ) {
			seen.add( publicPath.toLowerCase() );
			output.push( publicPath );
		}
	}

	return output;
}

/*
================
normalizeGroupName
================
*/
function normalizeGroupName( name ) {
	const normalized = String( name ?? "" )
		.trim()
		.toLowerCase()
		.replace( /[^a-z0-9]+/g, "-" )
		.replace( /^-+|-+$/g, "" );
	if ( !normalized ) {
		throw new Error( "Asset pack group is missing a usable name." );
	}
	return normalized;
}

/*
================
assertInside
================
*/
function assertInside( root, target, label ) {
	const relative = path.relative( path.resolve( root ), path.resolve( target ) );
	if ( relative === "" || (!relative.startsWith( ".." ) && !path.isAbsolute( relative )) ) {
		return;
	}

	throw new Error( `${label} must stay inside ${root}, got ${target}` );
}

/*
================
mimeTypeForPath
================
*/
function mimeTypeForPath( publicPath ) {
	const extension = path.extname( publicPath ).toLowerCase();
	switch ( extension ) {
		case ".png":
			return "image/png";
		case ".jpg":
		case ".jpeg":
			return "image/jpeg";
		case ".webp":
			return "image/webp";
		case ".dds":
			return "image/vnd-ms.dds";
		case ".json":
			return "application/json";
		case ".glb":
			return "model/gltf-binary";
		case ".mp3":
			return "audio/mpeg";
		case ".wav":
			return "audio/wav";
		case ".ttf":
			return "font/ttf";
		case ".cur":
			return "image/x-icon";
		default:
			return "application/octet-stream";
	}
}

/*
================
matchesPublicAsset
================
*/
function matchesPublicAsset( publicPath, extensions, exclude ) {
	if ( publicPath.toLowerCase().startsWith( "/assets/packs/" ) ) {
		return false;
	}

	return (
		(extensions.size === 0 || extensions.has( normalizeExtension( path.extname( publicPath ) ) )) &&
		!exclude.has( publicPath.toLowerCase() )
	);
}

/*
================
normalizeExtension
================
*/
function normalizeExtension( extension ) {
	return extension.startsWith( "." ) ? extension.toLowerCase() : `.${extension.toLowerCase()}`;
}

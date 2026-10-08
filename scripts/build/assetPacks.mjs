/*
===========================================================================

assetPacks.mjs - build the asset packs and publish their index

Groups the public assets into packs of about the target size, reuses any
pack whose members are unchanged, and publishes
the pack index (assets/packs/manifest.json) atomically. The same builder
writes the incremental slot packs, each with its own local index.

After the main index is published, every file under the packs root that it
no longer uses is soft-archived through the shared pack garbage rule
(assetPackGarbage.mjs), so superseded slots and transport files do not pile
up between full builds.

===========================================================================
*/

import { assertInsideRoot, containedPublicFile } from "./shared/assetPaths.mjs";
import { buildJobs } from "./shared/buildParallelism.mjs";
import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
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
import { createLimiter, mapWithConcurrency } from "./shared/asyncUtils.mjs";
import { compressZstd, DEFAULT_ZSTD_LEVEL, DEFAULT_ZSTD_WINDOW_LOG } from "./shared/compressionUtils.mjs";
import { openFileHashCache } from "./shared/fileHashCache.mjs";
import { listFiles } from "./shared/fsUtils.mjs";
import { sha256Hex } from "./shared/hash.mjs";
import { archiveGeneratedArtifact } from "./artifacts/generatedArtifactArchive.mjs";
import { livePackFiles } from "./assetPackLiveSet.mjs";
import { collectPackGarbage } from "./assetPackGarbage.mjs";
import { readJsonOrUndefined } from "./shared/jsonOut.mjs";
import { baselinePacksOf, planPackLayout, packSlotOf } from "./assetPackLayout.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const packsRoot = path.join( publicRoot, "assets", "packs" );

export const DEFAULT_ASSET_PACK_TARGET_BYTES = 50 * 1024 * 1024;
export const ASSET_PACK_MAGIC = "SROPACK1";
export const ASSET_PACK_ZSTD_LEVEL = DEFAULT_ZSTD_LEVEL;
export const ASSET_PACK_ZSTD_WINDOW_LOG = DEFAULT_ZSTD_WINDOW_LOG;

const FILE_HASH_CONCURRENCY = 8;
/** stat() sweeps are cheap syscalls; high fan-out matters on Windows where each is slow. */
const FILE_STAT_CONCURRENCY = 64;

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
		const absoluteRoot = containedPublicFile( root, rootPublicPath );
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

	assertInsideRoot( root, outputRoot, "asset pack output root" );
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
	const counters = { built: 0, reused: 0, kept: 0, fresh: 0 };
	const baselineIndex = await layoutBaseline( options, root, outputRoot, indexPath );

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

	// Every group builds at once under one pack budget: a group of three packs
	// must not leave the other cores idle while it compresses. Results keep the
	// caller's group order, so the index is the same whatever finishes first.
	const packSlots = createLimiter( buildJobs() );
	const groupResults = await Promise.all( groups.map( group =>
		buildAssetPackGroup( {
			publicRoot: root,
			outputRoot,
			group,
			targetBytes: group.targetBytes ?? defaultTargetBytes,
			reusablePacks,
			hashCache,
			counters,
			baselineIndex,
			packSlots
		} )
	) );
	for ( const groupResult of groupResults ) {
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
		keptPackCount: counters.kept,
		freshPackCount: counters.fresh,
		groups: index.groups,
		assets: index.assets
	};
}

/*
================
layoutBaseline

The index the pack layout stays stable against (assetPackLayout.mjs): for
the main index, options.baselineIndexPath or SRO_ASSET_PACK_BASELINE (a
release passes the live publication, so unchanged packs keep the URLs
players have cached); otherwise the previous local index. A named baseline
that cannot be read stops the build: silently repacking everything would
re-ship the whole data set.
================
*/
async function layoutBaseline( options, root, outputRoot, indexPath ) {
	const mainIndex = path.resolve( outputRoot ) === path.resolve( root, "assets", "packs" );
	const named = options.baselineIndexPath ?? (mainIndex ? process.env.SRO_ASSET_PACK_BASELINE?.trim() : undefined);
	if ( !named ) return readJsonOrUndefined( indexPath );
	const baseline = await readJsonOrUndefined( path.resolve( named ) );
	if ( baseline?.format !== "sro-asset-pack-index" || !Array.isArray( baseline.groups ) ) {
		throw new Error( `Asset pack layout baseline is not a pack index: ${named}` );
	}
	return baseline;
}

/*
================
buildAssetPackGroup
================
*/
async function buildAssetPackGroup(
	{ publicRoot, outputRoot, group, targetBytes, reusablePacks, hashCache, counters, baselineIndex, packSlots }
) {
	const name = normalizeGroupName( group.name );
	const publicPaths = uniquePublicPaths( group.files ?? [] );
	const files = await mapWithConcurrency( publicPaths, FILE_STAT_CONCURRENCY, async ( publicPath ) => {
		const absolutePath = containedPublicFile( publicRoot, publicPath );
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

	const ownDir = toPublicAssetPath( outputRoot, publicRoot );
	const chunks = planPackLayout( { files, baseline: baselinePacksOf( baselineIndex, name, ownDir ), targetBytes } );
	for ( const chunk of chunks ) {
		if ( chunk.kept ) counters.kept += 1;
		else counters.fresh += 1;
	}
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

	// Each pack in flight holds its buffer and zstd output (about 2 x 50 MiB);
	// packSlots bounds them across every group.
	const results = await Promise.all( chunks.map( plan =>
		packSlots( () =>
			buildOrReusePack( {
				name,
				plan,
				outputRoot: plan.dir ? containedPublicFile( publicRoot, plan.dir ) : outputRoot,
				publicRoot,
				reusablePacks,
				hashCache,
				counters
			} )
		)
	) );

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
			if ( typeof pack?.path === "string" ) {
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

The identity pack is on disk at its recorded size, or - in a compacted
tree, which keeps only the zstd copy `pnpm assets compact` made - that copy
is. Either way the unchanged pack is reused without rebuilding it.
================
*/
async function packOutputsIntact( publicRoot, pack ) {
	try {
		const binStat = await stat( containedPublicFile( publicRoot, pack.path ) ).catch( () => undefined );
		if ( binStat ) return binStat.isFile() && binStat.size === pack.bytes;
		if ( typeof pack.zstdPath !== "string" ) return false;
		const zstdStat = await stat( containedPublicFile( publicRoot, pack.zstdPath ) ).catch( () => undefined );
		return Boolean( zstdStat?.isFile() && zstdStat.size === pack.zstdBytes );
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
	{ name, plan, outputRoot, publicRoot, reusablePacks, hashCache, counters }
) {
	const { slot, files: chunk } = plan;
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
	// Folder and slot are part of the pack's URL: reuse only a pack built for both.
	const packDir = toPublicAssetPath( outputRoot, publicRoot );
	if (
		reusable && reusable.groupName === name && packSlotOf( reusable.pack.path ) === slot &&
		reusable.pack.path.slice( 0, reusable.pack.path.lastIndexOf( "/" ) ) === packDir &&
		(await packOutputsIntact( publicRoot, reusable.pack ))
	) {
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
	const packFileName = `${name}-${String( slot ).padStart( 3, "0" )}-${packHash.slice( 0, 12 )}.bin`;
	const packPath = path.join( outputRoot, packFileName );
	const packPublicPath = toPublicAssetPath( packPath, publicRoot );
	if ( plan.sha256 && plan.sha256 !== packHash ) {
		// The baseline's members rebuilt to other bytes (the pack format changed):
		// the pack is new to every client, so say so instead of hiding the cost.
		console.warn(
			`[asset-packs] kept pack ${packPublicPath} differs from its baseline ${plan.sha256.slice( 0, 12 )}`
		);
	}
	await mkdir( outputRoot, { recursive: true } );

	// The identity pack is what every reader serves. Its zstd-19 copy exists only
	// for the compact release footprint, so `pnpm assets compact` makes it
	// (compressAssetPackZstd); compressing every changed pack here cost most of a
	// clean build's pack step.
	await writeFile( packPath, buffer );
	counters.built += 1;

	const packEntry = {
		path: packPublicPath,
		bytes: buffer.length,
		sha256: packHash,
		assetCount: entries.length
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

The compact release's at-rest copy of one pack (pnpm assets compact).
================
*/
export function compressAssetPackZstd( bytes ) {
	return compressZstd( bytes, {
		level: ASSET_PACK_ZSTD_LEVEL,
		windowLog: ASSET_PACK_ZSTD_WINDOW_LOG
	} );
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

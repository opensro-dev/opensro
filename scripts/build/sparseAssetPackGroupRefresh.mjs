import { mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import * as zlib from "node:zlib";

import { buildAssetPacks, validateAssetPackIndex } from "./assetPacks.mjs";
import { ASSET_PACK_VERSION, decodeStoredMember, readPackPrefix, storedMemberBytes } from "./shared/packFormat.mjs";
import { assertInsideRoot, containedPublicFile, normalizePublicAssetPath } from "./shared/assetPaths.mjs";
import { mapWithConcurrency } from "./shared/asyncUtils.mjs";
import { openFileHashCache } from "./shared/fileHashCache.mjs";
import { sha256Hex } from "./shared/hash.mjs";

const HASH_CONCURRENCY = 8;

/**
 * Patch one asset-pack group from a sparse loose projection.
 *
 * Compaction intentionally deletes packed logical files. A focused rebuild may
 * recreate only one region, so absence cannot mean deletion. This routine
 * hashes the loose delta, reconstructs only each affected pack's missing
 * members from its existing identity/zstd bytes, and preserves every untouched
 * pack and asset row verbatim.
 */
export async function patchAssetPackGroupFromLooseFiles( options ) {
	const publicRoot = path.resolve( options.publicRoot );
	const outputRoot = path.resolve( options.outputRoot );
	assertInsideRoot( publicRoot, outputRoot, "sparse pack output root" );

	const previous = options.previousIndex;
	const groupName = String( options.groupName );
	const previousGroup = previous.groups?.find( ( group ) => group.name === groupName );
	if ( !previousGroup ) {
		throw new Error( `Cannot sparsely patch absent asset-pack group ${groupName}.` );
	}
	const previousAssets = previous.assets
		.filter( ( asset ) => asset.group === groupName )
		.map( ( asset ) => structuredClone( asset ) );
	const previousByPath = new Map( previousAssets.map( ( asset ) => [ asset.path, asset ] ) );
	const hashCache = await openFileHashCache( options.hashCachePath );
	const loosePaths = uniquePublicPaths( options.looseFiles ?? [] );
	const looseRows = await mapWithConcurrency( loosePaths, HASH_CONCURRENCY, async ( publicPath ) => {
		const absolutePath = containedPublicFile( publicRoot, publicPath );
		const fileStat = await stat( absolutePath );
		if ( !fileStat.isFile() ) {
			throw new Error( `Sparse pack input is not a file: ${publicPath}` );
		}
		return {
			publicPath,
			sha256: await hashCache.hashFile( absolutePath, fileStat )
		};
	} );
	await hashCache.save();

	const changedExisting = looseRows.filter( ( row ) => {
		const previousAsset = previousByPath.get( row.publicPath );
		return previousAsset && previousAsset.sha256 !== row.sha256;
	} );
	const newPaths = looseRows
		.filter( ( row ) => !previousByPath.has( row.publicPath ) )
		.map( ( row ) => row.publicPath );
	const affectedPackPaths = new Set(
		changedExisting.map( ( row ) => previousByPath.get( row.publicPath ).packPath )
	);

	if ( affectedPackPaths.size === 0 && newPaths.length === 0 ) {
		return {
			groups: [ structuredClone( previousGroup ) ],
			assets: previousAssets,
			builtPackCount: 0,
			reusedPackCount: previousGroup.packs.length,
			changedAssetCount: 0,
			hydratedAssetCount: 0
		};
	}

	const nextPacks = previousGroup.packs.map( ( pack ) => [ structuredClone( pack ) ] );
	const nextAssetsByPath = new Map( previousAssets.map( ( asset ) => [ asset.path, asset ] ) );
	let builtPackCount = 0;
	let reusedPackCount = previousGroup.packs.length - affectedPackPaths.size;
	let hydratedAssetCount = 0;

	for ( let packIndex = 0; packIndex < previousGroup.packs.length; packIndex += 1 ) {
		const previousPack = previousGroup.packs[packIndex];
		if ( !affectedPackPaths.has( previousPack.path ) ) {
			continue;
		}
		const members = previousAssets
			.filter( ( asset ) => asset.packPath === previousPack.path )
			.sort( ( left, right ) => left.offset - right.offset );
		const slotRoot = path.join( outputRoot, "slots", `slot-${previousPack.sha256}` );
		const rebuilt = await rebuildExistingPackSlot( {
			publicRoot,
			outputRoot: slotRoot,
			previousPack,
			members,
			targetBytes: previousGroup.targetBytes,
			hashCachePath: options.hashCachePath
		} );
		nextPacks[packIndex] = rebuilt.groups[0].packs;
		for ( const asset of rebuilt.assets ) {
			nextAssetsByPath.set( asset.path, asset );
		}
		builtPackCount += rebuilt.builtPackCount;
		reusedPackCount += rebuilt.reusedPackCount;
		hydratedAssetCount += rebuilt.hydratedAssetCount;
	}

	if ( newPaths.length > 0 ) {
		// A sparse append retains previous packs in the enclosing manifest.
		// buildAssetPacks archives outputs absent from its own local manifest;
		// reusing slots/new would archive packs still owned by that enclosing
		// manifest on the next append. Give each delta its own immutable namespace.
		const deltaHash = sha256Hex( Buffer.from( JSON.stringify(
			looseRows
				.filter( ( row ) => !previousByPath.has( row.publicPath ) )
				.sort( ( a, b ) => a.publicPath.localeCompare( b.publicPath ) )
		) ) );
		const newRoot = path.join( outputRoot, "slots", `new-${deltaHash}` );
		const appended = await buildAssetPacks( {
			publicRoot,
			outputRoot: newRoot,
			hashCachePath: options.hashCachePath,
			targetBytes: previousGroup.targetBytes,
			groups: [
				{
					name: groupName,
					load: previousGroup.load,
					targetBytes: previousGroup.targetBytes,
					files: newPaths
				}
			]
		} );
		nextPacks.push( appended.groups[0].packs );
		for ( const asset of appended.assets ) {
			nextAssetsByPath.set( asset.path, asset );
		}
		builtPackCount += appended.builtPackCount;
		reusedPackCount += appended.reusedPackCount;
	}

	const nextAssets = [ ...nextAssetsByPath.values() ].sort( ( left, right ) =>
		left.path.localeCompare( right.path )
	);
	const nextGroup = {
		...structuredClone( previousGroup ),
		packs: nextPacks.flat(),
		assetCount: nextAssets.length,
		totalBytes: nextAssets.reduce( ( sum, asset ) => sum + asset.length, 0 )
	};
	validateAssetPackIndex( {
		format: "sro-asset-pack-index",
		version: ASSET_PACK_VERSION,
		generatedAt: previous.generatedAt,
		targetPackBytes: previous.targetPackBytes,
		groups: [ nextGroup ],
		assets: nextAssets
	} );

	return {
		groups: [ nextGroup ],
		assets: nextAssets,
		builtPackCount,
		reusedPackCount,
		changedAssetCount: changedExisting.length + newPaths.length,
		hydratedAssetCount
	};
}

async function rebuildExistingPackSlot( options ) {
	const identity = await readPackIdentity( options.publicRoot, options.previousPack );
	const dataStart = packDataStart( identity, options.previousPack.path );
	const hydratedPaths = [];
	try {
		for ( const member of options.members ) {
			const targetPath = containedPublicFile( options.publicRoot, member.path );
			const targetStats = await stat( targetPath ).catch( () => undefined );
			if ( targetStats?.isFile() ) {
				continue;
			}
			const bytes = decodeStoredMember(
				storedMemberBytes( identity, dataStart, member, options.previousPack.path ),
				member
			);
			await mkdir( path.dirname( targetPath ), { recursive: true } );
			await writeFile( targetPath, bytes );
			hydratedPaths.push( targetPath );
		}

		const rebuilt = await buildAssetPacks( {
			publicRoot: options.publicRoot,
			outputRoot: options.outputRoot,
			hashCachePath: options.hashCachePath,
			targetBytes: options.targetBytes,
			groups: [
				{
					name: options.members[0].group,
					load: "manual",
					targetBytes: options.targetBytes,
					files: options.members.map( ( member ) => member.path )
				}
			]
		} );
		if ( rebuilt.packCount < 1 || rebuilt.assetCount !== options.members.length ) {
			throw new Error(
				`Sparse slot rebuild emitted ${rebuilt.packCount} packs/${rebuilt.assetCount} assets; expected nonempty packs/${options.members.length}.`
			);
		}
		return { ...rebuilt, hydratedAssetCount: hydratedPaths.length };
	} finally {
		await Promise.all( hydratedPaths.map( ( filePath ) => rm( filePath, { force: true } ) ) );
	}
}

async function readPackIdentity( publicRoot, pack ) {
	const identityPath = containedPublicFile( publicRoot, pack.path );
	let bytes;
	try {
		bytes = await readFile( identityPath );
	} catch ( error ) {
		if ( error?.code !== "ENOENT" ) {
			throw error;
		}
		if ( typeof zlib.zstdDecompressSync !== "function" || typeof pack.zstdPath !== "string" ) {
			throw new Error( `Cannot hydrate compact pack ${pack.path}: zstd decompression is unavailable.` );
		}
		bytes = zlib.zstdDecompressSync( await readFile( containedPublicFile( publicRoot, pack.zstdPath ) ) );
	}
	if ( bytes.length !== pack.bytes || sha256Hex( bytes ) !== pack.sha256 ) {
		throw new Error( `Pack identity mismatch while hydrating ${pack.path}.` );
	}
	return bytes;
}

function packDataStart( buffer, packPath ) {
	const dataStart = readPackPrefix( buffer, packPath );
	if ( dataStart > buffer.length ) {
		throw new Error( `Invalid header length in ${packPath}.` );
	}
	return dataStart;
}

function uniquePublicPaths( values ) {
	const byLower = new Map();
	for ( const value of values ) {
		const normalized = normalizePublicAssetPath( value );
		byLower.set( normalized.toLowerCase(), normalized );
	}
	return [ ...byLower.values() ].sort( ( left, right ) => left.localeCompare( right ) );
}

// Guards scripts/checks/check_asset_pack_integrity.mjs, in particular its stat-keyed
// hash-cache fast path: the cache may only ever CONFIRM the manifest's digests, a
// corrupted pack must fail the check, structural checks must run even on a warm
// cache, and SRO_ASSET_INTEGRITY_NO_CACHE=1 must force a full byte re-hash (the
// escape hatch for tampering that preserves size+mtime, which the stat key cannot
// see). Everything runs in a temp tree with a temp cache file, so neither the real
// packs nor the production .state caches are touched.

import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, mkdir, readFile, rm, stat, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { gunzipSync, gzipSync } from "node:zlib";
import test from "node:test";

import { reconcileAssetPackGroupFromLooseAuthority } from "../../build/assetPackGroupAuthority.mjs";
import { buildAssetPacks, compressAssetPackZstd } from "../../build/assetPacks.mjs";
import { patchAssetPackGroupFromLooseFiles } from "../../build/sparseAssetPackGroupRefresh.mjs";
import { decodeStoredMember, parsePackHeader, storedMemberBytes } from "../../build/shared/packFormat.mjs";

const execFileAsync = promisify( execFile );
const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const checkScript = path.resolve( scriptDir, "..", "..", "checks", "check_asset_pack_integrity.mjs" );

// Whole seconds survive the stat -> utimes -> stat round trip exactly, so the
// cache's mtimeMs key can be restored byte-for-byte after tampering.
const PINNED_MTIME = new Date( "2026-01-01T00:00:00Z" );

test("integrity check hash cache confirms, never masks, and stays escapable", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-integrity-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const { manifestPath, packPath, zstdPath } = await buildFixturePacks( tempRoot );
	const cachePath = path.join( tempRoot, "integrity-hash-cache.json" );

	// Cold run: everything is hashed from bytes; the run warms the check-owned cache.
	const cold = await runIntegrityCheck( manifestPath, cachePath );
	assert.equal( cold.code, 0, cold.output );
	assert.match( cold.output, /Asset pack integrity OK/ );
	assert.match( cold.output, /Pack SHA-256: 1 hashed, 0 stat-matched from cache/ );

	// Warm run: identical tree, so the pack digest is confirmed via size+mtime.
	const warm = await runIntegrityCheck( manifestPath, cachePath );
	assert.equal( warm.code, 0, warm.output );
	assert.match( warm.output, /Asset pack integrity OK/ );
	assert.match( warm.output, /Pack SHA-256: 0 hashed, 1 stat-matched from cache/ );

	// Structural checks still run on a warm cache: a manifest tamper that touches no
	// pack bytes (asset MIME flip) must fail even though the pack digest stat-matches.
	const manifestBackup = await readFile( manifestPath, "utf8" );
	const tamperedManifest = JSON.parse( manifestBackup );
	tamperedManifest.assets[0].mime = "application/x-tampered";
	await writeFile( manifestPath, JSON.stringify( tamperedManifest ), "utf8" );
	const structural = await runIntegrityCheck( manifestPath, cachePath );
	assert.notEqual( structural.code, 0, "manifest tamper must fail on a warm cache" );
	assert.match( structural.output, /MIME type/ );
	await writeFile( manifestPath, manifestBackup, "utf8" );

	// Corruption with a fresh mtime: the stat key misses, bytes are re-hashed, caught.
	const original = await readFile( packPath );
	await writeFile( packPath, flipByteInDataRegion( original ) );
	const corrupted = await runIntegrityCheck( manifestPath, cachePath );
	assert.notEqual( corrupted.code, 0, "byte flip with changed mtime must fail" );
	assert.match( corrupted.output, /SHA-256/ );

	// Corruption that preserves size AND mtime is the documented cache blind spot:
	// the stat key cannot see it, so the warm run passes...
	await utimes( packPath, PINNED_MTIME, PINNED_MTIME );
	const packStats = await stat( packPath );
	assert.equal( packStats.size, original.byteLength, "tamper must preserve size for this drill" );
	const masked = await runIntegrityCheck( manifestPath, cachePath );
	assert.equal( masked.code, 0, `size+mtime-preserving tamper is expected to stat-match: ${masked.output}` );
	assert.match( masked.output, /stat-matched from cache/ );

	// ...and SRO_ASSET_INTEGRITY_NO_CACHE=1 is the escape hatch that must catch it.
	const forced = await runIntegrityCheck( manifestPath, cachePath, { SRO_ASSET_INTEGRITY_NO_CACHE: "1" } );
	assert.notEqual( forced.code, 0, "NO_CACHE=1 must re-hash bytes and catch the tamper" );
	assert.match( forced.output, /SHA-256/ );

	// Restore the pack: the untampered tree must pass again, from cache.
	await writeFile( packPath, original );
	await utimes( packPath, PINNED_MTIME, PINNED_MTIME );
	const restored = await runIntegrityCheck( manifestPath, cachePath );
	assert.equal( restored.code, 0, restored.output );
	assert.match( restored.output, /stat-matched from cache/ );

	// The zstd sidecar's decompressed digest rides the same cache; prove it appears
	// in the warm output when the runtime can decompress zstd at all.
	if ( zstdPath ) {
		assert.match( restored.output, /Zstd sidecars: 0 hashed, 1 stat-matched from cache/ );

		// Compact releases intentionally remove identity .bin files. The same gate
		// must reconstruct the identity bytes from .bin.zst and still validate the
		// header, whole-pack digest, and every asset slice contract.
		await rm( packPath, { force: true } );
		const compact = await runIntegrityCheck( manifestPath, cachePath );
		assert.equal( compact.code, 0, compact.output );
		assert.match( compact.output, /1 sidecar-only packs/ );
	}
});

test("pack builder reuses unchanged zstd-only compact outputs", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-zstd-cache-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	const inputPath = path.join( publicRoot, "assets", "world", "outdoor", "region.json.gz" );
	const outputRoot = path.join( publicRoot, "assets", "packs", "outdoor" );
	await mkdir( path.dirname( inputPath ), { recursive: true } );
	await writeFile( inputPath, "stable-outdoor-fixture" );
	const options = {
		publicRoot,
		outputRoot,
		hashCachePath: path.join( tempRoot, "build-hash-cache.json" ),
		targetBytes: 1024,
		groups: [
			{
				name: "outdoor-world",
				load: "manual",
				files: [ "/assets/world/outdoor/region.json.gz" ]
			}
		]
	};

	const cold = await buildAssetPacks( options );
	assert.equal( cold.builtPackCount, 1 );
	const pack = cold.groups[0].packs[0];
	assert.equal( pack.zstdPath, undefined, "the build writes identity packs only" );
	const identityPath = path.join( publicRoot, pack.path.replace( /^\/+/, "" ) );
	// Compact the tree as `pnpm assets compact` does: the zstd copy recorded in
	// the index, the identity pack removed.
	const zstdPath = `${identityPath}.zst`;
	const compressed = await compressAssetPackZstd( await readFile( identityPath ) );
	await writeFile( zstdPath, compressed );
	const indexPath = cold.outputPath;
	const index = JSON.parse( await readFile( indexPath, "utf8" ) );
	Object.assign( index.groups[0].packs[0], { zstdPath: `${pack.path}.zst`, zstdBytes: compressed.length } );
	await writeFile( indexPath, JSON.stringify( index ) );
	await rm( identityPath, { force: true } );

	const warm = await buildAssetPacks( options );
	assert.equal( warm.builtPackCount, 0, "compact cache must not reconstruct and recompress identity bytes" );
	assert.equal( warm.reusedPackCount, 1 );
	await assert.rejects(
		stat( identityPath ),
		( error ) => error instanceof Error && "code" in error && error.code === "ENOENT"
	);
	assert.equal( (await stat( zstdPath )).size, compressed.length );
});

test("a reused pack beside its identity bytes drops its compact copy", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-zstd-retire-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	const inputPath = path.join( publicRoot, "assets", "world", "outdoor", "region.json.gz" );
	await mkdir( path.dirname( inputPath ), { recursive: true } );
	await writeFile( inputPath, "stable-outdoor-fixture" );
	const options = {
		publicRoot,
		outputRoot: path.join( publicRoot, "assets", "packs", "outdoor" ),
		hashCachePath: path.join( tempRoot, "build-hash-cache.json" ),
		targetBytes: 1024,
		groups: [ { name: "outdoor-world", load: "manual", files: [ "/assets/world/outdoor/region.json.gz" ] } ]
	};

	const cold = await buildAssetPacks( options );
	const pack = cold.groups[0].packs[0];
	const identityPath = path.join( publicRoot, pack.path.replace( /^\/+/, "" ) );
	// A copy an older build (or compact) recorded, while the identity pack stays.
	const compressed = await compressAssetPackZstd( await readFile( identityPath ) );
	await writeFile( `${identityPath}.zst`, compressed );
	const index = JSON.parse( await readFile( cold.outputPath, "utf8" ) );
	Object.assign( index.groups[0].packs[0], { zstdPath: `${pack.path}.zst`, zstdBytes: compressed.length } );
	await writeFile( cold.outputPath, JSON.stringify( index ) );

	const warm = await buildAssetPacks( options );
	assert.equal( warm.reusedPackCount, 1 );
	assert.equal( warm.groups[0].packs[0].zstdPath, undefined );
	assert.equal( (await stat( identityPath )).size, pack.bytes );
	await assert.rejects(
		stat( `${identityPath}.zst` ),
		( error ) => error instanceof Error && "code" in error && error.code === "ENOENT"
	);
});

test("sparse group refresh patches one compacted pack without dropping absent assets", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-sparse-refresh-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	const outputRoot = path.join( publicRoot, "assets", "packs" );
	const publicPaths = [ "a", "b", "c", "d" ].map(
		( name ) => `/assets/world/outdoor/${name}.json.gz`
	);
	for ( const publicPath of publicPaths ) {
		const absolutePath = path.join( publicRoot, publicPath.replace( /^\/+/, "" ) );
		await mkdir( path.dirname( absolutePath ), { recursive: true } );
		await writeFile( absolutePath, `original-${path.basename( publicPath )}-payload` );
	}
	const cold = await buildAssetPacks( {
		publicRoot,
		outputRoot,
		hashCachePath: path.join( tempRoot, "build-hash-cache.json" ),
		targetBytes: 55,
		groups: [ { name: "outdoor-world", load: "manual", targetBytes: 55, files: publicPaths } ]
	} );
	assert.ok( cold.packCount > 1, "fixture must span multiple packs" );
	const previous = JSON.parse( await readFile( cold.outputPath, "utf8" ) );
	const oldByPath = new Map( previous.assets.map( ( asset ) => [ asset.path, asset ] ) );

	for ( const publicPath of publicPaths ) {
		await rm( path.join( publicRoot, publicPath.replace( /^\/+/, "" ) ), { force: true } );
	}
	// Compact the tree as `pnpm assets compact` does: zstd copies recorded in the
	// index, the identity packs removed.
	for ( const pack of previous.groups[0].packs ) {
		const identityPath = path.join( publicRoot, pack.path.replace( /^\/+/, "" ) );
		const compressed = await compressAssetPackZstd( await readFile( identityPath ) );
		await writeFile( `${identityPath}.zst`, compressed );
		Object.assign( pack, { zstdPath: `${pack.path}.zst`, zstdBytes: compressed.length } );
		await rm( identityPath, { force: true } );
	}
	await writeFile( cold.outputPath, JSON.stringify( previous ) );
	const changedPath = publicPaths[1];
	const changedAbsolutePath = path.join( publicRoot, changedPath.replace( /^\/+/, "" ) );
	await mkdir( path.dirname( changedAbsolutePath ), { recursive: true } );
	await writeFile( changedAbsolutePath, "changed-b-payload-with-a-different-length" );

	const patched = await patchAssetPackGroupFromLooseFiles( {
		publicRoot,
		outputRoot: path.join( outputRoot, "outdoor" ),
		previousIndex: previous,
		groupName: "outdoor-world",
		looseFiles: [ changedPath ],
		hashCachePath: path.join( tempRoot, "build-hash-cache.json" )
	} );
	assert.equal( patched.assets.length, publicPaths.length );
	assert.equal(
		patched.builtPackCount,
		2,
		"a growing slot must split at the target instead of producing an oversized pack"
	);
	assert.equal( patched.reusedPackCount, cold.packCount - 1 );
	assert.ok( patched.hydratedAssetCount > 0 );
	assert.notEqual(
		patched.assets.find( ( asset ) => asset.path === changedPath ).sha256,
		oldByPath.get( changedPath ).sha256
	);
	for ( const publicPath of publicPaths.filter( ( candidate ) => candidate !== changedPath ) ) {
		assert.equal(
			patched.assets.find( ( asset ) => asset.path === publicPath ).sha256,
			oldByPath.get( publicPath ).sha256,
			`${publicPath} must survive an absent loose projection`
		);
		await assert.rejects(
			stat( path.join( publicRoot, publicPath.replace( /^\/+/, "" ) ) ),
			( error ) => error instanceof Error && "code" in error && error.code === "ENOENT"
		);
	}
});

test("authoritative group reconciliation replaces stale packed JSON sidecars", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-pack-authority-refresh-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );

	const publicRoot = path.join( tempRoot, "public" );
	const logicalPath = path.join( publicRoot, "assets", "world", "test", "world-regions-0001.json" );
	const packedPublicPath = "/assets/world/test/world-regions-0001.json.gz";
	const packedPath = `${logicalPath}.gz`;
	await mkdir( path.dirname( logicalPath ), { recursive: true } );
	await writeFile( logicalPath, JSON.stringify( { format: "sro-world-region-index", version: 1 } ) );
	await writeFile( packedPath, gzipSync( await readFile( logicalPath ) ) );

	const cold = await buildAssetPacks( {
		publicRoot,
		outputRoot: path.join( publicRoot, "assets", "packs" ),
		hashCachePath: path.join( tempRoot, "build-hash-cache.json" ),
		groups: [ { name: "game-data", load: "startup", files: [ packedPublicPath ] } ]
	} );
	const previous = JSON.parse( await readFile( cold.outputPath, "utf8" ) );
	const oldSha256 = previous.assets[0].sha256;
	await writeFile( logicalPath, JSON.stringify( { format: "sro-world-region-index", version: 2 } ) );

	const reconciled = await reconcileAssetPackGroupFromLooseAuthority( {
		publicRoot,
		outputRoot: path.join( publicRoot, "assets", "packs", "reconciled" ),
		previousIndex: previous,
		groupName: "game-data",
		hashCachePath: path.join( tempRoot, "reconcile-hash-cache.json" )
	} );

	assert.equal( reconciled.authorityFileCount, 1 );
	assert.equal( reconciled.jsonAuthorityCount, 1 );
	assert.equal( reconciled.refreshedJsonSidecarCount, 1 );
	assert.equal( reconciled.builtPackCount, 1 );
	assert.notEqual( reconciled.assets[0].sha256, oldSha256 );
	assert.equal( JSON.parse( gunzipSync( await readFile( packedPath ) ).toString( "utf8" ) ).version, 2 );
	assert.equal( await readPackedJsonVersion( publicRoot, reconciled, packedPublicPath ), 2 );
});

async function readPackedJsonVersion( publicRoot, index, publicPath ) {
	const asset = index.assets.find( ( candidate ) => candidate.path === publicPath );
	const packBytes = await readFile( path.join( publicRoot, asset.packPath.replace( /^\/+/, "" ) ) );
	const { dataStart } = parsePackHeader( packBytes, asset.packPath );
	const member = decodeStoredMember( storedMemberBytes( packBytes, dataStart, asset, asset.packPath ), asset );
	return JSON.parse( gunzipSync( member ).toString( "utf8" ) ).version;
}

async function buildFixturePacks( tempRoot ) {
	const publicRoot = path.join( tempRoot, "public" );
	const imageRoot = path.join( publicRoot, "assets", "images", "Media_extracted", "interface", "outer" );
	await mkdir( imageRoot, { recursive: true } );
	await writeFile( path.join( imageRoot, "button.png" ), "normal-bytes-normal-bytes" );
	await writeFile( path.join( imageRoot, "button_focus.png" ), "focus-bytes-focus-bytes" );

	const result = await buildAssetPacks( {
		publicRoot,
		outputRoot: path.join( publicRoot, "assets", "packs" ),
		// Keep the BUILD hash cache inside the temp tree too; the production
		// .state/file-hash-cache.json must never see fixture hashes.
		hashCachePath: path.join( tempRoot, "build-hash-cache.json" ),
		targetBytes: 50 * 1024 * 1024,
		groups: [
			{
				name: "native-ui",
				load: "startup",
				files: [
					"/assets/images/Media_extracted/interface/outer/button.png",
					"/assets/images/Media_extracted/interface/outer/button_focus.png"
				]
			}
		]
	} );

	const index = JSON.parse( await readFile( result.outputPath, "utf8" ) );
	const packPublicPath = index.groups[0].packs[0].path;
	const packPath = path.join( publicRoot, packPublicPath.replace( /^\/+/, "" ) );
	const zstdPath = index.groups[0].packs[0].zstdPath ? `${packPath}.zst` : null;

	// Pin mtimes to a whole second so the corruption drill can restore them exactly.
	await utimes( packPath, PINNED_MTIME, PINNED_MTIME );
	if ( zstdPath ) {
		await utimes( zstdPath, PINNED_MTIME, PINNED_MTIME );
	}

	return { manifestPath: result.outputPath, packPath, zstdPath };
}

async function runIntegrityCheck( manifestPath, cachePath, extraEnv = {} ) {
	try {
		const { stdout, stderr } = await execFileAsync( process.execPath, [ checkScript, manifestPath ], {
			env: {
				...process.env,
				SRO_ASSET_INTEGRITY_CACHE_PATH: cachePath,
				SRO_ASSET_INTEGRITY_NO_CACHE: "",
				...extraEnv
			}
		} );
		return { code: 0, output: `${stdout}\n${stderr}` };
	} catch ( error ) {
		return { code: error.code ?? 1, output: `${error.stdout ?? ""}\n${error.stderr ?? ""}` };
	}
}

/*
Flip one byte in the middle of the data region (past the 12-byte magic/length prefix
and the JSON header), so the tamper models silent payload corruption rather than a
malformed container.
*/
function flipByteInDataRegion( buffer ) {
	const headerLength = buffer.readUInt32LE( 8 );
	const dataStart = 12 + headerLength;
	const target = dataStart + Math.floor( (buffer.byteLength - dataStart) / 2 );
	const copy = Buffer.from( buffer );
	copy[target] ^= 0xff;
	return copy;
}

test("successive sparse appends preserve every pack still referenced by the enclosing manifest", async ( t ) => {
	const tempRoot = await mkdtemp( path.join( os.tmpdir(), "sro-sparse-append-" ) );
	t.after( () => rm( tempRoot, { recursive: true, force: true } ) );
	const publicRoot = path.join( tempRoot, "public" ),
		outputRoot = path.join( publicRoot, "assets/packs" ),
		hashCachePath = path.join( tempRoot, "hash.json" );
	await mkdir( path.join( publicRoot, "assets/images" ), { recursive: true } );
	const files = [ "a", "b", "c" ].map( n => `/assets/images/${n}.png` );
	for ( const [i, file] of files.entries() ) await writeFile( path.join( publicRoot, file ), Buffer.alloc( 32, i ) );
	const initial = await buildAssetPacks( {
		publicRoot,
		outputRoot,
		hashCachePath,
		groups: [ { name: "images", load: "startup", files: [ files[0] ] } ]
	} );
	let index = JSON.parse( await readFile( initial.outputPath, "utf8" ) );
	for ( const file of files.slice( 1 ) ) {
		const delta = await patchAssetPackGroupFromLooseFiles( {
			publicRoot,
			outputRoot: path.join( outputRoot, "incremental" ),
			previousIndex: index,
			groupName: "images",
			looseFiles: [ file ],
			hashCachePath
		} );
		index = { ...index, groups: delta.groups, assets: delta.assets };
		for ( const pack of index.groups[0].packs ) {
			assert.equal( (await readFile( path.join( publicRoot, pack.path ) )).length, pack.bytes );
		}
	}
	assert.equal( index.assets.length, 3 );
	assert.equal( index.groups[0].packs.length, 3 );
});

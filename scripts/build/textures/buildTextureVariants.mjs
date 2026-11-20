/*
===========================================================================

buildTextureVariants.mjs - ASTC re-encodings of the native textures

Port-only. Every published native texture (.texture, NTX) is re-encoded as
ASTC 4x4 for devices without BC (client foundation/assets/texture-variants
.ts) and written to its variant path beside the original, so each path
keeps one owning pack group. Every level is decoded with the client's own
NTX decoder and encoded by the pinned astcenc (encoders.mjs) at the
-medium preset; results are cached by the source's SHA-256.

A variant has no CPU decoder, so picking cannot read its alpha back. Each
one whose source has any alpha below 255 carries that alpha (level 0, one
byte a texel, decoded bit-exactly from the BC blocks) after its levels,
so a variant picks exactly as the original; an opaque source carries none
(native-texture.ts NTX_PICK_ALPHA). The mask has its own cache beside the
encoder's, keyed by the source's SHA-256 and by the code hash of the
byte code (textureVariantEncode.mjs, with the decoder it imports), so a
fix there recomputes every mask while an unchanged texture is never
decoded again. Cache records are
written whole (a temp name, then a rename). WebGPU requires a
block-compressed level 0 to be whole blocks, so a texture under 4x4 keeps
its original.

Each texture is also an action (graph/action.mjs, plan slice B2): keyed
on its source bytes, the byte code's closure (textureVariantEncode.mjs,
never this pass's plumbing), the encoder version and the preset, with its variant as the one output. A texture whose key has a
record is laid down from the action store (or left alone when its variant
already holds those bytes) without reading its source, its caches or
writing its variant: release r193 read 1000 MB and rewrote 2007 MB for
15463 unchanged textures (38.7 s). A miss runs the pass below and stores
its variant. --roundtrip writes a second file per texture, so it keeps to
the pass alone.

A miss's cache hit is file reads and writes only, done asynchronously so
the workers overlap them. Before, each hit decoded its level 0 on the one
thread, and the 15200 hits of a seeded release build took 71 s.

	node scripts/build/textures/buildTextureVariants.mjs
		[--jobs N] [--only /assets/prefix] [--limit N]
		[--out DIR] [--roundtrip DIR]

--out writes the variants (and their cache) below DIR instead of the
published tree: a private build that leaves the shared tree untouched.

--roundtrip writes, for each texture, the ASTC result decoded back to an
uncompressed BGRA NTX at DIR/<published path>: the texels a GPU sampling
the variant reads, for side-by-side frames on a machine without ASTC.

===========================================================================
*/
import { buildJobs } from "../shared/buildParallelism.mjs";
import { codeHash } from "../shared/codeStamp.mjs";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineAction, outputPath } from "../graph/action.mjs";
import { keyOf, materialize, openStore, readRecord, storeOutputs } from "../graph/store.mjs";
import { CLIENT_PUBLIC_ROOT, generatedPath, sharedCachePath } from "../../lib/generatedRoot.mjs";
import { claimPublicFile, isPublicationOpen } from "../shared/publicationLedger.mjs";
import { ENCODERS, encoderPath } from "./encoders.mjs";
import { ASTC_BLOCK, ENCODE_MODULE, encodeTexture, pickAlphaSection, PRESET } from "./textureVariantEncode.mjs";
import { decodeNativeTexture } from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";
import { textureVariantPath } from "../../../apps/client-next/src/engine/foundation/assets/texture-variants.ts";
import { compareText } from "../shared/textOrder.mjs";
import { sha256Hex } from "../shared/hash.mjs";

const VARIANT = "astc";
const DECODER_DIGEST_CHARS = 16;

/*
================
options
================
*/
function options( argv ) {
	const out = {
		jobs: buildJobs(),
		only: "/assets/",
		limit: Infinity,
		out: null,
		roundtrip: null
	};
	for ( let i = 0; i < argv.length; i++ ) {
		const value = argv[i + 1];
		if ( argv[i] === "--jobs" ) out.jobs = Math.max( 1, Number( value ) ), i++;
		else if ( argv[i] === "--only" ) out.only = value, i++;
		else if ( argv[i] === "--limit" ) out.limit = Number( value ), i++;
		else if ( argv[i] === "--roundtrip" ) out.roundtrip = path.resolve( value ), i++;
		else if ( argv[i] === "--out" ) out.out = path.resolve( value ), i++;
		else throw new Error( `Unknown option ${argv[i]}` );
	}
	return out;
}

/*
================
nativeTextures

Every published native texture below /assets/, as [file, public path],
leaving out the variants themselves.
================
*/
function nativeTextures( root, only ) {
	const out = [];
	const walk = directory => {
		for ( const entry of readdirSync( directory, { withFileTypes: true } ) ) {
			const file = path.join( directory, entry.name );
			if ( entry.isDirectory() ) {
				if ( entry.name !== "packs" ) walk( file );
				continue;
			}
			if ( !entry.name.endsWith( ".texture" ) ) continue;
			const publicPath = "/" + path.relative( root, file ).split( path.sep ).join( "/" );
			if ( publicPath.startsWith( only ) && textureVariantPath( publicPath, VARIANT ) ) {
				out.push( [ file, publicPath ] );
			}
		}
	};
	walk( path.join( root, "assets" ) );
	return out.sort( ( a, b ) => compareText( a[1], b[1] ) );
}

/*
================
maskCacheRoot

The mask cache for one mask writer: a folder named by its code hash
(maskCodeHash), so a change to the mask starts an empty one.
================
*/
export function maskCacheRoot( cacheRoot, decoder ) {
	return path.join( cacheRoot, `pick-alpha-${decoder.slice( 0, DECODER_DIGEST_CHARS )}` );
}

/*
================
maskCodeHash

The code hash of everything that shapes a mask (codeStamp.mjs, the owner
every builder stamp uses): textureVariantEncode.mjs, which holds the
opacity rule (pickAlphaSection), the container that writes the tag and
byte order, and the client's NTX decoder. A change to any serves no stale
mask; a change to this pass's plumbing keeps every mask.
================
*/
export function maskCodeHash() {
	return codeHash( ENCODE_MODULE );
}

/*
================
writeCacheFile

A cache record written whole: to a temp name, then renamed over the
target, so a build killed mid-write leaves no torn record. A torn empty
mask would read as an opaque source.
================
*/
function writeCacheFile( target, bytes ) {
	const temporary = `${target}.${process.pid}.tmp`;
	writeFileSync( temporary, bytes );
	renameSync( temporary, target );
}

/*
================
cachedPickAlpha

The source's picking mask from the mask cache (an empty file is an opaque
source), or computed by pick and recorded on a miss. The record is written
synchronously: two textures of the same bytes share a digest, and another
worker must never read a half-written mask.
================
*/
export async function cachedPickAlpha( maskRoot, digest, pick ) {
	const file = path.join( maskRoot, `${digest}.pka` );
	try {
		return await readFile( file );
	} catch ( error ) {
		if ( error.code !== "ENOENT" ) throw error;
	}
	const mask = pick();
	writeCacheFile( file, mask );
	return mask;
}

/*
================
textureVariantAction

One texture's variant as an action (see the file banner). roots.source is
the tree the texture is published in, roots.output the tree its variant
goes to (the published tree, or --out's private one). Nothing runs it
alone: a miss goes through the pass's worker, which stores what it wrote.
================
*/
function textureVariantAction( publicPath, roots ) {
	return defineAction( {
		name: `texture-variant:${publicPath}`,
		inputRoot: roots.source,
		inputs: [ publicPath.slice( 1 ) ],
		outputRoot: roots.output,
		outputs: [ textureVariantPath( publicPath, VARIANT ).replace( /^\//, "" ) ],
		code: [ ENCODE_MODULE ],
		params: { encoder: ENCODERS.astcenc.version, preset: PRESET },
		run: async () => {
			throw new Error( `texture variant ${publicPath} is made by its pass, never alone` );
		}
	} );
}

/*
================
buildTextureVariants

The pass, for the resource build (resourceBuild.mjs) and the CLI below.
Each written variant is claimed in the open publication, so the claim audit
keeps it and a variant whose texture was retired is left out. Only the
windows-x64 encoder is pinned: elsewhere the pass is skipped and clients
read the originals.
================
*/
export async function buildTextureVariants( flags = {} ) {
	flags = {
		jobs: buildJobs(),
		only: "/assets/",
		limit: Infinity,
		out: null,
		roundtrip: null,
		// The tree the textures are published in (a fixture's, in tests).
		publicRoot: CLIENT_PUBLIC_ROOT,
		// An open action store to use and leave open (tests); else the
		// shared one, opened and shut here.
		store: null,
		...flags
	};
	if ( process.platform !== "win32" ) {
		console.log( "texture variants: skipped (the pinned astcenc is windows-x64 only)" );
		return { skipped: true };
	}
	const astcenc = await encoderPath( "astcenc" );
	const cacheName = `astc-4x4-${ENCODERS.astcenc.version}${PRESET}`;
	const cacheRoot = flags.out ?
		path.join( flags.out, "intermediate", "texture-variants", cacheName ) :
		// Named by the source's SHA-256 under an encoder-and-preset folder, so
		// trees may share it (generatedRoot.mjs sharedCachePath).
		sharedCachePath(
			path.join( "texture-variants", cacheName ),
			generatedPath( "intermediate", "texture-variants", cacheName )
		);
	const outputRoot = flags.out ? path.join( flags.out, "client-public" ) : flags.publicRoot;
	const maskRoot = maskCacheRoot( cacheRoot, await maskCodeHash() );
	mkdirSync( maskRoot, { recursive: true } );
	const textures = nativeTextures( flags.publicRoot, flags.only ).slice( 0, flags.limit );
	const store = flags.roundtrip ? null : (flags.store ?? (await openStore()));
	const roots = { source: flags.publicRoot, output: outputRoot };
	// masked counts the variants this run produced; a stored one is not read.
	let next = 0,
		encoded = 0,
		cached = 0,
		stored = 0,
		placed = 0,
		unaligned = 0,
		masked = 0,
		sourceBytes = 0,
		variantBytes = 0;
	// One job per source digest in flight: textures of the same bytes await
	// it instead of reading a cache file another worker is writing.
	const inFlight = new Map();
	const startedAt = performance.now();
	/*
	================
	produce

	The encoded levels, round trip and picking mask of one source, from the
	caches or encoded and recorded there.
	================
	*/
	async function produce( digest, source, texture, scratch ) {
		const cachedVariant = path.join( cacheRoot, `${digest}.ntx` ),
			cachedBack = path.join( cacheRoot, `${digest}.back.ntx` );
		const mask = await cachedPickAlpha( maskRoot, digest, () => pickAlphaSection( texture ) );
		if ( existsSync( cachedVariant ) && (!flags.roundtrip || existsSync( cachedBack )) ) {
			const variant = await readFile( cachedVariant );
			const back = flags.roundtrip ? await readFile( cachedBack ) : null;
			return { variant, back, mask, hit: true };
		}
		const result = await encodeTexture( astcenc, source, scratch, !!flags.roundtrip );
		writeCacheFile( cachedVariant, result.variant );
		if ( result.roundtrip ) writeCacheFile( cachedBack, result.roundtrip );
		return { variant: result.variant, back: result.roundtrip, mask, hit: false };
	}
	/*
	================
	worker
	================
	*/
	async function worker() {
		const scratch = mkdtempSync( path.join( cacheRoot, ".scratch-" ) );
		try {
			while ( next < textures.length ) {
				const [file, publicPath] = textures[next++];
				const action = store ? textureVariantAction( publicPath, roots ) : null;
				const key = store ? await keyOf( store, action, {} ) : null;
				const record = store ? await readRecord( store, key ) : undefined;
				if ( record ) {
					// Await first, then add: `placed += await ...` reads placed before the
					// await, and a worker resuming in between loses its count.
					const laid = await materialize( store, action, record );
					placed += laid;
					const target = outputPath( action, action.outputs[0] );
					if ( !flags.out && isPublicationOpen() ) claimPublicFile( target );
					stored++;
					// The summary's BC side counts every texture, as the ASTC
					// side does: a stored variant's source is sized, never read.
					// Await first, then add (as placed above).
					const { size } = await stat( file );
					sourceBytes += size;
					variantBytes += record.outputs[0].size;
					continue;
				}
				const source = await readFile( file ), digest = sha256Hex( source );
				const texture = decodeNativeTexture( new Uint8Array( source ) );
				if ( texture.width % ASTC_BLOCK || texture.height % ASTC_BLOCK ) {
					unaligned++;
					continue;
				}
				let job = inFlight.get( digest );
				if ( !job ) {
					job = produce( digest, source, texture, scratch ).finally( () => inFlight.delete( digest ) );
					inFlight.set( digest, job );
				}
				const { variant: levels, back, mask, hit } = await job;
				if ( hit ) cached++;
				else encoded++;
				if ( mask.length ) masked++;
				const variant = Buffer.concat( [ levels, mask ] );
				const target = path.join( outputRoot, ...textureVariantPath( publicPath, VARIANT ).split( "/" ) );
				mkdirSync( path.dirname( target ), { recursive: true } );
				await writeFile( target, variant );
				if ( !flags.out && isPublicationOpen() ) claimPublicFile( target );
				if ( store ) await storeOutputs( store, action, key );
				if ( back ) {
					const mirror = path.join( flags.roundtrip, ...publicPath.split( "/" ) );
					mkdirSync( path.dirname( mirror ), { recursive: true } );
					writeFileSync( mirror, back );
				}
				sourceBytes += source.length;
				variantBytes += variant.length;
				if ( (encoded + cached) % 500 === 0 ) {
					console.log( `${encoded + cached} of ${textures.length} textures produced` );
				}
			}
		} finally {
			rmSync( scratch, { recursive: true, force: true } );
		}
	}
	try {
		await Promise.all( Array.from( { length: flags.jobs }, worker ) );
	} finally {
		if ( store && !flags.store ) await store.shutdown();
	}
	const mb = bytes => (bytes / 1048576).toFixed( 1 );
	console.log(
		`texture variants (astc 4x4 ${PRESET}): ${textures.length} textures, ${stored} from the action store ` +
			`(${placed} laid down), ${encoded} encoded, ${cached} from the encoder cache, ` +
			`${unaligned} under one block kept their original, ${masked} carry a picking mask, ` +
			`${mb( sourceBytes )} MB BC -> ${mb( variantBytes )} MB ASTC, ` +
			`${((performance.now() - startedAt) / 1000).toFixed( 1 )} s`
	);
	return { textures: textures.length, stored, placed, encoded, cached, unaligned, masked, sourceBytes, variantBytes };
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	await buildTextureVariants( options( process.argv.slice( 2 ) ) );
}

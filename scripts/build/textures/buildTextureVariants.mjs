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
encoder's, keyed by the source's SHA-256 and by the code hash of this
module and the decoder it imports, so a fix to either recomputes every
mask while an unchanged texture is never decoded again. Cache records are
written whole (a temp name, then a rename). WebGPU requires a
block-compressed level 0 to be whole blocks, so a texture under 4x4 keeps
its original.

Each texture is also an action (graph/action.mjs, plan slice B2): keyed
on its source bytes, this module's code closure, the encoder version and
the preset, with its variant as the one output. A texture whose key has a
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
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { buildJobs } from "../shared/buildParallelism.mjs";
import { codeHash } from "../shared/codeStamp.mjs";
import {
	existsSync,
	mkdirSync,
	mkdtempSync,
	readdirSync,
	readFileSync,
	renameSync,
	rmSync,
	writeFileSync
} from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { defineAction, outputPath } from "../graph/action.mjs";
import { keyOf, materialize, openStore, readRecord, storeOutputs } from "../graph/store.mjs";
import { CLIENT_PUBLIC_ROOT, generatedPath } from "../../lib/generatedRoot.mjs";
import { claimPublicFile, isPublicationOpen } from "../shared/publicationLedger.mjs";
import { ENCODERS, encoderPath } from "./encoders.mjs";
import {
	decodeNativeTexture,
	decodeNativeTextureLevel
} from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";
import { textureVariantPath } from "../../../apps/client-next/src/engine/foundation/assets/texture-variants.ts";
import { compareText } from "../shared/textOrder.mjs";

const run = promisify( execFile );

const NTX_MAGIC = 0x3158544e;
const NTX_HEADER_BYTES = 20;
const NTX_ASTC_4X4 = 0x34345341; // "AS44", native-texture.ts
const NTX_PICK_ALPHA = 0x38414b50; // "PKA8", native-texture.ts
const ASTC_BLOCK = 4;
const D3DFMT_A8R8G8B8 = 21;
const ASTC_HEADER_BYTES = 16;
const ASTC_MAGIC = 0x5ca1ab13;
const TGA_HEADER_BYTES = 18;
// Uncompressed true colour, 32 bits, top-left origin with 8 alpha bits.
const TGA_TRUE_COLOUR = 2;
const TGA_DESCRIPTOR = 0x28;
const KTX_HEADER_BYTES = 64;
const GL_UNSIGNED_BYTE = 0x1401;
const GL_RGB = 0x1907;
const GL_RGBA = 0x1908;
const GL_RED = 0x1903;
const GL_RG = 0x8227;
// Bytes a texel for each format astcenc writes an LDR decode in: it picks
// the fewest channels that hold the image. A grey image is RED (grey) or
// RG (grey, then alpha).
const KTX_CHANNELS = { [GL_RED]: 1, [GL_RG]: 2, [GL_RGB]: 3, [GL_RGBA]: 4 };
const PRESET = "-medium";
const VARIANT = "astc";
const DECODER_DIGEST_CHARS = 16;
const MODULE_FILE = fileURLToPath( import.meta.url );

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
tga

An RGBA level as a 32-bit top-left TGA (astcenc's simplest input).
================
*/
function tga( rgba, width, height ) {
	const out = Buffer.alloc( TGA_HEADER_BYTES + width * height * 4 );
	out[2] = TGA_TRUE_COLOUR;
	out.writeUInt16LE( width, 12 );
	out.writeUInt16LE( height, 14 );
	out[16] = 32;
	out[17] = TGA_DESCRIPTOR;
	for ( let i = 0; i < width * height; i++ ) {
		const at = TGA_HEADER_BYTES + i * 4;
		out[at] = rgba[i * 4 + 2];
		out[at + 1] = rgba[i * 4 + 1];
		out[at + 2] = rgba[i * 4];
		out[at + 3] = rgba[i * 4 + 3];
	}
	return out;
}

/*
================
ktxPixels

The BGRA pixels of the uncompressed KTX astcenc decodes to: RGBA8, RGB8,
or for a grey image RED8 (grey) or RG8 (grey, alpha); a missing alpha
reads 255.
Rows run top first.
================
*/
function ktxPixels( bytes, width, height ) {
	const glType = bytes.readUInt32LE( 16 ), glFormat = bytes.readUInt32LE( 24 );
	const channels = KTX_CHANNELS[glFormat] ?? 0;
	if ( bytes.subarray( 1, 7 ).toString( "latin1" ) !== "KTX 11" || glType !== GL_UNSIGNED_BYTE || !channels ) {
		throw new Error(
			`Unexpected decoded KTX layout (type ${glType.toString( 16 )}, format ${glFormat.toString( 16 )})`
		);
	}
	if ( bytes.readUInt32LE( 36 ) !== width || bytes.readUInt32LE( 40 ) !== height ) {
		throw new Error( "Decoded KTX size differs" );
	}
	// The stored image size gives the row stride: astcenc's rows may or may
	// not carry KTX's four-byte padding, so neither is assumed.
	const start = KTX_HEADER_BYTES + bytes.readUInt32LE( 60 ) + 4, imageBytes = bytes.readUInt32LE( start - 4 );
	const rowBytes = imageBytes / height;
	if ( !Number.isInteger( rowBytes ) || rowBytes < width * channels || start + imageBytes > bytes.length ) {
		throw new Error( `Decoded KTX image size ${imageBytes} does not hold ${width}x${height}x${channels}` );
	}
	const out = Buffer.alloc( width * height * 4 );
	for ( let y = 0; y < height; y++ ) {
		for ( let x = 0; x < width; x++ ) {
			const from = start + y * rowBytes + x * channels, to = (y * width + x) * 4;
			if ( channels <= 2 ) {
				out[to] = out[to + 1] = out[to + 2] = bytes[from];
				out[to + 3] = channels === 2 ? bytes[from + 1] : 255;
				continue;
			}
			out[to] = bytes[from + 2];
			out[to + 1] = bytes[from + 1];
			out[to + 2] = bytes[from];
			out[to + 3] = channels === 4 ? bytes[from + 3] : 255;
		}
	}
	return out;
}

/*
================
ntx
================
*/
function ntx( width, height, code, levels ) {
	const header = Buffer.alloc( NTX_HEADER_BYTES );
	header.writeUInt32LE( NTX_MAGIC, 0 );
	header.writeUInt32LE( width, 4 );
	header.writeUInt32LE( height, 8 );
	header.writeUInt32LE( code, 12 );
	header.writeUInt32LE( levels.length, 16 );
	return Buffer.concat( [ header, ...levels ] );
}

/*
================
pickAlphaSection

The source's level-0 alpha as a trailing NTX section, or an empty buffer
when every texel is opaque.
================
*/
export function pickAlphaSection( texture ) {
	const rgba = decodeNativeTextureLevel( texture, 0 ), alpha = Buffer.alloc( texture.width * texture.height );
	let opaque = true;
	for ( let i = 0; i < alpha.length; i++ ) {
		alpha[i] = rgba[i * 4 + 3];
		if ( alpha[i] !== 255 ) opaque = false;
	}
	if ( opaque ) return Buffer.alloc( 0 );
	const tag = Buffer.alloc( 4 );
	tag.writeUInt32LE( NTX_PICK_ALPHA, 0 );
	return Buffer.concat( [ tag, alpha ] );
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
every builder stamp uses): this module, which writes the tag, the
opacity rule and the byte order (pickAlphaSection), and the client's NTX
decoder it imports. A change to either serves no stale mask.
================
*/
export function maskCodeHash() {
	return codeHash( import.meta.url );
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
encodeTexture

One native texture to its ASTC 4x4 NTX and, when asked, its decoded BGRA NTX.
================
*/
async function encodeTexture( astcenc, source, scratch, roundtrip ) {
	const texture = decodeNativeTexture( new Uint8Array( source ) );
	const astc = [], decoded = [];
	for ( let level = 0; level < texture.levels.length; level++ ) {
		const width = Math.max( 1, texture.width >> level ), height = Math.max( 1, texture.height >> level );
		const input = path.join( scratch, `${level}.tga` ), output = path.join( scratch, `${level}.astc` );
		writeFileSync( input, tga( decodeNativeTextureLevel( texture, level ), width, height ) );
		await run( astcenc, [ "-cl", input, output, "4x4", PRESET, "-silent" ] );
		const file = readFileSync( output );
		if ( file.readUInt32LE( 0 ) !== ASTC_MAGIC || file[4] !== 4 || file[5] !== 4 ) {
			throw new Error( "Unexpected astcenc output" );
		}
		astc.push( file.subarray( ASTC_HEADER_BYTES ) );
		if ( roundtrip ) {
			const back = path.join( scratch, `${level}.back.ktx` );
			await run( astcenc, [ "-dl", output, back, "-silent" ] );
			decoded.push( ktxPixels( readFileSync( back ), width, height ) );
		}
	}
	return {
		variant: ntx( texture.width, texture.height, NTX_ASTC_4X4, astc ),
		roundtrip: roundtrip ? ntx( texture.width, texture.height, D3DFMT_A8R8G8B8, decoded ) : null
	};
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
export function textureVariantAction( publicPath, roots ) {
	return defineAction( {
		name: `texture-variant:${publicPath}`,
		inputRoot: roots.source,
		inputs: [ publicPath.slice( 1 ) ],
		outputRoot: roots.output,
		outputs: [ textureVariantPath( publicPath, VARIANT ).replace( /^\//, "" ) ],
		code: [ MODULE_FILE ],
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
		generatedPath( "intermediate", "texture-variants", cacheName );
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
					variantBytes += record.outputs[0].size;
					continue;
				}
				const source = await readFile( file ), digest = createHash( "sha256" ).update( source ).digest( "hex" );
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

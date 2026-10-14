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
(native-texture.ts NTX_PICK_ALPHA). The mask is taken from the source at
every run, outside the encoder cache. WebGPU requires a block-compressed
level 0 to be whole blocks, so a texture under 4x4 keeps its original.

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
import { cpus } from "node:os";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { CLIENT_PUBLIC_ROOT, generatedPath } from "../../lib/generatedRoot.mjs";
import { claimPublicFile, isPublicationOpen } from "../shared/publicationLedger.mjs";
import { ENCODERS, encoderPath } from "./encoders.mjs";
import {
	decodeNativeTexture,
	decodeNativeTextureLevel
} from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";
import { textureVariantPath } from "../../../apps/client-next/src/engine/foundation/assets/texture-variants.ts";

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

/*
================
options
================
*/
function options( argv ) {
	const out = {
		jobs: Math.max( 1, cpus().length - 1 ),
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
	return out.sort( ( a, b ) => a[1].localeCompare( b[1] ) );
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
		jobs: Math.max( 1, cpus().length - 1 ),
		only: "/assets/",
		limit: Infinity,
		out: null,
		roundtrip: null,
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
	const outputRoot = flags.out ? path.join( flags.out, "client-public" ) : CLIENT_PUBLIC_ROOT;
	mkdirSync( cacheRoot, { recursive: true } );
	const textures = nativeTextures( CLIENT_PUBLIC_ROOT, flags.only ).slice( 0, flags.limit );
	let next = 0, encoded = 0, cached = 0, unaligned = 0, masked = 0, sourceBytes = 0, variantBytes = 0;
	const startedAt = performance.now();
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
				const source = readFileSync( file ), digest = createHash( "sha256" ).update( source ).digest( "hex" );
				const texture = decodeNativeTexture( new Uint8Array( source ) );
				if ( texture.width % ASTC_BLOCK || texture.height % ASTC_BLOCK ) {
					unaligned++;
					continue;
				}
				const cachedVariant = path.join( cacheRoot, `${digest}.ntx` ),
					cachedBack = path.join( cacheRoot, `${digest}.back.ntx` );
				let variant, back = null;
				if ( existsSync( cachedVariant ) && (!flags.roundtrip || existsSync( cachedBack )) ) {
					variant = readFileSync( cachedVariant );
					if ( flags.roundtrip ) back = readFileSync( cachedBack );
					cached++;
				} else {
					const result = await encodeTexture( astcenc, source, scratch, !!flags.roundtrip );
					variant = result.variant;
					back = result.roundtrip;
					writeFileSync( cachedVariant, variant );
					if ( back ) writeFileSync( cachedBack, back );
					encoded++;
				}
				const mask = pickAlphaSection( texture );
				if ( mask.length ) masked++;
				variant = Buffer.concat( [ variant, mask ] );
				const target = path.join( outputRoot, ...textureVariantPath( publicPath, VARIANT ).split( "/" ) );
				mkdirSync( path.dirname( target ), { recursive: true } );
				writeFileSync( target, variant );
				if ( !flags.out && isPublicationOpen() ) claimPublicFile( target );
				if ( back ) {
					const mirror = path.join( flags.roundtrip, ...publicPath.split( "/" ) );
					mkdirSync( path.dirname( mirror ), { recursive: true } );
					writeFileSync( mirror, back );
				}
				sourceBytes += source.length;
				variantBytes += variant.length;
				if ( (encoded + cached) % 500 === 0 ) {
					console.log( `${encoded + cached}/${textures.length} textures` );
				}
			}
		} finally {
			rmSync( scratch, { recursive: true, force: true } );
		}
	}
	await Promise.all( Array.from( { length: flags.jobs }, worker ) );
	const mb = bytes => (bytes / 1048576).toFixed( 1 );
	console.log(
		`texture variants (astc 4x4 ${PRESET}): ${textures.length} textures, ${encoded} encoded, ${cached} cached, ` +
			`${unaligned} under one block kept their original, ${masked} carry a picking mask, ` +
			`${mb( sourceBytes )} MB BC -> ${mb( variantBytes )} MB ASTC, ` +
			`${((performance.now() - startedAt) / 1000).toFixed( 1 )} s`
	);
	return { textures: textures.length, encoded, cached, unaligned, masked, sourceBytes, variantBytes };
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	await buildTextureVariants( options( process.argv.slice( 2 ) ) );
}

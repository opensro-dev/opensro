/*
===========================================================================

textureVariantEncode.mjs - the bytes of one ASTC texture variant

Port-only. Everything that decides a variant's bytes and its picking
mask, and nothing else: the client's NTX decoder, the TGA hand-off to
astcenc, the ASTC block layout and container, and the mask's opacity rule.
buildTextureVariants.mjs owns the pass around it (the walk, the caches,
the action store and the publication claims).

This module is the code an action key and the mask cache name cover
(codeStamp.mjs codeClosure). Before the split they covered the whole pass
module, whose closure also holds the action store, the publication ledgers
and the path resolvers: on 2026-11-20 a change to world/paths.mjs alone
(#3948) re-keyed all 19003 textures of release r203. Keep build plumbing
out of this file's imports, or every plumbing change throws the texture
caches away again.

===========================================================================
*/
import { execFile } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import {
	astcBlocks,
	NTX_ASTC_4X4,
	ntxContainer,
	pickAlphaSection as alphaSection,
	tgaImage
} from "./textureContainers.mjs";
import {
	decodeNativeTexture,
	decodeNativeTextureLevel
} from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";

const run = promisify( execFile );

export const ASTC_BLOCK = 4;
export const PRESET = "-medium";
const D3DFMT_A8R8G8B8 = 21;
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

// The entry an action key and the mask cache name hash (see the banner).
export const ENCODE_MODULE = fileURLToPath( import.meta.url );

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
pickAlphaSection

The source's level-0 alpha as a trailing NTX section, or an empty buffer
when every texel is opaque.
================
*/
export function pickAlphaSection( texture ) {
	return alphaSection( decodeNativeTextureLevel( texture, 0 ), texture.width, texture.height );
}

/*
================
encodeTexture

One native texture to its ASTC 4x4 NTX and, when asked, its decoded BGRA
NTX. astcenc is the pinned encoder's path (encoders.mjs); its version is
in the action's params, not in this module's code.
================
*/
export async function encodeTexture( astcenc, source, scratch, roundtrip ) {
	const texture = decodeNativeTexture( new Uint8Array( source ) );
	const astc = [], decoded = [];
	for ( let level = 0; level < texture.levels.length; level++ ) {
		const width = Math.max( 1, texture.width >> level ), height = Math.max( 1, texture.height >> level );
		const input = path.join( scratch, `${level}.tga` ), output = path.join( scratch, `${level}.astc` );
		writeFileSync( input, tgaImage( decodeNativeTextureLevel( texture, level ), width, height ) );
		await run( astcenc, [ "-cl", input, output, "4x4", PRESET, "-silent" ] );
		astc.push( astcBlocks( readFileSync( output ), ASTC_BLOCK ) );
		if ( roundtrip ) {
			const back = path.join( scratch, `${level}.back.ktx` );
			await run( astcenc, [ "-dl", output, back, "-silent" ] );
			decoded.push( ktxPixels( readFileSync( back ), width, height ) );
		}
	}
	return {
		variant: ntxContainer( texture.width, texture.height, NTX_ASTC_4X4, astc ),
		roundtrip: roundtrip ? ntxContainer( texture.width, texture.height, D3DFMT_A8R8G8B8, decoded ) : null
	};
}

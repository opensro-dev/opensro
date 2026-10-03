/*
===========================================================================

native-texture.ts - admission and fallback decoding for retained native mips

The NTX container preserves the source format and a complete mip chain from
the build-time D3DX loader. GPU-capable devices upload the blocks unchanged;
devices without BC support decode one mip at a time without retaining RGBA.
The format rules also serve CPU residency accounting and input validation.

===========================================================================
*/
import type { NativeTexture } from "@/engine/contracts/texture";

const NTX_MAGIC = 0x3158544e;
const HEADER_BYTES = 20;
const MAX_DIMENSION = 8192;
const MAX_TEXTURE_BYTES = 67108864;
const BLOCK_SIDE = 4;
const COLOR_BLOCK_BYTES = 8;
const ALPHA_BLOCK_BYTES = 16;
const CHANNELS = 4;
const D3DFMT_BGRA = 21;
const D3DFMT_DXT1 = 0x31545844;
const D3DFMT_DXT3 = 0x33545844;
const D3DFMT_DXT5 = 0x35545844;

export const NATIVE_TEXTURE_MIME = "application/x-sro-texture";

/*
================
nativeTextureBlockBytes

Zero denotes uncompressed BGRA. Every compressed format uses 4x4 blocks,
including mip levels whose logical dimensions are smaller than one block.
================
*/
export function nativeTextureBlockBytes( format: NativeTexture["format"] ): number {
	if ( format === "bgra8unorm" ) return 0;
	if ( format === "bc1-rgba-unorm" ) return COLOR_BLOCK_BYTES;
	if ( format === "bc2-rgba-unorm" || format === "bc3-rgba-unorm" ) return ALPHA_BLOCK_BYTES;
	throw Error( "Unsupported native texture format" );
}

/*
================
nativeTextureLevelBytes

Compute the exact payload size without allocating a decoded surface.
================
*/
export function nativeTextureLevelBytes( format: NativeTexture["format"], width: number, height: number ): number {
	const blockBytes = nativeTextureBlockBytes( format );
	return blockBytes ?
		Math.ceil( width / BLOCK_SIDE ) * Math.ceil( height / BLOCK_SIDE ) * blockBytes :
		width * height * CHANNELS;
}

/*
================
validateNativeTexture

Validate retained resources again at GPU admission: callers can construct
contracts directly, and corrupt dimensions must not reach createTexture.
================
*/
export function validateNativeTexture( texture: NativeTexture ): number {
	const { width, height, levels } = texture;
	if (
		!Number.isInteger( width ) || !Number.isInteger( height ) || width < 1 || height < 1 ||
		width > MAX_DIMENSION || height > MAX_DIMENSION || (width & (width - 1)) || (height & (height - 1)) ||
		levels.length !== 1 + Math.floor( Math.log2( Math.max( width, height ) ) )
	) throw Error( "Invalid native texture dimensions or mip count" );
	let bytes = 0;
	for ( let level = 0; level < levels.length; level++ ) {
		const data = levels[level]!;
		const expected = nativeTextureLevelBytes(
			texture.format,
			Math.max( 1, width >> level ),
			Math.max( 1, height >> level )
		);
		if ( !(data instanceof Uint8Array) || data.byteLength !== expected ) {
			throw Error( "Invalid native texture mip extent" );
		}
		bytes += expected;
	}
	if ( bytes > MAX_TEXTURE_BYTES ) throw Error( "Native texture exceeds byte budget" );
	return bytes;
}

/*
================
decodeNativeTexture

Copy each level into its own transferable buffer. A view into the complete
asset would retain unrelated bytes or detach siblings when crossing workers.
================
*/
export function decodeNativeTexture( bytes: Uint8Array ): NativeTexture {
	if ( bytes.byteLength < HEADER_BYTES || bytes.byteLength > MAX_TEXTURE_BYTES + HEADER_BYTES ) {
		throw Error( "Invalid native texture extent" );
	}
	const view = new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength );
	if ( view.getUint32( 0, true ) !== NTX_MAGIC ) throw Error( "Invalid native texture header" );
	const width = view.getUint32( 4, true ), height = view.getUint32( 8, true );
	const code = view.getUint32( 12, true ), count = view.getUint32( 16, true );
	const format: NativeTexture["format"] = code === D3DFMT_BGRA ?
		"bgra8unorm" :
		code === D3DFMT_DXT1 ?
		"bc1-rgba-unorm" :
		code === D3DFMT_DXT3 ?
		"bc2-rgba-unorm" :
		code === D3DFMT_DXT5 ?
		"bc3-rgba-unorm" :
		(() => {
			throw Error( "Unsupported native texture format" );
		})();
	if (
		width < 1 || height < 1 || width > MAX_DIMENSION || height > MAX_DIMENSION ||
		(width & (width - 1)) || (height & (height - 1)) ||
		count !== 1 + Math.floor( Math.log2( Math.max( width, height ) ) )
	) {
		throw Error( "Invalid native texture dimensions or mip count" );
	}
	const levels: Uint8Array[] = [];
	let offset = HEADER_BYTES;
	for ( let level = 0; level < count; level++ ) {
		const size = nativeTextureLevelBytes( format, Math.max( 1, width >> level ), Math.max( 1, height >> level ) );
		if ( offset + size > bytes.byteLength ) throw Error( "Truncated native texture" );
		levels.push( bytes.slice( offset, offset + size ) );
		offset += size;
	}
	if ( offset !== bytes.byteLength ) throw Error( "Invalid native texture extent" );
	return { kind: "native-texture", width, height, format, levels };
}

/*
================
colorPalette

BC1's endpoint order selects transparent black. BC2 and BC3 always use four
opaque RGB colors; their independent alpha block supplies transparency.
Writes into the caller's palette: this runs once per 4x4 block.
================
*/
function colorPalette( data: DataView, offset: number, transparent: boolean, palette: Uint8Array ): Uint8Array {
	const first = data.getUint16( offset, true ), second = data.getUint16( offset + 2, true );
	for ( let index = 0; index < 2; index++ ) {
		const value = index === 0 ? first : second, at = index * CHANNELS;
		const red = value >> 11, green = (value >> 5) & 63, blue = value & 31;
		palette[at] = (red << 3) | (red >> 2);
		palette[at + 1] = (green << 2) | (green >> 4);
		palette[at + 2] = (blue << 3) | (blue >> 2);
		palette[at + 3] = 255;
	}
	const threeColors = transparent && first <= second;
	for ( let channel = 0; channel < 3; channel++ ) {
		const a = palette[channel]!, b = palette[CHANNELS + channel]!;
		palette[2 * CHANNELS + channel] = Math.floor( threeColors ? (a + b) / 2 : (2 * a + b) / 3 );
		palette[3 * CHANNELS + channel] = threeColors ? 0 : Math.floor( (a + 2 * b) / 3 );
	}
	palette[2 * CHANNELS + 3] = 255;
	palette[3 * CHANNELS + 3] = threeColors ? 0 : 255;
	return palette;
}

/*
================
bc3Alpha

Decode the three-bit alpha selector without a 48-bit bitwise operation.
JavaScript bitwise operators truncate to 32 bits, so read its two-byte window.
================
*/
function bc3Alpha( bytes: Uint8Array, offset: number, pixel: number ): number {
	const first = bytes[offset]!, second = bytes[offset + 1]!;
	const bit = pixel * 3, byte = offset + 2 + Math.floor( bit / 8 );
	const selector = ((bytes[byte]! | ((bytes[byte + 1] ?? 0) << 8)) >>> (bit % 8)) & 7;
	if ( selector === 0 ) return first;
	if ( selector === 1 ) return second;
	if ( first > second ) return Math.floor( ((8 - selector) * first + (selector - 1) * second) / 7 );
	if ( selector === 6 ) return 0;
	if ( selector === 7 ) return 255;
	return Math.floor( ((6 - selector) * first + (selector - 1) * second) / 5 );
}

/*
================
decodeNativeTextureLevel

Produce a transient RGBA surface for an adapter without BC support. The
caller releases this array after upload and retains the compressed source
for device restoration. Cropping only removes padding outside the mip.
================
*/
export function decodeNativeTextureLevel( texture: NativeTexture, level: number ): Uint8Array {
	if ( !Number.isInteger( level ) || level < 0 || level >= texture.levels.length ) {
		throw Error( "Invalid native mip level" );
	}
	validateNativeTexture( texture );
	const width = Math.max( 1, texture.width >> level ), height = Math.max( 1, texture.height >> level );
	return decodeNativeSurface( { width, height, format: texture.format, bytes: texture.levels[level]! } );
}

/*
================
decodeNativeSurface

Share block decoding with the terrain DDS reader. A surface may be a
non-power-of-two DDS rectangle or one mip of a validated native resource.
Reject its allocation size before decoding any blocks.
================
*/
export function decodeNativeSurface( surface: {
	readonly width: number;
	readonly height: number;
	readonly format: NativeTexture["format"];
	readonly bytes: Uint8Array;
} ): Uint8Array {
	const { width, height, format, bytes } = surface;
	if (
		!Number.isInteger( width ) || !Number.isInteger( height ) ||
		width < 1 || height < 1 || width > MAX_DIMENSION || height > MAX_DIMENSION ||
		width * height * CHANNELS > MAX_TEXTURE_BYTES ||
		bytes.byteLength !== nativeTextureLevelBytes( format, width, height )
	) {
		throw Error( "Invalid native surface dimensions or payload" );
	}
	const result = new Uint8Array( width * height * CHANNELS );
	if ( format === "bgra8unorm" ) {
		for ( let pixel = 0; pixel < result.length; pixel += CHANNELS ) {
			const blue = bytes[pixel]!;
			result[pixel] = bytes[pixel + 2]!;
			result[pixel + 1] = bytes[pixel + 1]!;
			result[pixel + 2] = blue;
			result[pixel + 3] = bytes[pixel + 3]!;
		}
		return result;
	}
	const blockBytes = nativeTextureBlockBytes( format ), columns = Math.ceil( width / BLOCK_SIDE );
	const data = new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength );
	// One palette for the surface: a per-block array and a per-pixel subarray
	// view were the asset worker's largest garbage while decoding regions.
	const scratch = new Uint8Array( BLOCK_SIDE * CHANNELS );
	for ( let y = 0; y < height; y += BLOCK_SIDE ) {
		for ( let x = 0; x < width; x += BLOCK_SIDE ) {
			const offset = ((y / BLOCK_SIDE) * columns + x / BLOCK_SIDE) * blockBytes;
			const colors = offset + (format === "bc1-rgba-unorm" ? 0 : COLOR_BLOCK_BYTES);
			const palette = colorPalette( data, colors, format === "bc1-rgba-unorm", scratch );
			const selectors = data.getUint32( colors + CHANNELS, true );
			for ( let py = 0; py < BLOCK_SIDE && y + py < height; py++ ) {
				for ( let px = 0; px < BLOCK_SIDE && x + px < width; px++ ) {
					const pixel = py * BLOCK_SIDE + px, selector = (selectors >>> (pixel * 2)) & 3;
					const target = ((y + py) * width + x + px) * CHANNELS;
					const from = selector * CHANNELS;
					result[target] = palette[from]!;
					result[target + 1] = palette[from + 1]!;
					result[target + 2] = palette[from + 2]!;
					result[target + 3] = palette[from + 3]!;
					if ( format === "bc2-rgba-unorm" ) {
						result[target + 3] = ((bytes[offset + Math.floor( pixel / 2 )]! >>> ((pixel % 2) * 4)) & 15) *
							17;
					} else if ( format === "bc3-rgba-unorm" ) {
						result[target + 3] = bc3Alpha( bytes, offset, pixel );
					}
				}
			}
		}
	}
	return result;
}

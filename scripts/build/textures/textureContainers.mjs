/*
===========================================================================

textureContainers.mjs - the file formats the texture builders write and read

The NTX container (the client's native texture, decoded by
apps/client-next/src/engine/foundation/assets/native-texture.ts), its
trailing picking-alpha section, the TGA an encoder reads and the .astc an
encoder writes. The texture variant pass, the HD pass and the world block
textures each carried their own copy of these writers; they live here so
the byte layout has one owner on the build side.

===========================================================================
*/

export const NTX_MAGIC = 0x3158544e; // "NTX1"
export const NTX_HEADER_BYTES = 20;
// Port-only variant codes, as native-texture.ts decodes them.
export const NTX_ASTC_4X4 = 0x34345341; // "AS44"
export const NTX_BC7 = 0x55374342; // "BC7U", the HD variant's code (#2061)
// A variant's optional trailing picking mask: this tag, then width x height
// alpha bytes of level 0 (NativeTexture.pickAlpha).
export const NTX_PICK_ALPHA = 0x38414b50; // "PKA8"

const OPAQUE = 255;
const RGBA_CHANNELS = 4;
const TAG_BYTES = 4;
const TGA_HEADER_BYTES = 18;
// Uncompressed true colour, 32 bits, top-left origin with 8 alpha bits.
const TGA_TRUE_COLOUR = 2;
const TGA_BITS_PER_PIXEL = 32;
const TGA_DESCRIPTOR = 0x28;
const ASTC_HEADER_BYTES = 16;
const ASTC_MAGIC = 0x5ca1ab13;

/*
================
ntxContainer

An NTX file: the header (magic, width, height, format code, level count),
then the levels as given, then any trailing sections.
================
*/
export function ntxContainer( width, height, code, levels, trailer = [] ) {
	const header = Buffer.alloc( NTX_HEADER_BYTES );
	header.writeUInt32LE( NTX_MAGIC, 0 );
	header.writeUInt32LE( width, 4 );
	header.writeUInt32LE( height, 8 );
	header.writeUInt32LE( code, 12 );
	header.writeUInt32LE( levels.length, 16 );
	return Buffer.concat( [ header, ...levels, ...trailer ] );
}

/*
================
pickAlphaSection

The alpha of an RGBA level 0 as a trailing NTX section, or an empty
buffer when every texel is opaque: the client then picks by the box.
================
*/
export function pickAlphaSection( rgba, width, height ) {
	const alpha = Buffer.alloc( width * height );
	let opaque = true;
	for ( let i = 0; i < alpha.length; i++ ) {
		alpha[i] = rgba[i * RGBA_CHANNELS + 3];
		if ( alpha[i] !== OPAQUE ) opaque = false;
	}
	if ( opaque ) return Buffer.alloc( 0 );
	const tag = Buffer.alloc( TAG_BYTES );
	tag.writeUInt32LE( NTX_PICK_ALPHA, 0 );
	return Buffer.concat( [ tag, alpha ] );
}

/*
================
tgaImage

An RGBA level as the uncompressed 32-bit TGA the encoders read (BGRA
order, top-left origin).
================
*/
export function tgaImage( rgba, width, height ) {
	const out = Buffer.alloc( TGA_HEADER_BYTES + width * height * RGBA_CHANNELS );
	out[2] = TGA_TRUE_COLOUR;
	out.writeUInt16LE( width, 12 );
	out.writeUInt16LE( height, 14 );
	out[16] = TGA_BITS_PER_PIXEL;
	out[17] = TGA_DESCRIPTOR;
	for ( let i = 0; i < width * height; i++ ) {
		const at = TGA_HEADER_BYTES + i * RGBA_CHANNELS;
		out[at] = rgba[i * RGBA_CHANNELS + 2];
		out[at + 1] = rgba[i * RGBA_CHANNELS + 1];
		out[at + 2] = rgba[i * RGBA_CHANNELS];
		out[at + 3] = rgba[i * RGBA_CHANNELS + 3];
	}
	return out;
}

/*
================
astcBlocks

The block payload of an astcenc .astc file, after checking it holds the
block size asked for.
================
*/
export function astcBlocks( file, block ) {
	if ( file.readUInt32LE( 0 ) !== ASTC_MAGIC || file[4] !== block || file[5] !== block ) {
		throw new Error( "Unexpected astcenc output" );
	}
	return file.subarray( ASTC_HEADER_BYTES );
}

/*
================
tgaPixels

The RGBA pixels of the uncompressed 32-bit TGAs texconv writes, rows
top first whatever origin the header names.
================
*/
export function tgaPixels( bytes ) {
	if (
		bytes.length < TGA_HEADER_BYTES || bytes[2] !== TGA_TRUE_COLOUR || bytes[1] !== 0 ||
		bytes[16] !== TGA_BITS_PER_PIXEL
	) {
		throw new Error( "Unexpected TGA layout" );
	}
	const width = bytes.readUInt16LE( 12 ), height = bytes.readUInt16LE( 14 );
	// Pixels follow the header and its image ID; texconv appends a TGA 2.0
	// footer after them, so the file runs longer than the pixel bytes.
	const pixels = TGA_HEADER_BYTES + bytes[0];
	if ( bytes.length < pixels + width * height * 4 ) {
		throw new Error( `TGA size ${bytes.length} does not hold ${width}x${height}x4` );
	}
	const topOrigin = (bytes[17] & 0x20) !== 0, rightOrigin = (bytes[17] & 0x10) !== 0;
	const rgba = Buffer.alloc( width * height * 4 );
	for ( let y = 0; y < height; y++ ) {
		const sourceY = topOrigin ? y : height - 1 - y;
		for ( let x = 0; x < width; x++ ) {
			const sourceX = rightOrigin ? width - 1 - x : x;
			const from = pixels + (sourceY * width + sourceX) * 4, to = (y * width + x) * 4;
			rgba[to] = bytes[from + 2];
			rgba[to + 1] = bytes[from + 1];
			rgba[to + 2] = bytes[from];
			rgba[to + 3] = bytes[from + 3];
		}
	}
	return { rgba, width, height };
}

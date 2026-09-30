/*
===========================================================================

dds.ts - bounded admission of the MAPT-embedded DDS surface

Terrain embeds a single DXT1 image. Header validation stays here, while
native-texture owns the shared BC palette and block decoding rules.

===========================================================================
*/
import { decodeNativeSurface, nativeTextureLevelBytes } from "./native-texture";

const DDS_HEADER_BYTES = 128;
const DDS_MAGIC = 0x20534444;
const DDS_STRUCTURE_BYTES = 124;
const PIXEL_FORMAT_BYTES = 32;
const FOURCC_FLAG = 4;
const DXT1 = 0x31545844;
const MAX_DIMENSION = 8192;
const MAX_IMAGE_BYTES = 67108864;
const CHANNELS = 4;

/*
================
decodeDxt1

Accept the authored base level, including non-square final blocks. Additional
DDS mip bytes are left unused because the existing terrain owner generates
its GPU mip chain after bitmap upload.
================
*/
export function decodeDxt1( bytes: Uint8Array, limit = MAX_IMAGE_BYTES ) {
	if ( bytes.byteLength < DDS_HEADER_BYTES ) throw Error( "Truncated DDS header" );
	const view = new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength );
	if (
		view.getUint32( 0, true ) !== DDS_MAGIC ||
		view.getUint32( 4, true ) !== DDS_STRUCTURE_BYTES ||
		view.getUint32( 76, true ) !== PIXEL_FORMAT_BYTES ||
		!(view.getUint32( 80, true ) & FOURCC_FLAG) ||
		view.getUint32( 84, true ) !== DXT1 ||
		view.getUint32( 112, true ) !== 0 || view.getUint32( 24, true ) > 1
	) {
		throw Error( "Unsupported DDS surface" );
	}
	const width = view.getUint32( 16, true ), height = view.getUint32( 12, true );
	const size = nativeTextureLevelBytes( "bc1-rgba-unorm", width, height );
	if (
		!width || !height || width > MAX_DIMENSION || height > MAX_DIMENSION ||
		width * height * CHANNELS > limit || DDS_HEADER_BYTES + size > bytes.byteLength
	) {
		throw Error( "DDS dimensions or payload exceed budget" );
	}
	const rgba = decodeNativeSurface( {
		width,
		height,
		format: "bc1-rgba-unorm",
		bytes: bytes.subarray( DDS_HEADER_BYTES, DDS_HEADER_BYTES + size )
	} );
	return { width, height, pixels: new Uint8ClampedArray( rgba.buffer as ArrayBuffer ) };
}

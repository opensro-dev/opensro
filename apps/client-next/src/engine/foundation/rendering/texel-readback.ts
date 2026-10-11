/*
===========================================================================

texel-readback.ts - a GPU texture copy unpacked to 8-bit RGBA

copyTextureToBuffer pads each row to 256 bytes. This unpacks those rows
into tightly packed RGBA bytes for an ImageData or a PNG, from the scene
formats a portrait target uses: rgba8unorm, bgra8unorm, or rgba16float when
the HDR stage is on (clamped to 0..1, alpha kept). Pure.

===========================================================================
*/

// COPY_ROW_ALIGNMENT is WebGPU's bytesPerRow alignment for a buffer copy.
export const COPY_ROW_ALIGNMENT = 256;

/*
================
texelBytes

Bytes per texel of a readable format, or 0 for one this does not unpack.
================
*/
export function texelBytes( format: string ): number {
	switch ( format ) {
		case "rgba8unorm":
		case "bgra8unorm":
			return 4;
		case "rgba16float":
			return 8;
	}
	return 0;
}

/*
================
paddedRowBytes

The bytesPerRow a copy of this width and format uses.
================
*/
export function paddedRowBytes( width: number, format: string ): number {
	const row = width * texelBytes( format );
	return Math.ceil( row / COPY_ROW_ALIGNMENT ) * COPY_ROW_ALIGNMENT;
}

/*
================
halfToFloat

An IEEE 754 binary16 value.
================
*/
function halfToFloat( half: number ): number {
	const sign = half & 0x8000 ? -1 : 1, exponent = (half >> 10) & 0x1f, fraction = half & 0x3ff;
	if ( exponent === 0 ) return sign * fraction * 2 ** -24;
	if ( exponent === 0x1f ) return fraction ? NaN : sign * Infinity;
	return sign * (1 + fraction / 1024) * 2 ** (exponent - 15);
}

/*
================
unorm8

A float channel clamped to 0..1 and rounded to a byte; NaN is zero.
================
*/
function unorm8( value: number ): number {
	return Number.isNaN( value ) ? 0 : Math.round( Math.min( 1, Math.max( 0, value ) ) * 255 );
}

/*
================
unpackRgba8

Packed RGBA bytes of a width x height copy with the given padded rows.
================
*/
export function unpackRgba8( source: Uint8Array, width: number, height: number, format: string ): Uint8ClampedArray {
	const texel = texelBytes( format ), stride = paddedRowBytes( width, format );
	if ( !texel || !Number.isInteger( width ) || !Number.isInteger( height ) || width < 1 || height < 1 ) {
		throw Error( "Unreadable texture copy" );
	}
	if ( source.byteLength < stride * (height - 1) + width * texel ) throw Error( "Short texture copy" );
	const out = new Uint8ClampedArray( width * height * 4 );
	const halves = format === "rgba16float" ?
		new DataView( source.buffer, source.byteOffset, source.byteLength ) :
		null;
	for ( let y = 0; y < height; y++ ) {
		for ( let x = 0; x < width; x++ ) {
			const from = y * stride + x * texel, to = (y * width + x) * 4;
			if ( halves ) {
				for ( let c = 0; c < 4; c++ ) {
					out[to + c] = unorm8( halfToFloat( halves.getUint16( from + c * 2, true ) ) );
				}
			} else if ( format === "bgra8unorm" ) {
				out[to] = source[from + 2]!;
				out[to + 1] = source[from + 1]!;
				out[to + 2] = source[from]!;
				out[to + 3] = source[from + 3]!;
			} else {
				out.set( source.subarray( from, from + 4 ), to );
			}
		}
	}
	return out;
}

/*
================
opaqueBox

The box of every pixel with alpha above zero in packed RGBA, as
[left, top, right, bottom] with right and bottom exclusive, or null when
nothing is drawn. The 3D viewer frames what it drew (viewer-camera.ts).
================
*/
export function opaqueBox(
	rgba: Uint8ClampedArray,
	width: number,
	height: number
): [number, number, number, number] | null {
	let left = width, top = height, right = 0, bottom = 0;
	for ( let y = 0; y < height; y++ ) {
		for ( let x = 0; x < width; x++ ) {
			if ( rgba[(y * width + x) * 4 + 3] === 0 ) continue;
			if ( x < left ) left = x;
			if ( x >= right ) right = x + 1;
			if ( y < top ) top = y;
			if ( y >= bottom ) bottom = y + 1;
		}
	}
	return right > left ? [ left, top, right, bottom ] : null;
}

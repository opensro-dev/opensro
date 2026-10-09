/*
===========================================================================

pngRgba.mjs - decode the converter's PNGs to RGBA for pixel comparisons

The image converter writes 8-bit RGBA, non-interlaced PNGs. Tests compare
published textures against them pixel for pixel; this reads exactly that
shape and refuses any other, rather than guessing at color types.

===========================================================================
*/
import { inflateSync } from "node:zlib";

const SIGNATURE = Buffer.from( [ 137, 80, 78, 71, 13, 10, 26, 10 ] );
const CHANNELS = 4;

/*
================
decodePngRgba

{ width, height, rgba } for an 8-bit RGBA non-interlaced PNG.
================
*/
export function decodePngRgba( bytes ) {
	if ( !bytes.subarray( 0, 8 ).equals( SIGNATURE ) ) throw Error( "Not a PNG" );
	let offset = 8, width = 0, height = 0;
	const data = [];
	while ( offset < bytes.length ) {
		const length = bytes.readUInt32BE( offset ), type = bytes.toString( "latin1", offset + 4, offset + 8 );
		const body = bytes.subarray( offset + 8, offset + 8 + length );
		if ( type === "IHDR" ) {
			width = body.readUInt32BE( 0 );
			height = body.readUInt32BE( 4 );
			if ( body[8] !== 8 || body[9] !== 6 || body[12] !== 0 ) {
				throw Error( "Only 8-bit RGBA non-interlaced PNGs" );
			}
		} else if ( type === "IDAT" ) {
			data.push( body );
		} else if ( type === "IEND" ) {
			break;
		}
		offset += 12 + length;
	}
	const raw = inflateSync( Buffer.concat( data ) ), stride = width * CHANNELS;
	const rgba = new Uint8Array( stride * height );
	for ( let y = 0; y < height; y++ ) {
		const filter = raw[y * (stride + 1)], row = raw.subarray( y * (stride + 1) + 1, (y + 1) * (stride + 1) );
		for ( let x = 0; x < stride; x++ ) {
			const left = x >= CHANNELS ? rgba[y * stride + x - CHANNELS] : 0;
			const up = y > 0 ? rgba[(y - 1) * stride + x] : 0;
			const upLeft = y > 0 && x >= CHANNELS ? rgba[(y - 1) * stride + x - CHANNELS] : 0;
			rgba[y * stride + x] = row[x] + unfilter( filter, left, up, upLeft );
		}
	}
	return { width, height, rgba };
}

/*
================
unfilter

The predictor PNG filter type `filter` adds back (RFC 2083 6.2).
================
*/
function unfilter( filter, left, up, upLeft ) {
	if ( filter === 0 ) return 0;
	if ( filter === 1 ) return left;
	if ( filter === 2 ) return up;
	if ( filter === 3 ) return (left + up) >> 1;
	if ( filter === 4 ) {
		const p = left + up - upLeft, pa = Math.abs( p - left ), pb = Math.abs( p - up ), pc = Math.abs( p - upLeft );
		return pa <= pb && pa <= pc ? left : pb <= pc ? up : upLeft;
	}
	throw Error( `Unknown PNG filter ${filter}` );
}

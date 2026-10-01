/*
===========================================================================

zip.ts - a minimal ZIP writer (stored entries, no compression)

The bug reporter exports a saved report (its full-quality replay and a
JSON description) as one .zip a player can send when asked. The video is
already compressed, so entries are stored: every unzip tool opens them,
and writing is a copy plus a CRC-32.

Layout follows PKWARE's APPNOTE: a local header and data per file, then
the central directory and its end record. Names are UTF-8 (flag bit 11).
Without ZIP64, the whole archive must stay under 4 GiB.

===========================================================================
*/

const LOCAL_SIGNATURE = 0x04034b50;
const CENTRAL_SIGNATURE = 0x02014b50;
const END_SIGNATURE = 0x06054b50;
const VERSION = 20;
const UTF8_NAMES = 0x0800;
const CRC_POLYNOMIAL = 0xedb88320;

/*
================
ZipFile
================
*/
export interface ZipFile {
	readonly name: string;
	readonly data: Uint8Array;
}

/*
================
zipStore

`modified` stamps every entry (DOS time has two-second resolution).
================
*/
export function zipStore( files: readonly ZipFile[], modified: Date ): Uint8Array {
	const table = crcTable(), [time, date] = dosTime( modified ), encoder = new TextEncoder();
	const entries = files.map( file => ({
		...file,
		name: encoder.encode( file.name ),
		crc: crc32( file.data, table )
	}) );
	const localSize = entries.reduce( ( sum, entry ) => sum + 30 + entry.name.length + entry.data.length, 0 );
	const centralSize = entries.reduce( ( sum, entry ) => sum + 46 + entry.name.length, 0 );
	const out = new Uint8Array( localSize + centralSize + 22 ), view = new DataView( out.buffer );
	if ( out.length > 0xffffffff ) throw Error( "ZIP archive over 4 GiB" );
	let at = 0;
	const offsets: number[] = [];
	for ( const entry of entries ) {
		offsets.push( at );
		view.setUint32( at, LOCAL_SIGNATURE, true );
		view.setUint16( at + 4, VERSION, true );
		view.setUint16( at + 6, UTF8_NAMES, true );
		view.setUint16( at + 8, 0, true );
		view.setUint16( at + 10, time, true );
		view.setUint16( at + 12, date, true );
		view.setUint32( at + 14, entry.crc, true );
		view.setUint32( at + 18, entry.data.length, true );
		view.setUint32( at + 22, entry.data.length, true );
		view.setUint16( at + 26, entry.name.length, true );
		view.setUint16( at + 28, 0, true );
		out.set( entry.name, at + 30 );
		out.set( entry.data, at + 30 + entry.name.length );
		at += 30 + entry.name.length + entry.data.length;
	}
	const central = at;
	for ( const [index, entry] of entries.entries() ) {
		view.setUint32( at, CENTRAL_SIGNATURE, true );
		view.setUint16( at + 4, VERSION, true );
		view.setUint16( at + 6, VERSION, true );
		view.setUint16( at + 8, UTF8_NAMES, true );
		view.setUint16( at + 10, 0, true );
		view.setUint16( at + 12, time, true );
		view.setUint16( at + 14, date, true );
		view.setUint32( at + 16, entry.crc, true );
		view.setUint32( at + 20, entry.data.length, true );
		view.setUint32( at + 24, entry.data.length, true );
		view.setUint16( at + 28, entry.name.length, true );
		view.setUint32( at + 42, offsets[index]!, true );
		out.set( entry.name, at + 46 );
		at += 46 + entry.name.length;
	}
	view.setUint32( at, END_SIGNATURE, true );
	view.setUint16( at + 8, entries.length, true );
	view.setUint16( at + 10, entries.length, true );
	view.setUint32( at + 12, at - central, true );
	view.setUint32( at + 16, central, true );
	return out;
}

/*
================
crc32
================
*/
export function crc32( data: Uint8Array, table: Uint32Array = crcTable() ): number {
	let crc = 0xffffffff;
	for ( let index = 0; index < data.length; index++ ) crc = table[(crc ^ data[index]!) & 0xff]! ^ crc >>> 8;
	return (crc ^ 0xffffffff) >>> 0;
}

/*
================
crcTable

Built per call: shared modules hold no state, and 256 entries are cheap
next to hashing a video.
================
*/
function crcTable(): Uint32Array {
	const table = new Uint32Array( 256 );
	for ( let n = 0; n < 256; n++ ) {
		let c = n;
		for ( let bit = 0; bit < 8; bit++ ) c = c & 1 ? CRC_POLYNOMIAL ^ c >>> 1 : c >>> 1;
		table[n] = c >>> 0;
	}
	return table;
}

/*
================
dosTime

[time, date] in MS-DOS format, local time, clamped to its 1980 epoch.
================
*/
function dosTime( value: Date ): readonly [number, number] {
	const year = Math.max( 1980, value.getFullYear() );
	return [
		value.getHours() << 11 | value.getMinutes() << 5 | value.getSeconds() >> 1,
		year - 1980 << 9 | value.getMonth() + 1 << 5 | value.getDate()
	];
}

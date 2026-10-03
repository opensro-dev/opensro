/*
===========================================================================

journal-cost.ts - the world journal's byte budget without serialising

The reliable world journal bounds its backlog by the UTF-16 size of the
events it holds (entities.ts). It used to measure each event with
JSON.stringify, which built and dropped a full JSON string for every
sampled pose of every moving entity on every tick: an allocation trace
showed 57 MB of garbage in 12 s from that alone. This walks the value
and returns the same estimate (2 bytes per JSON character) allocating
nothing.

The figure is a budget, not a wire size: strings are not scanned for
escapes and a non-integer number counts as its widest JSON form.

===========================================================================
*/

// "-1.2345678901234567e-123" is the widest a double prints in JSON.
const WIDEST_NUMBER_CHARS = 24;
const MAX_DEPTH = 64;

/*
================
journalCost

UTF-16 bytes of the JSON a value would serialise to, as estimated above.
================
*/
export function journalCost( value: unknown ): number {
	return jsonChars( value, 0 ) * 2;
}

/*
================
jsonChars
================
*/
function jsonChars( value: unknown, depth: number ): number {
	switch ( typeof value ) {
		case "string":
			return value.length + 2;
		case "number":
			return numberChars( value );
		case "boolean":
			return value ? 4 : 5;
		case "bigint":
			throw TypeError( "Do not know how to serialize a BigInt" );
		case "undefined":
		case "function":
		case "symbol":
			return 0;
	}
	if ( value === null ) return 4;
	if ( depth > MAX_DEPTH ) throw RangeError( "World journal event nests too deeply" );
	if ( ArrayBuffer.isView( value ) ) {
		// JSON writes a typed array as an object of index keys.
		const length = (value as unknown as ArrayLike<number>).length ?? value.byteLength;
		return 2 + length * (WIDEST_NUMBER_CHARS + 8);
	}
	if ( Array.isArray( value ) ) {
		let chars = 2 + Math.max( 0, value.length - 1 );
		for ( let i = 0; i < value.length; i++ ) {
			const item = value[i];
			// JSON writes null for array holes and unserialisable items.
			chars += item === undefined || typeof item === "function" || typeof item === "symbol" ?
				4 :
				jsonChars( item, depth + 1 );
		}
		return chars;
	}
	const toJSON = (value as { toJSON?: unknown; }).toJSON;
	if ( typeof toJSON === "function" ) return jsonChars( toJSON.call( value ), depth + 1 );
	let chars = 2, members = 0;
	for ( const key in value as object ) {
		if ( !Object.prototype.hasOwnProperty.call( value, key ) ) continue;
		const item = (value as Record<string, unknown>)[key];
		if ( item === undefined || typeof item === "function" || typeof item === "symbol" ) continue;
		chars += key.length + 3 + jsonChars( item, depth + 1 );
		members++;
	}
	return chars + Math.max( 0, members - 1 );
}

/*
================
numberChars
================
*/
function numberChars( value: number ): number {
	if ( !Number.isFinite( value ) ) return 4;
	if ( !Number.isInteger( value ) || Math.abs( value ) >= 1e21 ) return WIDEST_NUMBER_CHARS;
	let chars = value < 0 ? 2 : 1;
	for ( let n = Math.abs( value ); n >= 10; n = Math.floor( n / 10 ) ) chars++;
	return chars;
}

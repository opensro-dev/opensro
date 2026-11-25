/*
===========================================================================

renderer-row.mjs - whether two renderer rows are the same input

The presentation-due tests (tests/runtime/presentation-due.test.mjs and
presentation-due-scene.test.mjs) run the presentation twice and demand
both runs publish the same renderer rows every frame. This is that one
comparison, by the crowd capture's rules (tools/perf/core/crowd-capture.mjs
encodeCrowdValue): -0 is not 0, a Float32Array compares by its values,
an own key holding undefined reads as an absent one, and a value the
capture could not encode - a function, a symbol, a bigint, a non-finite
number, or an object that is not plain data - is refused, so a row that
gains one fails loudly instead of comparing by identity. Compared in
place rather than encoded and stringified: encoding the rows of every
frame was most of the scene test's time (#3756).

===========================================================================
*/
// The nesting past which a row is refused, as the crowd capture refuses it.
const MAX_ROW_DEPTH = 64;

/*
================
definedKeys

A plain object's own keys that hold a value. An own key holding undefined
reads as an absent one (no renderer reader enumerates a row's keys): the
one difference the two owners' attachment paths may leave, an in-place copy
writing every optional field.
================
*/
function definedKeys( value ) {
	return Object.keys( value ).filter( key => value[key] !== undefined );
}

/*
================
sameRow
================
*/
export function sameRow( a, b, depth = 0 ) {
	if ( depth > MAX_ROW_DEPTH ) throw Error( "Renderer row nesting limit" );
	if (
		typeof a === "function" || typeof a === "symbol" || typeof a === "bigint" ||
		typeof b === "function" || typeof b === "symbol" || typeof b === "bigint"
	) {
		throw Error( "Unsupported renderer row value" );
	}
	if ( typeof a === "number" || typeof b === "number" ) {
		if ( typeof a === "number" && !Number.isFinite( a ) || typeof b === "number" && !Number.isFinite( b ) ) {
			throw Error( "Non-finite renderer row value" );
		}
		return Object.is( a, b );
	}
	if ( a === null || b === null || typeof a !== "object" || typeof b !== "object" ) return a === b;
	if ( a instanceof Float32Array || b instanceof Float32Array ) {
		if ( !(a instanceof Float32Array && b instanceof Float32Array) || a.length !== b.length ) return false;
		for ( let i = 0; i < a.length; i++ ) {
			if ( !sameRow( a[i], b[i], depth + 1 ) ) return false;
		}
		return true;
	}
	if ( Array.isArray( a ) || Array.isArray( b ) ) {
		if ( !(Array.isArray( a ) && Array.isArray( b )) || a.length !== b.length ) return false;
		for ( let i = 0; i < a.length; i++ ) {
			if ( !sameRow( a[i], b[i], depth + 1 ) ) return false;
		}
		return true;
	}
	for ( const value of [ a, b ] ) {
		if ( ![ Object.prototype, null ].includes( Object.getPrototypeOf( value ) ) ) {
			throw Error( "Unsupported renderer row object" );
		}
	}
	const keys = definedKeys( a );
	if ( keys.length !== definedKeys( b ).length ) return false;
	for ( const key of keys ) {
		if ( !Object.hasOwn( b, key ) || !sameRow( a[key], b[key], depth + 1 ) ) return false;
	}
	return true;
}

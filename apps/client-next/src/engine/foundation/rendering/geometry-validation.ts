/*
================
finiteGeometryValues
================
*/
// Admission still inspects every element. IEEE-754 float32 is finite exactly
// when its exponent is not all ones. Inspecting the stored bits avoids widening
// every scalar to a JS number and calling Number.isFinite on large cold arrays.
export function finiteGeometryValues( values: Float32Array ): boolean {
	const bits = new Uint32Array( values.buffer, values.byteOffset, values.length );
	for ( let i = 0; i < bits.length; i++ ) {
		if ( (bits[i]! & 0x7f800000) === 0x7f800000 ) return false;
	}
	return true;
}

/*
================
geometryIndicesInRange
================
*/
export function geometryIndicesInRange( values: Uint32Array, limit: number ): boolean {
	for ( let i = 0; i < values.length; i++ ) {
		if ( values[i]! >= limit ) return false;
	}
	return true;
}

/*
================
finiteNumbers
================
*/
// Plain loops for decoded JSON arrays: Array.prototype.every/some box each
// double into a heap number for the callback, which an allocation trace of
// region decoding showed as tens of MB per region.
export function finiteNumbers( values: ArrayLike<number> ): boolean {
	for ( let i = 0; i < values.length; i++ ) {
		if ( !Number.isFinite( values[i] ) ) return false;
	}
	return true;
}

/*
================
integerIndicesInRange
================
*/
export function integerIndicesInRange( values: ArrayLike<number>, limit: number ): boolean {
	for ( let i = 0; i < values.length; i++ ) {
		const value = values[i]!;
		if ( !Number.isInteger( value ) || value < 0 || value >= limit ) return false;
	}
	return true;
}

/*
================
numbersWithin
================
*/
export function numbersWithin( values: ArrayLike<number>, low: number, high: number ): boolean {
	for ( let i = 0; i < values.length; i++ ) {
		const value = values[i]!;
		if ( value < low || value > high ) return false;
	}
	return true;
}

/*
===========================================================================

geometry-instances.ts - validated GPU instance packing with reusable CPU storage

===========================================================================
*/
/*
================
instanceCapacity
================
*/
export function instanceCapacity( count: number ): number {
	return 2 ** Math.ceil( Math.log2( Math.max( 1, count ) ) );
}
/*
================
createInstancePacking
================
*/
export function createInstancePacking() {
	// writeBuffer copies its source bytes before returning. One device-owned
	// scratch stream can serve every synchronous update without per-draw storage.
	let instanceScratch = new Float32Array( 0 );
	/*
	================
	finiteValues
	================
	*/
	function finiteValues( values: Float32Array ): boolean {
		for ( let i = 0; i < values.length; i++ ) if ( !Number.isFinite( values[i] ) ) return false;
		return true;
	}
	/*
	================
	unitValues

	Every value finite and within 0..1.
	================
	*/
	function unitValues( values: Float32Array ): boolean {
		for ( let i = 0; i < values.length; i++ ) {
			const v = values[i]!;
			if ( !(Number.isFinite( v ) && v >= 0 && v <= 1) ) return false;
		}
		return true;
	}
	/*
	================
	packInstances
	================
	*/
	function packInstances(
		instances: Float32Array,
		opacity?: Float32Array,
		appearance?: Float32Array,
		pointLights?: Float32Array,
		paletteOffsets?: Uint32Array
	) {
		const count = instances.length / 16;
		// Plain loops: these run on every instance upload, every frame.
		if ( !Number.isInteger( count ) || opacity && (opacity.length !== count || !unitValues( opacity )) ) {
			throw new Error( "Invalid instance opacity" );
		}
		if ( pointLights && (pointLights.length !== count * 12 || !finiteValues( pointLights )) ) {
			throw Error( "Invalid point light stream" );
		}
		if ( appearance && (appearance.length !== count * 8 || !finiteValues( appearance )) ) {
			throw new Error( "Invalid instance appearance" );
		}
		if ( paletteOffsets && (paletteOffsets.length !== count || paletteOffsets.some( v => v >= 16777216 )) ) {
			throw Error( "Invalid palette offsets" );
		}
		if ( instanceScratch.length < count * 40 ) instanceScratch = new Float32Array( instanceCapacity( count ) * 40 );
		const packed = instanceScratch;
		for ( let i = 0; i < count; i++ ) {
			const offset = i * 40;
			for ( let j = 0; j < 16; j++ ) packed[offset + j] = instances[i * 16 + j]!;
			for ( let j = 0; j < 12; j++ ) packed[offset + 28 + j] = pointLights?.[i * 12 + j] ?? 0;
			packed[offset + 16] = opacity?.[i] ?? 1;
			packed[offset + 17] = paletteOffsets?.[i] ?? 0;
			packed[offset + 18] = paletteOffsets ? 1 : 0;
			packed[offset + 19] = 0;
			if ( appearance ) { for ( let j = 0; j < 8; j++ ) packed[offset + 20 + j] = appearance[i * 8 + j]!; }
			else {
				packed.fill( 1, offset + 20, offset + 26 );
				packed[offset + 26] = packed[offset + 27] = 0;
			}
		}
		return packed.subarray( 0, count * 40 );
	}
	return packInstances;
}

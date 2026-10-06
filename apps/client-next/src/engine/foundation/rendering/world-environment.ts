import { cameraBasis } from "@/engine/foundation/rendering/world-math";
import type { EnvironmentTrack, WorldEnvironment, WorldCamera } from "@/engine/contracts/scene";

// Retail sub_4dc920/sub_4dcab0: clamped endpoints, linear interpolation,
// and a lower-key step for spans below the native epsilon.
export function sampleEnvironment(
	track: readonly EnvironmentTrack[] | undefined,
	time: number,
	key: "r" | "g" | "b" | "value",
	fallback: number
): number {
	if ( !track?.length ) return fallback;
	let lower = track[0]!, upper = lower;
	for ( const entry of track ) {
		upper = entry;
		if ( time <= entry.t ) break;
		lower = entry;
	}
	const span = upper.t - lower.t,
		fraction = span > 9.999999747378752e-5 ? Math.max( 0, Math.min( 1, (time - lower.t) / span ) ) : 0;
	return (lower[key] ?? fallback) + ((upper[key] ?? fallback) - (lower[key] ?? fallback)) * fraction;
}

// SWorld 8A7D10 event option 25 palette (8A8150 onward), before smoothing.
function eventColor( id: string ): readonly number[] | undefined {
	switch ( id ) {
		case "zenith":
			return [ 46, 75, 156 ];
		case "horizon":
			return [ 105, 76, 138 ];
		case "scatter":
			return [ 104, 66, 115 ];
		case "color0x88":
			return [ 255, 255, 255 ];
		case "color0xf0":
			return [ 128, 49, 62 ];
		case "color0x124":
			return [ 117, 101, 120 ];
		case "color0x18c":
			return [ 134, 139, 86 ];
		case "color0x1c0":
			return [ 1, 1, 1 ];
		case "color0x228":
			return [ 88, 151, 192 ];
		case "color0x2b4":
			return [ 11, 9, 12 ];
		default:
			return undefined;
	}
}
// The first 30 entries are vectors (including fog); the last six are
// already transformed native scalar channels. Never smooth packed GPU colors.
export function environmentTarget(
	environment: WorldEnvironment | undefined,
	time: number,
	weather?: import("@/engine/foundation/gameplay/weather").WeatherOptions | null,
	sceneryDistance = 3500
): Float32Array {
	const tracks = environment?.tracks,
		fogRange = Math.min( 2500, Math.fround( Math.fround( sceneryDistance ) * Math.fround( .8 ) ) );
	const color = ( id: string, fallback: number ) => {
		const override = weather?.eventRain ? eventColor( id ) : undefined;
		return override ?
			override.map( v => Math.fround( v / 255 ) ) :
			[ "r", "g", "b" ].map( key =>
				Math.fround( sampleEnvironment( tracks?.[id], time, key as "r" | "g" | "b", fallback ) )
			);
	};
	const scalar = ( id: string, fallback: number ) => sampleEnvironment( tracks?.[id], time, "value", fallback );
	const colors = [
		...color( "color0x88", 1 ),
		...color( "zenith", .35 ),
		...color( "color0xf0", 1 ),
		...color( "color0x124", 1 ),
		...color( "scatter", 1 ),
		...color( "color0x18c", 0 ),
		...color( "horizon", .7 ),
		...color( "color0x1c0", 0 ),
		...color( "color0x228", 1 ),
		...color( "color0x2b4", .7 )
	];
	const target = new Float32Array( [
		...colors,
		(scalar( "scalar0x25c", -1 ) + 1) * .5,
		1 - Math.max( -1, Math.min( 1, scalar( "scalar0x288", -1 ) ) ),
		scalar( "scalar0x2e8", 0 ) * fogRange,
		scalar( "scalar0x314", 1 ) * fogRange,
		(scalar( "cloudAlpha", -1 ) + 1) * .5,
		scalar( "starAlpha", 0 )
	] );
	if ( weather?.eventRain ) target.set( [ .695, 1, Math.fround( -.2 ) * fogRange, fogRange, 1, -1 ], 30 );
	else if ( weather && weather.mode !== 1 ) {
		// 882BE0 averages all three channels; 882C10 adds -0.2 and clamps.
		for ( const offset of [ 3, 27 ] ) {
			const gray = Math.fround( (target[offset]! + target[offset + 1]! + target[offset + 2]!) / 3 );
			target.fill( gray, offset, offset + 3 );
		}
		target.set( target.subarray( 3, 6 ), 12 );
		target.set( target.subarray( 27, 30 ), 18 );
		for ( const offset of [ 6, 15 ] ) {
			for ( let i = 0; i < 3; i++ ) {
				target[offset + i] = Math.max(
					0,
					Math.min( 1, Math.fround( target[offset + i]! + Math.fround( -.2 ) ) )
				);
			}
		}
		target[30] = 0;
		target[31] = 1;
		target[32] = Math.fround( target[32]! * .5 );
		target[34] = 0;
		target[35] = -1;
	}
	return target;
}
export function advanceEnvironment(
	previous: Float32Array | null,
	target: Float32Array,
	delta: number,
	immediate = false
): Float32Array {
	if (
		target.length !== 36 || previous && previous.length !== 36 || !Number.isFinite( delta ) || delta < 0 ||
		!target.every( Number.isFinite )
	) throw Error( "Invalid environment transition" );
	if ( !previous || immediate ) return target.slice();
	const factor = Math.min( 1, Math.fround( Math.fround( delta ) * .5 ) ), next = new Float32Array( 36 );
	for ( let i = 0; i < 36; i++ ) {
		// Vector path 8A8436..8A89D8 stores difference and product separately.
		// Scalar path 8A89DC..8A8A7E stores only the final x87 result.
		next[i] = i < 30 ?
			Math.fround( previous[i]! + Math.fround( Math.fround( target[i]! - previous[i]! ) * factor ) ) :
			Math.fround( previous[i]! + (target[i]! - previous[i]!) * factor );
	}
	return next;
}
export function environmentTime(
	environment: WorldEnvironment | undefined,
	seconds: number,
	clock: { timeOfDay: number; } | null = null
) {
	return clock?.timeOfDay ?? ((environment?.startTimeOfDay ?? .5) + seconds * (environment?.ratePerSecond ?? 0)) % 1;
}
export function worldEnvironment(
	environment: WorldEnvironment | undefined,
	camera: WorldCamera,
	aspect: number,
	seconds: number,
	clock: { timeOfDay: number; lunarDay: number; } | null = null,
	weather?: import("@/engine/foundation/gameplay/weather").WeatherOptions | null,
	state?: Float32Array
): Float32Array {
	const time = environmentTime( environment, seconds, clock ),
		values = state ?? environmentTarget( environment, time, weather );
	const color = ( index: number ) => values.subarray( index, index + 3 );
	const { forward: f, right, up } = cameraBasis( camera );
	const tangent = Math.tan( camera.fov / 2 ), near = values[32]!, far = values[33]!;
	const fog = [ ...color( 27 ) ].map( v => Math.fround( Math.max( 0, Math.min( 1, v ) ) ) );
	const packed = ( v: number ) => Math.trunc( v * 255 ) / 255;
	const terrainRadius = Math.trunc( (Math.fround( far ) + 1280) / 320 );
	return new Float32Array( [
		...color( 3 ),
		values[31]! * 2,
		...color( 18 ),
		0,
		...[ ...color( 6 ) ].map( v => v * .6 ),
		0,
		...color( 9 ),
		0,
		...f,
		0,
		...right.map( v => v * tangent * aspect ),
		0,
		...up.map( v => v * tangent ),
		0,
		// 8A4D40 packs the shadow-floor TFACTOR as truncated RGB bytes before ADD.
		// settings.w carries the camera eye height for the exp2 fog's height term.
		...fog.map( packed ),
		near,
		far,
		environment && far > near ? 1 : 0,
		Math.floor( Math.max( 0, seconds ) * 10 ),
		Math.fround( camera.eye[1] ),
		...color( 24 ),
		0,
		...[ ...color( 21 ) ].map( v => (Math.trunc( v * 255 ) & 255) / 255 ),
		0,
		...color( 12 ),
		Math.max( 1, values[30]! * 80000 ),
		time,
		seconds % 500 / 500,
		values[34]!,
		values[35]!,
		...color( 0 ),
		values[31]! * .5,
		clock?.lunarDay ?? 14,
		0,
		0,
		0,
		255,
		255,
		255,
		255,
		255,
		255,
		255,
		255,
		255,
		255,
		255,
		0,
		...fog.map( v => packed( Math.fround( Math.sqrt( v ) ) ) ),
		0,
		Math.floor( camera.eye[0] / 320 ),
		Math.floor( camera.eye[2] / 320 ),
		terrainRadius * terrainRadius,
		environment ? 1 : 0,
		...[ ...color( 6 ) ].map( v => (Math.min( 255, Math.trunc( v * 255 ) ) & 255) / 255 ),
		1
	] );
}

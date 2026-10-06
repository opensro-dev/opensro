/*
===========================================================================

cloth.ts - native BMS cloth integration and distance constraints

State belongs to one mesh instance. Positions are in the model's coordinate
system, matching AEADE0 skin anchors and A77B30's dynamic vertex copy.

===========================================================================
*/
const STEP_MS = 50;
const MAX_ACCUMULATOR_MS = 200;
const CONSTRAINT_PASSES = 7;
const EXTENSION_TOLERANCE = 0.009999999776482582;
const FORCE_STEP = 0.00039999998989515007;

/*
================
ClothData
================
*/
export interface ClothData {
	readonly mobility: readonly number[];
	readonly pins: readonly number[];
	readonly constraints: readonly (readonly [number, number, number])[];
	readonly order: readonly number[];
	readonly force: readonly [number, number, number] | null;
	readonly gravity: number;
	readonly gravityMobility: number;
	readonly windMobility: number;
	readonly damping: number;
	readonly windPeriod: number;
}

/*
================
clothData

Validate the worker boundary before admitting indices or simulation constants.
================
*/
export function clothData( value: unknown, vertices: number ): ClothData | undefined {
	if ( value === undefined ) return undefined;
	const d = value as ClothData;
	if (
		!d || !Array.isArray( d.mobility ) || d.mobility.length !== vertices ||
		!Array.isArray( d.pins ) || d.pins.length !== vertices ||
		!Array.isArray( d.constraints ) || !d.constraints.length || !Array.isArray( d.order ) ||
		d.order.length !== d.constraints.length ||
		d.mobility.some( n => !Number.isFinite( n ) || n < 0 ) ||
		d.pins.some( n => !Number.isInteger( n ) || n < 0 ) ||
		d.constraints.some( e =>
			!Array.isArray( e ) || e.length !== 3 ||
			!Number.isInteger( e[0] ) || e[0] < 0 || e[0] >= vertices ||
			!Number.isInteger( e[1] ) || e[1] < 0 || e[1] >= vertices ||
			!Number.isFinite( e[2] ) || e[2] < 0 || d.mobility[e[0]]! + d.mobility[e[1]]! === 0
		) ||
		d.order.some( n => !Number.isInteger( n ) || n < 0 || n >= d.constraints.length ) ||
		new Set( d.order ).size !== d.order.length ||
		(d.force !== null &&
			(!Array.isArray( d.force ) || d.force.length !== 3 || d.force.some( n => !Number.isFinite( n ) ))) ||
		![ d.gravity, d.gravityMobility, d.windMobility, d.damping ].every( Number.isFinite ) ||
		!Number.isInteger( d.windPeriod ) || d.windPeriod < 1
	) throw Error( "Invalid native cloth data" );
	return d;
}

/*
================
createCloth

A9EFB0 caps accumulated time at 200ms and drains 50ms steps. A77530 uses
Verlet positions, authored damping, gravity, and ordered extension constraints.
The random function is supplied so tests can exercise the native gust gate.
================
*/
export function createCloth( data: ClothData, rest: Float32Array ) {
	const positions = new Float32Array( rest ), previous = new Float32Array( rest );
	let elapsed = 0, initialized = false;
	/*
	================
	step
	================
	*/
	function step( direction: readonly number[], speed: number, random: () => number ) {
		const force = data.force ?? direction;
		// A77530 clamps positive motion to .2..1 before scaling by 900.
		const wind = (speed > 0 ? Math.max( .2, Math.min( 1, speed ) ) * 900 : speed) + 100;
		for ( let i = 0; i < data.mobility.length; i++ ) {
			if ( data.pins[i] === 1 ) continue;
			const mobility = data.mobility[i]!;
			const gust = random() % data.windPeriod === 0;
			for ( let axis = 0; axis < 3; axis++ ) {
				const at = i * 3 + axis;
				const gravity = axis === 1 ?
					Math.fround( (data.gravityMobility * mobility + 1) * data.gravity * -5 ) :
					0;
				const push = gust ?
					Math.fround(
						Math.fround( force[axis]! * wind ) * Math.fround( data.windMobility * mobility + 1 )
					) :
					0;
				const acceleration = Math.fround( Math.fround( gravity + push ) * FORCE_STEP );
				const current = positions[at]!;
				const velocity = Math.fround( Math.fround( current - previous[at]! ) * data.damping );
				previous[at] = current;
				positions[at] = current + Math.fround( velocity + acceleration );
			}
		}
		for ( let pass = 0; pass < CONSTRAINT_PASSES; pass++ ) {
			let settled = true;
			for ( const index of data.order ) {
				const [a, b, restLength] = data.constraints[index]!;
				const dx = Math.fround( positions[b * 3]! - positions[a * 3]! );
				const dy = Math.fround( positions[b * 3 + 1]! - positions[a * 3 + 1]! );
				const dz = Math.fround( positions[b * 3 + 2]! - positions[a * 3 + 2]! );
				const length = Math.fround( Math.sqrt( Math.fround( dx * dx + dy * dy + dz * dz ) ) );
				const extension = Math.fround( length - restLength );
				// A779D3..A779E6 corrects extension only; compressed edges stay free.
				if ( extension < EXTENSION_TOLERANCE ) continue;
				settled = false;
				const sum = Math.fround( data.mobility[a]! + data.mobility[b]! );
				const wa = Math.fround( data.mobility[a]! / sum ), wb = Math.fround( data.mobility[b]! / sum );
				for ( let axis = 0; axis < 3; axis++ ) {
					const delta = axis === 0 ? dx : axis === 1 ? dy : dz;
					const correction = Math.fround( Math.fround( delta / length ) * extension );
					positions[a * 3 + axis]! += Math.fround( correction * wa );
					positions[b * 3 + axis]! -= Math.fround( correction * wb );
				}
			}
			if ( settled ) break;
		}
	}
	return {
		/*
		================
		advance
		================
		*/
		advance(
			input: {
				anchors: Float32Array;
				deltaMs: number;
				enabled: boolean;
				direction: readonly number[];
				speed: number;
				random: () => number;
			}
		) {
			if ( !initialized || !input.enabled ) {
				positions.set( input.anchors );
				previous.set( input.anchors );
				elapsed = 0;
				initialized = true;
			}
			for ( let i = 0; i < data.pins.length; i++ ) {
				if ( data.pins[i] !== 0 ) positions.set( input.anchors.subarray( i * 3, i * 3 + 3 ), i * 3 );
			}
			if ( input.enabled ) {
				elapsed = Math.min( MAX_ACCUMULATOR_MS, elapsed + Math.max( 0, input.deltaMs ) );
				while ( elapsed >= STEP_MS ) {
					step( input.direction, input.speed, input.random );
					elapsed -= STEP_MS;
				}
			}
			return positions;
		}
	};
}

/*
================
clothBytes
================
*/
export function clothBytes( data: ClothData | undefined ): number {
	return data ? 128 + data.mobility.length * 16 + data.constraints.length * 56 : 0;
}

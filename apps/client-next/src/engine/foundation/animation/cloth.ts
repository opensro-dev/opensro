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
	// A step may move pinned vertices (pin 2 integrates, constraints pull a
	// mobile pin 1). due checks whether re-anchoring would actually change them.
	let stepped = false;
	/*
	================
	step
	================
	*/
	function step( direction: readonly number[], speed: number, random: () => number ) {
		const force = data.force ?? direction;
		const { mobility, pins, order, constraints, damping } = data;
		// A77530 clamps positive motion to .2..1 before scaling by 900.
		const wind = (speed > 0 ? Math.max( .2, Math.min( 1, speed ) ) * 900 : speed) + 100;
		for ( let i = 0; i < mobility.length; i++ ) {
			if ( pins[i] === 1 ) continue;
			const weight = mobility[i]!;
			const gust = random() % data.windPeriod === 0;
			const gravityY = Math.fround( (data.gravityMobility * weight + 1) * data.gravity * -5 );
			const windWeight = Math.fround( data.windMobility * weight + 1 );
			for ( let axis = 0; axis < 3; axis++ ) {
				const at = i * 3 + axis;
				const gravity = axis === 1 ? gravityY : 0;
				const push = gust ?
					Math.fround( Math.fround( force[axis]! * wind ) * windWeight ) :
					0;
				const acceleration = Math.fround( Math.fround( gravity + push ) * FORCE_STEP );
				const current = positions[at]!;
				const velocity = Math.fround( Math.fround( current - previous[at]! ) * damping );
				previous[at] = current;
				positions[at] = current + Math.fround( velocity + acceleration );
			}
		}
		for ( let pass = 0; pass < CONSTRAINT_PASSES; pass++ ) {
			let settled = true;
			for ( let edge = 0; edge < order.length; edge++ ) {
				const constraint = constraints[order[edge]!]!;
				const a = constraint[0], b = constraint[1], at = a * 3, bt = b * 3;
				const dx = Math.fround( positions[bt]! - positions[at]! );
				const dy = Math.fround( positions[bt + 1]! - positions[at + 1]! );
				const dz = Math.fround( positions[bt + 2]! - positions[at + 2]! );
				const length = Math.fround( Math.sqrt( Math.fround( dx * dx + dy * dy + dz * dz ) ) );
				const extension = Math.fround( length - constraint[2] );
				// A779D3..A779E6 corrects extension only; compressed edges stay free.
				if ( extension < EXTENSION_TOLERANCE ) continue;
				settled = false;
				const sum = Math.fround( mobility[a]! + mobility[b]! );
				const wa = Math.fround( mobility[a]! / sum ), wb = Math.fround( mobility[b]! / sum );
				// Keep the float32 stores and a/b order while exposing fixed axes to the JIT.
				const cx = Math.fround( Math.fround( dx / length ) * extension );
				positions[at]! += Math.fround( cx * wa );
				positions[bt]! -= Math.fround( cx * wb );
				const cy = Math.fround( Math.fround( dy / length ) * extension );
				positions[at + 1]! += Math.fround( cy * wa );
				positions[bt + 1]! -= Math.fround( cy * wb );
				const cz = Math.fround( Math.fround( dz / length ) * extension );
				positions[at + 2]! += Math.fround( cz * wa );
				positions[bt + 2]! -= Math.fround( cz * wb );
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
				if ( data.pins[i] === 0 ) continue;
				// Preserve the anchor copy without allocating a view for every pin/frame.
				const at = i * 3;
				positions[at] = input.anchors[at]!;
				positions[at + 1] = input.anchors[at + 1]!;
				positions[at + 2] = input.anchors[at + 2]!;
			}
			stepped = false;
			if ( input.enabled ) {
				elapsed = Math.min( MAX_ACCUMULATOR_MS, elapsed + Math.max( 0, input.deltaMs ) );
				while ( elapsed >= STEP_MS ) {
					step( input.direction, input.speed, input.random );
					elapsed -= STEP_MS;
					stepped = true;
				}
			}
			return positions;
		},

		/*
		================
		due

		Whether advancing by deltaMs would change the positions with unchanged
		anchors: a step falls due, or re-anchoring changes a pin. Check pins only
		once after each step. Holding never changes the solver clock or RNG.
		================
		*/
		due( deltaMs: number, anchors: Float32Array ): boolean {
			if ( !initialized || elapsed + Math.max( 0, deltaMs ) >= STEP_MS ) return true;
			if ( !stepped ) return false;
			for ( let i = 0; i < data.pins.length; i++ ) {
				if ( data.pins[i] === 0 ) continue;
				for ( let axis = 0; axis < 3; axis++ ) {
					const at = i * 3 + axis, position = positions[at]!;
					// Preserve signed zero; a NaN payload is not proven equal by Object.is.
					if ( !Object.is( position, anchors[at] ) || Number.isNaN( position ) ) return true;
				}
			}
			stepped = false;
			return false;
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

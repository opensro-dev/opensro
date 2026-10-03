/*
===========================================================================

effect-billboard.ts - the EFP view modes applied to an effect element

CEFEffect_Render (b153a0) picks each element's rotation from its view code
(CEECommandFactory_View* afd190..afd210 store 0..3):

  ViewNone       0  the element's own matrix under its instance;
  ViewBillboard  1  the camera basis (global cde078);
  ViewYBillboard 2  the camera basis turned about world Y only (cde0b8);
  ViewVBillboard 3  NOT a camera mode: the element's Y axis follows its
                    velocity (the vector the program's velocity commands,
                    opcodes 0x3a..0x4b, write at element +0x1b8). A zero
                    velocity, or one that normalizes to exactly (0,1,0),
                    keeps the element's own rotation on world axes.

The translation is always the element's world position. The shared
skinning draw applies the instance matrix, so each basis is converted back
through it and applied once.

===========================================================================
*/

type Vector3 = [number, number, number];

/*
================
length3

The length of a 3-vector as the native vector length takes it: the root of
the summed squares (no Math.hypot, which allocates its arguments).
================
*/
function length3( x: number, y: number, z: number ): number {
	return Math.sqrt( x * x + y * y + z * z );
}

/*
================
cross
================
*/
function cross( u: readonly number[], v: readonly number[] ): Vector3 {
	return [ u[1]! * v[2]! - u[2]! * v[1]!, u[2]! * v[0]! - u[0]! * v[2]!, u[0]! * v[1]! - u[1]! * v[0]! ];
}

/*
================
dot
================
*/
function dot( u: readonly number[], v: readonly number[] ): number {
	return u[0]! * v[0]! + u[1]! * v[1]! + u[2]! * v[2]!;
}

/*
================
normalized

The unit vector, or null for a zero-length one.
================
*/
function normalized( v: readonly number[] ): Vector3 | null {
	const length = length3( v[0]!, v[1]!, v[2]! );
	return length > 0 ? [ v[0]! / length, v[1]! / length, v[2]! / length ] : null;
}

/*
================
velocityBasis

CEFEffect_Render b1556c..b156e8: row 1 = v, row 2 = normalize(v x up),
row 0 = normalize(v x row 2). Returns null when the element keeps its own
rotation: no velocity, or velocity straight up.
================
*/
export function velocityBasis( velocity: readonly number[] | undefined ): Vector3[] | null {
	const v = velocity ? normalized( velocity ) : null;
	if ( !v || (v[0] === 0 && v[1] === 1 && v[2] === 0) ) return null;
	const side = normalized( cross( v, [ 0, 1, 0 ] ) );
	if ( !side ) return null;
	const forward = normalized( cross( v, side ) );
	if ( !forward ) return null;
	return [ forward, v, side ];
}

/*
================
cameraAxes

Write the billboard basis for the camera-facing modes into axes (three
column vectors, nine values). Runs per drawn particle per frame, so it
writes into the caller's scratch instead of building arrays.
================
*/
function cameraAxes( view: Float32Array, mode: "camera" | "y", axes: Float64Array ): void {
	axes[0] = view[0]!;
	axes[1] = view[4]!;
	axes[2] = view[8]!;
	axes[3] = view[1]!;
	axes[4] = view[5]!;
	axes[5] = view[9]!;
	axes[6] = view[3]!;
	axes[7] = view[7]!;
	axes[8] = view[11]!;
	if ( mode === "camera" ) return;
	const fwdX = view[3]!, fwdZ = view[11]!, fwdLen = Math.sqrt( fwdX * fwdX + fwdZ * fwdZ );
	if ( fwdLen < 1e-12 ) {
		// The camera basis stands unless one of its axes has collapsed.
		for ( let column = 0; column < 3; column++ ) {
			if ( !(length3( axes[column * 3]!, axes[column * 3 + 1]!, axes[column * 3 + 2]! ) > 1e-12) ) {
				axes.fill( 0 );
				axes[0] = axes[4] = axes[8] = 1;
				return;
			}
		}
		return;
	}
	const x = fwdX / fwdLen, z = fwdZ / fwdLen;
	axes[0] = z;
	axes[1] = 0;
	axes[2] = -x;
	axes[3] = 0;
	axes[4] = 1;
	axes[5] = 0;
	axes[6] = x;
	axes[7] = 0;
	axes[8] = z;
}

/*
================
faceEffectMesh

Replaces the element's rotation in its palette with the view mode's basis,
keeping its scale. velocity is the element's current velocity (required
meaning only for "v"; an element without one keeps world axes). axes is
optional scratch of nine values; the renderer passes one per frame.
================
*/
export function faceEffectMesh(
	palette: Float32Array,
	offset: number,
	instance: Float32Array,
	view: Float32Array,
	mode: "camera" | "y" | "v" = "camera",
	velocity?: readonly number[],
	axes: Float64Array = new Float64Array( 9 )
): void {
	const a0 = instance[0]!, a1 = instance[1]!, a2 = instance[2]!;
	const b0 = instance[4]!, b1 = instance[5]!, b2 = instance[6]!;
	const c0 = instance[8]!, c1 = instance[9]!, c2 = instance[10]!;
	// rows = [ b x c, c x a, a x b ]
	const r00 = b1 * c2 - b2 * c1, r01 = b2 * c0 - b0 * c2, r02 = b0 * c1 - b1 * c0;
	const r10 = c1 * a2 - c2 * a1, r11 = c2 * a0 - c0 * a2, r12 = c0 * a1 - c1 * a0;
	const r20 = a1 * b2 - a2 * b1, r21 = a2 * b0 - a0 * b2, r22 = a0 * b1 - a1 * b0;
	const det = a0 * r00 + a1 * r01 + a2 * r02;
	if ( Math.abs( det ) < 1e-12 ) return; // A zero-sized actor is already invisible.
	const p00 = palette[offset]!, p01 = palette[offset + 1]!, p02 = palette[offset + 2]!;
	const p10 = palette[offset + 4]!, p11 = palette[offset + 5]!, p12 = palette[offset + 6]!;
	const p20 = palette[offset + 8]!, p21 = palette[offset + 9]!, p22 = palette[offset + 10]!;
	// A still ViewVBillboard element keeps its own rotation, now on world
	// axes (b1556c skips the +0xac instance product): its columns are the
	// basis and carry their own sign, so the scale is their length.
	const moving = mode === "v" ? velocityBasis( velocity ) : null;
	const kept = mode === "v" && !moving;
	if ( mode !== "v" ) cameraAxes( view, mode, axes );
	else if ( moving ) {
		for ( let column = 0; column < 3; column++ ) {
			const axis = moving[column]!;
			axes[column * 3] = axis[0];
			axes[column * 3 + 1] = axis[1];
			axes[column * 3 + 2] = axis[2];
		}
	} else {
		axes[0] = p00;
		axes[1] = p01;
		axes[2] = p02;
		axes[3] = p10;
		axes[4] = p11;
		axes[5] = p12;
		axes[6] = p20;
		axes[7] = p21;
		axes[8] = p22;
	}
	const s0 = !kept && p01 === 0 && p02 === 0 ? p00 : length3( p00, p01, p02 );
	const s1 = !kept && p10 === 0 && p12 === 0 ? p11 : length3( p10, p11, p12 );
	const s2 = !kept && p20 === 0 && p21 === 0 ? p22 : length3( p20, p21, p22 );
	const scale0 = length3( a0, a1, a2 ) * s0,
		scale1 = length3( b0, b1, b2 ) * s1,
		scale2 = length3( c0, c1, c2 ) * s2;
	for ( let col = 0; col < 3; col++ ) {
		const x = axes[col * 3]!, y = axes[col * 3 + 1]!, z = axes[col * 3 + 2]!, length = length3( x, y, z );
		if ( !Number.isFinite( length ) || length < 1e-12 ) throw new Error( "Invalid effect camera basis" );
		const scale = col === 0 ? scale0 : col === 1 ? scale1 : scale2;
		palette[offset + col * 4] = (r00 * x + r01 * y + r02 * z) / det / length * scale;
		palette[offset + col * 4 + 1] = (r10 * x + r11 * y + r12 * z) / det / length * scale;
		palette[offset + col * 4 + 2] = (r20 * x + r21 * y + r22 * z) / det / length * scale;
	}
}

/*
================
faceEffectPlate
================
*/
export function faceEffectPlate(
	palette: Float32Array,
	offset: number,
	instance: Float32Array,
	view: Float32Array,
	mode: "camera" | "y" | "v" = "camera",
	velocity?: readonly number[]
): void {
	faceEffectMesh( palette, offset, instance, view, mode, velocity );
}

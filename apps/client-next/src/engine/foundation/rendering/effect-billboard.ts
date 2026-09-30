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
	const length = Math.hypot( v[0]!, v[1]!, v[2]! );
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

The billboard basis for the camera-facing modes.
================
*/
function cameraAxes( view: Float32Array, mode: "camera" | "y" ): number[][] {
	const camera = [ [ view[0]!, view[4]!, view[8]! ], [ view[1]!, view[5]!, view[9]! ], [
		view[3]!,
		view[7]!,
		view[11]!
	] ];
	if ( mode === "camera" ) return camera;
	const fwdX = view[3]!, fwdZ = view[11]!, fwdLen = Math.hypot( fwdX, fwdZ );
	if ( fwdLen < 1e-12 ) {
		return camera.every( a => Math.hypot( ...a ) > 1e-12 ) ? camera : [ [ 1, 0, 0 ], [ 0, 1, 0 ], [ 0, 0, 1 ] ];
	}
	const fwd = [ fwdX / fwdLen, 0, fwdZ / fwdLen ];
	return [ [ fwd[2]!, 0, -fwd[0]! ], [ 0, 1, 0 ], fwd ];
}

/*
================
faceEffectMesh

Replaces the element's rotation in its palette with the view mode's basis,
keeping its scale. velocity is the element's current velocity (required
meaning only for "v"; an element without one keeps world axes).
================
*/
export function faceEffectMesh(
	palette: Float32Array,
	offset: number,
	instance: Float32Array,
	view: Float32Array,
	mode: "camera" | "y" | "v" = "camera",
	velocity?: readonly number[]
): void {
	const a = [ instance[0]!, instance[1]!, instance[2]! ],
		b = [ instance[4]!, instance[5]!, instance[6]! ],
		c = [ instance[8]!, instance[9]!, instance[10]! ];
	const rows = [ cross( b, c ), cross( c, a ), cross( a, b ) ], det = dot( a, rows[0]! );
	if ( Math.abs( det ) < 1e-12 ) return; // A zero-sized actor is already invisible.
	const col0 = [ palette[offset]!, palette[offset + 1]!, palette[offset + 2]! ];
	const col1 = [ palette[offset + 4]!, palette[offset + 5]!, palette[offset + 6]! ];
	const col2 = [ palette[offset + 8]!, palette[offset + 9]!, palette[offset + 10]! ];
	// A still ViewVBillboard element keeps its own rotation, now on world
	// axes (b1556c skips the +0xac instance product): its columns are the
	// basis and carry their own sign, so the scale is their length.
	const moving = mode === "v" ? velocityBasis( velocity ) : null;
	const kept = mode === "v" && !moving;
	const axes = mode === "v" ? moving ?? [ col0, col1, col2 ] : cameraAxes( view, mode );
	const s0 = !kept && col0[1] === 0 && col0[2] === 0 ? col0[0]! : Math.hypot( ...col0 );
	const s1 = !kept && col1[0] === 0 && col1[2] === 0 ? col1[1]! : Math.hypot( ...col1 );
	const s2 = !kept && col2[0] === 0 && col2[1] === 0 ? col2[2]! : Math.hypot( ...col2 );
	const scale = [ Math.hypot( ...a ) * s0, Math.hypot( ...b ) * s1, Math.hypot( ...c ) * s2 ];
	for ( let col = 0; col < 3; col++ ) {
		const axis = axes[col]!, length = Math.hypot( axis[0]!, axis[1]!, axis[2]! );
		if ( !Number.isFinite( length ) || length < 1e-12 ) throw new Error( "Invalid effect camera basis" );
		for ( let row = 0; row < 3; row++ ) {
			palette[offset + col * 4 + row] = dot( rows[row]!, axis ) / det / length * scale[col]!;
		}
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

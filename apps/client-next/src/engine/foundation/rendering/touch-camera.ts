/*
===========================================================================

touch-camera.ts - camera control from touch screens

The original client is mouse-only: the camera orbits while the camera button
is held and zooms with the wheel (67CBC0). A touch pointer reports only the
primary button, so on a phone or tablet the camera could not move at all.
This adapter turns touch gestures into the same camera input the mouse
produces, so the camera owner stays unchanged:

	one finger dragging   -> a drag with the camera button held (orbit)
	two fingers pinching  -> wheel deltas (spreading the fingers zooms in)

Owner-authorized port-only mobile input: the platform admits canvas touches
and dispatches a tap only after this owner rules out a drag or pinch.

===========================================================================
*/

import type { CameraWheelDelta } from "@/engine/contracts/input";

// Wheel units per pixel of pinch distance change. zoomCamera divides by 20,
// so 5 units per pixel moves the camera a quarter unit per pixel: a 280 px
// pinch spans most of the 10..150 rail.
const PINCH_WHEEL_PER_PIXEL = 5;
const TAP_SLOP_PIXELS = 8;

/*
================
TouchCameraOutput

Camera input in the RawInput vocabulary, minus the timestamp the platform
adds.
================
*/
export type TouchCameraOutput =
	| { readonly kind: "pointer"; readonly x: number; readonly y: number; readonly buttons: number; }
	| { readonly kind: "wheel"; readonly delta: CameraWheelDelta; }
	| { readonly kind: "release"; };

/*
================
createTouchCamera

Each event carries the button mask the camera owner orbits on (2 in mouse
mode 0, 1 in mouse mode 1), so the binding can change between gestures.
================
*/
export function createTouchCamera() {
	const touches = new Map<number, { x: number; y: number; }>();
	// A pinch ends the gesture for good: lifting one finger must not turn the
	// remaining one into an orbit until every finger has been lifted.
	let pinched = false, pinchDistance = 0;
	let interrupted = false;
	let tapOrigin: { x: number; y: number; } | null = null;

	/*
	================
	spread
	================
	*/
	function spread() {
		const [a, b] = [ ...touches.values() ];
		return a && b ? Math.hypot( a.x - b.x, a.y - b.y ) : 0;
	}

	return {
		/*
		================
		interrupt

		A touch owned by UI ends both orbit and zoom until all canvas fingers lift.
		================
		*/
		interrupt(): readonly TouchCameraOutput[] {
			if ( touches.size === 0 ) return [];
			interrupted = true;
			pinched = true;
			tapOrigin = null;
			return [ { kind: "release" } ];
		},
		/*
		================
		owns
		================
		*/
		owns( id: number ): boolean {
			return touches.has( id );
		},
		/*
		================
		tap

		Read before up; a gesture that ever pinched or moved cannot become a tap.
		================
		*/
		tap( id: number, x: number, y: number ): boolean {
			return touches.has( id ) && touches.size === 1 && !pinched && tapOrigin !== null &&
				Math.hypot( x - tapOrigin.x, y - tapOrigin.y ) <= TAP_SLOP_PIXELS;
		},
		/*
		================
		reset
		================
		*/
		reset(): void {
			touches.clear();
			interrupted = false;
			pinched = false;
			pinchDistance = 0;
			tapOrigin = null;
		},
		/*
		================
		down
		================
		*/
		down( id: number, x: number, y: number, cameraButtons: number ): readonly TouchCameraOutput[] {
			touches.set( id, { x, y } );
			if ( interrupted ) return [];
			if ( touches.size === 1 && !pinched ) {
				tapOrigin = { x, y };
				return [ { kind: "pointer", x, y, buttons: cameraButtons } ];
			}
			tapOrigin = null;
			if ( touches.size === 2 ) {
				pinched = true;
				pinchDistance = spread();
				return [ { kind: "release" } ];
			}
			return [];
		},
		/*
		================
		move
		================
		*/
		move( id: number, x: number, y: number, cameraButtons: number ): readonly TouchCameraOutput[] {
			if ( !touches.has( id ) || interrupted ) return [];
			if ( tapOrigin && Math.hypot( x - tapOrigin.x, y - tapOrigin.y ) > TAP_SLOP_PIXELS ) tapOrigin = null;
			touches.set( id, { x, y } );
			if ( touches.size === 2 ) {
				const distance = spread(), delta = (pinchDistance - distance) * PINCH_WHEEL_PER_PIXEL;
				pinchDistance = distance;
				return delta ? [ { kind: "wheel", delta: delta as CameraWheelDelta } ] : [];
			}
			if ( touches.size === 1 && !pinched ) return [ { kind: "pointer", x, y, buttons: cameraButtons } ];
			return [];
		},
		/*
		================
		up

		Also handles pointercancel: the gesture ends either way.
		================
		*/
		up( id: number ): readonly TouchCameraOutput[] {
			if ( !touches.delete( id ) ) return [];
			if ( touches.size === 2 ) pinchDistance = spread();
			if ( touches.size > 0 ) return [];
			const orbiting = !pinched;
			interrupted = false;
			pinched = false;
			tapOrigin = null;
			return orbiting ? [ { kind: "release" } ] : [];
		}
	};
}

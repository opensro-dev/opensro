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

A tap still reaches the world as a click: browsers synthesize mouse events
for a tap but not for a drag, and this adapter never emits a click itself.

===========================================================================
*/

import type { CameraWheelDelta } from "@/engine/contracts/input";

// Wheel units per pixel of pinch distance change. zoomCamera divides by 20,
// so 5 units per pixel moves the camera a quarter unit per pixel: a 280 px
// pinch spans most of the 10..150 rail.
const PINCH_WHEEL_PER_PIXEL = 5;

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
		down
		================
		*/
		down( id: number, x: number, y: number, cameraButtons: number ): readonly TouchCameraOutput[] {
			touches.set( id, { x, y } );
			if ( touches.size === 1 && !pinched ) return [ { kind: "pointer", x, y, buttons: cameraButtons } ];
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
			if ( !touches.has( id ) ) return [];
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
			if ( touches.size > 0 ) return [];
			const orbiting = !pinched;
			pinched = false;
			return orbiting ? [ { kind: "release" } ] : [];
		}
	};
}

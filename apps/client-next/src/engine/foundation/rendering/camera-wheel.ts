/*
===========================================================================

camera-wheel.ts - mouse wheel deltas become camera distance steps

===========================================================================
*/
import type { CameraWheelDelta } from "@/engine/contracts/input";

// The normal mission rail's ends: the 10-unit floor always applies; the
// 150-unit ceiling applies only while the interface's zoom byte (+0x3CF)
// stands, as the constructor (68EFEF) leaves it. 690C40 case 0x10 (/zoom)
// flips the byte and prints nothing.
const MIN_CAMERA_DISTANCE = 10;
const MAX_CAMERA_DISTANCE = 150;

/*
================
cameraWheelDelta

Preserve Chromium's native wheel ticks where available (120 per detent).
Other browsers expose scrolling units: use a declared 100px / 3-line
detent mapping, preserving fractional and coalesced movement.
================
*/
export function cameraWheelDelta( event: {
	deltaY: number;
	deltaMode: number;
	wheelDeltaY?: number;
} ): CameraWheelDelta {
	const native = event.wheelDeltaY;
	return (Number.isFinite( native ) ?
		-native! :
		event.deltaY * (event.deltaMode === 1 ? 40 : event.deltaMode === 2 ? 120 : 1.2)) as CameraWheelDelta;
}

/*
================
zoomCamera

67CBC0 (and 67B5E0 on the captured-wheel path): signed wheel delta / 20,
float store, then clamp. Our adapter reverses Win32's sign so positive
means zoom out. The floor clamp is unconditional (67CC32); the 150
ceiling reads the zoom byte - [esi+0x3CF] at 67CC3C - so with the byte
clear the wheel pulls the camera past 150.
================
*/
export function zoomCamera( distance: number, delta: CameraWheelDelta, rail = true ): number {
	const stepped = Math.fround( distance + delta / 20 );
	return rail ?
		Math.max( MIN_CAMERA_DISTANCE, Math.min( MAX_CAMERA_DISTANCE, stepped ) ) :
		Math.max( MIN_CAMERA_DISTANCE, stepped );
}

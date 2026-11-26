/*
===========================================================================

camera-script.ts - the camera-shake script contract

One SCT_SHAKECAM* event: when it fires (milliseconds on the
presentation timer clock), how hard it shakes, and for how long at
what period. The values are literal unions - the shipped scripts use
no others. Owned by the camera-script reader; players of the shake
consume this shape.

===========================================================================
*/
export interface CameraScript {
	readonly atMs: number;
	readonly amplitude: 50 | 200 | 300 | 400;
	readonly durationMs: 500;
	readonly periodMs: 20 | 25;
}

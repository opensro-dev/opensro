// SCT_SHAKECAM* event. Milliseconds use the presentation timer clock.
export interface CameraScript {
	readonly atMs: number;
	readonly amplitude: 50 | 200 | 300 | 400;
	readonly durationMs: 500;
	readonly periodMs: 20 | 25;
}

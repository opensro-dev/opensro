/*
===========================================================================

gpu-work.ts - how much queue work the client asks the GPU driver for

Frame and CPU times say a frame is slow but not why. On a driver with
high per-call overhead (the Firefox report, #4392) the suspects are the
number of queue submits and buffer uploads a frame costs, so the device
owner counts them where they funnel: every queue submit goes through
the geometry upload arena or a device-owned helper, and the counted
upload streams are the arena's batched writes and the particle
presentation's page and stream writes - the two uploads that scale
with the scene. Small constant uniform writes elsewhere (animation,
shadows, UI) are not counted; they do not scale with the scene. The
counters live as long as their device owner, across device
recreations, so a reader diffs two reads over the frames between them.

===========================================================================
*/

/*
================
GpuWorkTotals

Queue submits and upload writes since the counters' owner started.
Upload bytes are the bytes those writes carried.
================
*/
export interface GpuWorkTotals {
	readonly submits: number;
	readonly uploadWrites: number;
	readonly uploadBytes: number;
}

export type GpuWork = ReturnType<typeof createGpuWork>;

/*
================
createGpuWork
================
*/
export function createGpuWork() {
	let submits = 0, uploadWrites = 0, uploadBytes = 0;
	return {
		/*
		================
		noteSubmit

		One queue.submit call.
		================
		*/
		noteSubmit() {
			submits++;
		},
		/*
		================
		noteUpload

		One queue.writeBuffer call carrying bytes of upload data.
		================
		*/
		noteUpload( bytes: number ) {
			uploadWrites++;
			uploadBytes += bytes;
		},
		/*
		================
		read

		The totals so far, as plain values.
		================
		*/
		read(): GpuWorkTotals {
			return { submits, uploadWrites, uploadBytes };
		}
	};
}

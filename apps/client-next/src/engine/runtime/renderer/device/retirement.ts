/*
===========================================================================

retirement.ts - when a GPU resource is destroyed

WebGPU validates a command buffer when it is submitted: every buffer and
texture its passes, bind groups and render bundles name must still be alive,
or the device raises "used in submit while destroyed" and the renderer
fails. A frame names resources from the moment it starts preparing until
its last submit (two submits when the deferred particle pass waits for its
visibility query), and the owners that prepare it (characters, world,
particles, shadows, the interface) release and grow resources in that same
span.

So destruction belongs to the frame, not to the caller. A resource retired
while a frame is open is destroyed when that frame closes; outside a frame
nothing is recorded, and it is destroyed at once.

===========================================================================
*/

/*
================
Retired

What a frame's command buffers can name and an owner can destroy.
================
*/
export type Retired = GPUBuffer | GPUTexture;

/*
================
Retire

Hands a GPU buffer or texture back for destruction.
================
*/
export type Retire = ( resource: Retired ) => void;

/*
================
destroyNow

The Retire of an owner built without a frame (tests, tools): nothing is
recorded, so nothing waits.
================
*/
export const destroyNow: Retire = resource => resource.destroy();

/*
================
createRetirement

The device's retirement queue. open and close bracket one frame; flush
destroys whatever waits (close, and the device's own disposal).
================
*/
export function createRetirement() {
	const waiting: Retired[] = [];
	let open = false;
	/*
	================
	flush
	================
	*/
	function flush() {
		// A destroy may not retire more (owners hand over leaf resources), but
		// drain by index so one that does is still destroyed in this flush.
		for ( let i = 0; i < waiting.length; i++ ) waiting[i]!.destroy();
		waiting.length = 0;
	}
	return {
		/*
		================
		retire
		================
		*/
		retire: (resource => {
			if ( open ) waiting.push( resource );
			else resource.destroy();
		}) as Retire,
		/*
		================
		open

		A frame starts preparing: from here its command buffers may name any
		live resource.
		================
		*/
		open() {
			open = true;
		},
		/*
		================
		close

		The frame's last command buffer is submitted (or the frame was
		abandoned): nothing names the retired resources any more.
		================
		*/
		close() {
			open = false;
			flush();
		},
		flush,
		waiting: () => waiting.length
	};
}

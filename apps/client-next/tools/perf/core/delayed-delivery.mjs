/*
===========================================================================

delayed-delivery.mjs - FIFO delivery under simulated transport latency

One timer owns the head of a direction's queue. Independently rounded browser
timeouts do not preserve packet order when a stalled worker resumes.

===========================================================================
*/

/*
================
createDelayedDelivery
================
*/
export function createDelayedDelivery( clock ) {
	const queue = [];
	let timer;
	/*
	================
wake
	================
	*/
	function wake() {
		timer = undefined;
		while ( queue.length && queue[0].due <= clock.now() ) queue.shift().deliver();
		if ( queue.length ) timer = clock.schedule( wake, Math.max( 0, queue[0].due - clock.now() ) );
	}
	return {
		/*
		================
push
		================
		*/
		push( delay, deliver ) {
			const due = Math.max( clock.now() + delay, queue.at( -1 )?.due ?? 0 );
			queue.push( { due, deliver } );
			if ( timer === undefined ) timer = clock.schedule( wake, Math.max( 0, due - clock.now() ) );
		},
		/*
		================
clear
		================
		*/
		clear() {
			if ( timer !== undefined ) clock.cancel( timer );
			timer = undefined;
			const count = queue.length;
			queue.length = 0;
			return count;
		}
	};
}

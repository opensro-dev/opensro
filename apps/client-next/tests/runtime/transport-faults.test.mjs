/*
===========================================================================

transport-faults.test.mjs - transport fault injection must preserve wire order

Early rounded timers and late wakes must not turn jitter into packet loss or
reordering. Reconnect cleanup must discard callbacks from the closed socket.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createDelayedDelivery } from "../../tools/perf/core/delayed-delivery.mjs";

test("jitter retains FIFO through early timers, worker stalls and cancellation", () => {
	let now = 0, next = 0;
	const timers = new Map(), received = [];
	const queue = createDelayedDelivery( {
		now: () => now,
		schedule( callback, delay ) {
			timers.set( ++next, { callback, delay } );
			return next;
		},
		cancel: id => timers.delete( id )
	} );
	/*
	================
wake
	================
	*/
	function wake( time ) {
		now = time;
		assert.equal( timers.size, 1, "one timer owns all pending packets" );
		const [id, task] = [ ...timers ][0];
		timers.delete( id );
		task.callback();
	}
	queue.push( 150.9, () => received.push( "begin" ) );
	now = .4;
	queue.push( 125, () => received.push( "body" ) );
	now = .8;
	queue.push( 100, () => received.push( "end" ) );
	wake( 150 );
	assert.deepEqual( received, [] );
	wake( 1150 );
	assert.deepEqual( received, [ "begin", "body", "end" ] );
	assert.equal( timers.size, 0 );
	queue.push( 100, () => received.push( "old socket" ) );
	assert.equal( queue.clear(), 1 );
	assert.equal( timers.size, 0 );
	queue.push( 50, () => received.push( "new socket" ) );
	wake( 1200 );
	assert.deepEqual( received, [ "begin", "body", "end", "new socket" ] );
});

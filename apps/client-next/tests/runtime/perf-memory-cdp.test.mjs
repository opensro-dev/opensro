/*
===========================================================================

perf-memory-cdp.test.mjs - memory runs never enable DevTools network inspection

With the Network domain enabled, the renderer keeps a copy of every response
body, which a memory number would count as the client's. The raw CDP
connection refuses every Network.* method before it reaches Chrome, and
the dump parser reads Chrome's per-process footprint and allocators. A
run whose browser died or stopped answering fails instead of hanging.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { EventEmitter } from "node:events";
import { createCdpConnection, memoryDump, parseMemoryDump, stopChrome } from "../../tools/perf/core/raw-cdp.mjs";

/*
================
fakeSocket

Records what is sent and answers each command with { echo: method }.
================
*/
function fakeSocket() {
	const sent = [];
	/** @type {any} A WebSocket's send, close and onmessage. */
	const socket = {
		onmessage: null,
		send( text ) {
			const message = JSON.parse( text );
			sent.push( message );
			queueMicrotask( () =>
				socket.onmessage( { data: JSON.stringify( { id: message.id, result: { echo: message.method } } ) } )
			);
		},
		close() {}
	};
	return { socket, sent };
}

test("a memory run's connection refuses every Network method and sends nothing for it", async () => {
	const { socket, sent } = fakeSocket();
	const cdp = createCdpConnection( socket );
	for ( const method of [ "Network.enable", "Network.setCacheDisabled", "Network.getResponseBody" ] ) {
		await assert.rejects( cdp.send( method, {}, "session" ), /refused/ );
	}
	assert.equal( sent.length, 0 );
	assert.deepEqual( await cdp.send( "Tracing.start", {}, "session" ), { echo: "Tracing.start" } );
	assert.equal( sent[0].sessionId, "session" );
});

test("protocol errors reject and events reach the listeners", async () => {
	/** @type {any} */
	const socket = { onmessage: null, send() {}, close() {} };
	const cdp = createCdpConnection( socket );
	const pending = cdp.send( "Page.navigate" );
	socket.onmessage( { data: JSON.stringify( { id: 1, error: { code: -32000, message: "no page" } } ) } );
	await assert.rejects( pending, /no page \(-32000\)/ );
	const seen = [];
	const stop = cdp.on( message => seen.push( message.method ) );
	socket.onmessage( { data: JSON.stringify( { method: "Tracing.tracingComplete", params: {} } ) } );
	stop();
	socket.onmessage( { data: JSON.stringify( { method: "Tracing.dataCollected", params: { value: [] } } ) } );
	assert.deepEqual( seen, [ "Tracing.tracingComplete" ] );
});

test("a dump becomes one row per process with its footprint and allocators in MiB", () => {
	const mib = n => "0x" + (n * 1024 * 1024).toString( 16 );
	const events = [
		{
			ph: "v",
			pid: 7,
			args: {
				dumps: {
					process_totals: { private_footprint_bytes: mib( 1040 ) },
					allocators: {
						"partition_alloc/partitions/array_buffer": { attrs: { size: { value: mib( 455 ) } } },
						"partition_alloc/partitions/buffer": { attrs: { size: { value: mib( 9 ) } } },
						v8: { attrs: { size: { value: mib( 214 ) } } },
						"v8/main": { attrs: { size: { value: mib( 150 ) } } }
					}
				}
			}
		},
		{ ph: "v", pid: 9, args: { dumps: { process_totals: { private_footprint_bytes: mib( 667 ) } } } },
		{ ph: "X", pid: 7, args: {} }
	];
	const rows = parseMemoryDump( events, new Map( [ [ 7, "renderer" ], [ 9, "GPU" ] ] ) );
	assert.deepEqual( rows, [
		{
			pid: 7,
			type: "renderer",
			footprint: 1040,
			allocators: {
				"partition_alloc/partitions/array_buffer": 455,
				"partition_alloc/partitions/buffer": 9,
				v8: 214
			}
		},
		{ pid: 9, type: "GPU", footprint: 667, allocators: {} }
	] );
});

test("a closed connection rejects every pending and later command", async () => {
	/** @type {any} */
	const socket = { onmessage: null, onclose: null, send() {}, close() {} };
	const cdp = createCdpConnection( socket );
	const pending = cdp.send( "Tracing.end" );
	socket.onclose();
	await assert.rejects( pending, /closed/ );
	await assert.rejects( cdp.send( "Tracing.start" ), /closed/ );
});

test("a memory dump whose trace never completes fails at its deadline", async () => {
	/** @type {any} Answers every command, never sends tracingComplete. */
	const cdp = { send: async () => ({ success: true }), on: () => () => {} };
	await assert.rejects( memoryDump( cdp, 50 ), /did not finish the memory dump within 50 ms/ );
});

test("stopping a Chrome that already exited does not wait for another exit", async () => {
	let killed = false;
	const exited = Object.assign( new EventEmitter(), { exitCode: 1, signalCode: null, kill: () => killed = true } );
	await stopChrome( exited );
	assert.equal( killed, false );
	const running = Object.assign( new EventEmitter(), {
		exitCode: null,
		signalCode: null,
		kill: () => queueMicrotask( () => running.emit( "exit" ) )
	} );
	await stopChrome( running );
});

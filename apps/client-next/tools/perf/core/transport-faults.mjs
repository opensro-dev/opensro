/*
===========================================================================

transport-faults.mjs - bounded faults on actual authenticated WebSocket traffic

Used by the recovery and acceptance harnesses before world admission.

===========================================================================
*/
import assert from "node:assert/strict";

/*
================
installFaults

The bounded queues preserve FIFO in each direction. All data still passes
through the real browser WebSocket and real authenticated GameWorld server.
================
*/
export async function installFaults( page ) {
	const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
	assert.ok( worker, "simulation worker is running before login" );
	await worker.evaluate( async () => {
		const { createDelayedDelivery } = await import( "/tools/perf/core/delayed-delivery.mjs" );
		const clock = {
			now: () => performance.now(),
			schedule: ( callback, delay ) => setTimeout( callback, delay ),
			cancel: timer => clearTimeout( timer )
		};
		const link = globalThis.__recoveryLink = {
			delay: 0,
			jitter: 0,
			rx: 0,
			tx: 0,
			ready: 0,
			pending: 0,
			peak: 0,
			closes: []
		};
		const OriginalSocket = WebSocket;
		globalThis.WebSocket = class extends OriginalSocket {
			constructor( ...args ) {
				super( ...args );
				this.addEventListener( "close", event => {
					link.pending -= this.queues.rx.clear() + this.queues.tx.clear();
					link.closes.push( { code: event.code, reason: event.reason, clean: event.wasClean } );
					if ( link.closes.length > 16 ) link.closes.shift();
				} );
			}
			queues = { rx: createDelayedDelivery( clock ), tx: createDelayedDelivery( clock ) };
			schedule( direction, deliver ) {
				link[direction]++;
				if ( !link.delay && !link.pending ) {
					deliver();
					return;
				}
				if ( link.pending >= 512 ) throw Error( "Recovery harness transport queue exceeded" );
				link.pending++;
				link.peak = Math.max( link.peak, link.pending );
				this.queues[direction].push( link.delay + (link[direction] % 3) * link.jitter, () => {
					link.pending--;
					if ( this.readyState === OriginalSocket.OPEN ) deliver();
				} );
			}
			set onmessage( handler ) {
				super.onmessage = event => this.schedule( "rx", () => handler.call( this, event ) );
			}
			send( bytes ) {
				this.schedule( "tx", () => {
					super.send( bytes );
					const WORLD_READY_OPCODE = 0x3012;
					if (
						bytes instanceof Uint8Array && bytes.byteLength >= 2 &&
						new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength ).getUint16( 0, true ) ===
							WORLD_READY_OPCODE
					) link.ready++;
				} );
			}
		};
	} );
}

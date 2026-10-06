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
	await worker.evaluate( () => {
		const link = globalThis.__recoveryLink = { delay: 0, jitter: 0, rx: 0, tx: 0, pending: 0, peak: 0, closes: [] };
		const OriginalSocket = WebSocket;
		globalThis.WebSocket = class extends OriginalSocket {
			constructor( ...args ) {
				super( ...args );
				this.addEventListener( "close", event => {
					link.closes.push( { code: event.code, reason: event.reason, clean: event.wasClean } );
					if ( link.closes.length > 16 ) link.closes.shift();
				} );
			}
			rxDue = 0;
			txDue = 0;
			schedule( direction, deliver ) {
				link[direction]++;
				if ( !link.delay ) {
					deliver();
					return;
				}
				if ( link.pending >= 512 ) throw Error( "Recovery harness transport queue exceeded" );
				link.pending++;
				link.peak = Math.max( link.peak, link.pending );
				const now = performance.now(), key = direction + "Due";
				const due = this[key] = Math.max( this[key], now + link.delay + (link[direction] % 3) * link.jitter );
				setTimeout( () => {
					link.pending--;
					if ( this.readyState === OriginalSocket.OPEN ) deliver();
				}, due - now );
			}
			set onmessage( handler ) {
				super.onmessage = event => this.schedule( "rx", () => handler.call( this, event ) );
			}
			send( bytes ) {
				this.schedule( "tx", () => super.send( bytes ) );
			}
		};
	} );
}

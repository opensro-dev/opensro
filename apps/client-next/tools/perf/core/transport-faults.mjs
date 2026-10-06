/*
===========================================================================

transport-faults.mjs - bounded faults on actual authenticated WebSocket traffic

Used by the recovery and acceptance harnesses before world admission. Two
fault shapes share one FIFO queue per direction: a steady latency (delay,
jitter) and a stall (holdStream), which holds the whole direction until a
time and then releases it in order - a late server tick or handler delays
everything behind it, so a stall never reorders frames.

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
			closes: [],
			// Stall windows (worker performance.now) and what each frame waited.
			holdUntil: { rx: 0, tx: 0 },
			log: [],
			logged: 0,
			transport: null
		};
		const OriginalSocket = WebSocket;
		globalThis.WebSocket = class extends OriginalSocket {
			constructor( ...args ) {
				super( ...args );
				const url = new URL( this.url );
				link.transport = url.protocol === "ws:" || url.protocol === "wss:" ? "websocket" : url.protocol;
				this.addEventListener( "close", event => {
					link.pending -= this.queues.rx.clear() + this.queues.tx.clear();
					link.closes.push( { code: event.code, reason: event.reason, clean: event.wasClean } );
					if ( link.closes.length > 16 ) link.closes.shift();
				} );
			}
			queues = { rx: createDelayedDelivery( clock ), tx: createDelayedDelivery( clock ) };
			schedule( direction, deliver, bytes ) {
				link[direction]++;
				const queuedAt = performance.now(), hold = link.holdUntil[direction] - queuedAt;
				if ( !link.delay && !link.pending && hold <= 0 ) {
					deliver();
					return;
				}
				if ( link.pending >= 512 ) throw Error( "Recovery harness transport queue exceeded" );
				link.pending++;
				link.peak = Math.max( link.peak, link.pending );
				const delay = Math.max( link.delay + (link[direction] % 3) * link.jitter, hold );
				this.queues[direction].push( delay, () => {
					link.pending--;
					if ( this.readyState !== OriginalSocket.OPEN ) return;
					deliver();
					if ( hold > 0 ) {
						const view = bytes && (bytes instanceof ArrayBuffer ?
							new Uint8Array( bytes ) :
							new Uint8Array( bytes.buffer, bytes.byteOffset, bytes.byteLength ));
						link.log.push( {
							seq: ++link.logged,
							direction,
							opcode: view && view.byteLength >= 2 ? view[0] | (view[1] << 8) : null,
							bytes: view ? view.byteLength : null,
							heldMs: performance.now() - queuedAt
						} );
						if ( link.log.length > 4096 ) link.log.shift();
					}
				} );
			}
			set onmessage( handler ) {
				super.onmessage = event => this.schedule( "rx", () => handler.call( this, event ), event.data );
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
				}, bytes );
			}
		};
	} );
}

/*
================
holdStream

Stalls one whole direction for ms from now ("rx" server to client, "tx"
client to server); frames arriving meanwhile are released in order when the
hold ends. Returns the hold's end on the worker's clock.
================
*/
export function holdStream( page, direction, ms ) {
	assert.ok( direction === "rx" || direction === "tx", "hold direction is rx or tx" );
	const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
	return worker.evaluate( ( { direction, ms } ) => {
		const link = globalThis.__recoveryLink;
		link.holdUntil[direction] = Math.max( link.holdUntil[direction], performance.now() + ms );
		return link.holdUntil[direction];
	}, { direction, ms } );
}

/*
================
faultLog

The transport in use and every held frame's direction, opcode, size and
actual wait (frames that were not held are not logged). Entries carry a
running seq; after keeps only those logged since an earlier logged count,
so one lane's record never includes another's.
================
*/
export function faultLog( page, after = 0 ) {
	const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
	return worker.evaluate( after => ({
		transport: globalThis.__recoveryLink.transport,
		logged: globalThis.__recoveryLink.logged,
		held: globalThis.__recoveryLink.log.filter( entry => entry.seq > after )
	}), after );
}

/*
================
stallServer

The command is held for ms and the downlink for twice that, so the reply
to the released command is itself held: a request and its answer both
late. A delayed-command surrogate for a late tick
or blocked handler, not a paused tick (server AI and movement keep
advancing meanwhile). A downlink-only holdStream( "rx" ) is network delay,
where the server applies the command on time. Label runs network-delay or
bidirectional-delay.
================
*/
export function stallServer( page, ms ) {
	const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
	// One evaluation, so both windows start together.
	return worker.evaluate( ms => {
		const link = globalThis.__recoveryLink, until = performance.now() + ms;
		link.holdUntil.tx = Math.max( link.holdUntil.tx, until );
		link.holdUntil.rx = Math.max( link.holdUntil.rx, until + ms );
		return until;
	}, ms );
}

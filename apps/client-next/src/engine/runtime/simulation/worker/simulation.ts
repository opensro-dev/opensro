/*
===========================================================================

simulation.ts - fixed-step session execution and bounded snapshot publication

Owns the worker clock, input acknowledgements and snapshot buffer pool.
Between-tick commands use the same time origin as displayed movement;
the session preserves monotonic time when a queued tick catches up.

===========================================================================
*/
import { createSession } from "./session/session";
import { createSimulationInput } from "./input/input";
import { createClock } from "./clock/clock";
import { PROTOCOL_VERSION, SNAPSHOT_BYTES, SIMULATION_STEP_MS, writeSnapshot } from "@/engine/contracts/simulation";
import type { HostMessage, WorkerMessage } from "@/engine/contracts/simulation";
const MAX_SNAPSHOTS = 3;

// How often the transport's frame journal goes to the host (simulation ms).
const NET_JOURNAL_PUSH_MS = 1000;

/*
================
createSimulation
================
*/
export function createSimulation( send: ( message: WorkerMessage, transfer: Transferable[] ) => void ) {
	const session = createSession();
	const input = createSimulationInput();
	const pool: ArrayBuffer[] = [
		new ArrayBuffer( SNAPSHOT_BYTES ),
		new ArrayBuffer( SNAPSHOT_BYTES ),
		new ArrayBuffer( SNAPSHOT_BYTES )
	];
	let sequence = 0, timeMs = 0, started = false, stopped = false, journalSentAtMs = 0;
	/*
	================
	fail
	================
	*/
	const fail = ( error: unknown ) => {
		stopped = true;
		clock.dispose();
		session.dispose();
		send( { kind: "failure", message: String( error ) }, [] );
	};
	/*
	================
	step
	================
	*/
	function step( skippedMs: number ) {
		// Expiry and authoritative sampling follow elapsed time, not the number
		// of callbacks Chrome allowed while the tab was suspended.
		timeMs += skippedMs;
		const sessionState = session.step( timeMs );
		const batch = session.takeWorld();
		if ( batch ) send( { kind: "world", batch }, [] );
		// Bug-report frame journal: one transfer a second, not one per frame.
		if ( timeMs - journalSentAtMs >= NET_JOURNAL_PUSH_MS ) {
			journalSentAtMs = timeMs;
			const records = session.takeNetJournal();
			if ( records ) send( { kind: "net-journal", records }, [ records.buffer ] );
		}
		if ( sessionState ) {
			send( { kind: "session", state: sessionState }, [] );
		}
		// Escape is UI-only, as CGInterface_HandleEscapeKey (69F450) is: it closes
		// windows or opens the system menu and never cancels the action.
		const acceptedInputSequence = input.commit();
		timeMs += SIMULATION_STEP_MS;
		sequence++;
		const buffer = pool.pop();
		if ( buffer ) {
			writeSnapshot(
				buffer,
				sequence,
				timeMs,
				performance.timeOrigin + performance.now(),
				acceptedInputSequence
			);
			send( { kind: "snapshot", buffer, clock: clock.sample() }, [ buffer ] );
		}
	}
	const clock = createClock( step, fail );
	return {
		/*
		================
		receive
		================
		*/
		receive( message: HostMessage ) {
			if ( stopped ) {
				return;
			}
			if ( message.kind === "start" ) {
				if ( message.version !== PROTOCOL_VERSION ) {
					fail( "Unsupported host protocol" );
					return;
				}
				if ( !started ) {
					started = true;
					clock.start();
				}
			} else if ( message.kind === "session" ) {
				const origin = clock.sample().originMs;
				session.command(
					message.command,
					origin ? Math.max( 0, performance.timeOrigin + performance.now() - origin ) : undefined
				);
			} else if ( message.kind === "world-ack" ) {
				try {
					session.ackWorld( message.sequence );
				} catch ( error ) {
					fail( error );
				}
			} else if ( message.kind === "input" ) {
				try {
					input.receive( message.batch );
				} catch ( error ) {
					fail( error );
				}
			} else if ( message.kind === "recycle" ) {
				if ( message.buffer.byteLength !== SNAPSHOT_BYTES || pool.length >= MAX_SNAPSHOTS ) {
					fail( "Invalid recycled snapshot buffer" );
					return;
				}
				pool.push( message.buffer );
			} else {
				stopped = true;
				clock.dispose();
				session.dispose();
			}
		}
	};
}

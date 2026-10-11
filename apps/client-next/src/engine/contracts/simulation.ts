import type { SessionCommand, SessionState } from "./session";
import type { InputBatch } from "./input";
export const PROTOCOL_VERSION = 3;
export const SNAPSHOT_BYTES = 32;
export const SIMULATION_STEP_MS = 16;
export type HostMessage = { kind: "world-ack"; sequence: number; } | {
	kind: "session";
	command: SessionCommand;
} | {
	kind: "input";
	batch: InputBatch;
} | {
	kind: "start";
	version: 3;
} | {
	kind: "recycle";
	buffer: ArrayBuffer;
} | {
	kind: "stop";
};
export type WorkerMessage = { kind: "world"; batch: import("./world").WorldBatch; } | {
	kind: "session";
	state: SessionState;
} | {
	kind: "snapshot";
	/** Last completed wake; the publishing wake is still executing. */
	clock?: import("./runtime").ClockSample;
	buffer: ArrayBuffer;
} | {
	kind: "failure";
	message: string;
} | {
	// The transport's frame journal for bug reports, about once a second.
	kind: "net-journal";
	records: Float64Array;
};
// Foundation snapshots intentionally contain no fabricated gameplay state.
export function writeSnapshot(
	buffer: ArrayBuffer,
	sequence: number,
	timeMs: number,
	publishedAtMs: number,
	acceptedInputSequence = 0
): void {
	const view = new DataView( buffer );
	view.setUint32( 0, PROTOCOL_VERSION, true );
	view.setUint32( 4, sequence, true );
	view.setFloat64( 8, timeMs, true );
	view.setFloat64( 16, publishedAtMs, true );
	view.setUint32( 24, acceptedInputSequence, true );
	view.setUint32( 28, 0, true );
}
export function readSnapshot( buffer: ArrayBuffer ): {
	sequence: number;
	timeMs: number;
	publishedAtMs: number;
	acceptedInputSequence: number;
} {
	if ( buffer.byteLength !== SNAPSHOT_BYTES ) {
		throw new Error( "Invalid snapshot size" );
	}
	const view = new DataView( buffer );
	if ( view.getUint32( 0, true ) !== PROTOCOL_VERSION ) {
		throw new Error( "Unsupported simulation snapshot version" );
	}
	const timeMs = view.getFloat64( 8, true ), publishedAtMs = view.getFloat64( 16, true );
	if ( !Number.isFinite( timeMs ) || timeMs < 0 || !Number.isFinite( publishedAtMs ) || publishedAtMs < 0 ) {
		throw new Error( "Invalid simulation time" );
	}
	return {
		sequence: view.getUint32( 4, true ),
		timeMs,
		publishedAtMs,
		acceptedInputSequence: view.getUint32( 24, true )
	};
}

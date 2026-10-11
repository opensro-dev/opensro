/*
===========================================================================

network.ts - the world transport contract: frames and the socket owner

===========================================================================
*/
/*
================
WireFrame
================
*/
export interface WireFrame {
	readonly opcode: number;
	readonly payload: Uint8Array;
}
// The transport frame journal (NetworkOwner.takeJournal): per frame, these
// many numbers: epoch ms, direction (NET_JOURNAL_IN or _OUT), opcode, bytes.
export const NET_JOURNAL_FIELDS = 4;
export const NET_JOURNAL_IN = 0;
export const NET_JOURNAL_OUT = 1;
/*
================
NetworkOwner
================
*/
export interface NetworkOwner {
	pingMs(): number | null;
	connect( url: string, admission: string, resume?: Uint8Array ): void;
	send( frame: WireFrame ): void;
	// False retains this frame and its successors while admission awaits data.
	drain( consume: ( frame: WireFrame ) => boolean | void ): void;
	disconnect(): void;
	enterWorld( division: string, character: string, token: string ): void;
	// Frames journaled since the last take (epoch ms, direction, opcode,
	// bytes per frame), or null; the bug report's network.json.
	takeJournal(): Float64Array | null;
	dispose(): void;
}
/*
================
ClientIncident

A fatal client failure reported to the Agent (POST /client/incident) before
the session ends. payload is a hex dump of at most INCIDENT_DUMP_BYTES of
the frame being applied; payloadSize is its full length.
================
*/
export interface ClientIncident {
	readonly id?: string;
	readonly session?: string;
	readonly code?: string;
	readonly category?: NetworkFailure["category"];
	readonly stack?: string;
	readonly kind: "packet" | "transport" | "runtime" | "asset" | "unsupported";
	readonly message: string;
	readonly opcode?: number;
	readonly payload?: string;
	readonly payloadSize?: number;
	readonly phase?: string;
	readonly character?: string;
	readonly region?: number;
	readonly build?: string;
}

/*
================
NetworkFailure
================
*/
export interface NetworkFailure {
	stack?: string;
	readonly category: "expected" | "connection" | "software" | "unknown";
	readonly code: string;
	readonly message: string;
}
// The Agent's bound on the dumped frame bytes (client_incident.go).
export const INCIDENT_DUMP_BYTES = 512;
// The Agent's bound on the message length.
export const INCIDENT_MESSAGE_LENGTH = 500;

// A missing movement receipt invalidates prediction; the session must resume
// through fresh server state without replaying the unacknowledged command.
export const MOVEMENT_RECEIPT_TIMEOUT = "movement_receipt_timeout";

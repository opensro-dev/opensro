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
/*
================
NetworkOwner
================
*/
export interface NetworkOwner {
	connect( url: string, admission: string, resume?: Uint8Array ): void;
	send( frame: WireFrame ): void;
	// False retains this frame and its successors while admission awaits data.
	drain( consume: ( frame: WireFrame ) => boolean | void ): void;
	disconnect(): void;
	enterWorld( division: string, character: string, token: string ): void;
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

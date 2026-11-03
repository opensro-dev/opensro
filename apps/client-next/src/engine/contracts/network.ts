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
// A gap: frames the journal dropped (bytes = how many), timed at the last.
export const NET_JOURNAL_GAP = 2;
// A fatal incident carries the frames before it (NetworkOwner.recentJournal):
// no failure that ends a session should arrive without its evidence. The
// Agent bounds the same rows (client_incident.go maxIncidentJournalRows).
export const INCIDENT_JOURNAL_ROWS = 200;
// Older rows are not evidence of this failure (maxIncidentJournalAge); the
// Agent refuses a report that carries one, so they are left out here.
export const INCIDENT_JOURNAL_AGE_MS = 600000; // ten minutes
/*
================
NetworkOwner
================
*/
export interface NetworkOwner {
	pingMs(): number | null;
	// Every frame received on the current socket, transport control (pings,
	// pongs) included: a live link changes it even when no game frame comes.
	received(): number;
	// Every inbound byte, counted as it arrives: a record still being
	// reassembled (the ~1 MB 0x0007 answer) moves this long before
	// received() can, so the world's stall detector reads a link that
	// is still streaming as alive (#2311).
	receivedBytes(): number;
	// When the last frame arrived, on the event clock (performance.now()),
	// or -Infinity before the current socket's first frame. The one owner
	// of the last-inbound time (#1474): the close detail's silence and the
	// world session's stall wording both read this stamp, never a second
	// clock's dating of it.
	receivedAt(): number;
	connect( url: string, admission: string, resume?: Uint8Array ): void;
	// Experimental (#1448): attempt WebTransport (QUIC) before the socket
	// on connect, the socket the fallback when the path fails. Applies
	// from the next connect; off is the socket alone.
	webTransport( enabled: boolean ): void;
	send( frame: WireFrame ): void;
	// False retains this frame and its successors while admission awaits data.
	drain( consume: ( frame: WireFrame ) => boolean | void ): void;
	disconnect(): void;
	enterWorld( division: string, character: string, token: string ): void;
	// Frames journaled since the last take (epoch ms, direction, opcode,
	// bytes per frame), or null; the bug report's network.json.
	takeJournal(): Float64Array | null;
	// The last INCIDENT_JOURNAL_ROWS frames whatever takeJournal drained, as
	// flattened rows: ms before now (<= 0, whole), direction, opcode, bytes.
	recentJournal(): number[];
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
	readonly category?: NetworkFailure["category"] | "degraded";
	readonly stack?: string;
	// "degraded" alone is non-fatal: a cosmetic step was skipped (Degradation).
	readonly kind: "packet" | "transport" | "runtime" | "asset" | "unsupported" | "degraded";
	readonly message: string;
	readonly opcode?: number;
	readonly payload?: string;
	readonly payloadSize?: number;
	readonly phase?: string;
	readonly character?: string;
	readonly region?: number;
	readonly build?: string;
	// The frames before the failure (NetworkOwner.recentJournal), never payloads.
	readonly journal?: readonly number[];
}

/*
================
NetworkFailure
================
*/
export interface NetworkFailure {
	stack?: string;
	// What the browser said about a lost connection (closeDetail): reported
	// with the incident, never part of the error text the retry rule matches.
	readonly detail?: string;
	readonly category: "expected" | "connection" | "software" | "unknown";
	readonly code: string;
	readonly message: string;
}
/*
================
Degradation

A cosmetic step the client skipped instead of ending the session: an effect
whose item it cannot resolve, a visual with nothing to draw. Only a frame
whose rest still parses may degrade; one that leaves the stream unreadable
stays fatal. code names the site, stable across builds, so the console
groups by it and an agent finds the code from it (apps/client-next
AGENTS.md "Degraded reports").
================
*/
export interface Degradation {
	readonly code: string;
	readonly message: string;
	readonly frame?: WireFrame;
}
// One diagnostic session reports each degraded code once, and at most this
// many codes: a bad item or effect cannot flood the Agent.
export const DEGRADED_CODES_PER_SESSION = 16;
// The Agent's bound on the dumped frame bytes (client_incident.go).
export const INCIDENT_DUMP_BYTES = 512;
// The Agent's bound on the message length.
export const INCIDENT_MESSAGE_LENGTH = 500;

// A missing movement receipt invalidates prediction; the session must resume
// through fresh server state without replaying the unacknowledged command.
export const MOVEMENT_RECEIPT_TIMEOUT = "movement_receipt_timeout";
// An unanswered inventory transaction is the same loss on the item lane: the
// transport keeps no frame already written to a dying socket (server
// transport doc.go), so the request or its answer is gone. Native replies
// carry no transaction ID; the session resumes through fresh server state.
export const INVENTORY_TRANSACTION_TIMEOUT = "inventory_transaction_timeout";
// The target lane's release (B4B3) carries no identity either: an unanswered
// one is the same loss, resumed the same way.
export const TARGET_RELEASE_TIMEOUT = "target_release_timeout";
// The connection itself stalled: a request has waited TRANSPORT_STALL_MS
// (world.ts) with no frame of any kind arriving since it left. The lane
// timeouts above would fire later for the same loss and blame the request;
// this resumes the session sooner and names the link.
export const TRANSPORT_STALLED = "transport_stalled";

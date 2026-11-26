// Package transport is the browser-facing game transport for the Go gateway:
// WebTransport (HTTP/3 over UDP) as the primary channel with a WebSocket
// alternate served from the same binary. It replaces the Node HTTP GET/POST
// bridge; the game plane speaks binary frames, never request/response JSON.
//
// # Wire contract (the envelope)
//
// A frame is a native SRO opcode plus an opaque payload:
//
//	frame = nativeOpcode uint16 (little-endian) ++ payload bytes
//
// Encodings per channel:
//
//   - WebSocket: one binary WebSocket message per frame, no extra framing.
//   - WebTransport bidirectional stream: length-prefixed. Each frame is
//     preceded by a uint32 little-endian length equal to 2+len(payload).
//     The stream is the session's reliable, ordered channel. A client
//     that asks for it in the session URL (?compress=gzip) may be sent a
//     large server frame gzip-compressed: the length's top bit marks the
//     record, and the other bits are the gzip stream's length
//     (conn_wt_compress.go; port-only, off by default).
//   - WebTransport datagram: one datagram per frame, no length prefix.
//     Unreliable and unordered; used for state that is superseded every
//     tick (movement), never for state transitions.
//
// Client frames are capped at 64 KiB before allocation or dispatch.
// Server frames retain a 16 MiB ceiling for complete bootstrap snapshots.
//
// Native opcodes are the SRO v1.150 opcodes already used by internal/game/item/wire
// (0x706D item move, 0xB06D result, 0x30D7 spawn, ...). The range
// 0x0001-0x00FF never appears in the native plane and belongs to the
// transport plane, split in two:
//
//   - 0x0001-0x0005 session-internal: HELLO, WELCOME, PING, PONG, BYE.
//     Handled inside the transport; not registrable.
//   - 0x0006-0x00FF control extensions with frozen layouts (envelope.go)
//     whose semantics the game lanes implement via Hub.Handle:
//     0x0006 OpEnterWorld (C->S): [u16 divLen][div][u16 nameLen][name]
//     [u16 tokenLen][token], where the launcher-issued token is required for
//     the post-WELCOME identity bind. The transport enforces
//     single-bind per division:character (BindExclusive: the older
//     session gets BYE reason 6 Replaced and its resume token dies) and,
//     verifies the token BEFORE the game handler runs. Every server,
//     including loopback tests, must install the verifier before Server.Start
//     opens either listener.
//     0x0007 OpEnterWorldResult (S->C): [u8 ok][u32 err][u32 blobLen][blob]
//     — blob is the versioned bootstrap sidecar (DTO minus packets);
//     world state then arrives as ordinary native frames in order.
//     0x0008 OpStructuredSidecar (S->C): [u16 kind][u32 blobLen][blob] —
//     structured fields native packets cannot carry, pushed in-order
//     after the triggering request's native frames. Serial per-session
//     dispatch makes FIFO-per-kind correlation sound without request
//     IDs.
//
// All integers little-endian, matching the native SRO plane.
//
// HELLO has one current layout:
//
//	[u8 version=2][u8 resumeLen][resume token]
//	[u16 admissionLen][one-use Agent-minted admission ticket]
//
// The ticket authenticates the account and owned shard before a Session is
// allocated. SetHelloAuth installs its verifier; Server.Start fails closed
// when no verifier is installed. WELCOME carries the current protocol version.
//
// # Session lifecycle
//
// After the transport connects (WT session or WS upgrade), the client must
// send HELLO within HelloTimeout. A HELLO without a resume token creates a
// session; the server replies WELCOME carrying the session ID and a
// 16-byte resume token. When the connection drops, the session detaches
// and survives for GracePeriod; a new connection presenting HELLO with the
// old token reattaches to it and receives WELCOME with resumed=1, followed
// by any frames queued while detached. A HELLO whose token is unknown or
// expired silently gets a fresh session (resumed=0) so clients recover
// from long outages without a special path.
//
// Delivery guarantee across a reconnect: frame order is preserved and
// frames not yet handed to the dying transport are kept, but a frame
// already written to a socket that never arrived is not re-sent. Game
// lanes that need a hard guarantee re-push authoritative state from the
// OnSessionResumed hook.
//
// # Plugging in game handlers
//
// The Hub owns dispatch. Register per-opcode handlers before Server.Start:
//
//	ts, _ := transport.NewServerFromViper()
//	ts.Hub.Handle(wire.OpItemMoveRequest, func(s *transport.Session, op uint16, payload []byte) { ... })
//	ts.Hub.OnSessionOpen(func(s *transport.Session) { ... })
//	ts.Hub.OnSessionResumed(func(s *transport.Session) { /* re-push state */ })
//	ts.Start()
//
// Handlers for one session run serially on that session's read loop, so
// per-session ordering matches the Node server's per-connection semantics.
// Session.Send queues a reliable frame (payload copied; buffers reusable).
// Session.SendUnreliable uses a WT datagram when the client is on
// WebTransport and falls back to the reliable queue on WebSocket. For
// per-entity superseded state (the movement tick), use
// Session.SendUnreliableKeyed(op, entityGid, payload): on WebSocket it
// coalesces per (opcode, key) so a backlogged client receives only the
// latest position per entity and backlog cannot close the session.
// Hub.BroadcastFunc(pred, op, payload) pushes to a filtered session set
// (division scoping etc) without the transport knowing game concepts.
//
// # Dev TLS and how the browser trusts it
//
// WebTransport requires TLS. For development the server self-manages an
// ECDSA P-256 self-signed certificate valid for 12 days — inside the
// 14-day limit the WebTransport serverCertificateHashes mechanism imposes
// — regenerating it on startup when missing or within 48h of expiry
// (certs.go). The browser client does NOT need the cert in any trust
// store: it fetches GET /transport/cert-hash from the TCP listener, which
// returns the SHA-256 of the certificate's DER encoding, and passes it to
// the constructor:
//
//	const info = await (await fetch("http://127.0.0.1:8788/transport/cert-hash")).json();
//	const wt = new WebTransport("https://127.0.0.1:8788/transport/wt", {
//	    serverCertificateHashes: [{
//	        algorithm: "sha-256",
//	        value: Uint8Array.from(atob(info.sha256Base64), c => c.charCodeAt(0)),
//	    }],
//	});
//
// The WebSocket alternate runs plain ws:// on the TCP listener; browsers
// treat 127.0.0.1 as a secure context so a https-served page may open it
// in dev. Production terminates real TLS (wss://) in front of the TCP
// listener or supplies a CA-issued cert.
package transport

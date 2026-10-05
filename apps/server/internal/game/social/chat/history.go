/*
===========================================================================

history.go - bounded beta public chat and ordered session admission

One runtime owns the public transcript and its recipients. Admission replays
the last ten public messages before subscribing to live delivery under the
same lock, preventing a message from being missed or replayed twice. The
replay is one OpChatHistory frame, never live 0x3667 lines, so the client
can tell a transcript from speech. Private channels never enter this owner.
History is ephemeral and ends with the shard.

===========================================================================
*/
package chat

import (
	"sync"

	"opensro.online/server/internal/transport"
)

const publicHistoryLimit = 10

/*
================
publicMember
================
*/
type publicMember struct {
	division string
	session  *transport.Session
}

/*
================
Runtime

This is the beta public-chat owner, not a store for whispers, guild or party
messages. Registration creates one instance for the lifetime of the hub.
================
*/
type Runtime struct {
	mu      sync.Mutex
	history map[string][][]byte
	members map[uint64]publicMember
}

/*
================
WorldBound

The composition root calls this after exclusive character admission. Scene
reentry on the same session retains its client transcript and does not replay.
================
*/
func (rt *Runtime) WorldBound(session *transport.Session, division string) {
	if !ClosedBetaGlobalChat || session == nil || !session.WorldReady() || session.Evicted() {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if _, exists := rt.members[session.ID]; exists {
		return
	}
	if rows := rt.history[division]; len(rows) != 0 {
		if err := session.Send(OpChatHistory, encodeChatHistory(rows)); err != nil {
			return
		}
	}
	rt.members[session.ID] = publicMember{division: division, session: session}
}

/*
================
sessionClosed
================
*/
func (rt *Runtime) sessionClosed(session *transport.Session, _ error) {
	rt.mu.Lock()
	delete(rt.members, session.ID)
	rt.mu.Unlock()
}

/*
================
publish

Only the server's named global broadcast is eligible. Send the authoritative
line to its author too: the native acknowledgement echoes the requested
channel and therefore cannot describe a beta remap from All to Global.
================
*/
func (rt *Runtime) publish(division string, payload []byte) {
	if len(payload) == 0 || payload[0] != ChatTypeGlobal {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rows := rt.history[division]
	if len(rows) == publicHistoryLimit {
		copy(rows, rows[1:])
		rows = rows[:publicHistoryLimit-1]
	}
	rows = append(rows, append([]byte(nil), payload...))
	rt.history[division] = rows
	for _, member := range rt.members {
		if member.division == division && member.session.WorldReady() && !member.session.Evicted() {
			_ = member.session.Send(OpChatBroadcast, payload)
		}
	}
}

/*
================
encodeChatHistory

The OpChatHistory payload: version, count, then each retained 0x3667
payload with its u16 length. The transcript holds at most ten lines, each
bounded by the chat text limit, so the frame stays small.
================
*/
func encodeChatHistory(rows [][]byte) []byte {
	out := []byte{chatHistoryVersion, byte(len(rows))}
	for _, row := range rows {
		out = append(out, byte(len(row)), byte(len(row)>>8))
		out = append(out, row...)
	}
	return out
}

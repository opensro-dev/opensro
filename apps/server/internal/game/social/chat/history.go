/*
===========================================================================

history.go - bounded beta public chat and ordered session admission

One runtime owns the public transcript and its recipients. Admission replays
the last ten public messages before subscribing to live delivery under the
same lock, preventing a message from being missed or replayed twice. The
replay is one OpChatHistory frame, never live 0x3667 lines, so the client
can tell a transcript from speech. Each retained line keeps the server time
it was said, so a player who logs in later sees when it was really sent
(BUG-067). Private channels never enter this owner. History is ephemeral and
ends with the shard.

===========================================================================
*/
package chat

import (
	"encoding/binary"
	"opensro.online/server/internal/game/item/wire"
	"sync"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/game/social/chatreport"
	"opensro.online/server/internal/transport"
)

const publicHistoryLimit = 10

/*
================
publicLine

One retained public line: its 0x3667 payload and the server wall-clock
time it was published, in Unix milliseconds.
================
*/
type publicLine struct {
	payload  []byte
	sentAtMs int64
	// prelude is the line's item reference frames (itemlinks.go), sent
	// ahead of it live and ahead of the replayed history.
	prelude []wire.Frame
}

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
	history map[string][]publicLine
	members map[uint64]publicMember
	// now stamps published lines; Register sets time.Now, tests may pin it.
	now func() time.Time

	// Unions answers union chat; set by wiring before the hub serves.
	Unions UnionView
	// Stalls answers stall chat; set by wiring before the hub serves.
	Stalls StallView
	// ItemLinks is the port-only SRO_CHAT_ITEM_LINKS switch (itemlinks.go):
	// nil keeps the native lane. Set by wiring before the hub serves.
	ItemLinks *ItemLinks
	// Feed receives every published public line for the website
	// (chatfeed, port-only); nil keeps the transcript in-game only.
	Feed *chatfeed.Feed
	// ShadowMuted reports a staff shadow-muted line, by account or by word (shadowmute,
	// port-only, not native); nil mutes no one.
	ShadowMuted func(accountID, text string) bool
	// ModerationLog keeps every accepted line for the staff (chatlog,
	// port-only, not native), muted lines included and marked; nil
	// keeps no log.
	ModerationLog *chatlog.Log
	// Reports is the /report chat-abuse intake (chatreport, port-only,
	// not native, #2959); nil leaves "/report ..." as ordinary chat
	// text, the native posture for a command the original lacks.
	Reports *chatreport.Service
	// Contributor is the /contributor title switch (contributor,
	// port-only, not native, #3358): the answer line and true when the
	// line was the command; nil leaves "/contributor" ordinary text.
	Contributor func(accountID, message string) (answer string, handled bool)
	// PlusFailNotice commits the speaker's failed-reinforcement
	// announcement choice (the /plusfail command, plusfail.go,
	// port-only, not native, #3682): on asks for the announcements on,
	// and the result reports whether the choice was stored. Nil
	// leaves "/plusfail" ordinary text.
	PlusFailNotice func(character *enterworld.Character, on bool) bool
	// EventBotWhisper is the EventBot host's intake (social/eventbot,
	// port-only, not native, #3533): the host is asked about every
	// whisper and claims the ones addressed to it (handled), answering
	// with the bot's reply lines. Nil is never asked: a whisper to the
	// bot's name is the native cannot-find-target refusal.
	EventBotWhisper func(divisionID string, sender *enterworld.Character, target, text string) (replies []string, handled bool)
	// CaravanCommands is the AI caravan command intake (caravanai,
	// port-only, not native, #3704 slice 4): it is offered every All
	// line and claims the caravan commands (!accept/!hire/!memory),
	// answering with a whisper authored by the named agent (speaker),
	// a notice when no agent answers, or nothing when the reply is
	// empty (the rate limit). Nil is never asked: the lines stay
	// ordinary chat, the native posture for commands the original
	// lacks.
	CaravanCommands func(divisionID string, sender *enterworld.Character, message string) (speaker, reply string, handled bool)
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
		for _, row := range rows {
			sendPrelude(session, row.prelude)
		}
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
author is the sender's character name, threaded from the handler: the
payload carries only the display name, and the feed's identity must never
be decoded back out of it. text is the line as the feed stores it
(Outcome.PlainText: item-link markers replaced by labels).
================
*/
func (rt *Runtime) publish(division, author, text string, line publicLine) {
	payload := line.payload
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
	rows = append(rows, publicLine{payload: append([]byte(nil), payload...), sentAtMs: rt.now().UnixMilli(), prelude: line.prelude})
	rt.history[division] = rows
	if _, name, _, ok := decodeNamedHead(payload); ok {
		rt.Feed.Record(division, chatfeed.KindChat, author, name, text)
	}
	for _, member := range rt.members {
		if member.division == division && member.session.WorldReady() && !member.session.Evicted() {
			sendPrelude(member.session, line.prelude)
			_ = member.session.Send(OpChatBroadcast, payload)
		}
	}
}

/*
================
encodeChatHistory

The OpChatHistory payload (version 2): version, count, then for each
retained line its send time as u64 little-endian Unix milliseconds, its u16
payload length and the 0x3667 payload. The transcript holds at most ten
lines, each bounded by the chat text limit, so the frame stays small.
================
*/
func encodeChatHistory(rows []publicLine) []byte {
	out := []byte{chatHistoryVersion, byte(len(rows))}
	for _, row := range rows {
		out = binary.LittleEndian.AppendUint64(out, uint64(row.sentAtMs))
		out = append(out, byte(len(row.payload)), byte(len(row.payload)>>8))
		out = append(out, row.payload...)
	}
	return out
}

/*
===========================================================================

baseline.go - which guild block each session holds, and delta delivery

A client applies a 0x3B29 guild delta (any subOp but 0, 1 and the war
reply) only to the guild block it already holds: the 0x32C4 seed at entry,
the 0x32C4 a joiner receives, the 0xB663 create answer or a re-seed. A
delta that reaches a session before that block ends the session ("Guild
update without baseline"); prod 2026-10-12 caught a member's leave
reaching such a session. Every delta and every block now go through this
file: a session receives a guild's deltas only after that guild's block
was queued to it, and never once the block is retired (leave, kick,
break, a later entry without the guild).

The order is per session: the block's enqueue and its mark, and a delta's
check and enqueue, run under that session's baseline lock, so no delta
can slip between a block and its mark. Sends only enqueue; nothing that
holds the session's own lock takes this one, and no store door is held
here.

===========================================================================
*/
package guild

import (
	"sync"

	"opensro.online/server/internal/game/item/wire"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/transport"
)

/*
================
sessionBaseline

The guild block one session holds; zero is none.
================
*/
type sessionBaseline struct {
	mu    sync.Mutex
	guild int64
}

// baselines maps a session ID to its *sessionBaseline.
var baselines sync.Map

/*
================
baselineOf
================
*/
func baselineOf(s *transport.Session) *sessionBaseline {
	value, _ := baselines.LoadOrStore(s.ID, &sessionBaseline{})
	return value.(*sessionBaseline)
}

/*
================
SendBaseline

Queues a guild block (0x32C4, 0xB663 or the entry batch carrying the seed)
and records that the session now holds guildID's block. guildID 0 records
that it holds none, as an entry without a guild does.
================
*/
func SendBaseline(s *transport.Session, guildID int64, send func() error) error {
	if s == nil {
		return send()
	}
	b := baselineOf(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := send(); err != nil {
		return err
	}
	b.guild = guildID
	return nil
}

/*
================
SendDelta

Queues guild deltas to a session only while it holds guildID's block.
Reports whether they were queued.
================
*/
func SendDelta(s *transport.Session, guildID int64, send func() error) bool {
	if s == nil || guildID == 0 {
		return false
	}
	b := baselineOf(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.guild != guildID {
		return false
	}
	return send() == nil
}

/*
================
WithBlock

Runs send under the session's baseline lock, telling it whether the
session holds guildID's block, so a mixed batch can drop its deltas and
keep its other frames in one enqueue.
================
*/
func WithBlock(s *transport.Session, guildID int64, send func(holds bool)) {
	if s == nil {
		send(false)
		return
	}
	b := baselineOf(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	send(guildID != 0 && b.guild == guildID)
}

/*
================
RetireBaseline

The session no longer holds guildID's block: its own leave, kick or the
guild's break was queued to it. A block of another guild stays.
================
*/
func RetireBaseline(s *transport.Session, guildID int64) {
	if s == nil {
		return
	}
	b := baselineOf(s)
	b.mu.Lock()
	if b.guild == guildID {
		b.guild = 0
	}
	b.mu.Unlock()
}

/*
================
ForgetSession

Drops a closed session's record.
================
*/
func ForgetSession(s *transport.Session) {
	if s != nil {
		baselines.Delete(s.ID)
	}
}

/*
================
EntryGuild

The guild whose 0x32C4 an entry seed carries: the character's persisted
guild when its record exists (AppendSeedFrame), else 0.
================
*/
func EntryGuild(guilds domain.GuildStore, divisionID string, character *domain.Character) int64 {
	if guilds == nil || character == nil || character.GuildID == nil {
		return 0
	}
	if _, _, ok := guilds.Guild(divisionID, *character.GuildID); !ok {
		return 0
	}
	return *character.GuildID
}

/*
================
EntryPublisher

The enter-world PublishEntry hook: the entry batch (with its 0x32C4 seed,
or none) is queued under the session's baseline lock and records the guild
the seed carries. The GameWorld and the wire tests install the same hook.
================
*/
func EntryPublisher(guilds func() domain.GuildStore) func(*transport.Session, string, *domain.Character, func() error) error {
	return func(s *transport.Session, divisionID string, character *domain.Character, send func() error) error {
		return SendBaseline(s, EntryGuild(guilds(), divisionID, character), send)
	}
}

/*
================
FilterDeltas

A member's mixed guild batch as one session may receive it: everything
while it holds the guild's block, otherwise every frame but the 0x3B29
deltas, in order.
================
*/
func FilterDeltas(holds bool, frames []wire.Frame) []wire.Frame {
	if holds {
		return frames
	}
	kept := make([]wire.Frame, 0, len(frames))
	for _, frame := range frames {
		if frame.Opcode != OpGuildUpdatePush {
			kept = append(kept, frame)
		}
	}
	return kept
}

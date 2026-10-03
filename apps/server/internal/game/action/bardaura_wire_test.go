/*
===========================================================================

bardaura_wire_test.go - what a party member's client receives when one
instrument aura replaces another

Live finding: when a Bard's new instrument replaced its own previous one,
or two Bards' instruments were settled, the losing aura's copies ended and
the new aura's copies were installed in one update, and the member's client
received them in an order that left it wrong: an END (0xB6A0) for a token
whose INSTALL (0xB419) arrived after it, a phantom icon for good, and a last
0x343C still counting the ended aura. These tests record the frames each
character receives the way production delivers them (the pushed frames
during the tick, then the tick's returned frames) and check, per member,
the order, the tokens and the final stats.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// attachedEffectTokenOffset is the InstanceToken's offset in a 0xB419
// payload: GID, then SkillID, then the token.
const attachedEffectTokenOffset = 8

/*
================
memberWire

The ordered frames each recorded character receives. ticks keeps one
slice per recorded tick so a test can count one update's frames.
================
*/
type memberWire struct {
	rt     *Runtime
	chars  []*enterworld.Character
	frames map[string][]wire.Frame
	ticks  []map[string][]wire.Frame
}

/*
================
recordMemberWire

Install the production push hooks so pushed frames reach the recorder in
the order the runtime pushes them.
================
*/
func recordMemberWire(rt *Runtime, chars ...*enterworld.Character) *memberWire {
	w := &memberWire{rt: rt, chars: chars, frames: map[string][]wire.Frame{}}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		w.deliver(name, frames)
	}
	rt.PushDivisionPeerFrames = func(_, except string, frames []wire.Frame) {
		for _, c := range w.chars {
			if c.Name != except {
				w.deliver(c.Name, frames)
			}
		}
	}
	return w
}

// deliver appends frames to name's stream and to the open tick's.
func (w *memberWire) deliver(name string, frames []wire.Frame) {
	w.frames[name] = append(w.frames[name], frames...)
	if len(w.ticks) != 0 {
		open := w.ticks[len(w.ticks)-1]
		open[name] = append(open[name], frames...)
	}
}

/*
================
tick

Advance the clock by d and run one simulation tick. The tick's returned
frames reach every recorded character after the frames it pushed; a private
batch reaches its character only.
================
*/
func (w *memberWire) tick(clock *fakeClock, d time.Duration) {
	clock.Advance(d)
	w.ticks = append(w.ticks, map[string][]wire.Frame{})
	for _, batch := range w.rt.TickHook()(clock.NowMs()) {
		var frames []wire.Frame
		for _, frame := range batch.Frames {
			frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
		}
		for _, c := range w.chars {
			if batch.OnlyCharacterID == 0 || batch.OnlyCharacterID == c.ID {
				w.deliver(c.Name, frames)
			}
		}
	}
}

// cast casts id on c after the previous cast's bracket closed.
func (w *memberWire) cast(t *testing.T, clock *fakeClock, c *enterworld.Character, id uint32) {
	t.Helper()
	w.tick(clock, bardCastGap)
	if result := castSelf(w.rt, c, id); result.DiagnosticRefusal != "" || !hasSkillEffect(w.rt, c.Name, id) {
		t.Fatalf("%s: cast %d refused: %+v", c.Name, id, result)
	}
}

/*
================
check

Per recorded character: no token is installed after its end, no ended
token is still live on anyone, inside every tick each 0xB6A0 precedes each
0xB419 and at most one 0x343C arrives. For each of members, the last 0x343C
received equals the character's stats now; a Bard's own stats ride its cast
replies, which this recorder does not see.
================
*/
func (w *memberWire) check(t *testing.T, members ...*enterworld.Character) {
	t.Helper()
	live := map[uint32]bool{}
	for _, c := range w.chars {
		for _, e := range w.rt.effects.Snapshot(testDivision, c.Name) {
			live[e.InstanceToken] = true
		}
	}
	for _, c := range w.chars {
		ended := map[uint32]bool{}
		var last []byte
		for _, frame := range w.frames[c.Name] {
			switch frame.Opcode {
			case wire.OpEndedEffectInstances:
				body, err := wire.DecodeEndedEffectInstances(frame.Payload)
				if err != nil {
					t.Fatal(err)
				}
				for _, token := range body.InstanceTokens {
					ended[token] = true
					if live[token] {
						t.Errorf("%s: token %#x ended on the wire but still live", c.Name, token)
					}
				}
			case wire.OpAttachedEffect:
				if token := binary.LittleEndian.Uint32(frame.Payload[attachedEffectTokenOffset:]); ended[token] {
					t.Errorf("%s: token %#x installed after its end", c.Name, token)
				}
			case wire.OpBaseStats:
				last = frame.Payload
			}
		}
		for index, tick := range w.ticks {
			installed, stats := false, 0
			for _, frame := range tick[c.Name] {
				switch frame.Opcode {
				case wire.OpAttachedEffect:
					installed = true
				case wire.OpEndedEffectInstances:
					if installed {
						t.Errorf("%s: tick %d ended an instance after installing one", c.Name, index)
					}
				case wire.OpBaseStats:
					stats++
				}
			}
			if stats > 1 {
				t.Errorf("%s: tick %d sent %d stats frames, want one", c.Name, index, stats)
			}
		}
		w.checkLastStats(t, c, last, members)
	}
}

// checkLastStats compares c's last received 0x343C with its stats now when
// c is one of members.
func (w *memberWire) checkLastStats(t *testing.T, c *enterworld.Character, last []byte, members []*enterworld.Character) {
	t.Helper()
	for _, m := range members {
		if m != c {
			continue
		}
		now, err := w.rt.PlayerBaseStats(testDivision, c)
		if err != nil {
			t.Fatal(err)
		}
		if string(last) != string(now.Encode()) {
			t.Errorf("%s: last 0x343C %x, want the surviving set's %x", c.Name, last, now.Encode())
		}
	}
}

/*
================
TestOwnInstrumentReplacementReachesMembersInOrder

Hit March, then Clout March from the same Bard: the member ends up with
Clout March only, and its client saw the Hit copy end before the Clout copy
was installed, and a last 0x343C with Clout's hit rate alone.
================
*/
func TestOwnInstrumentReplacementReachesMembersInOrder(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, hitMarchFirstID)
	learnAura(t, rt, c, cloutMarchID)
	mate := partyMate(rt, c, 12, "replace-wire-mate", 100)
	setParty(rt, c, mate)
	base, err := rt.PlayerBaseStats(testDivision, mate)
	if err != nil {
		t.Fatal(err)
	}
	w := recordMemberWire(rt, c, mate)

	w.cast(t, clock, c, hitMarchFirstID)
	w.tick(clock, time.Millisecond)
	w.cast(t, clock, c, cloutMarchID)
	w.tick(clock, time.Millisecond)
	w.tick(clock, time.Millisecond)

	if hasSkillEffect(rt, mate.Name, hitMarchFirstID) || !hasSkillEffect(rt, mate.Name, cloutMarchID) {
		t.Fatal("the member does not hold Clout March alone")
	}
	now, err := rt.PlayerBaseStats(testDivision, mate)
	if err != nil {
		t.Fatal(err)
	}
	if want := base.HitRate + uint16(hitMarchFlat[cloutMarchID]); now.HitRate != want {
		t.Fatalf("member hit rate %d, want %d (Clout March alone)", now.HitRate, want)
	}
	w.check(t, mate)
}

/*
================
TestRivalInstrumentSettlementReachesMembersInOrder

A second Bard plays a lower instrument beside the first Bard's Guard
Tambour (rank 8): the lower aura is settled away in the update that would
first have joined it, and no member's client is left with a copy of it.
================
*/
func TestRivalInstrumentSettlementReachesMembersInOrder(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, guardTambour8ID)
	b := rivalBard(t, rt, c, hitMarchFirstID)
	mate := partyMate(rt, c, 13, "rival-wire-mate", 100)
	setParty(rt, c, b, mate)
	hit, ok := rt.deps.SkillData().SkillByID(hitMarchFirstID)
	guard, known := rt.deps.SkillData().SkillByID(guardTambour8ID)
	if !ok || !known || auraLevel(hit) >= auraLevel(guard) {
		t.Fatalf("Hit March must be the lower instrument: %d against %d", auraLevel(hit), auraLevel(guard))
	}
	w := recordMemberWire(rt, c, b, mate)

	w.cast(t, clock, c, guardTambour8ID)
	w.tick(clock, time.Millisecond)
	w.cast(t, clock, b, hitMarchFirstID)
	w.tick(clock, time.Millisecond)
	w.tick(clock, time.Millisecond)

	for _, who := range []*enterworld.Character{c, b, mate} {
		if hasSkillEffect(rt, who.Name, hitMarchFirstID) || !hasSkillEffect(rt, who.Name, guardTambour8ID) {
			t.Fatalf("%s does not hold Guard Tambour alone", who.Name)
		}
	}
	w.check(t, mate)
}

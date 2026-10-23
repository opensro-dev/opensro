package monster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"testing"
)

// This is a bounded comparison against emulated original-server instructions,
// with an injected random stream and explicit assertion boundary. It does not
// validate OS effects, concurrency, complete AI or live packet consumption.
func TestAITimerFrozenNativeDifferential(t *testing.T) {
	type outcome struct {
		Asserted bool
		Result   uint32
		Consumed int
		Slots    [][4]uint32
		Selected int
	}
	var corpus struct {
		Schema          string
		CandidateSHA256 string
		BinarySHA256    string
		Cases           []struct {
			Name     string
			Op       string
			ID       AITimerID
			Flags    uint8
			Base     uint32
			Jitter   uint32
			Now      uint32
			Select   bool
			Slots    [][4]uint32
			Selected int
			Draws    []uint32
			Expected outcome
		}
	}
	payload, err := licensed.ServerFixture(t, "internal/game/world/monster/testdata/ai-timer-native-v1188.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Schema != "sro-ai-timer-differential-v1" || !licensed.SHA256Hex(corpus.BinarySHA256) || len(corpus.Cases) == 0 {
		t.Fatal("invalid native differential evidence binding")
	}
	source, err := os.ReadFile("ai_time_manager.go")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != corpus.CandidateSHA256 {
		t.Fatal("timer candidate changed after native differential freeze; evidence is stale")
	}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			m := NewAITimeManager()
			if len(c.Slots) != 12 || len(c.Expected.Slots) != 12 {
				t.Fatal("invalid native bank snapshot")
			}
			for i, slot := range c.Slots {
				entry := AITimerEntry{ID: AITimerID(slot[0]), ArmedImmediate: slot[1] == 1, LastCheckMs: slot[2], IntervalMs: slot[3]}
				if i < 9 {
					m.first[i] = entry
				} else {
					m.second[i-9] = entry
				}
			}
			m.selected.active, m.selected.id = c.Selected != -1, AITimerID(c.Selected)
			consumed := 0
			next := func() uint32 {
				if consumed >= len(c.Draws) {
					t.Fatal("candidate consumed unexpected random draw")
				}
				value := c.Draws[consumed]
				consumed++
				return value
			}
			asserted, result := false, uint32(0)
			func() {
				defer func() { asserted = recover() != nil }()
				switch c.Op {
				case "init":
					m.InitAcquisitionTimer(c.Flags, next)
				case "set":
					result = m.SetTimer(c.ID, c.Base, c.Jitter, c.Now, c.Select, next)
				case "check":
					if m.CheckTimer(c.ID, c.Now) {
						result = 1
					}
				default:
					t.Fatalf("unknown corpus operation %q", c.Op)
				}
			}()
			if asserted != c.Expected.Asserted || consumed != c.Expected.Consumed {
				t.Fatalf("assert=%v/draws=%d, native assert=%v/draws=%d", asserted, consumed, c.Expected.Asserted, c.Expected.Consumed)
			}
			if !asserted && c.Op != "init" && result != c.Expected.Result {
				t.Fatalf("result=%d, native=%d", result, c.Expected.Result)
			}
			selected := -1
			if m.selected.active {
				selected = int(m.selected.id)
			}
			if selected != c.Expected.Selected {
				t.Fatalf("selected=%d, native=%d", selected, c.Expected.Selected)
			}
			for i, want := range c.Expected.Slots {
				var entry AITimerEntry
				if i < 9 {
					entry = m.first[i]
				} else {
					entry = m.second[i-9]
				}
				armed := uint32(0)
				if entry.ArmedImmediate {
					armed = 1
				}
				got := [4]uint32{uint32(entry.ID), armed, entry.LastCheckMs, entry.IntervalMs}
				if got != want {
					t.Fatalf("slot %d=%v, native=%v", i, got, want)
				}
			}
		})
	}
}

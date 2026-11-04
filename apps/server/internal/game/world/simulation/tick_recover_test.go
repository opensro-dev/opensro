package simulation

// Tick panic containment: the tick goroutine is the whole world's
// heartbeat, so a panic in a hook, a provider or one session's legs must
// cost at most that tick (or that session's slice of it) - never the
// process, and never the ticker.

import (
	"testing"
)

// panickyPusher panics on PushToSession for one designated session and
// records everything else like fakePusher.
type panickyPusher struct {
	fakePusher
	panicOn string
}

func (p *panickyPusher) PushToSession(sessionID string, frames []Frame) {
	if sessionID == p.panicOn {
		panic("pusher exploded for " + sessionID)
	}
	p.fakePusher.PushToSession(sessionID, frames)
}

// A panicking hook must not kill the ticker: the panic is recovered at the
// tick boundary and the NEXT tick runs the hook again.
func TestTickerSurvivesPanickingHook(t *testing.T) {
	source := &fakeSource{}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	calls := 0
	ticker.Hooks = []NamedTickHook{{Hook: func(nowMs int64) []DivisionFrames {
		calls++
		panic("hook exploded")
	}}}

	ticker.RunTick(1_784_000_000_000)
	ticker.RunTick(1_784_000_000_250)

	if calls != 2 {
		t.Fatalf("hook ran %d time(s), want 2 - the first panic stopped the ticker", calls)
	}
}

// A panicking session source must not kill the ticker either (the
// tick-level recover, not the per-session one).
func TestTickerSurvivesPanickingSource(t *testing.T) {
	panics := 0
	source := snapshotFunc(func() []SessionSnapshot {
		panics++
		if panics == 1 {
			panic("source exploded")
		}
		return nil
	})
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(1_784_000_000_000)
	ticker.RunTick(1_784_000_000_250)

	if panics != 2 {
		t.Fatalf("source polled %d time(s), want 2 - the panicking tick stopped the ticker", panics)
	}
}

// One bad session cannot blank the rest of the world's tick: the panic is
// contained per session, the other sessions get their frames and the
// tick's tail (hooks) still runs.
func TestTickerIsolatesPanickingSession(t *testing.T) {
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "bad", DivisionID: "DIV_A", CharacterID: 1, World: DefaultWorldState(EuropeStartProfile()), NpcAnchor: NpcShopSpawn(), NpcsEnabled: true},
		{SessionID: "good", DivisionID: "DIV_A", CharacterID: 2, World: DefaultWorldState(EuropeStartProfile()), NpcAnchor: NpcShopSpawn(), NpcsEnabled: true},
	}}
	push := &panickyPusher{panicOn: "bad"}
	ticker := newTestTicker(source, push)
	hookRan := false
	ticker.Hooks = []NamedTickHook{{Hook: func(nowMs int64) []DivisionFrames {
		hookRan = true
		return nil
	}}}

	ticker.RunTick(1_784_000_000_000)

	var goodPushes int
	for _, pushed := range push.toSession {
		if pushed.sessionID != "good" {
			t.Fatalf("frames leaked to session %q", pushed.sessionID)
		}
		goodPushes++
	}
	if goodPushes == 0 {
		t.Fatal("the good session got no frames - the bad session's panic blanked the tick")
	}
	if !hookRan {
		t.Fatal("the tick's hooks never ran - the per-session panic escaped its scope")
	}
}

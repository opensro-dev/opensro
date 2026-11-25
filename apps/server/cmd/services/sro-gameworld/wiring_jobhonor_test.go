/*
===========================================================================

wiring_jobhonor_test.go - the job honor tier roster's reach

One tiered character is online when the roster first goes out; a
tier-less session enters afterwards and must still get the standing
roster at the next sweep, on its own session, while nobody is sent
the same bytes twice.

===========================================================================
*/
package main

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/jobhonor"
)

/*
================
tierTickSource

The tick's session source: a session list the test extends between
sweeps, as entries arrive.
================
*/
type tierTickSource struct {
	sessions []simulation.SessionSnapshot
}

/*
================
tierTickSource::SnapshotSessions
================
*/
func (s *tierTickSource) SnapshotSessions() []simulation.SessionSnapshot {
	return s.sessions
}

// tierTickDelivery is one recorded push: the target (a session id, or
// the division marker) and the frames it carried.
type tierTickDelivery struct {
	target string
	frames []simulation.Frame
}

/*
================
tierTickPush

The tick's pusher: every delivery is recorded, nothing is sent.
================
*/
type tierTickPush struct {
	division []tierTickDelivery
	sessions []tierTickDelivery
}

/*
================
tierTickPush::PushToSession
================
*/
func (p *tierTickPush) PushToSession(sessionID string, frames []simulation.Frame) {
	p.sessions = append(p.sessions, tierTickDelivery{target: sessionID, frames: frames})
}

/*
================
tierTickPush::PushToDivision
================
*/
func (p *tierTickPush) PushToDivision(divisionID string, frames []simulation.Frame, _ string) {
	p.division = append(p.division, tierTickDelivery{target: divisionID, frames: frames})
}

/*
================
TestJobHonorTierTickReachesNewArrivals

The 0x3FC7 roster goes to the division when its bytes change; a
session that enters later - tier-less, so its own arrival changes no
byte - gets the standing roster from the next sweep, addressed to its
session alone.
================
*/
func TestJobHonorTierTickReachesNewArrivals(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	honor, err := jobhonor.Open(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer honor.Close()
	honor.Award(jobhonor.Award{Account: "a", CharacterID: 1, Job: jobhonor.JobTrader, Points: 300, At: now})
	if _, err := honor.Recompute(); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 11, 17, 5, 0, 30, 0, time.UTC)
	if _, err := honor.Recompute(); err != nil {
		t.Fatal(err)
	}
	held, _, err := honor.Previous()
	if err != nil || len(held) != 1 || held[0].CharacterID != 1 || held[0].Tier != jobhonor.TierKing {
		t.Fatalf("the ledger's snapshot is not the one tiered trader: %+v (err %v)", held, err)
	}

	traderSuit := uint16(3<<2 | 1<<5 | 7<<7 | jobhonor.JobTrader<<11)
	tiered := &domain.Character{ID: 1, Name: "Tiered", AccountID: "a",
		MissionInventory: []domain.InventoryRow{{Slot: domain.JobSuitSlot, TypeFlags: traderSuit}}}
	plain := &domain.Character{ID: 2, Name: "Plain", AccountID: "b"}
	deps := &enterworld.Deps{Characters: enterworld.StaticCharacterSource{
		"tier-division": {tiered, plain},
	}}
	game := &gameplayPlane{divisionID: "tier-division", deps: deps, jobHonor: honor}
	source := &tierTickSource{sessions: []simulation.SessionSnapshot{
		{SessionID: "one", DivisionID: "tier-division", CharacterID: 1},
	}}
	push := &tierTickPush{}
	ticker := &simulation.Ticker{Source: source, Push: push}
	wantRoster := wire.EncodeJobHonorTierRoster([]wire.JobHonorTierEntry{{Name: "Tiered", Tier: jobhonor.TierKing}})

	game.jobHonorTierTick(ticker, 0)
	if len(push.division) != 1 || len(push.division[0].frames) != 1 ||
		push.division[0].frames[0].Opcode != wire.OpJobHonorTierRoster ||
		!bytes.Equal(push.division[0].frames[0].Payload, wantRoster) {
		t.Fatalf("the first sweep did not push the roster to the division: %+v", push.division)
	}
	if len(push.sessions) != 0 {
		t.Fatalf("the change sweep also pushed per session: %+v", push.sessions)
	}

	source.sessions = append(source.sessions,
		simulation.SessionSnapshot{SessionID: "two", DivisionID: "tier-division", CharacterID: 2})
	game.jobHonorTierTick(ticker, jobHonorTierSweepMs)
	if len(push.division) != 1 {
		t.Fatalf("an arrival with no roster change pushed to the division again: %+v", push.division)
	}
	if len(push.sessions) != 1 || push.sessions[0].target != "two" || len(push.sessions[0].frames) != 1 ||
		push.sessions[0].frames[0].Opcode != wire.OpJobHonorTierRoster ||
		!bytes.Equal(push.sessions[0].frames[0].Payload, wantRoster) {
		t.Fatalf("the new arrival did not get the standing roster on its own session: %+v", push.sessions)
	}

	game.jobHonorTierTick(ticker, 2*jobHonorTierSweepMs)
	if len(push.division) != 1 || len(push.sessions) != 1 {
		t.Fatalf("a quiet sweep pushed again: division %+v sessions %+v", push.division, push.sessions)
	}
}

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestDormantTimerRestoresWithoutReset(t *testing.T) {
	p := monster.NewAITimeManager()
	p.SetTimer(1, 1234, 7, 0, true, func() uint32 { return 17 })
	p.CheckTimer(1, 0xfffffffe)
	want := *p
	state := divisionMonsterState{aiTimers: map[uint32]*monster.AITimeManager{77: p}}
	state.storeAITimer(77)
	if state.aiTimers[77] != nil || len(state.storedAITimers) != 1 {
		t.Fatal("mutable bank retained")
	}
	got := state.aiTimer(77)
	if got == nil || *got != want || len(state.storedAITimers) != 0 {
		t.Fatal("restoration lost timer state")
	}
	if state.aiTimer(77) != got {
		t.Fatal("mutable pointer identity changed")
	}
	state.storeAITimer(77)
	// The wake forgets a registered sleeper: production stores a bank
	// only from tryDormant, which registers the gid first (#2029).
	state.dormant = map[uint32]uint16{77: 7}
	state.dormantRegions = map[uint16]map[uint32]struct{}{7: {77: {}}}
	state.forgetDormant(77)
	if state.aiTimers[77] == nil || *state.aiTimers[77] != want {
		t.Fatal("wake did not restore timers")
	}
	if len(state.dormant) != 0 || len(state.dormantRegions) != 0 {
		t.Fatal("wake left the sleeper registered")
	}
	if state.aiTimer(999) != nil {
		t.Fatal("lookup fabricated timers")
	}
}

func TestForgetDormantLeavesUnregisteredBankStored(t *testing.T) {
	p := monster.NewAITimeManager()
	p.SetTimer(1, 1234, 7, 0, true, func() uint32 { return 17 })
	want := *p
	state := divisionMonsterState{aiTimers: map[uint32]*monster.AITimeManager{77: p}}
	state.storeAITimer(77)
	// The schedulers forget every due actor, awake ones included; an
	// unregistered gid has no wake to perform, so its bank tables stay
	// untouched (#2029).
	state.forgetDormant(77)
	if state.aiTimers[77] != nil || len(state.storedAITimers) != 1 {
		t.Fatal("forget restored a bank for an actor that never slept")
	}
	got := state.aiTimer(77)
	if got == nil || *got != want || len(state.storedAITimers) != 0 {
		t.Fatal("stored bank changed across the skipped forget")
	}
}

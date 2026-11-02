/*
===========================================================================

monsteraction_gate_test.go - the monster-action hook's tick gate bound

The monster-action hook settles on absolute due times, so the tick
gate's bounded wait costs it nothing when the gate is held: a cut pass
settles nothing, and the next served pass settles the same work (#2294).

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestMonsterActionHookCutsAheldGate

With an entry holding the publication gate, the monster-action hook
returns promptly with nothing settled; once the gate frees, the same
due work settles on the next pass.
================
*/
func TestMonsterActionHookCutsAheldGate(t *testing.T) {
	refs := map[uint32]monster.MonsterRef{
		1: {TidWord: 0x00C6, RefObjID: 1, Codename: "MOB_CH_TIGERWOMAN", MaxHP: 1000, BodyRadius: 10, WalkSpeed: 8, RunSpeed: 20},
		2: {TidWord: 0x00C6, RefObjID: 2, Codename: "MOB_AM_WOLF", MaxHP: 100, BodyRadius: 2, WalkSpeed: 8, RunSpeed: 20, DefaultSkillIDs: [10]uint32{10498}},
	}
	now := int64(1000)
	s := simulation.NewMonsterState(monster.TemplateFromParts(refs, nil))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(now) })
	s.SetRandomSource(func() float64 { return 0 })
	parent, err := s.DevelopmentCreateLeader("summon", 1, monster.Pose{RegionID: 0x62aa, X: 1910, Y: 20, Z: 100}, 100000)
	if err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{Monsters: s}
	hook := rt.MonsterActionTickHook()

	// The hook's own summon advance materializes the child.
	hit, _ := s.ApplyDamage("summon", parent.Gid, 100)
	wave := monster.SummonSkill{Present: true, HPPercent: 80, Entries: [9]monster.SummonEntry{{RefObjID: 2, Grade: 0, Minimum: 1, Maximum: 1}}}
	if _, ok := s.BeginSummon("summon", hit.Instance, wave, now, now+100, now+100, map[uint32]float64{1: 100, 2: 10}); !ok {
		t.Fatal("summon reservation refused")
	}
	hook(now + 101)
	var child uint32
	for _, actor := range s.MaterializedInstances("summon") {
		if actor.SummonerGID == parent.Gid {
			child = actor.Gid
		}
	}
	if child == 0 {
		t.Fatal("summon was not created")
	}
	childActor, _ := s.Get("summon", child)
	if _, ok := s.ApplyDamage("summon", child, childActor.CurrentHP/2+1); !ok {
		t.Fatal("child damage refused")
	}
	if selected, ok := s.SelectConditionalSkill("summon", child); !ok || selected != 10498 {
		t.Fatal("child condition", selected, ok)
	}
	until := now + 501
	effect := monster.SelfEffect{SkillID: 10498, Token: 77, Tag: 0x6372, StartedAtMs: now + 101, UntilMs: until}
	if !s.InstallMonsterSelfEffect("summon", child, effect, now+101) {
		t.Fatal("effect refused")
	}

	unlock := rt.LockPublication("summon", "enterWorld")
	returned := make(chan []simulation.DivisionFrames, 1)
	go func() { returned <- hook(until + 1) }()
	select {
	case frames := <-returned:
		if len(frames) != 0 {
			unlock()
			t.Fatalf("cut pass settled %d batches behind the held gate", len(frames))
		}
	case <-time.After(2 * time.Second):
		unlock()
		t.Fatal("monster-action hook waited without bound on the held gate")
	}
	// The due retirement still stands: cutting settled nothing.
	if rt.tickGateCuts != 1 {
		unlock()
		t.Fatalf("tickGateCuts = %d, want 1", rt.tickGateCuts)
	}
	unlock()

	frames := hook(until + 1)
	got := 0
	for _, batch := range frames {
		for _, frame := range batch.Frames {
			if frame.Opcode != wire.OpEndedEffectInstances {
				continue
			}
			ended, err := wire.DecodeEndedEffectInstances(frame.Payload)
			if err != nil || len(ended.InstanceTokens) != 1 || ended.InstanceTokens[0] != 77 {
				t.Fatal("expiry payload", ended, err)
			}
			got++
		}
	}
	if got != 1 {
		t.Fatalf("served pass retired %d effect batches, want 1", got)
	}
}

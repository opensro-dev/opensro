/*
===========================================================================

monsterselfeffect_tick_test.go - retired summon buffs reach viewers through the tick

===========================================================================
*/

package action

import (
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

type tickSessionSource struct {
	sessions []simulation.SessionSnapshot
}

func (s tickSessionSource) SnapshotSessions() []simulation.SessionSnapshot { return s.sessions }

type shownBuffPush struct {
	frames []simulation.Frame
}

func (p *shownBuffPush) PushToSession(_ string, frames []simulation.Frame) {
	p.frames = append(p.frames, frames...)
}

func (p *shownBuffPush) PushToDivision(string, []simulation.Frame, string) {}

// The summon is admitted by several monster legs before the effect expires.
// Retirement goes through MonsterActionTickHook, not a hand-built frame.
func TestRetiredSummonBuffReachesViewerAfterSeveralShownTicks(t *testing.T) {
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
	hit, _ := s.ApplyDamage("summon", parent.Gid, 100)
	wave := monster.SummonSkill{Present: true, HPPercent: 80, Entries: [9]monster.SummonEntry{{RefObjID: 2, Grade: 0, Minimum: 1, Maximum: 1}}}
	if _, ok := s.BeginSummon("summon", hit.Instance, wave, now, now+100, now+100, map[uint32]float64{1: 100, 2: 10}); !ok {
		t.Fatal("summon reservation refused")
	}
	rt := &Runtime{Monsters: s}
	ops := &simulation.MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0 }}
	ops.AttackPlan = func(monster.Instance, uint32, simulation.AttackPick) (simulation.MonsterAttackPlan, bool) {
		return simulation.MonsterAttackPlan{}, false
	}
	lease, ok := s.ObjectPopulation("summon", parent.Gid)
	if !ok {
		t.Fatal("no population")
	}
	viewer := simulation.SessionSnapshot{
		SessionID: "viewer", DivisionID: "summon", CharacterID: 1, Population: lease, CombatEligible: true,
		World: simulation.WorldState{SpawnSet: true, Spawn: simulation.Spawn{RegionID: parent.Spawn.RegionID, X: parent.Spawn.X, Y: parent.Spawn.Y, Z: parent.Spawn.Z}},
	}
	push := &shownBuffPush{}
	ticker := simulation.NewTicker(tickSessionSource{sessions: []simulation.SessionSnapshot{viewer}}, push)
	ticker.Monsters = ops
	ticker.BeforeHooks = []simulation.TickHook{rt.MonsterActionTickHook()}
	var child uint32
	for step := int64(0); step < 5; step++ {
		now += 100
		ticker.RunTick(now)
		for _, actor := range s.MaterializedInstances("summon") {
			if actor.SummonerGID == parent.Gid {
				child = actor.Gid
			}
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
	until := now + 400
	effect := monster.SelfEffect{SkillID: 10498, Token: 77, Tag: 0x6372, StartedAtMs: now, UntilMs: until}
	if !s.InstallMonsterSelfEffect("summon", child, effect, now) {
		t.Fatal("effect refused")
	}
	for step := 0; step < 3; step++ {
		now += 100
		ticker.RunTick(now)
	}
	push.frames = nil
	now = until + 1
	ticker.RunTick(now)
	got := 0
	for _, frame := range push.frames {
		if frame.Opcode == wire.OpEndedEffectInstances {
			got++
			ended, err := wire.DecodeEndedEffectInstances(frame.Payload)
			if err != nil || len(ended.InstanceTokens) != 1 || ended.InstanceTokens[0] != 77 {
				t.Fatal("expiry payload", ended, err)
			}
		}
	}
	if got != 1 {
		t.Fatalf("shown viewer received %d buff-end frames", got)
	}
}

/*
===========================================================================

chainstage_timing_test.go - stage and queued-press timing against the tick

The port runs server-driven stages and queued presses on its 100 ms
simulation tick, where the native client sends each command the moment
the previous motion ends. These tests pin that the tick's rounding never
costs a combo its window: a chain stage meets its target as it stood at
the stage's mark, and a queued skill press starts in the turn that closes
the action it waited behind.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
knockdownTestRuntime

The combat fixture with a monster that can be knocked down, rebuilt as the
knockdown tests do so the template carries the flag.
================
*/
func knockdownTestRuntime(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance, func(code string) uint32) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	target.Ref.Knockdown = 1
	target.Ref.KORecoverMs = 3000
	nest := target.Nest
	nest.HasRarityOverride, nest.RarityOverride = false, 0
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{target.Ref.RefObjID: target.Ref}, []monster.NestRow{nest}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	target = rt.Monsters.InstancesInRegions(testDivision, []uint16{target.Spawn.RegionID})[0]
	c.CurrentMP = testInt64(100000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skills := rt.deps.SkillData().(staticSkillSource)
	source := shippedSkillSource(t)
	learn := func(code string) uint32 {
		row := shippedOffense(t, code)
		// The timing is the subject, not the price.
		row.Consumption.MP = 1
		skills[row.ID] = row
		c.Skills = append(c.Skills, row.ID)
		for next := row.ChainNext; next != 0; {
			stage, ok := source.SkillByID(next)
			if !ok {
				t.Fatalf("missing stage %d", next)
			}
			stage.Consumption.MP = 1
			skills[stage.ID] = stage
			next = stage.ChainNext
		}
		return row.ID
	}
	return rt, clock, c, target, learn
}

/*
================
TestChainStageMeetsItsTargetAtTheStageMark

Dragon Sore Blade (DOWNATTACK_C) chops a downed target twice; both rows
want a knocked-down target (reqc bit 0). Its second chop is due 1212 ms
after the root (388 casting + 824 duration) and runs on the 1300 ms tick.
A knockdown that ends between the mark and that tick was still holding
when the native stage arrived: the chop lands. One that ended before the
mark is refused 0x3006 as natively.
================
*/
func TestChainStageMeetsItsTargetAtTheStageMark(t *testing.T) {
	for _, tc := range []struct {
		name   string
		downMs int64
		lands  bool
	}{
		{"down through the mark", 1250, true},
		{"up before the mark", 1150, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, target, learn := knockdownTestRuntime(t)
			root := learn("SKILL_CH_SWORD_DOWNATTACK_C_09")
			start := clock.NowMs()
			mover, _ := rt.Monsters.Mover(testDivision, target.Gid)
			rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{{GID: target.Gid, Knockdown: &simulation.MonsterKnockdownPlan{Pose: mover.Pose, UntilMs: start + tc.downMs}}})
			press := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root, HasTarget: true, TargetGid: target.Gid}.Encode())
			if len(press.Frames) == 0 || press.Frames[0].Opcode != wire.OpSkillCastResult || press.Frames[0].Payload[0] != 1 {
				t.Fatalf("root refused %+v", press)
			}
			stage := shippedOffense(t, "SKILL_CH_SWORD_DOWNATTACK_C2_09").ID
			landed, refused := false, uint8(0)
			for tick := 1; tick <= 20; tick++ {
				for _, route := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
					for _, f := range route.Frames {
						if f.Opcode != wire.OpSkillCastResult || len(f.Payload) < 2 {
							continue
						}
						if f.Payload[0] == 1 && len(f.Payload) >= 6 && binary.LittleEndian.Uint32(f.Payload[2:6]) == stage {
							landed = true
						}
						if f.Payload[0] == 2 {
							refused = f.Payload[1]
						}
					}
				}
			}
			if landed != tc.lands {
				t.Fatalf("second chop landed %v, want %v (refusal 0x30%02x)", landed, tc.lands, refused)
			}
			if !tc.lands && refused != 0x06 {
				t.Fatalf("second chop refusal 0x30%02x, want 0x3006", refused)
			}
		})
	}
}

/*
================
TestQueuedSkillPressStartsInTheClosingTurn

A down attack pressed while Knockdown runs is queued (B2CD count two).
The turn that closes Knockdown publishes B505, releases the queue (B2CD
count one) and starts the queued press in that same turn, in that order:
its approach toward the displaced target, or its cast when in reach.
================
*/
func TestQueuedSkillPressStartsInTheClosingTurn(t *testing.T) {
	rt, clock, c, target, learn := knockdownTestRuntime(t)
	knockdown := learn("SKILL_CH_SWORD_KNOCKDOWN_A_01")
	down := learn("SKILL_CH_SWORD_DOWNATTACK_A_01")
	if r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: knockdown, HasTarget: true, TargetGid: target.Gid}.Encode()); len(r.Frames) == 0 {
		t.Fatal("knockdown refused")
	}
	pressed := false
	for tick := 1; tick <= 40; tick++ {
		now := clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()
		var order []uint16
		for _, route := range rt.TickHook()(now) {
			for _, f := range route.Frames {
				if f.Opcode == wire.OpSkillEffectControl || f.Opcode == wire.OpActionState || f.Opcode == wire.OpSkillCastResult || f.Opcode == simulation.OpMovementAck {
					order = append(order, f.Opcode)
				}
			}
		}
		if pressed && len(order) > 0 && order[0] == wire.OpSkillEffectControl {
			if len(order) < 3 || order[1] != wire.OpActionState || (order[2] != simulation.OpMovementAck && order[2] != wire.OpSkillCastResult) {
				t.Fatalf("closing turn %x, want B505, B2CD and the queued press's start", order)
			}
			return
		}
		mob, _ := rt.Monsters.Get(testDivision, target.Gid)
		if !pressed && mob.Motion.StateAt(now) == 8 {
			queued := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: down, HasTarget: true, TargetGid: target.Gid}.Encode())
			if len(queued.Frames) == 0 || queued.Frames[len(queued.Frames)-1].Opcode != wire.OpActionState {
				t.Fatalf("down attack not queued %+v", queued)
			}
			pressed = true
		}
	}
	t.Fatal("knockdown never closed")
}

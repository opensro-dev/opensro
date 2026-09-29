/*
===========================================================================

lightning_chain_test.go - lightning chain brackets and stages

===========================================================================
*/

package action

import (
	"encoding/binary"
	"fmt"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func shippedSkillSource(t *testing.T) *enterworld.TextdataSkills {
	t.Helper()
	dir := os.Getenv("SRO_SKILL_INVENTORY_DATA")
	if dir == "" {
		dir = gamedatatest.TextdataDir(t)
	}
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); err != nil {
		t.Skip("v1.150 textdata unavailable")
	}
	return enterworld.NewTextdataSkills(dir)
}

/*
==================
TestLightningChainRootBracketSpansEveryStage

The v1.150 client appends every linked stage to the ROOT deco (85CB60),
whose one ANI_SKILL_25 clip carries all six SHOT events. The root bracket
therefore has to stay open until the last stage's action ends; closing it
at the root's own duration left the caster idle while stages 2..6 kept
landing as result-only temporaries. Step waits are native ref+78 minus the
500 ms command latency budget (4AEC2D..4AEC62), gated only by the casting
instance (4AECB6).
==================
*/
func TestLightningChainRootBracketSpansEveryStage(t *testing.T) {
	source := shippedSkillSource(t)
	first, ok := source.SkillByCodename("SKILL_CH_SWORD_CHAIN_F_1S_01")
	if !ok {
		t.Fatal("missing rank one")
	}
	stages, ok := enterworld.OffensiveSequence(source, first.ID)
	if !ok || len(stages) != 6 {
		t.Fatal("rank one refused")
	}
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skills := rt.deps.SkillData().(staticSkillSource)
	for _, stage := range stages {
		skills[stage.ID] = stage
	}
	root := stages[0]
	c.Skills = append(c.Skills, root.ID)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	started := clock.NowMs()
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("root refused %+v", start)
	}
	rootToken := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	stageAt := map[uint32]int64{root.ID: started}
	var closes []uint32
	var rootClosedAt int64
	for clock.NowMs()-started < 6000 && rootClosedAt == 0 {
		clock.now = clock.now.Add(10 * time.Millisecond)
		for _, batch := range rt.TickHook()(clock.NowMs()) {
			for _, f := range batch.Frames {
				switch {
				case f.Opcode == wire.OpSkillCastResult && len(f.Payload) >= 14 && f.Payload[0] == 1:
					if rootClosedAt == 0 {
						stageAt[binary.LittleEndian.Uint32(f.Payload[2:])] = clock.NowMs()
					}
				case f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2:
					token := binary.LittleEndian.Uint32(f.Payload[2:])
					closes = append(closes, token)
					if token == rootToken {
						rootClosedAt = clock.NowMs()
					}
				}
			}
		}
	}
	if len(stageAt) != 6 || rootClosedAt == 0 {
		t.Fatalf("stages=%v rootClosedAt=%d closes=%v", stageAt, rootClosedAt, closes)
	}
	if len(closes) != 1 {
		t.Fatalf("stages must not open or close brackets of their own: %v", closes)
	}
	// Root: 340 of the 500 budget -> no wait, held only by its 256 ms casting
	// instance. Stage 2: 1188-160. Stages 3..5 wait their full duration.
	waits := []int64{int64(root.ActionCastingTimeMs), int64(stages[1].ActionDurationMs) - 160,
		int64(stages[2].ActionDurationMs), int64(stages[3].ActionDurationMs), int64(stages[4].ActionDurationMs)}
	for i := 1; i < 6; i++ {
		gap := stageAt[stages[i].ID] - stageAt[stages[i-1].ID]
		if gap <= waits[i-1] || gap > waits[i-1]+30 {
			t.Fatalf("stage %d after %d ms, want just over %d", i+1, gap, waits[i-1])
		}
	}
	last := stageAt[stages[5].ID]
	if closeAfter := rootClosedAt - last; closeAfter < int64(stages[5].ActionDurationMs) || closeAfter > int64(stages[5].ActionDurationMs)+20 {
		t.Fatalf("root bracket closed %d ms after the final stage, want its %d ms action", closeAfter, stages[5].ActionDurationMs)
	}
}

func TestLightningChainBrokenChainClosesRootAtCurrentStage(t *testing.T) {
	source := shippedSkillSource(t)
	first, _ := source.SkillByCodename("SKILL_CH_SWORD_CHAIN_F_1S_01")
	stages, ok := enterworld.OffensiveSequence(source, first.ID)
	if !ok || len(stages) != 6 {
		t.Fatal("rank one refused")
	}
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skills := rt.deps.SkillData().(staticSkillSource)
	for _, stage := range stages {
		skills[stage.ID] = stage
	}
	c.Skills = append(c.Skills, stages[0].ID)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: stages[0].ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	rootToken := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	var secondAt, closedAt int64
	third := false
	for n := 0; n < 400 && closedAt == 0; n++ {
		clock.now = clock.now.Add(10 * time.Millisecond)
		for _, batch := range rt.TickHook()(clock.NowMs()) {
			for _, f := range batch.Frames {
				if f.Opcode == wire.OpSkillCastResult && len(f.Payload) >= 14 && f.Payload[0] == 1 {
					switch binary.LittleEndian.Uint32(f.Payload[2:]) {
					case stages[1].ID:
						secondAt = clock.NowMs()
						// A superseding command ends the chain after stage two.
						rt.ClearCombatIntent(testDivision, c.Name)
					case stages[2].ID:
						third = true
					}
				}
				if f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2 && binary.LittleEndian.Uint32(f.Payload[2:]) == rootToken {
					closedAt = clock.NowMs()
				}
			}
		}
	}
	if secondAt == 0 || closedAt == 0 || third {
		t.Fatalf("second=%d closed=%d third=%v", secondAt, closedAt, third)
	}
	if after := closedAt - secondAt; after < int64(stages[1].ActionDurationMs) || after > int64(stages[1].ActionDurationMs)+20 {
		t.Fatalf("root closed %d ms after stage two, want its %d ms action", after, stages[1].ActionDurationMs)
	}
}

func TestLightningChainShippedRanksAndSixStageExecution(t *testing.T) {
	source := shippedSkillSource(t)
	var stages []enterworld.SkillRow
	for rank := 1; rank <= 9; rank++ {
		root, ok := source.SkillByCodename(fmt.Sprintf("SKILL_CH_SWORD_CHAIN_F_1S_%02d", rank))
		if !ok {
			t.Fatal("missing rank", rank)
		}
		sequence, ok := enterworld.OffensiveSequence(source, root.ID)
		if !ok || len(sequence) != 6 {
			t.Fatalf("rank%d refused %s", rank, root.OffenseRefusal)
		}
		_, bleeding := sequence[1].Abnormal.Param(abnormal.Bleeding)
		impotent, hasImpotent := sequence[3].Abnormal.Param(abnormal.Impotent)
		division, hasDivision := sequence[5].Abnormal.Param(abnormal.Division)
		if !bleeding || !hasImpotent || !hasDivision || impotent.Args[3] != 35 || division.Args[3] != 35 {
			t.Fatal("lost authored descriptors", rank)
		}
		if rank == 1 {
			stages = sequence
		}
	}
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skills := rt.deps.SkillData().(staticSkillSource)
	for _, stage := range stages {
		skills[stage.ID] = stage
	}
	root := stages[0]
	c.Skills = append(c.Skills, root.ID)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	started := clock.NowMs()
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("root refused %+v", start)
	}
	clock.now = time.UnixMilli(started + int64(root.ActionCastingTimeMs) + 1)
	releasePreparedSkillForTest(t, rt, clock.NowMs())
	seen := map[uint32]bool{root.ID: true}
	bothMask := false
	for n := 0; n < 150; n++ {
		clock.now = clock.now.Add(100 * time.Millisecond)
		for _, batch := range rt.TickHook()(clock.NowMs()) {
			for _, f := range batch.Frames {
				if f.Opcode == wire.OpSkillCastResult && len(f.Payload) >= 6 && f.Payload[0] == 1 {
					seen[binary.LittleEndian.Uint32(f.Payload[2:])] = true
				}
				if f.Opcode == simulation.OpVitalsUpdate && len(f.Payload) >= 18 && f.Payload[6] == 5 && binary.LittleEndian.Uint32(f.Payload[11:])&0x180800 == 0x180800 {
					bothMask = true
				}
			}
		}
		if len(seen) == 6 {
			break
		}
	}
	current, _ := rt.Monsters.Get(testDivision, target.Gid)
	slot := func(instance monster.Instance, s abnormal.Status) abnormal.Slot {
		if instance.Abnormal == nil {
			return abnormal.Slot{}
		}
		return instance.Abnormal.Slots[s]
	}
	if len(seen) != 6 || slot(current, abnormal.Impotent).Grade != 8 || slot(current, abnormal.Division).Grade != 8 || !bothMask {
		t.Fatalf("stages=%v block=%+v combinedMask=%v", seen, current.Abnormal, bothMask)
	}
	if got := enterworld.CurrentMP(c); got != 10000-int64(root.Consumption.MP) {
		t.Fatalf("cost paid more than once: %d root%+v", got, root.Consumption)
	}
	rt.ClearCombatIntent(testDivision, c.Name)
	frames := rt.advanceMonsterAbnormals(slot(current, abnormal.Impotent).StartedAt + 30001)
	next, _ := rt.Monsters.Get(testDivision, target.Gid)
	if slot(next, abnormal.Impotent).Active || !slot(next, abnormal.Division).Active || len(frames) == 0 {
		t.Fatal("first slot expiry", next.Abnormal)
	}
	rt.advanceMonsterAbnormals(slot(current, abnormal.Division).StartedAt + 30001)
	next, _ = rt.Monsters.Get(testDivision, target.Gid)
	if slot(next, abnormal.Impotent).Active || slot(next, abnormal.Division).Active {
		t.Fatal("final expiry")
	}
}

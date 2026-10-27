package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func installSwordCombo(t *testing.T, rt *Runtime, c *enterworld.Character) enterworld.SkillRow {
	t.Helper()
	root := shippedOffense(t, "SKILL_CH_SWORD_CHAIN_A_1S_01")
	for _, code := range []string{"SKILL_CH_SWORD_CHAIN_A_1S_01", "SKILL_CH_SWORD_CHAIN_A_2S_01", "SKILL_CH_SWORD_CHAIN_A_3S_01"} {
		row := shippedOffense(t, code)
		rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	}
	c.Skills = append(c.Skills, root.ID)
	c.CurrentMP = testInt64(100)
	return root
}

func TestComboCommitsSeparateTimedStagesWithOneRootCost(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	root := installSwordCombo(t, rt, c)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("root refused %+v", result)
	}
	if *c.CurrentMP != 68 || rt.castTokenCounter != 1 {
		t.Fatal("root cost/token")
	}
	initial, _ := rt.Monsters.Get(testDivision, target.Gid)
	if intents := rt.combatIntentSnapshot(); len(intents) != 1 {
		t.Fatalf("root did not retain sequence: %v", intents)
	}
	if _, _, refusal := rt.resolveOffensiveStage(rt.characterSnapshot(testDivision, c), root.ID, root.ChainNext); refusal != "" {
		t.Fatal(refusal)
	}
	var ids []uint32
	var ticks []int
	for tick := 1; tick <= 30; tick++ {
		for _, route := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
			for _, frame := range route.Frames {
				if frame.Opcode == wire.OpSkillCastResult && frame.Payload[0] == 1 {
					ids = append(ids, binary.LittleEndian.Uint32(frame.Payload[2:6]))
					ticks = append(ticks, tick)
				}
			}
		}
	}
	if len(ids) != 3 || ids[0] != 7 || ids[1] != 8 || ids[2] != 2 {
		t.Fatalf("continuations %v", ids)
	}
	// Native step waits: the root's 428 ms fits the 500 ms latency
	// budget, so stage 7 follows at once; stage 7 waits 612-72 ms; the basic
	// attack waits stage 8's 993 ms plus the 72 ms the chain borrowed.
	if ticks[0] != 1 || ticks[1] != 7 || ticks[2] != 18 {
		t.Fatalf("continuation ticks %v", ticks)
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= initial.CurrentHP || *c.CurrentMP != 68 || rt.castTokenCounter != 4 {
		t.Fatal("sequence damage/cost/token")
	}
	if c.OffensiveSkillCooldowns[root.Group] != clock.NowMs()+8000 {
		t.Fatal("tail reset root cooldown")
	}
	if intents := rt.combatIntentSnapshot(); len(intents) != 1 || intents[0].SingleCast || intents[0].ComboRootID != 0 || intents[0].SkillID != 2 {
		t.Fatal("combo did not transfer target to basic attack", intents)
	}
}

func TestComboTailCannotBeRequestedOrSurviveCancellation(t *testing.T) {
	for _, mode := range []string{"direct-tail", "cancel", "fatal", "unsupported-tail", "unequip", "unlearn", "player-death"} {
		t.Run(mode, func(t *testing.T) {
			hp := uint32(100000)
			if mode == "fatal" {
				hp = 1
			}
			rt, clock, c, target := newCombatTestRuntime(t, hp)
			root := installSwordCombo(t, rt, c)
			id := root.ID
			if mode == "direct-tail" {
				id = 7
				c.Skills = append(c.Skills, 7)
			}
			if mode == "unsupported-tail" {
				s := rt.deps.SkillData().(staticSkillSource)
				row := s[8]
				row.OffensiveStagePinned = false
				row.DirectOffensePinned = false
				s[8] = row
			}
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: id, HasTarget: true, TargetGid: target.Gid}.Encode())
			if mode == "cancel" {
				rt.ClearCombatIntent(testDivision, c.Name)
			}
			if mode == "unequip" {
				c.MissionInventory = nil
			}
			if mode == "unlearn" {
				c.Skills = []uint32{2}
			}
			if mode == "player-death" {
				c.CurrentHP = testInt64(0)
			}
			for tick := 1; tick <= 30; tick++ {
				rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli())
			}
			want := uint32(1)
			mp := int64(68)
			if mode == "direct-tail" || mode == "unsupported-tail" {
				want = 0
				mp = 100
			}
			if rt.castTokenCounter != want || *c.CurrentMP != mp || len(rt.combatIntentSnapshot()) != 0 {
				t.Fatalf("tokens=%d MP=%d intent=%v", rt.castTokenCounter, *c.CurrentMP, rt.combatIntentSnapshot())
			}
		})
	}
}

func TestCombatIntentSnapshotCannotResurrectCancelledOrReplacedCombo(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	root := installSwordCombo(t, rt, c)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	intents := rt.combatIntentSnapshot()
	if len(intents) != 1 {
		t.Fatalf("combo admission did not create one intent: %+v; result=%+v", intents, result)
	}
	old := intents[0]
	if !rt.combatIntentIsCurrent(old) {
		t.Fatal("current intent rejected")
	}
	rt.ClearCombatIntent(testDivision, c.Name)
	if rt.combatIntentIsCurrent(old) {
		t.Fatal("cancelled snapshot remains executable")
	}
	replacement := old
	replacement.ComboRootID = 0
	replacement.SkillID = root.ID
	rt.setCombatIntent(replacement)
	if rt.combatIntentIsCurrent(old) {
		t.Fatal("superseded stage remains executable")
	}
}

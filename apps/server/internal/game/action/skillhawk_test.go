/*
===========================================================================

skillhawk_test.go - the attacking hawk (Black Hawk Summon, summ)

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
hawkStrikes

Ticks rt for ms in 100 ms steps from its clock and returns each hawk strike
frame's payload.
================
*/
func hawkStrikes(rt *Runtime, ms int64) [][]byte {
	var out [][]byte
	now := rt.Now()
	for step := int64(0); step < ms; step += 100 {
		now = now.Add(100 * time.Millisecond)
		at := now
		rt.Now = func() time.Time { return at }
		for _, batch := range rt.TickHook()(at.UnixMilli()) {
			for _, f := range batch.Frames {
				if f.Opcode == wire.OpSummonedHawkStrike {
					out = append(out, f.Payload)
				}
			}
		}
	}
	return out
}

/*
================
TestBlackHawkStrikesWhatItsOwnerAttacks

582750: the hawk strikes only after its owner attacks, once per summ
interval, while the owner attacked within the last interval. Each strike is
the 357A frame {effect token, target, damage} and damages the monster.
================
*/
func TestBlackHawkStrikesWhatItsOwnerAttacks(t *testing.T) {
	rt, c, target, _, _ := arrowFixture(t)
	skill := shippedOffense(t, "SKILL_CH_BOW_CALL_B_01")
	// The level-1 fixture's MP cannot pay the summon; cost is not tested here.
	skill.Consumption.MP, skill.Consumption.MPPercent = 0, 0
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	if result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode()); result.DiagnosticRefusal != "" {
		t.Fatalf("summon refused: %s", result.DiagnosticRefusal)
	}
	hawk := skill.TimedEffect.Hawk
	if !hawk.Present || hawk.IntervalMs != 3500 || hawk.Physical == 0 {
		t.Fatalf("Black Hawk summ block: %+v", hawk)
	}
	if strikes := hawkStrikes(rt, 5000); len(strikes) != 0 || !hasSkillEffect(rt, c.Name, skill.ID) {
		t.Fatalf("the hawk struck before its owner attacked (%d), or never came", len(strikes))
	}
	arrow := shippedOffense(t, "SKILL_CH_BOW_CRITICAL_A_01")
	c.MissionInventory[len(c.MissionInventory)-1].StackCount = 200
	before, _ := rt.Monsters.Get(testDivision, target)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: arrow.ID, HasTarget: true, TargetGid: target}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatalf("owner attack refused: %s", result.DiagnosticRefusal)
	}
	strikes := hawkStrikes(rt, 3000)
	if len(strikes) != 1 {
		t.Fatalf("%d strikes in the first interval after the attack, want 1", len(strikes))
	}
	p := strikes[0]
	if len(p) != 10 || binary.LittleEndian.Uint32(p[4:]) != target || binary.LittleEndian.Uint16(p[8:])&^wire.HawkFatalBit == 0 {
		t.Fatalf("strike payload % x", p)
	}
	token, _, ok := rt.activeHawk(testDivision, c.Name)
	if !ok || binary.LittleEndian.Uint32(p) != token {
		t.Fatalf("strike names instance %d, the hawk's effect is %d", binary.LittleEndian.Uint32(p), token)
	}
	after, _ := rt.Monsters.Get(testDivision, target)
	if after.CurrentHP >= before.CurrentHP {
		t.Fatalf("monster HP %d -> %d", before.CurrentHP, after.CurrentHP)
	}
	// The owner stopped attacking: no strike once the last attack is more
	// than one interval old.
	if strikes := hawkStrikes(rt, 8000); len(strikes) != 0 {
		t.Fatalf("the hawk kept striking without its owner: %d", len(strikes))
	}
}

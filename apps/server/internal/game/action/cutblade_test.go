/*
===========================================================================

cutblade_test.go - Devil and Demon Cut Blade chain their two blade forces

Tiers C and D of the sword's blade force (SKILL_CH_SWORD_GEOMGI_) are
projectile-handler roots that link a zero-preparation second stage, as the
bow's and crossbow's combo lines do, with no ammunition (#509).

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
TestDevilCutBladeStrikesTwice

The shipped C_01 root at the fixture monster: the root and its C2 stage
both open a successful cast, in that order, and the monster takes damage.
================
*/
func TestDevilCutBladeStrikesTwice(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	root := shippedOffense(t, "SKILL_CH_SWORD_GEOMGI_C_01")
	stage := shippedOffense(t, "SKILL_CH_SWORD_GEOMGI_C2_01")
	if root.OffenseRefusal != "" || stage.OffenseRefusal != "" || root.ChainNext != stage.ID || !stage.ChainSub {
		t.Fatalf("chain not admitted: root %q -> %d, stage %d %q", root.OffenseRefusal, root.ChainNext, stage.ID, stage.OffenseRefusal)
	}
	source := rt.deps.SkillData().(staticSkillSource)
	source[root.ID], source[stage.ID] = root, stage
	c.Skills = append(c.Skills, root.ID)
	// C_01 costs 458 MP; Intellect lifts the level-one fixture's maximum.
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	before, _ := rt.Monsters.Get(testDivision, target.Gid)

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
		t.Fatalf("Devil Cut Blade refused: %q %+v", out.DiagnosticRefusal, out.Frames)
	}
	ids := []uint32{binary.LittleEndian.Uint32(out.Frames[0].Payload[2:6])}
	for tick := 1; tick <= 40; tick++ {
		for _, route := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
			for _, frame := range route.Frames {
				if frame.Opcode == wire.OpSkillCastResult && frame.Payload[0] == 1 {
					ids = append(ids, binary.LittleEndian.Uint32(frame.Payload[2:6]))
				}
			}
		}
	}
	if len(ids) < 2 || ids[0] != root.ID || ids[1] != stage.ID {
		t.Fatalf("casts %v, want %d then %d", ids, root.ID, stage.ID)
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= before.CurrentHP {
		t.Fatalf("no damage: %d -> %d", before.CurrentHP, after.CurrentHP)
	}
}

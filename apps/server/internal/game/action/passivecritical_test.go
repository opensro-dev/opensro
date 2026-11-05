package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

func TestPassiveCriticalChangesRealBasicAttackResult(t *testing.T) {
	for _, equipped := range []bool{false, true} {
		t.Run(map[bool]string{false: "wrong_weapon", true: "twohand"}[equipped], func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 10000)
			passive := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDP_CRITICALUP_A_01")
			if !passive.PassiveCritical.Pinned {
				t.Fatal("authored passive missing")
			}
			skills := rt.deps.SkillData().(staticSkillSource)
			skills[passive.ID] = passive
			c.Skills = append(c.Skills, passive.ID)
			if equipped {
				weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
				weapon.TypeIDs[3] = 8
				c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
				// Synthetic attack identity isolates the new producer; the passive
				// itself is the unchanged authored row. Live probe uses real EU base.
				skill := skills[2]
				skill.RequiredWeaponKinds = [2]uint8{8, 255}
				skills[2] = skill
			}
			rt.CombatRoll = func() (uint32, error) { return 4, nil } // base3 fails, passive5 succeeds
			cast := wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}
			r, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, rt.Now().UnixMilli(), rt.handlerDoors())
			if decision != skillCastAccepted || len(r.Frames) == 0 || len(r.Frames[0].Payload) < 34 {
				t.Fatalf("refused %+v", r)
			}
			p := r.Frames[0].Payload
			want := byte(1)
			if equipped {
				want = 2
			}
			if p[26] != want {
				t.Fatalf("critical=%d want=%d", p[26], want)
			}
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			damage := uint32(0)
			for i := 0; i < int(p[19]); i++ {
				damage += binary.LittleEndian.Uint32(p[26+i*9:]) >> 8
			}
			if target.CurrentHP-after.CurrentHP != damage {
				t.Fatal("HP differs from serialized damage")
			}
		})
	}
}

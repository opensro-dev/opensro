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

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
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

Natively the hawk strikes only after its owner attacks, once per summ
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

/*
================
hawkAntelopeRuntime

The Antelope strike fixture (#2500): the combat fixture's target
re-authored with MOB_RM_ANTELOPE's shipped defenses (characterdata:
physical defense 922, parry 83) under its own codename, the caster
holding the fixture bow with arrows, knowing the bow base row, the
punch base row and the Lightning Hawk summon (SKILL_CH_BOW_CALL_D_05,
its summ word 1883; cost waived, as in the Black Hawk case), rolls
fixed at zero. The hawk is already summoned; the caller aims it, then
dresses the caster before the strike tick.
================
*/
func hawkAntelopeRuntime(t *testing.T) (*Runtime, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, _, c, target := newCombatTestRuntimeMonsterTuned(t, 1000000, 86, 0, func(ref *monster.MonsterRef) {
		ref.RefObjID, ref.Codename = 3871, "MOB_RM_ANTELOPE"
		ref.PhysicalDefense, ref.ParryRate = 922, 83
	})
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	arrow := &enterworld.ItemRef{RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	items[arrow.Codename] = arrow
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 200})
	arrowSkill := shippedOffense(t, "SKILL_CH_BOW_CRITICAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[arrowSkill.ID] = arrowSkill
	bow := shippedOffense(t, "SKILL_CH_BOW_BASE_01")
	punch := shippedOffense(t, "SKILL_PUNCH_01")
	hawk := shippedOffense(t, "SKILL_CH_BOW_CALL_D_05")
	hawk.Consumption.MP, hawk.Consumption.MPPercent = 0, 0
	for _, row := range []enterworld.SkillRow{bow, punch, hawk} {
		rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	}
	c.Skills = append(c.Skills, arrowSkill.ID, bow.ID, punch.ID, hawk.ID)
	c.CurrentMP = testInt64(10000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	if result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: hawk.ID}.Encode()); result.DiagnosticRefusal != "" {
		t.Fatalf("summon refused: %s", result.DiagnosticRefusal)
	}
	return rt, c, target
}

/*
================
hawkAntelopeStrike

Dresses the caster for the case, aims rt's summoned hawk with the
case's owner attack (an arrow cast with the bow on, a barehanded engage
without it), ticks span, and returns the one strike's damage word.
================
*/
func hawkAntelopeStrike(t *testing.T, rt *Runtime, c *enterworld.Character, target monster.Instance, dress func(c *enterworld.Character), aim func() string, span int64) uint16 {
	t.Helper()
	dress(c)
	if refusal := aim(); refusal != "" {
		t.Fatalf("owner attack refused: %s", refusal)
	}
	strikes := hawkStrikes(rt, span)
	if len(strikes) != 1 {
		t.Fatalf("%d hawk strikes after the owner's attack, want 1", len(strikes))
	}
	p := strikes[0]
	if len(p) != 10 || binary.LittleEndian.Uint32(p[4:]) != target.Gid {
		t.Fatalf("strike payload % x", p)
	}
	return binary.LittleEndian.Uint16(p[8:]) &^ wire.HawkFatalBit
}

/*
================
aimWithCriticalArrow
================
*/
func aimWithCriticalArrow(rt *Runtime, c *enterworld.Character, target monster.Instance) func() string {
	return func() string {
		var arrowID uint32
		for id, row := range rt.deps.SkillData().(staticSkillSource) {
			if row.Codename == "SKILL_CH_BOW_CRITICAL_A_01" {
				arrowID = id
			}
		}
		return rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: arrowID, HasTarget: true, TargetGid: target.Gid}.Encode()).DiagnosticRefusal
	}
}

/*
================
aimWithAttackNote

The barehanded case: the owner's attack stamp itself, as its swing
would land it (noteHawkOwnerAttack) - the fixture's punch swing is not
the behavior under test, the strike's rank read is.
================
*/
func aimWithAttackNote(rt *Runtime, c *enterworld.Character, target monster.Instance) func() string {
	return func() string {
		rt.noteHawkOwnerAttack(testDivision, c, target.Gid, rt.Now().UnixMilli())
		return ""
	}
}

/*
================
TestHawkStrikeReadsRankFromTheDefaultAttackRow

Natively the periodic damage reads the mastery rank on the caster's
main-weapon default attack row, never on the summon row (no hawk row
carries MAAT, so the strike always read rank 0 before #2500 and a
max-rank Lightning Hawk hit an Antelope for 106: 1883/1.83-922). On
MOB_RM_ANTELOPE (defense 922, parry 83) against the D_05 summ word
1883: bow mastery 80 reads rank 80 (1883*1.8/1.83-922 = 930); an
untrained bow mastery reads rank 1, the zero mastery id's floor on the
bow base row's [259, 0] ids (1883*1.01/1.83-922 = 117); with no weapon
the resolved row is the punch base, which carries no MAAT, so the rank
is 0 and the strike is the old 106.
================
*/
func TestHawkStrikeReadsRankFromTheDefaultAttackRow(t *testing.T) {
	cases := []struct {
		name  string
		dress func(c *enterworld.Character)
		aim   func(rt *Runtime, c *enterworld.Character, target monster.Instance) func() string
		span  int64
		want  uint16
	}{
		{"bow mastery 80", func(c *enterworld.Character) {
			c.Masteries = append(c.Masteries, domain.CharacterMastery{ID: 259, Level: 80})
		}, aimWithCriticalArrow, 3000, 930},
		{"bow mastery untrained", func(c *enterworld.Character) {}, aimWithCriticalArrow, 3000, 117},
		{"no weapon", func(c *enterworld.Character) {
			c.MissionInventory = c.MissionInventory[1:2]
		}, aimWithAttackNote, 3000, 106},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, target := hawkAntelopeRuntime(t)
			if got := hawkAntelopeStrike(t, rt, c, target, tc.dress, tc.aim(rt, c, target), tc.span); got != tc.want {
				t.Fatalf("hawk strike dealt %d, want %d", got, tc.want)
			}
		})
	}
}

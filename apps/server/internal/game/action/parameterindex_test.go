/*
===========================================================================

parameterindex_test.go - Wizard rows the shared parameter index now reaches

Root and Mesh Root carry getv WIRU without att, Earth Barrier and Earth
Fence carry getv WIMD behind a zero padding word, and the Chinese Fire
Shield rows carry reqi 4 1 behind one. Each is observed through the owner
that consumes it: cast reach, the prepared MP cost and the equipment gates.

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	magicBoundA3      = "SKILL_EU_WIZARD_MANAP_RANGE_A_03"    // setv WIRU 30
	magicBoundA3Reach = 30                                    // the WIRU value it teaches
	intelligenceA3    = "SKILL_EU_WIZARD_MANAP_DECREASE_A_03" // setv WIMD 7
	rootAuthoredRange = 150                                   // Root's column range
	fireShieldA1      = "SKILL_CH_FIRE_SHIELD_A_01"           // bgra 63 18 0 reqi 4 1
)

/*
================
wizardCaster

The combat fixture as a European caster holding the row's own weapon.
================
*/
func wizardCaster(t *testing.T, code string) (*Runtime, *enterworld.Character, enterworld.SkillRow, monster.Instance) {
	t.Helper()
	rt, _, c, target := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, code)
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	return rt, c, skill, target
}

/*
================
TestRootReachAddsMagicBound

4AE87E: a cast whose row asks for WIRU reaches its column range plus the
caster's Magic Bound. Root is a status cast (no att), so only the shared
index records its getv WIRU. The press stores that reach on the approach
intent the cast then walks under.
================
*/
func TestRootReachAddsMagicBound(t *testing.T) {
	for _, code := range []string{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_A_01", "SKILL_EU_WIZARD_EARTHA_ABNORMAL_B_02"} {
		t.Run(code, func(t *testing.T) {
			rt, c, skill, target := wizardCaster(t, code)
			if !skill.StatusCast || skill.ActionRange != rootAuthoredRange {
				t.Fatalf("row shape: status %v range %g", skill.StatusCast, skill.ActionRange)
			}
			resolved, loadout, refusal := rt.resolveOffensiveSkill(c, skill.ID)
			if refusal != "" || resolved.ID != skill.ID {
				t.Fatalf("Root not resolvable: %s", refusal)
			}
			if got := rt.playerActionReach(testDivision, c, skill, loadout); got != rootAuthoredRange {
				t.Fatalf("reach without Magic Bound %g", got)
			}
			learnShippedPassive(t, rt, c, magicBoundA3)
			want := simulation.ActionReach(rootAuthoredRange + magicBoundA3Reach)
			if got := rt.playerActionReach(testDivision, c, skill, loadout); got != want {
				t.Fatalf("reach with Magic Bound %g, want %g", got, want)
			}

			*c.World.Spawn.X = target.Spawn.X - 400
			cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}
			rt.HandleTargetInteract(testDivision, c, cast.Encode())
			intents := rt.combatIntentSnapshot()
			if len(intents) != 1 || intents[0].ActionReach != want || !intents[0].HasApproach {
				t.Fatalf("approach intent %+v, want reach %g", intents, want)
			}
		})
	}
}

/*
================
TestEarthBarrierCostCutByIntelligence

5868F1: an instant cast whose row asks for WIMD pays its prepared MP less
the caster's Intelligence percentage. Earth Barrier's getv WIMD sits behind
the zero word after odar, and Earth Fence shares the shape.
================
*/
func TestEarthBarrierCostCutByIntelligence(t *testing.T) {
	// Pinned prepared costs under the level-1 fixture, before and after the
	// 7% cut, so the expectation does not restate the production formula.
	cases := []struct {
		code       string
		plain, cut int64
	}{
		{"SKILL_EU_WIZARD_EARTHA_GUARD_A_01", 219, 203},
		{"SKILL_EU_WIZARD_EARTHA_GUARD_B_01", 1460, 1357},
	}
	for _, tc := range cases {
		code := tc.code
		t.Run(code, func(t *testing.T) {
			rt, c, skill, _ := wizardCaster(t, code)
			plain, err := rt.preparedExecutionMPCost(testDivision, c, skill)
			if err != nil {
				t.Fatal(err)
			}
			learnShippedPassive(t, rt, c, intelligenceA3)
			cut, err := rt.preparedExecutionMPCost(testDivision, c, skill)
			if err != nil {
				t.Fatal(err)
			}
			if plain != tc.plain || cut != tc.cut {
				t.Fatalf("prepared MP %d -> %d, want %d -> %d", plain, cut, tc.plain, tc.cut)
			}

			before := enterworld.CurrentMP(c)
			out := castSelf(rt, c, skill.ID)
			if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
				t.Fatalf("%s refused: %+v", code, out)
			}
			if spent := before - enterworld.CurrentMP(c); spent != cut {
				t.Fatalf("charged %d, prepared %d", spent, cut)
			}
		})
	}
}

/*
================
TestFireShieldReqiGates

The Chinese Fire Shield's reqi 4 1 (a shield in slot 7) is indexed on the
row: without a shield the cast is refused with 58D480's 0x300D, and with one
it casts (bgra is pinned, #508). The 59F0E0 walk is pinned on seeded
instances: it would retire a self-applied instance whose reqi the
equipment no longer meets, but 59F397 exempts SKILL_CH_FIRE_SHIELD_ rows
by name, so only the same row under another codename is retired when the
shield leaves.
================
*/
func TestFireShieldReqiGates(t *testing.T) {
	rt, _, c, shield, _ := shieldFixture(t)
	fire := shippedOffense(t, fireShieldA1)
	source := rt.deps.SkillData().(staticSkillSource)
	source[fire.ID] = fire
	c.Skills = append(c.Skills, fire.ID)
	c.CurrentMP = testInt64(10000)

	out := castSelf(rt, c, fire.ID)
	if len(out.Frames) == 0 || !bytes.Equal(out.Frames[0].Payload, []byte{2, 0x0d}) {
		t.Fatalf("Fire Shield without a shield: %+v", out)
	}

	renamed := fire
	renamed.ID, renamed.Group = 910900, 910900
	renamed.Codename = "SKILL_TEST_REQI_SHIELD"
	source[renamed.ID] = renamed
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: shield.RefObjID, Codename: shield.Codename, TypeFlags: shield.TypeFlags(), VarianceBits: "0", Durability: 1, StackCount: 1,
	})
	// With its shield, Fire Shield casts (#508): bgra is pinned.
	armed := castSelf(rt, c, fire.ID)
	if armed.DiagnosticRefusal != "" || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatalf("Fire Shield with a shield refused: %+v", armed)
	}
	for i, row := range []enterworld.SkillRow{renamed} {
		e := statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID, SkillGroup: row.Group,
			InstanceToken: uint32(200000 + i), State: statuseffect.StateActive, Phase: 1}
		if !rt.effects.Apply(e) {
			t.Fatalf("seed %s", row.Codename)
		}
	}
	move := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 7, DestSlot: 20, Quantity: 1}))
	if len(move.Frames) == 0 || move.Frames[0].Payload[0] != 1 {
		t.Fatalf("unequip refused: %+v", move.Frames)
	}
	live := map[uint32]bool{}
	for _, e := range rt.effects.Snapshot(testDivision, c.Name) {
		live[e.SkillID] = live[e.SkillID] || !e.StopRequested
	}
	if !live[fire.ID] || live[renamed.ID] {
		t.Fatalf("after unequip: Fire Shield live %v, renamed live %v", live[fire.ID], live[renamed.ID])
	}
}

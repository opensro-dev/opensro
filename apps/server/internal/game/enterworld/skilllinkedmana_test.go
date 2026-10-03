/*
===========================================================================

skilllinkedmana_test.go - admission of the lkdh damage-to-MP link

The Bard's Mana Switch (BATTLAA_MPSTEAL_A) is lnks + lks2 + dura + lkdh +
getv BDMD + reqi on the targeted ally envelope, which also names the
enemy columns. lkdh only has an owner through the link context, so it is
admitted with a targeted lnks and nothing else the link would drop.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

const (
	manaSwitchLine       = "SKILL_EU_BARD_BATTLAA_MPSTEAL_A"
	manaSwitchDurationMs = 20000
	manaSwitchGroup      = 15
	manaSwitchDistance   = 700
	manaSwitchPercent    = 50
	harpReqiKind         = 6
	bardMPDecreaseKey    = 0x42444d44
)

/*
================
manaLinkFields

The targeted envelope with Mana Switch's enemy columns (29, 30) set.
================
*/
func manaLinkFields(tail []uint32) []string {
	fields := linkedThreatFields(tail)
	fields[29], fields[30] = "1", "1"
	return fields
}

/*
================
TestShippedManaSwitchTiersAreDamageLinks

All 13 tiers are targeted 20 s links (group 15, distance 700, no outgoing
count, hidden board) whose lkdh hands 50 percent of each hit, capped at
305 .. 1596, to the Bard, with the harp reqi and BDMD kept.
================
*/
func TestShippedManaSwitchTiersAreDamageLinks(t *testing.T) {
	source := sharedShippedSkills(t)
	caps := []uint32{305, 359, 418, 486, 562, 647, 742, 848, 967, 1100, 1248, 1412, 1596}
	for i, ceiling := range caps {
		code := fmt.Sprintf("%s_%02d", manaSwitchLine, i+1)
		row, ok := source.SkillByCodename(code)
		if !ok {
			t.Fatalf("missing %s", code)
		}
		d := row.TimedEffect
		if !d.Pinned || !d.Targeted || d.Persistent || d.Area.Present || d.Defense || d.Strength.Present ||
			d.Intellect.Present || row.EffectDurationMs != manaSwitchDurationMs || row.Targets.Self {
			t.Fatalf("%s: duration %d program %+v", code, row.EffectDurationMs, d)
		}
		want := SkillEffectLink{Present: true, Group: manaSwitchGroup, MaxDistance: manaSwitchDistance,
			PerTarget: true, Mana: true, ManaPercent: manaSwitchPercent, ManaCap: ceiling}
		if d.Link != want {
			t.Fatalf("%s link %+v, want %+v", code, d.Link, want)
		}
		if !row.Reqi.Present || row.Reqi.Count != 1 || row.Reqi.Pairs[0] != (SkillReqiPair{Kind: harpReqiKind, Value: harpWeaponKind}) {
			t.Fatalf("%s reqi %+v", code, row.Reqi)
		}
		if !row.Attack.Parameters.Has(ParameterBardMPDecrease) {
			t.Fatalf("%s: getv BDMD lost", code)
		}
		if plan := source.ExecutionPlan(row.ID); plan.Kind() != SkillExecutionTimedEffect {
			t.Fatalf("%s execution plan %v", code, plan.Kind())
		}
	}
}

/*
================
TestLinkedDamageProgramAdmissionIsAtomic

lkdh {0, percent, cap} after a targeted lnks pins the program. lkdh
before lnks or without one, an HP word, a zero or over-whole percent, a
zero cap, a duplicate, lks2 on a counted link, lkag or stat writes beside
it, and the enemy columns without lkdh keep the row unsupported.
================
*/
func TestLinkedDamageProgramAdmissionIsAtomic(t *testing.T) {
	link := []uint32{tagTimedLink, manaSwitchGroup, manaSwitchDistance, 0, 0}
	lks2 := []uint32{tagTimedLinkPerTarget}
	lkdh := []uint32{tagTimedLinkedDamage, 0, manaSwitchPercent, 1596}
	join := func(parts ...[]uint32) []uint32 {
		out := []uint32{tagDura, manaSwitchDurationMs}
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	tail := []uint32{tagGetv, bardMPDecreaseKey, tagReqi, harpReqiKind, harpWeaponKind}
	for _, tc := range []struct {
		name   string
		fields []string
		valid  bool
	}{
		{"mana switch", manaLinkFields(join(link, lks2, lkdh, tail)), true},
		{"without lks2", manaLinkFields(join(link, lkdh, tail)), true},
		{"damage before link", manaLinkFields(join(lkdh, link, tail)), false},
		{"damage without link", manaLinkFields(join(lkdh, tail)), false},
		{"hp word", manaLinkFields(join(link, []uint32{tagTimedLinkedDamage, 10, manaSwitchPercent, 1596})), false},
		{"zero percent", manaLinkFields(join(link, []uint32{tagTimedLinkedDamage, 0, 0, 1596})), false},
		{"percent above whole", manaLinkFields(join(link, []uint32{tagTimedLinkedDamage, 0, maxLinkedDamagePercent + 1, 1596})), false},
		{"zero cap", manaLinkFields(join(link, []uint32{tagTimedLinkedDamage, 0, manaSwitchPercent, 0})), false},
		{"duplicate damage", manaLinkFields(join(link, lkdh, lkdh)), false},
		{"lks2 on a counted link", manaLinkFields(join([]uint32{tagTimedLink, manaSwitchGroup, manaSwitchDistance, 2, 0}, lks2, lkdh)), false},
		{"with threat", manaLinkFields(join(link, lkdh, []uint32{tagTimedLinkedThreat, 36, 0})), false},
		{"with strength", manaLinkFields(join(link, lkdh, []uint32{tagTimedStrength, 4, 50})), false},
		{"enemy columns without lkdh", manaLinkFields(join(link, []uint32{tagTimedLinkedThreat, 36, 0})), false},
		{"music getv without lkdh", linkedThreatFields(join(link, []uint32{tagTimedLinkedThreat, 36, 0}, tail)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: manaSwitchDurationMs,
			}
			parseSkillTimedEffect(tc.fields, &row)
			d := row.TimedEffect
			if d.Pinned != tc.valid {
				t.Fatalf("admission %v, want %v: %+v", d.Pinned, tc.valid, d)
			}
			if tc.valid && (!d.Targeted || !d.Link.Mana || d.Link.ManaPercent != manaSwitchPercent || d.Link.ManaCap != 1596 || d.Link.Threat) {
				t.Fatalf("mana switch program: %+v", d)
			}
		})
	}
}

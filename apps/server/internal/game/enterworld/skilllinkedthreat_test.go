/*
===========================================================================

skilllinkedthreat_test.go - admission of the lkag linked-threat program

The Warrior's Protect (GUARDA_AGGRO_A) is lnks + dura + lkag on the
targeted ally envelope. lkag only has an owner through the link context,
so it is admitted with a targeted lnks and nothing the link refuses.

===========================================================================
*/

package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

const (
	protectDurationMs = 1800000
	protectFirstID    = 7246 // SKILL_EU_WARRIOR_GUARDA_AGGRO_A_01
	protectTiers      = 9
	protectGroup      = 418
)

/*
================
linkedThreatFields

The targeted ally envelope (column 21 range, Required+Animal+Ally+Party)
around a supplied instruction stream.
================
*/
func linkedThreatFields(tail []uint32) []string {
	fields := marchProgramFields(tail)
	fields[21] = "150"
	fields[22], fields[23], fields[27], fields[28] = "1", "1", "1", "1"
	return fields
}

/*
================
TestLinkedThreatProgramAdmissionIsAtomic

lkag {percent, 0} after a targeted lnks pins the program and rides the
link. lkag before lnks, a nonzero second word, a percent above 100, a
duplicate, a missing link, the combinations the link already refuses (defp,
area, cbuf) and blk or odar, which the linked runtime would drop, keep the
whole row unsupported.
================
*/
func TestLinkedThreatProgramAdmissionIsAtomic(t *testing.T) {
	link := []uint32{tagTimedLink, 3, 1500, 2, 1}
	lkag := []uint32{tagTimedLinkedThreat, 36, 0}
	join := func(parts ...[]uint32) []uint32 {
		out := []uint32{tagDura, protectDurationMs}
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	// The native index keeps lkag wherever it is authored, 5A03A0 reads word
	// 0 only and forms the share as authored: order, word 1 and a share above
	// the whole aggression are all admitted.
	for _, tc := range []struct {
		name    string
		tail    []uint32
		odar    bool
		valid   bool
		percent uint32
	}{
		{"protect", join(link, lkag, []uint32{tagReqi, 6, 7, tagReqi, 6, 8, tagReqi, 6, 9}), false, true, 36},
		{"threat before link", join(lkag, link), false, true, 36},
		{"link without threat", join(link), false, false, 0},
		{"threat without link", join(lkag), false, false, 0},
		{"nonzero second word", join(link, []uint32{tagTimedLinkedThreat, 36, 1}), false, true, 36},
		{"percent above whole", join(link, []uint32{tagTimedLinkedThreat, 150, 0}), false, true, 150},
		{"duplicate threat", join(link, lkag, lkag), false, false, 0},
		{"with defense", join(link, lkag, []uint32{tagTimedDefense, 10, 10, 100}), false, false, 0},
		{"persistent", join(link, lkag, []uint32{tagCbuf}), false, false, 0},
		{"with block", join(link, lkag, []uint32{tagTimedBlock, 1, 10}), false, false, 0},
		{"with odar", join(link, lkag, []uint32{tagTimedIncomingReduction, 0, 20}), true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: protectDurationMs,
				// The projection agrees with odar, so only the link guard
				// can refuse that case.
				BuffModifiers: SkillBuffModifiers{Odar: tc.odar, OdarWord: 20},
			}
			parseSkillTimedEffect(linkedThreatFields(tc.tail), &row)
			d := row.TimedEffect
			if d.Pinned != tc.valid {
				t.Fatalf("admission %v, want %v: %+v", d.Pinned, tc.valid, d)
			}
			if tc.valid && (!d.Targeted || !d.Link.Present || !d.Link.Threat || d.Link.ThreatPercent != tc.percent ||
				d.Link.Group != 3 || d.Link.MaxDistance != 1500 || d.Link.MaxOutgoing != 2) {
				t.Fatalf("protect program: %+v", d)
			}
		})
	}

	// An area selection cannot join a targeted envelope, so the area guard
	// is exercised on the untargeted envelope: lkag never reaches a link.
	row := SkillRow{
		Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
		ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
		EffectDurationMs: protectDurationMs,
	}
	parseSkillTimedEffect(marchProgramFields(join([]uint32{tagEfr, 1, 1, 300, 8, 0, SelectCaster | SelectParty}, lkag)), &row)
	if row.TimedEffect.Pinned {
		t.Fatalf("area lkag admitted: %+v", row.TimedEffect)
	}
}

/*
================
TestShippedProtectTiersAreLinkedThreat

Every shipped Protect tier is admitted as a targeted linked timed effect
whose lkag share rises 36..60 in steps of three.
================
*/
func TestShippedProtectTiersAreLinkedThreat(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	skills := sharedShippedSkills(t)
	for tier := uint32(0); tier < protectTiers; tier++ {
		id := protectFirstID + tier
		row, ok := skills.SkillByID(id)
		if !ok {
			t.Fatalf("missing Protect tier %d", id)
		}
		d := row.TimedEffect
		if row.Group != protectGroup || row.EffectDurationMs != protectDurationMs ||
			!d.Pinned || !d.Targeted || d.Persistent || d.Defense || d.Area.Present ||
			d.Strength.Present || d.Intellect.Present {
			t.Fatalf("tier %d: group %d duration %d program %+v", id, row.Group, row.EffectDurationMs, d)
		}
		want := SkillEffectLink{Present: true, Group: 3, MaxDistance: 1500, MaxOutgoing: 2, Board: 1, Threat: true, ThreatPercent: 36 + 3*tier}
		if d.Link != want {
			t.Fatalf("tier %d link %+v, want %+v", id, d.Link, want)
		}
		if plan := skills.ExecutionPlan(id); plan.Kind() != SkillExecutionTimedEffect {
			t.Fatalf("tier %d execution plan %v", id, plan.Kind())
		}
	}
}

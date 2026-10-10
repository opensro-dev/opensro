/*
===========================================================================

skilllinkedfence_test.go - admission of the lkdr fence and lkdd quota links

The Warrior's Physical and Magical Fence (GUARDA_PHYSICAL_A / GUARDA_MAGIC_A)
are lnks + dura + lkdr + reqi, and Pain Quota (GUARDA_DIVIDE_A) is lnks +
dura + lkdd + reqi, all on the targeted envelope. Each share only has an
owner through the link context, so it is admitted with a targeted lnks and
nothing else the link would drop.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

const (
	fenceDurationMs = 1800000
	quotaDurationMs = 300000
	fenceDistance   = 1500
	fenceOutgoing   = 2
)

/*
================
TestShippedFenceAndQuotaTiersAreLinks

Every Physical Fence tier moves its percent of the physical lane (mask 4
completed to 7), every Magical Fence tier the magical lane (8 to 11), with
no hit limit; every Pain Quota tier keeps its quota percent, in link group 0.
================
*/
func TestShippedFenceAndQuotaTiersAreLinks(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, line := range []struct {
		code     string
		tiers    int
		group    uint32
		duration uint32
		mask     uint32
		quota    bool
	}{
		{"SKILL_EU_WARRIOR_GUARDA_PHYSICAL_A", 11, 1, fenceDurationMs, 4 | 1 | 2, false},
		{"SKILL_EU_WARRIOR_GUARDA_MAGIC_A", 9, 2, fenceDurationMs, 8 | 1 | 2, false},
		{"SKILL_EU_WARRIOR_GUARDA_DIVIDE_A", 4, 0, quotaDurationMs, 0, true},
	} {
		for tier := 1; tier <= line.tiers; tier++ {
			code := fmt.Sprintf("%s_%02d", line.code, tier)
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			d := row.TimedEffect
			if !d.Pinned || !d.Targeted || d.Persistent || d.Area.Present || row.EffectDurationMs != line.duration {
				t.Fatalf("%s: duration %d program %+v", code, row.EffectDurationMs, d)
			}
			l := d.Link
			if !l.Present || l.Group != line.group || l.MaxDistance != fenceDistance || l.MaxOutgoing != fenceOutgoing ||
				l.Threat || l.Mana {
				t.Fatalf("%s link %+v", code, l)
			}
			if line.quota {
				if !l.Quota || l.Fence || l.QuotaPercent == 0 || l.QuotaPercent > 100 {
					t.Fatalf("%s quota %+v", code, l)
				}
			} else if !l.Fence || l.Quota || l.FenceMask != line.mask || l.FencePercent == 0 || l.FencePercent > 100 ||
				l.FenceMaxHits != 0 {
				t.Fatalf("%s fence %+v", code, l)
			}
			if plan := source.ExecutionPlan(row.ID); plan.Kind() != SkillExecutionTimedEffect {
				t.Fatalf("%s execution plan %v", code, plan.Kind())
			}
		}
	}
}

/*
================
TestFenceMaskCompletion

588A06: a lane value gains both share bits, a share value both lanes;
anything else is kept as authored.
================
*/
func TestFenceMaskCompletion(t *testing.T) {
	for word, want := range map[uint32]uint32{4: 7, 8: 11, 12: 15, 1: 13, 2: 14, 3: 15, 5: 5, 0: 0} {
		if got := linkFenceMask(word); got != want {
			t.Fatalf("mask %d completed to %d, want %d", word, got, want)
		}
	}
}

/*
================
TestFenceAndQuotaProgramAdmissionIsAtomic

lkdr {mask, percent, max hits} or lkdd {percent} after a targeted lnks pins
the program. A missing link, a zero or over-whole percent, a wrong word
count, a duplicate, two link kinds on one row and stat writes beside the
share keep the row unsupported.
================
*/
func TestFenceAndQuotaProgramAdmissionIsAtomic(t *testing.T) {
	link := []uint32{tagTimedLink, 1, fenceDistance, fenceOutgoing, 1}
	fence := []uint32{tagTimedLinkedFence, 4, 33, 0}
	quota := []uint32{tagTimedLinkedQuota, 35}
	join := func(parts ...[]uint32) []uint32 {
		out := []uint32{tagDura, fenceDurationMs}
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	shield := []uint32{tagReqi, 6, 7, tagReqi, 6, 8, tagReqi, 6, 9}
	for _, tc := range []struct {
		name  string
		tail  []uint32
		valid bool
	}{
		{"physical fence", join(link, fence, shield), true},
		{"pain quota", join([]uint32{tagTimedLink, 0, fenceDistance, fenceOutgoing, 1}, quota, shield), true},
		{"fence before link", join(fence, link), true},
		{"fence without link", join(fence), false},
		{"quota without link", join(quota), false},
		{"zero fence percent", join(link, []uint32{tagTimedLinkedFence, 4, 0, 0}), false},
		{"fence above whole", join(link, []uint32{tagTimedLinkedFence, 4, 101, 0}), false},
		{"zero quota", join(link, []uint32{tagTimedLinkedQuota, 0}), false},
		{"quota above whole", join(link, []uint32{tagTimedLinkedQuota, 101}), false},
		{"duplicate fence", join(link, fence, fence), false},
		{"fence and quota", join(link, fence, quota), false},
		{"fence and threat", join(link, fence, []uint32{tagTimedLinkedThreat, 36, 0}), false},
		{"fence with block", join(link, fence, []uint32{tagTimedBlock, 1, 10}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: fenceDurationMs,
			}
			parseSkillTimedEffect(linkedThreatFields(tc.tail), &row)
			if row.TimedEffect.Pinned != tc.valid {
				t.Fatalf("admission %v, want %v: %+v", row.TimedEffect.Pinned, tc.valid, row.TimedEffect)
			}
		})
	}
}

/*
================
TestShippedScreamMaskTiersAreLinks

Every Scream Mask tier is a targeted link in group 0 whose abnb range
rolls the row's status blocks; a status block without abnb, or abnb
without a link, keeps a row unsupported.
================
*/
func TestShippedScreamMaskTiersAreLinks(t *testing.T) {
	source := sharedShippedSkills(t)
	for tier := 1; tier <= 7; tier++ {
		code := fmt.Sprintf("SKILL_EU_WARLOCK_SOULA_STUNLINK_A_%02d", tier)
		row, ok := source.SkillByCodename(code)
		if !ok {
			t.Fatalf("missing %s", code)
		}
		d := row.TimedEffect
		if !d.Pinned || !d.Targeted || !d.Link.Present || !d.Link.Scream || d.Link.ScreamRange != 150 || d.Link.Group != 0 ||
			d.Link.Fence || d.Link.Quota || !row.Abnormal.Present() {
			t.Fatalf("%s: %+v", code, d.Link)
		}
	}
	link := []uint32{tagTimedLink, 0, fenceDistance, fenceOutgoing, 1}
	scream := []uint32{tagTimedLinkedScream, 150}
	stun := []uint32{0x7374, 5000, 35, 3}
	for _, tc := range []struct {
		name  string
		tail  []uint32
		valid bool
	}{
		{"scream mask", append(append(append([]uint32{tagDura, 120000}, link...), scream...), stun...), true},
		{"scream without link", append(append([]uint32{tagDura, 120000}, scream...), stun...), false},
		{"status without scream", append(append([]uint32{tagDura, 120000}, link...), stun...), false},
		{"zero range", append(append(append([]uint32{tagDura, 120000}, link...), tagTimedLinkedScream, 0), stun...), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: 120000,
			}
			fields := linkedThreatFields(tc.tail)
			row.Abnormal = encodedAbnormalParams(fields)
			parseSkillTimedEffect(fields, &row)
			if row.TimedEffect.Pinned != tc.valid {
				t.Fatalf("admission %v, want %v: %+v", row.TimedEffect.Pinned, tc.valid, row.TimedEffect.Link)
			}
		})
	}
}

/*
===========================================================================

linkeddamage.go - the share of a hit a fence or Pain Quota link moves away

CSkillManager_ProcessDamageEffects (5A0B80) runs on the recipient of every
hit. A Physical or Magical Fence link on it (lkdr, the +2C8 list, 5A0F01)
moves part of the hit to the Warrior; Pain Quota (lkdd, +20C, 5A11BF) keeps
part and divides the rest among the recipient's party. This file owns the
arithmetic; the runtime owns which links apply, who takes the share and
the publication.

===========================================================================
*/

package combat

// FenceLaneMask bits of lkdr word 0 after the loader's completion
// (enterworld.linkFenceMask): a lane moves when its lane bit and a share
// bit are both set.
const (
	fenceShareMask    = 1 | 2
	fencePhysicalLane = 4
	fenceMagicalLane  = 8
	// fenceWholePercent moves the whole hit instead of its lanes (5A0F43).
	fenceWholePercent = 100
)

/*
==================
FenceShare

5A0F01 for one fence link: at 100 percent the whole hit (+0x20) moves and
the lanes stay; otherwise each enabled lane (+0x28 physical, +0x24
magical) moves ftol(percent / 100.0 * lane), and the hit loses their sum,
held to the hit (5A1050). Returns the hit the recipient keeps and the
amount the link source takes; zero moves nothing.
==================
*/
func FenceShare(mask, percent uint32, hit Result) (Result, uint32) {
	var moved uint32
	if percent == fenceWholePercent {
		moved = hit.Damage
	} else if mask&fenceShareMask != 0 {
		var physical, magical uint32
		if mask&fencePhysicalLane != 0 {
			physical = linkedLaneShare(percent, hit.PhysicalDamage)
		}
		if mask&fenceMagicalLane != 0 {
			magical = linkedLaneShare(percent, hit.MagicalDamage)
		}
		hit.PhysicalDamage -= physical
		hit.MagicalDamage -= magical
		moved = physical + magical
	}
	if moved == 0 {
		return hit, 0
	}
	moved = min(moved, hit.Damage)
	hit.Damage -= moved
	return hit, moved
}

/*
==================
linkedLaneShare

ftol(percent / 100.0 * lane) on the x87 stack (CRT_ftol truncates). All
four lane branches use it: 5A0FCA and 5A100C for physical, 5A105D and
5A109B for magical. The last reuses the 100.0 the physical branch left on
the stack (fdivrp at 5A10B1), which HLIL misreads as percent / percent.
Both operands are unsigned words loaded as signed and corrected, so they
are exact in a double.
==================
*/
func linkedLaneShare(percent, lane uint32) uint32 {
	return uint32(float64(percent) / 100.0 * float64(lane))
}

/*
==================
QuotaShare

5A11BF for Pain Quota: the recipient keeps ftol((100 - percent) / 100.0 *
hit) and each of the members counted divides the rest equally, the
remainder lost to the unsigned division (5A13D0). With no member, or a
share of zero, the hit is unchanged and nothing moves.
==================
*/
func QuotaShare(percent uint32, hit Result, members int) (Result, uint32) {
	if members <= 0 || percent == 0 || percent > 100 {
		return hit, 0
	}
	kept := uint32(float64(100-percent) / 100.0 * float64(hit.Damage))
	share := (hit.Damage - kept) / uint32(members)
	if share == 0 {
		return hit, 0
	}
	hit.Damage = kept
	return hit, share
}

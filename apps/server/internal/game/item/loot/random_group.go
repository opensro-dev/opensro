/*
===========================================================================

random_group.go - exact accepted-member probabilities without native retries

The native 724E30 rejection loop spends unbounded time on tiny member rates.
Conditioning its integer sample space on acceptance preserves reward outcomes,
including modulo bias, without preserving the number of random draws.

===========================================================================
*/
package loot

import "fmt"

const (
	randomBits       = 15
	randomRange      = uint64(1 << randomBits)
	combinedRange    = randomRange * randomRange
	probabilityScale = uint64(1_000_000)
	wideRandomBits   = 4 * randomBits
	wideRandomRange  = uint64(1 << wideRandomBits)
)

/*
================
moduloAcceptanceMass

Count inputs whose remainder is at most threshold. Native comparisons are
inclusive: even an authored zero member probability accepts remainder zero.
================
*/
func moduloAcceptanceMass(samples, denominator, threshold uint64) uint64 {
	accepted := min(threshold+1, denominator)
	return samples/denominator*accepted + min(samples%denominator, accepted)
}

/*
================
groupMemberMass

The candidate index uses one 15-bit draw; admission uses two. Selected distinct
members retain their original indices, since native rejects duplicates rather
than compacting the candidate vector.
================
*/
func groupMemberMass(index, count int, probability float32) uint64 {
	candidates := randomRange / uint64(count)
	if uint64(index) < randomRange%uint64(count) {
		candidates++
	}
	threshold := uint64(float64(probability) * float64(probabilityScale))
	return candidates * moduloAcceptanceMass(combinedRange, probabilityScale, threshold)
}

/*
================
uniformBelow

Four native-sized draws provide enough bits for the complete candidate and
admission sample space. Recycle rejection only at this unbiased integer door;
no admitted reward is discarded because its authored rate is small.
================
*/
func uniformBelow(bound uint64, roll func() (uint32, error)) (uint64, error) {
	if bound == 0 || bound > wideRandomRange || roll == nil {
		return 0, fmt.Errorf("invalid loot sampling bound %d", bound)
	}
	if bound == 1 {
		return 0, nil
	}
	limit := wideRandomRange - wideRandomRange%bound
	for {
		var value uint64
		for bit := 0; bit < wideRandomBits; bit += randomBits {
			n, err := roll()
			if err != nil {
				return 0, err
			}
			if uint64(n) >= randomRange {
				return 0, fmt.Errorf("loot random draw %d exceeds 15 bits", n)
			}
			value = value<<randomBits | uint64(n)
		}
		if value < limit {
			return value % bound, nil
		}
	}
}

/*
================
selectGroupMember
================
*/
func selectGroupMember(pool []groupDrop, selected []bool, roll func() (uint32, error)) (int, error) {
	var total uint64
	for i, row := range pool {
		if !selected[i] {
			total += groupMemberMass(i, len(pool), row.Probability)
		}
	}
	wanted, err := uniformBelow(total, roll)
	if err != nil {
		return 0, err
	}
	for i, row := range pool {
		if selected[i] {
			continue
		}
		mass := groupMemberMass(i, len(pool), row.Probability)
		if wanted < mass {
			return i, nil
		}
		wanted -= mass
	}
	return 0, fmt.Errorf("loot group has no selectable member")
}

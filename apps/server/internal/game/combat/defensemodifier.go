package combat

import (
	"fmt"
	"math"

	"opensro.online/server/internal/game/paramkeeper"
)

// DefenseModifierInput is an installation-time snapshot, not a live formula.
// 595209..59533C adds context +34/+38 with uint32 wrap, then optionally caps
// against the recipient's current Param5/6. Store the resulting writes for
// this effect's lifetime: reevaluating the cap after later buffs is incorrect.
// The caller owns reqi/reqn eligibility and resolves caster dictionary bonuses.
type DefenseModifierInput struct {
	Physical, Magical               uint32
	PhysicalBonus, MagicalBonus     uint32
	CapPercent                      uint32
	CurrentPhysical, CurrentMagical float32
}

// DefenseEffectWrites snapshots a timed application's native installation
// values. Source zero is bound by the effect registry to its unique owner.
func DefenseEffectWrites(in DefenseModifierInput) ([]paramkeeper.Write, error) {
	return defenseModifierWrites(0, in)
}

func defenseModifierWrites(source uint32, in DefenseModifierInput) ([]paramkeeper.Write, error) {
	values := [2]uint32{in.Physical + in.PhysicalBonus, in.Magical + in.MagicalBonus}
	if in.CapPercent != 0 {
		for i, current := range [2]float32{in.CurrentPhysical, in.CurrentMagical} {
			if math.IsNaN(float64(current)) || math.IsInf(float64(current), 0) || current < 0 {
				return nil, fmt.Errorf("combat: invalid defense cap snapshot")
			}
			// Native truncates an x87 product to int64 and retains its low u32.
			// The parameter definition bounds current to 9,999,999, keeping even
			// the largest authored percentage comfortably within int64.
			if current > 9999999 {
				return nil, fmt.Errorf("combat: defense snapshot exceeds native parameter limit")
			}
			cap := uint32(uint64(float64(current) * (float64(in.CapPercent) / 100)))
			if values[i] > cap {
				values[i] = cap
			}
		}
	}
	return []paramkeeper.Write{
		{Parameter: 5, Channel: paramkeeper.Flat, Source: source, Value: float32(values[0])},
		{Parameter: 6, Channel: paramkeeper.Flat, Source: source, Value: float32(values[1])},
	}, nil
}

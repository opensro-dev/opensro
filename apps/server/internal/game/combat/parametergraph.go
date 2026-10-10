/*
===========================================================================

parametergraph.go - the player parameter keeper graph (CGObjPC_InitializeParameterGraph 4E3200)

Build a detached graph from native definitions and explicit source writes.
Combat, recovery and reward consumers read the same modifier arithmetic;
the effect registry remains the sole owner of effect lifetime.

===========================================================================
*/

package combat

import (
	"fmt"

	"opensro.online/server/internal/game/paramkeeper"
)

// Detached projection of the player keeper built by 4E3200
// (CGObjPC_InitializeParameterGraph). Definitions are the native C64110 table
// (paramkeeper.NativeDefinition). Source 0 is native's built-in contribution;
// native small constants (5 = abnormal state) are sources too, so parameter
// node identities live in their own port range. Item/passive/effect keys are
// assigned separately (1024+slot, 2048+, 0x80000000+). This preserves source
// ownership, not native allocator pointer ordering.
const parameterNodeKeyBase uint32 = 0x40000000

const (
	// paramMPConsumptionRate is parameter 0x8D, the percent of its MP cost a
	// player is charged (base 100, 4E36AC).
	paramMPConsumptionRate uint16 = 0x8d

	// FullMPConsumptionRate is the rate of an actor without the parameter
	// (native scales only players by 0x8D, 58E24A..58E277) and of a cost-free
	// row, which no rate changes.
	FullMPConsumptionRate = 100
)

/*
================
MPConsumptionRate

The percent of an MP cost s is charged: its parameter 0x8D, 100 for
stats that do not keep it.
================
*/
func MPConsumptionRate(s Stats) float32 {
	if rate, ok := s.Param(paramMPConsumptionRate); ok {
		return rate
	}
	return FullMPConsumptionRate
}

/*
================
playerParameterDefinitions
================
*/
func playerParameterDefinitions() ([]paramkeeper.NodeDefinition, error) {
	// The nodes the combat closure, the magic-option switch (498690), the
	// abnormal-state callbacks and the block chance (410C20: 0x88..0x8B,
	// written by br) read or write, and the attack-range keeper (33, 0x21:
	// the weapon's reach plus every ru, CGObjChar_GetAttackRangeParam 4AC890).
	ids := [...]uint16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43,
		44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59,
		0x80, 0x81, 0x82, 0x83, 0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8f, 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96,
		0xa9, 0xaa, 0xab, 0xae, 0xaf, 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xbc}
	definitions := make([]paramkeeper.NodeDefinition, 0, len(ids))
	for _, id := range ids {
		d, ok := paramkeeper.NativeDefinition(id)
		if !ok {
			return nil, fmt.Errorf("combat: parameter %d has no native definition", id)
		}
		definitions = append(definitions, paramkeeper.NodeDefinition{ID: id, SourceKey: parameterNodeKeyBase + uint32(id), Definition: d})
	}
	return definitions, nil
}

/*
==================
playerParameterGraph

playerParameterGraph links the closure and returns its built-in writes for
one level. Links are {input, dependent, channel}: 4B3540 with EDI the
dependent and EBX the input.
==================
*/
func playerParameterGraph(level uint8) (*paramkeeper.Graph, []paramkeeper.Write, error) {
	definitions, err := playerParameterDefinitions()
	if err != nil {
		return nil, nil, err
	}
	// The client-authored shield tradeoff has a known percentage unit but
	// no verified native keeper identity. This detached node carries only
	// the projection's effect contribution, with the same bounded unit.
	definitions = append(definitions, paramkeeper.NodeDefinition{
		ID: shieldDefensePenaltyParameter, SourceKey: parameterNodeKeyBase + uint32(shieldDefensePenaltyParameter),
		Definition: paramkeeper.Definition{Minimum: 0, Maximum: shieldPenaltyPercent, Base: 0, Ignore: -1}})
	g, err := paramkeeper.NewGraph(definitions)
	if err != nil {
		return nil, nil, err
	}
	for _, link := range [][3]uint16{
		// 4E3294..4E330A: max HP 3 <- 54 <- 52 <- STR, max MP 4 <- 55 <- 53 <- INT.
		{54, 3, 0}, {55, 4, 0}, {52, 54, 0}, {53, 55, 0}, {1, 52, 0}, {2, 53, 0},
		{21, 5, 0}, {22, 6, 0}, {1, 21, 0}, {2, 22, 0},
		{17, 13, 0}, {18, 14, 0}, {19, 15, 0}, {20, 16, 0},
		{1, 17, 0}, {1, 18, 0}, {2, 19, 0}, {2, 20, 0},
		{34, 17, 3}, {36, 18, 3}, {35, 19, 3}, {37, 20, 3},
		{50, 21, 3}, {51, 22, 3}, {58, 50, 0}, {59, 51, 0},
		{0, 11, 0}, {0, 9, 0},
	} {
		if err = g.Link(link[0], link[1], paramkeeper.Channel(link[2])); err != nil {
			return nil, nil, err
		}
	}
	// 4E32CA..4E32F0: STR/INT feed the gauges at 1000% (x10).
	writes := []paramkeeper.Write{
		{Parameter: 52, Channel: paramkeeper.FactorProduct, Value: 1000},
		{Parameter: 53, Channel: paramkeeper.FactorProduct, Value: 1000},
		{Parameter: 58}, {Parameter: 59}}
	// 4E346F..4E34B6: authored float32 coefficient * double 100, stored
	// to float32 before insertion. Do not collapse the six entries to 0.19/0.305.
	coefficients := [6][2]float32{{.032, .052}, {.027, .043}, {.042, .067}, {.034, .055}, {.025, .039}, {.030, .049}}
	for i, pair := range coefficients {
		for axis := 0; axis < 2; axis++ {
			id := uint16(38 + i + axis*6)
			if err = g.Link(id, uint16(50+axis), paramkeeper.Flat); err != nil {
				return nil, nil, err
			}
			writes = append(writes, paramkeeper.Write{Parameter: id, Value: float32(float64(pair[axis]) * 100)})
		}
	}
	growth := levelGrowthPercent(level)
	writes = append(writes,
		paramkeeper.Write{Parameter: 34, Value: float32(30.5999985)},
		paramkeeper.Write{Parameter: 36, Value: float32(34.2000008)},
		paramkeeper.Write{Parameter: 35, Value: 49},
		paramkeeper.Write{Parameter: 37, Value: float32(54.7999992)},
		paramkeeper.Write{Parameter: 11, Value: 10}, paramkeeper.Write{Parameter: 9, Value: 10},
		paramkeeper.Write{Parameter: 54, Channel: paramkeeper.FactorProduct, Value: growth},
		paramkeeper.Write{Parameter: 55, Channel: paramkeeper.FactorProduct, Value: growth},
		// 4E3567..: movement and damage-scale bases the abnormal callbacks rewrite.
		paramkeeper.Write{Parameter: 0x8c, Value: 100},
		// 4E36AC: the MP consumption rate every player cost is scaled by
		// (58E25F, 583232); dcmp lowers it.
		paramkeeper.Write{Parameter: paramMPConsumptionRate, Value: 100})
	return g, writes, nil
}

/*
==================
levelGrowthPercent

levelGrowthPercent is 4E3527..4E3561: 1.02^(level-1) by binary
exponentiation on the x87 stack, stored to float32, then * 100.0 and
stored to float32 again.
==================
*/
func levelGrowthPercent(level uint8) float32 {
	exponent := int(level) - 1
	if exponent < 0 {
		return 100
	}
	base, result := 1.02, 1.0
	for exponent != 0 {
		if exponent&1 != 0 {
			result *= base
		}
		exponent >>= 1
		base *= base
	}
	return float32(float64(float32(result)) * 100)
}

/*
================
statWrites

Translate the contiguous native combat subset without allocating empty sources.
================
*/
func statWrites(stats Stats, source uint32) []paramkeeper.Write {
	values := []float64{stats.PhysicalDefense, stats.MagicalDefense, stats.ParryRate, stats.MagicalParry,
		stats.EvasionRate, stats.BlockRate, stats.HitRate, stats.CriticalRate,
		stats.PhysicalAttackMin, stats.PhysicalAttackMax, stats.MagicalAttackMin, stats.MagicalAttackMax}
	var writes []paramkeeper.Write
	for i, v := range values {
		if v != 0 {
			writes = append(writes, paramkeeper.Write{Parameter: uint16(5 + i), Source: source, Value: float32(v)})
		}
	}
	return writes
}

/*
================
readParameterStats

Materialize convenience fields while retaining the complete graph for other
consumers. An evaluation error invalidates the snapshot.
================
*/
func readParameterStats(g *paramkeeper.Graph, stats *Stats) error {
	fields := []*float64{&stats.PhysicalDefense, &stats.MagicalDefense, &stats.ParryRate, &stats.MagicalParry,
		&stats.EvasionRate, &stats.BlockRate, &stats.HitRate, &stats.CriticalRate,
		&stats.PhysicalAttackMin, &stats.PhysicalAttackMax, &stats.MagicalAttackMin, &stats.MagicalAttackMax}
	for i, dst := range fields {
		v, err := g.Value(uint16(i + 5))
		if err != nil {
			return err
		}
		*dst = float64(v)
	}
	str, err := g.Value(1)
	if err != nil {
		return err
	}
	intel, err := g.Value(2)
	if err != nil {
		return err
	}
	stats.Strength, stats.Intellect = float64(str), float64(intel)
	for i, dst := range []*float64{&stats.PhysicalBasicRate, &stats.PhysicalSkillRate, &stats.MagicalBasicRate, &stats.MagicalSkillRate} {
		v, err := g.Value(uint16(0x80 + i))
		if err != nil {
			return err
		}
		*dst = float64(v)
	}
	for i, dst := range []*float32{&stats.PhysicalBasicTaken, &stats.PhysicalSkillTaken, &stats.MagicalBasicTaken, &stats.MagicalSkillTaken} {
		v, err := g.Value(uint16(0xae + i))
		if err != nil {
			return err
		}
		*dst = v
	}
	for i, dst := range []*float32{&stats.PhysicalOutgoing, &stats.MagicalOutgoing, &stats.PhysicalIncoming, &stats.MagicalIncoming} {
		v, err := g.Value(uint16(0xb2 + i))
		if err != nil {
			return err
		}
		*dst = v
	}
	return nil
}

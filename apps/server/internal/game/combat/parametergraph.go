/*
===========================================================================

parametergraph.go - the player parameter keeper graph (the server's player graph builder)

Build a detached graph from native definitions and explicit source writes.
Combat, recovery and reward consumers read the same modifier arithmetic;
the effect registry remains the sole owner of effect lifetime.

===========================================================================
*/

package combat

import (
	"fmt"
	"sync"

	"opensro.online/server/internal/game/paramkeeper"
)

// Detached projection of the player keeper the reference server builds
// at character init. Definitions are the native definition table
// (paramkeeper.NativeDefinition). Source 0 is native's built-in contribution;
// native small constants (5 = abnormal state) are sources too, so parameter
// node identities live in their own port range. Item/passive/effect keys are
// assigned separately (1024+slot, 2048+, 0x80000000+). This preserves source
// ownership, not native allocator pointer ordering.
const parameterNodeKeyBase uint32 = 0x40000000

const (
	// paramMPConsumptionRate is parameter 0x8D, the percent of its MP cost a
	// player is charged (base 100, written by the graph builder).
	paramMPConsumptionRate uint16 = 0x8d

	// FullMPConsumptionRate is the rate of an actor without the parameter
	// (native's skill cost scales only players by 0x8D) and of a cost-free
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
	// The nodes the combat closure, the item magic-option switch, the
	// abnormal-state callbacks and the block chance formula (0x88..0x8B,
	// written by br) read or write, and the attack-range keeper (33, 0x21:
	// the weapon's reach plus every ru, as the server's attack-range read sums).
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

var cachedPlayerTemplateGraph = sync.OnceValues(func() (*paramkeeper.Graph, error) {
	definitions, err := playerParameterDefinitions()
	if err != nil {
		return nil, err
	}
	g, err := paramkeeper.NewGraph(definitions)
	if err != nil {
		return nil, err
	}
	for _, link := range playerParameterLinks() {
		if err = g.Link(link[0], link[1], paramkeeper.Channel(link[2])); err != nil {
			return nil, err
		}
	}
	return g, nil
})

/*
==================
playerParameterGraph

playerParameterGraph links the closure and returns its built-in writes for
one level. Links are {input, dependent, channel}: the server's link routine with EDI the
dependent and EBX the input.
==================
*/
func playerParameterGraph(level uint8) (*paramkeeper.Graph, []paramkeeper.Write, error) {
	tmpl, err := cachedPlayerTemplateGraph()
	if err != nil {
		return nil, nil, err
	}
	return tmpl.Clone(), playerBaseWrites(level), nil
}

/*
==================
playerParameterLinks

The closure's links in the graph builder's order, the same for every level.
==================
*/
func playerParameterLinks() [][3]uint16 {
	links := [][3]uint16{
		// The builder's gauge chains: max HP 3 <- 54 <- 52 <- STR, max MP 4 <- 55 <- 53 <- INT.
		{54, 3, 0}, {55, 4, 0}, {52, 54, 0}, {53, 55, 0}, {1, 52, 0}, {2, 53, 0},
		{21, 5, 0}, {22, 6, 0}, {1, 21, 0}, {2, 22, 0},
		{17, 13, 0}, {18, 14, 0}, {19, 15, 0}, {20, 16, 0},
		{1, 17, 0}, {1, 18, 0}, {2, 19, 0}, {2, 20, 0},
		{34, 17, 3}, {36, 18, 3}, {35, 19, 3}, {37, 20, 3},
		{50, 21, 3}, {51, 22, 3}, {58, 50, 0}, {59, 51, 0},
		{0, 11, 0}, {0, 9, 0},
	}
	// The builder's coefficient block: each authored coefficient feeds 50 or 51 (Flat).
	for i := range playerCoefficients {
		for axis := 0; axis < 2; axis++ {
			links = append(links, [3]uint16{uint16(38 + i + axis*6), uint16(50 + axis), uint16(paramkeeper.Flat)})
		}
	}
	return links
}

// playerCoefficients are the graph builder's authored float32 coefficients,
// multiplied by double 100 and stored to float32 before insertion. Do not
// collapse the six entries to 0.19/0.305.
var playerCoefficients = [6][2]float32{{.032, .052}, {.027, .043}, {.042, .067}, {.034, .055}, {.025, .039}, {.030, .049}}

/*
==================
playerBaseWrites

The closure's built-in writes for one level, in the graph builder's order.
==================
*/
func playerBaseWrites(level uint8) []paramkeeper.Write {
	// The builder's first writes: STR/INT feed the gauges at 1000% (x10).
	writes := make([]paramkeeper.Write, 0, 64)
	writes = append(writes,
		paramkeeper.Write{Parameter: 52, Channel: paramkeeper.FactorProduct, Value: 1000},
		paramkeeper.Write{Parameter: 53, Channel: paramkeeper.FactorProduct, Value: 1000},
		paramkeeper.Write{Parameter: 58},
		paramkeeper.Write{Parameter: 59},
	)
	for i, pair := range playerCoefficients {
		for axis := 0; axis < 2; axis++ {
			writes = append(writes, paramkeeper.Write{Parameter: uint16(38 + i + axis*6), Value: float32(float64(pair[axis]) * 100)})
		}
	}
	growth := levelGrowthPercent(level)
	return append(writes,
		paramkeeper.Write{Parameter: 34, Value: float32(30.5999985)},
		paramkeeper.Write{Parameter: 36, Value: float32(34.2000008)},
		paramkeeper.Write{Parameter: 35, Value: 49},
		paramkeeper.Write{Parameter: 37, Value: float32(54.7999992)},
		paramkeeper.Write{Parameter: 11, Value: 10}, paramkeeper.Write{Parameter: 9, Value: 10},
		paramkeeper.Write{Parameter: 54, Channel: paramkeeper.FactorProduct, Value: growth},
		paramkeeper.Write{Parameter: 55, Channel: paramkeeper.FactorProduct, Value: growth},
		// Then the movement and damage-scale bases the abnormal callbacks rewrite.
		paramkeeper.Write{Parameter: 0x8c, Value: 100},
		// Last, the MP consumption rate every player cost is scaled by
		// (on both server cost paths); dcmp lowers it.
		paramkeeper.Write{Parameter: paramMPConsumptionRate, Value: 100})
}

/*
==================
levelGrowthPercent

levelGrowthPercent is the graph builder's level growth: 1.02^(level-1) by binary
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
	values := [12]float64{stats.PhysicalDefense, stats.MagicalDefense, stats.ParryRate, stats.MagicalParry,
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
	fields := [12]*float64{&stats.PhysicalDefense, &stats.MagicalDefense, &stats.ParryRate, &stats.MagicalParry,
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
	for i, dst := range [4]*float64{&stats.PhysicalBasicRate, &stats.PhysicalSkillRate, &stats.MagicalBasicRate, &stats.MagicalSkillRate} {
		v, err := g.Value(uint16(0x80 + i))
		if err != nil {
			return err
		}
		*dst = float64(v)
	}
	for i, dst := range [4]*float32{&stats.PhysicalBasicTaken, &stats.PhysicalSkillTaken, &stats.MagicalBasicTaken, &stats.MagicalSkillTaken} {
		v, err := g.Value(uint16(0xae + i))
		if err != nil {
			return err
		}
		*dst = v
	}
	for i, dst := range [4]*float32{&stats.PhysicalOutgoing, &stats.MagicalOutgoing, &stats.PhysicalIncoming, &stats.MagicalIncoming} {
		v, err := g.Value(uint16(0xb2 + i))
		if err != nil {
			return err
		}
		*dst = v
	}
	return nil
}

/*
===========================================================================

parametergraph_flat_test.go - the production graph equals an Element oracle

For every level 1..140 the production definitions, links and base writes
are replayed through per-parameter Elements (the representation the flat
graph replaced) while the flat graph takes the same steps: the base batch,
fuzzed STR/INT, equipment-slot writes, effect writes in shuffled order,
Ignore-sentinel writes on linked intermediate parameters, removals, and
refused writes. Every parameter's value must agree bitwise after every
step, and a refused step must leave both projections untouched.
===========================================================================
*/

package combat

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
playerOracleEdge

One dependent in the oracle: target, channel, and the target's source
identity ordering the source's dependents.
================
*/
type playerOracleEdge struct {
	target  uint16
	channel paramkeeper.Channel
	key     uint32
}

/*
================
playerOracle

The production closure rebuilt on per-parameter Elements: the equality
reference for the flat graph at player shape.
================
*/
type playerOracle struct {
	elements   map[uint16]*paramkeeper.Element
	keys       map[uint16]uint32
	reserved   map[uint32]bool
	dependents map[uint16][]playerOracleEdge
}

/*
================
newPlayerOracle
================
*/
func newPlayerOracle(definitions []paramkeeper.NodeDefinition) (*playerOracle, error) {
	o := &playerOracle{
		elements:   make(map[uint16]*paramkeeper.Element),
		keys:       make(map[uint16]uint32),
		reserved:   make(map[uint32]bool),
		dependents: make(map[uint16][]playerOracleEdge),
	}
	for _, d := range definitions {
		element, err := paramkeeper.New(d.Definition)
		if err != nil {
			return nil, err
		}
		o.elements[d.ID] = element
		o.keys[d.ID] = d.SourceKey
		o.reserved[d.SourceKey] = true
	}
	for _, link := range playerParameterLinks() {
		o.link(link[0], link[1], paramkeeper.Channel(link[2]))
	}
	return o, nil
}

/*
================
playerOracle.link

The first registration for a target wins; dependents stay in ascending
target-identity order.
================
*/
func (o *playerOracle) link(source, target uint16, channel paramkeeper.Channel) {
	deps := o.dependents[source]
	for _, dep := range deps {
		if dep.target == target {
			return
		}
	}
	edge := playerOracleEdge{target: target, channel: channel, key: o.keys[target]}
	pos := len(deps)
	for i, dep := range deps {
		if dep.key > edge.key {
			pos = i
			break
		}
	}
	deps = append(deps, playerOracleEdge{})
	copy(deps[pos+1:], deps[pos:])
	deps[pos] = edge
	o.dependents[source] = deps
}

/*
================
playerOracle.applyWrite
================
*/
func (o *playerOracle) applyWrite(w paramkeeper.Write) error {
	changed, err := o.elements[w.Parameter].Apply(w.Channel, w.Source, w.Value)
	if err == nil && changed {
		err = o.propagate(w.Parameter)
	}
	return err
}

/*
================
playerOracle.propagate
================
*/
func (o *playerOracle) propagate(id uint16) error {
	v, err := o.elements[id].Value()
	if err != nil {
		return fmt.Errorf("paramkeeper: evaluating node %d: %w", id, err)
	}
	for _, dep := range o.dependents[id] {
		if err = o.applyWrite(paramkeeper.Write{Parameter: dep.target, Channel: dep.channel, Source: o.keys[id], Value: v}); err != nil {
			return err
		}
	}
	return nil
}

/*
================
proofStep

One step both projections take: a batch (or a single write when it holds
one write), or a removal.
================
*/
type proofStep struct {
	writes []paramkeeper.Write
	remove bool
	id     uint16
	source uint32
}

/*
================
playerOracle.step
================
*/
func (o *playerOracle) step(step proofStep) error {
	if step.remove {
		if _, ok := o.elements[step.id]; !ok || o.reserved[step.source] {
			return fmt.Errorf("paramkeeper: refused removal")
		}
		removed := o.elements[step.id].Remove(step.source)
		if removed == 0 {
			return nil
		}
		return o.propagate(step.id)
	}
	for _, w := range step.writes {
		if _, ok := o.elements[w.Parameter]; !ok || o.reserved[w.Source] {
			return fmt.Errorf("paramkeeper: refused write")
		}
	}
	for _, w := range step.writes {
		if err := o.applyWrite(w); err != nil {
			return err
		}
	}
	return nil
}

/*
================
replayPlayerOracle

A fresh oracle after the given steps; an error means the last step (the
candidate under test) was refused.
================
*/
func replayPlayerOracle(definitions []paramkeeper.NodeDefinition, steps []proofStep) (*playerOracle, error) {
	o, err := newPlayerOracle(definitions)
	if err != nil {
		return nil, err
	}
	for _, step := range steps {
		if err := o.step(step); err != nil {
			return nil, err
		}
	}
	return o, nil
}

/*
================
runProofStep
================
*/
func runProofStep(g *paramkeeper.Graph, step proofStep) error {
	if step.remove {
		_, err := g.Remove(step.id, step.source)
		return err
	}
	if len(step.writes) == 1 {
		w := step.writes[0]
		_, err := g.Apply(w.Parameter, w.Channel, w.Source, w.Value)
		return err
	}
	return g.ApplyBatch(step.writes)
}

/*
================
assertGraphMatchesOracle
================
*/
func assertGraphMatchesOracle(t *testing.T, g *paramkeeper.Graph, o *playerOracle, definitions []paramkeeper.NodeDefinition, where string) {
	t.Helper()
	for _, d := range definitions {
		got, gotErr := g.Value(d.ID)
		want, wantErr := o.elements[d.ID].Value()
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("%s param %d: graph err %v, oracle err %v", where, d.ID, gotErr, wantErr)
		}
		if gotErr == nil && math.Float32bits(got) != math.Float32bits(want) {
			t.Fatalf("%s param %d: graph %v (%08x), oracle %v (%08x)",
				where, d.ID, got, math.Float32bits(got), want, math.Float32bits(want))
		}
	}
}

/*
================
levelProofValue

An equipment/effect amount: sentinels and zeroes among ordinary values.
================
*/
func levelProofValue(rng *rand.Rand) float32 {
	switch rng.Intn(10) {
	case 0:
		return 0
	case 1:
		return -1
	}
	return float32(rng.Intn(240)) / 4
}

/*
================
levelProofSteps

The step sequence for one level: the base batch with fuzzed STR/INT,
shuffled equipment-slot writes, effect writes, sentinel writes on the
linked intermediates (1 -> 52 -> 54 -> 3 and its siblings), removals of
equipment and effect sources, and refused writes at the end.
================
*/
func levelProofSteps(level uint8, strength, intellect int64, base []paramkeeper.Write, ids []uint16, rng *rand.Rand) []proofStep {
	initial := append(append([]paramkeeper.Write{}, base...),
		paramkeeper.Write{Parameter: 0, Value: float32(level)},
		paramkeeper.Write{Parameter: 1, Value: float32(strength)},
		paramkeeper.Write{Parameter: 2, Value: float32(intellect)})
	steps := []proofStep{{writes: initial}}

	var equipment []paramkeeper.Write
	for slot := int64(0); slot < 13; slot++ {
		source := uint32(1024 + slot)
		for param := uint16(5); param <= 16; param++ {
			if rng.Intn(3) == 0 {
				equipment = append(equipment, paramkeeper.Write{
					Parameter: param, Channel: paramkeeper.Flat, Source: source, Value: levelProofValue(rng),
				})
			}
		}
		if rng.Intn(2) == 0 {
			equipment = append(equipment, paramkeeper.Write{
				Parameter: 3, Channel: paramkeeper.PercentSum, Source: source, Value: levelProofValue(rng),
			})
		}
	}
	rng.Shuffle(len(equipment), func(i, j int) { equipment[i], equipment[j] = equipment[j], equipment[i] })
	steps = append(steps, proofStep{writes: equipment})

	for i := 0; i < 18; i++ {
		steps = append(steps, proofStep{writes: []paramkeeper.Write{{
			Parameter: ids[rng.Intn(len(ids))],
			Channel:   paramkeeper.Channel(rng.Intn(4)),
			Source:    0x80000000 + uint32(rng.Intn(6)),
			Value:     levelProofValue(rng),
		}}})
	}

	// Linked intermediates and their Ignore sentinels: a write STR's
	// parameter ignores, a sentinel landing on 52 mid-chain, then the
	// removal that makes propagation carry the fallback downstream while
	// the sentinel-touched target entries stay as they were.
	steps = append(steps,
		proofStep{writes: []paramkeeper.Write{{Parameter: 1, Source: 0x900001, Value: 250}}},
		proofStep{writes: []paramkeeper.Write{{Parameter: 1, Source: 0x900001, Value: 0}}},
		proofStep{writes: []paramkeeper.Write{{Parameter: 52, Source: 0x900002, Value: 0}}},
		proofStep{writes: []paramkeeper.Write{{Parameter: 52, Channel: paramkeeper.FactorProduct, Source: 0x900003, Value: -1}}},
		proofStep{remove: true, id: 1, source: 0x900001},
		proofStep{writes: []paramkeeper.Write{{Parameter: 2, Source: 0x900001, Value: 300}}},
		proofStep{remove: true, id: 5, source: 1024},
		proofStep{remove: true, id: 5, source: 0x80000000},
		proofStep{remove: true, id: 52, source: parameterNodeKeyBase + 1},
	)

	// Refused steps: a batch whose second write has no channel, a write
	// under a reserved parameter identity, and a non-finite write. Each
	// must leave both projections exactly as they were.
	steps = append(steps,
		proofStep{writes: []paramkeeper.Write{
			{Parameter: 5, Source: 0x900010, Value: 10},
			{Parameter: 5, Channel: paramkeeper.Channel(4), Source: 0x900011, Value: 10},
		}},
		proofStep{writes: []paramkeeper.Write{{Parameter: 5, Source: parameterNodeKeyBase + 5, Value: 1}}},
		proofStep{writes: []paramkeeper.Write{{Parameter: 5, Source: 0x900012, Value: float32(math.NaN())}}},
	)
	return steps
}

/*
================
TestPlayerGraphMatchesElementOracleLevels1To140

Every level, step by step: the flat production graph and the Element
oracle agree on every parameter, or refuse the same step.
================
*/
func TestPlayerGraphMatchesElementOracleLevels1To140(t *testing.T) {
	tier.Long(t, "the player graph equals the element oracle at levels 1 to 140")
	definitions, err := playerParameterDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint16, len(definitions))
	for i, d := range definitions {
		ids[i] = d.ID
	}
	for level := 1; level <= 140; level++ {
		rng := rand.New(rand.NewSource(int64(876000 + level)))
		strength := int64(20 + rng.Intn(600))
		intellect := int64(20 + rng.Intn(600))
		g, base, err := playerParameterGraph(uint8(level))
		if err != nil {
			t.Fatal(err)
		}
		steps := levelProofSteps(uint8(level), strength, intellect, base, ids, rng)
		var committed []proofStep
		for i, step := range steps {
			where := fmt.Sprintf("level %d step %d", level, i)
			gotErr := runProofStep(g, step)
			candidate := append(append([]proofStep{}, committed...), step)
			oracle, oracleErr := replayPlayerOracle(definitions, candidate)
			if (gotErr == nil) != (oracleErr == nil) {
				t.Fatalf("%s: graph err %v, oracle err %v (step %+v)", where, gotErr, oracleErr, step)
			}
			if oracleErr == nil {
				committed = candidate
			} else {
				oracle, oracleErr = replayPlayerOracle(definitions, committed)
				if oracleErr != nil {
					t.Fatalf("%s: committed replay refused: %v", where, oracleErr)
				}
			}
			assertGraphMatchesOracle(t, g, oracle, definitions, where)
		}
	}
}

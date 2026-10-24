/*
===========================================================================

graph_flat_test.go - the flat graph against an Element-built reference

The reference graph below keeps the pre-flat representation: one Element
per node (Element's own bucket code, unchanged) and dependents in maps.
A deterministic fuzz walk applies writes, removals, batches, clones and
links to both and compares every node's value bitwise after every step,
including the steps an Ignore sentinel or a refused write leaves behind.
===========================================================================
*/

package paramkeeper

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

/*
================
referenceEdge

One dependent in the reference graph: target, channel, and the target's
source identity ordering the source's dependents.
================
*/
type referenceEdge struct {
	target  uint16
	channel Channel
	key     uint32
}

/*
================
referenceGraph

The old representation kept as the equality oracle: per-node Elements and
map-held dependents. It shares no code with the flat graph except Element
itself, whose bucket semantics both must reproduce.
================
*/
type referenceGraph struct {
	ids        []uint16
	elements   map[uint16]*Element
	definition map[uint16]Definition
	keys       map[uint16]uint32
	reserved   map[uint32]bool
	dependents map[uint16][]referenceEdge
	linked     map[uint16]bool
}

/*
================
newReferenceGraph
================
*/
func newReferenceGraph(t *testing.T, definitions []NodeDefinition) *referenceGraph {
	t.Helper()
	r := &referenceGraph{
		elements:   make(map[uint16]*Element),
		definition: make(map[uint16]Definition),
		keys:       make(map[uint16]uint32),
		reserved:   make(map[uint32]bool),
		dependents: make(map[uint16][]referenceEdge),
		linked:     make(map[uint16]bool),
	}
	for _, d := range definitions {
		element, err := New(d.Definition)
		if err != nil {
			t.Fatal(err)
		}
		r.ids = append(r.ids, d.ID)
		r.elements[d.ID] = element
		r.definition[d.ID] = d.Definition
		r.keys[d.ID] = d.SourceKey
		r.reserved[d.SourceKey] = true
	}
	return r
}

/*
================
referenceGraph.link

The first registration for a target wins; dependents stay in ascending
target-identity order, as the old graph kept them.
================
*/
func (r *referenceGraph) link(source, target uint16, channel Channel) {
	deps := r.dependents[source]
	for _, dep := range deps {
		if dep.target == target {
			return
		}
	}
	edge := referenceEdge{target: target, channel: channel, key: r.keys[target]}
	pos := len(deps)
	for i, dep := range deps {
		if dep.key > edge.key {
			pos = i
			break
		}
	}
	deps = append(deps, referenceEdge{})
	copy(deps[pos+1:], deps[pos:])
	deps[pos] = edge
	r.dependents[source] = deps
	r.linked[target] = true
}

/*
================
referenceGraph.snapshot

A value copy of every element, for the reference's atomic operations.
================
*/
func (r *referenceGraph) snapshot() map[uint16]*Element {
	out := make(map[uint16]*Element, len(r.elements))
	for id, element := range r.elements {
		copyOf := &Element{}
		element.cloneInto(copyOf)
		out[id] = copyOf
	}
	return out
}

/*
================
referenceGraph.check
================
*/
func (r *referenceGraph) check(id uint16, source uint32) error {
	if _, ok := r.elements[id]; !ok {
		return fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	if r.reserved[source] {
		return fmt.Errorf("paramkeeper: external source %d collides with parameter identity", source)
	}
	return nil
}

/*
================
referenceGraph.applyInternal
================
*/
func (r *referenceGraph) applyInternal(id uint16, channel Channel, source uint32, value float32) (bool, error) {
	changed, err := r.elements[id].Apply(channel, source, value)
	if err == nil && changed {
		err = r.propagate(id)
	}
	return changed, err
}

/*
================
referenceGraph.propagate
================
*/
func (r *referenceGraph) propagate(id uint16) error {
	v, err := r.elements[id].Value()
	if err != nil {
		return fmt.Errorf("paramkeeper: evaluating node %d: %w", id, err)
	}
	for _, dep := range r.dependents[id] {
		if _, err = r.applyInternal(dep.target, dep.channel, r.keys[id], v); err != nil {
			return err
		}
	}
	return nil
}

/*
================
referenceGraph.apply
================
*/
func (r *referenceGraph) apply(id uint16, channel Channel, source uint32, value float32) (bool, error) {
	if err := r.check(id, source); err != nil {
		return false, err
	}
	before := r.snapshot()
	changed, err := r.applyInternal(id, channel, source, value)
	if err != nil {
		r.elements = before
		return false, err
	}
	return changed, nil
}

/*
================
referenceGraph.remove
================
*/
func (r *referenceGraph) remove(id uint16, source uint32) (uint8, error) {
	if err := r.check(id, source); err != nil {
		return 0, err
	}
	before := r.snapshot()
	removed := r.elements[id].Remove(source)
	if removed == 0 {
		return 0, nil
	}
	if err := r.propagate(id); err != nil {
		r.elements = before
		return 0, err
	}
	return removed, nil
}

/*
================
referenceGraph.applyBatch
================
*/
func (r *referenceGraph) applyBatch(writes []Write) error {
	for _, w := range writes {
		if err := r.check(w.Parameter, w.Source); err != nil {
			return err
		}
	}
	before := r.snapshot()
	for _, w := range writes {
		if _, err := r.applyInternal(w.Parameter, w.Channel, w.Source, w.Value); err != nil {
			r.elements = before
			return err
		}
	}
	return nil
}

/*
================
flatProofDefinitions

Sparse IDs, identities deliberately out of ID order, and a mix of Ignore
sentinels (0 and -1), bases and clamps for the fuzz walk.
================
*/
func flatProofDefinitions() []NodeDefinition {
	return []NodeDefinition{
		{ID: 50, SourceKey: 90050, Definition: Definition{Minimum: -1000, Maximum: 1000, Base: 100, Ignore: -1}},
		{ID: 1, SourceKey: 90001, Definition: Definition{Minimum: -1000000, Maximum: 1000000}},
		{ID: 2, SourceKey: 90002, Definition: Definition{Minimum: -1000000, Maximum: 1000000, Ignore: -1}},
		{ID: 52, SourceKey: 90052, Definition: Definition{Minimum: -100000, Maximum: 100000}},
		{ID: 21, SourceKey: 90021, Definition: Definition{Minimum: -1000000, Maximum: 1000000}},
		{ID: 5, SourceKey: 90005, Definition: Definition{Minimum: 0, Maximum: 1000}},
		{ID: 7, SourceKey: 90007, Definition: Definition{Minimum: -500, Maximum: 500, Base: -25}},
		{ID: 80, SourceKey: 90080, Definition: Definition{Minimum: 0, Maximum: 100, Base: 40, Ignore: -1}},
		{ID: 300, SourceKey: 90300, Definition: Definition{Minimum: -1000000, Maximum: 1000000}},
	}
}

// flatProofLinks feed earlier parameters into later ones (a DAG), with two
// sources sharing targets 21 and 5 through different channels.
var flatProofLinks = []struct {
	source, target uint16
	channel        Channel
}{
	{50, 21, FactorProduct}, {1, 52, Flat}, {52, 21, Flat}, {21, 5, Flat},
	{2, 52, PercentSum}, {50, 7, PercentProduct}, {21, 80, FactorProduct},
	{5, 300, Flat}, {7, 300, PercentSum},
}

/*
================
flatProofValue

One fuzz value: sentinels, zeroes, ordinary amounts, and the shapes a
write can be refused or overflow for.
================
*/
func flatProofValue(rng *rand.Rand) float32 {
	switch rng.Intn(16) {
	case 0:
		return 0
	case 1:
		return -1
	case 2:
		return float32(math.Inf(1))
	case 3:
		return float32(math.NaN())
	case 4:
		return 3e38
	case 5:
		return -3e38
	}
	return float32(rng.Intn(2000)-500) / 8
}

/*
================
compareGraphs

Every defined node's value, bitwise, and every node's linked flag.
================
*/
func compareGraphs(t *testing.T, g *Graph, r *referenceGraph, step int) {
	t.Helper()
	for _, id := range r.ids {
		got, gotErr := g.Value(id)
		want, wantErr := r.elements[id].Value()
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("step %d param %d: graph err %v, reference err %v", step, id, gotErr, wantErr)
		}
		if gotErr == nil && math.Float32bits(got) != math.Float32bits(want) {
			t.Fatalf("step %d param %d: graph %v (%08x), reference %v (%08x)",
				step, id, got, math.Float32bits(got), want, math.Float32bits(want))
		}
		if g.Linked(id) != r.linked[id] {
			t.Fatalf("step %d param %d: graph linked %v, reference %v", step, id, g.Linked(id), r.linked[id])
		}
	}
}

/*
================
TestFlatGraphMatchesElementReference

The fuzz walk: applies, removals, atomic batches, clones and late links
in one deterministic sequence per seed, comparing after every step.
Write order varies across steps; buckets must still reduce in ascending
source order because both representations keep them sorted.
================
*/
func TestFlatGraphMatchesElementReference(t *testing.T) {
	definitions := flatProofDefinitions()
	sources := []uint32{0, 1, 7, 90, 1024, 2048, 4096, 0x80000000, 0xffffffff, 90005}
	ids := []uint16{50, 1, 2, 52, 21, 5, 7, 80, 300, 499}
	for seed := int64(1); seed <= 6; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g, err := NewGraph(definitions)
		if err != nil {
			t.Fatal(err)
		}
		reference := newReferenceGraph(t, definitions)
		for _, link := range flatProofLinks {
			if err := g.Link(link.source, link.target, link.channel); err != nil {
				t.Fatal(err)
			}
			reference.link(link.source, link.target, link.channel)
		}
		// First registration wins on both, whatever channel arrives second.
		if err := g.Link(1, 52, PercentProduct); err != nil {
			t.Fatal(err)
		}
		reference.link(1, 52, PercentProduct)
		compareGraphs(t, g, reference, -1)
		for step := 0; step < 400; step++ {
			switch rng.Intn(10) {
			case 0, 1, 2, 3, 4:
				id := ids[rng.Intn(len(ids))]
				channel := Channel(rng.Intn(5))
				source := sources[rng.Intn(len(sources))]
				value := flatProofValue(rng)
				gotChanged, gotErr := g.Apply(id, channel, source, value)
				wantChanged, wantErr := reference.apply(id, channel, source, value)
				if gotChanged != wantChanged || (gotErr == nil) != (wantErr == nil) {
					t.Fatalf("seed %d step %d Apply(%d,%d,%d,%v): graph (%v,%v), reference (%v,%v)",
						seed, step, id, channel, source, value, gotChanged, gotErr, wantChanged, wantErr)
				}
			case 5:
				id := ids[rng.Intn(len(ids))]
				source := sources[rng.Intn(len(sources))]
				gotRemoved, gotErr := g.Remove(id, source)
				wantRemoved, wantErr := reference.remove(id, source)
				if gotRemoved != wantRemoved || (gotErr == nil) != (wantErr == nil) {
					t.Fatalf("seed %d step %d Remove(%d,%d): graph (%d,%v), reference (%d,%v)",
						seed, step, id, source, gotRemoved, gotErr, wantRemoved, wantErr)
				}
			case 6:
				writes := make([]Write, rng.Intn(5))
				for i := range writes {
					writes[i] = Write{
						Parameter: ids[rng.Intn(len(ids))],
						Channel:   Channel(rng.Intn(5)),
						Source:    sources[rng.Intn(len(sources))],
						Value:     flatProofValue(rng),
					}
				}
				gotErr := g.ApplyBatch(writes)
				wantErr := reference.applyBatch(writes)
				if (gotErr == nil) != (wantErr == nil) {
					t.Fatalf("seed %d step %d ApplyBatch(%+v): graph %v, reference %v",
						seed, step, writes, gotErr, wantErr)
				}
			case 7:
				g = g.Clone()
				snapshot := reference.snapshot()
				reference.elements = snapshot
			case 8:
				// A link registered after clones exist: neither graph is
				// frozen, and both take the same first registration.
				if err := g.Link(2, 21, PercentSum); err != nil {
					t.Fatal(err)
				}
				reference.link(2, 21, PercentSum)
			}
			compareGraphs(t, g, reference, step)
		}
	}
}

/*
================
TestFlatGraphReduceUsesAscendingSourceOrder

Inserted in any order, a bucket reduces in ascending source order: at
2^24 the +1 is absorbed before the -2^24 lands, whatever arrived first.
================
*/
func TestFlatGraphReduceUsesAscendingSourceOrder(t *testing.T) {
	values := map[uint32]float32{1: 16777216, 2: 1, 0xffffffff: -16777216}
	for _, order := range [][]uint32{{0xffffffff, 1, 2}, {2, 1, 0xffffffff}, {1, 2, 0xffffffff}} {
		g, err := NewGraph([]NodeDefinition{
			{ID: 9, SourceKey: 42, Definition: Definition{Minimum: -1000000000, Maximum: 1000000000, Ignore: -1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range order {
			if _, err := g.Apply(9, Flat, source, values[source]); err != nil {
				t.Fatal(err)
			}
		}
		got, err := g.Value(9)
		if err != nil || math.Float32bits(got) != 0 {
			t.Fatalf("order %v: value %v (%08x), err %v", order, got, math.Float32bits(got), err)
		}
	}
}

/*
================
TestFlatGraphCloneLinksDoNotLeak

A clone is not frozen and not coupled: a Link on the clone feeds only the
clone, and a Link on the original afterwards feeds only the original.
================
*/
func TestFlatGraphCloneLinksDoNotLeak(t *testing.T) {
	g := testGraph(t)
	clone := g.Clone()
	if err := clone.Link(50, 5, Flat); err != nil {
		t.Fatal(err)
	}
	graphApply(t, clone, 50, Flat, 0, 19)
	graphValue(t, clone, 5, 19)
	graphValue(t, g, 5, 0)
	if err := g.Link(50, 5, Flat); err != nil {
		t.Fatal(err)
	}
	graphApply(t, g, 50, Flat, 0, 30)
	graphValue(t, g, 5, 30)
	graphValue(t, clone, 5, 19)
}

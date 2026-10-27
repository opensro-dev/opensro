/*
===========================================================================

graph.go - dependency propagation over parameter elements (4B3060 / 4B3130)

Detached, bounded parameter projection with synchronous propagation in native
source-identity order. Parameter IDs index the topology directly; bucket
entries live in one slice-backed arena and dependents in one flat edge slice,
so building, cloning and applying a graph copies slices rather than walking
a map per node and channel.
===========================================================================
*/

package paramkeeper

import (
	"fmt"
	"slices"
	"sort"
)

const maxGraphNodeID = 512

const bucketAllocated uint32 = 1 << 31

// NodeDefinition gives each parameter a distinct source identity for dependency
// propagation. In native this identity is the CParam pointer, not its numeric
// parameter ID. External modifiers must not reuse these reserved identities.
type NodeDefinition struct {
	ID        uint16
	SourceKey uint32
	Definition
}

/*
================
dependentEdge

One Link: the target node's index, the channel the source's value lands in,
and the target's source identity, which orders a source's dependents.
================
*/
type dependentEdge struct {
	target  int16
	channel Channel
	key     uint32
}

/*
================
graphTopology

The immutable shape shared by clones: definitions and identities by node
index, a fixed index from parameter ID to node, the reserved identities in
sorted order for collision checks, and every dependent edge in one slice
grouped by source (depStarts holds each source's range). Link copies the
topology before writing when clones share it, so no graph is frozen.
================
*/
type graphTopology struct {
	indexByID   [maxGraphNodeID]int16
	ids         []uint16
	definitions []Definition
	keys        []uint32
	reserved    []uint32
	dependents  []dependentEdge
	depStarts   []int32
	linked      []bool
	shared      bool
}

/*
================
bucketState

One parameter/channel bucket: the head entry in the graph's arena and the
entry count, with bucketAllocated marking a bucket a write has allocated
even after its last entry is removed.
================
*/
type bucketState struct {
	head  int32
	count uint32
}

/*
================
arenaEntry

One source contribution in the graph's entry arena. Entries in a bucket
chain in ascending source order through next; -1 ends the bucket.
================
*/
type arenaEntry struct {
	source uint32
	value  float32
	next   int32
}

// Graph is a detached, bounded parameter projection. It has no effect timers,
// persistence, live character pointers or keeper-specific 23/24 callbacks.
// Successful operations propagate synchronously as in 4B3130; invalid inputs
// and arithmetic errors leave the entire projection unchanged.
type Graph struct {
	topology *graphTopology
	buckets  []bucketState
	entries  []arenaEntry
	dirty    bool
}

// Write is a source contribution to a single parameter/channel.
type Write struct {
	Parameter uint16
	Channel   Channel
	Source    uint32
	Value     float32
}

/*
================
Graph.ApplyBatch

ApplyBatch commits a complete projection update atomically. It copies the
flat bucket state once for the whole batch, rather than once per parameter
in a character snapshot, and swaps it in only when every write applies.
================
*/
func (g *Graph) ApplyBatch(writes []Write) error {
	for _, w := range writes {
		if err := g.checkExternalSource(w.Parameter, w.Source); err != nil {
			return err
		}
	}
	next := g.cloneState(len(writes) + len(g.topology.dependents))
	for _, w := range writes {
		if _, err := next.apply(next.nodeIndex(w.Parameter), w.Channel, w.Source, w.Value); err != nil {
			return err
		}
	}
	g.buckets = next.buckets
	g.entries = next.entries
	g.dirty = true
	return nil
}

/*
================
Graph.ApplyBatchInPlace

ApplyBatchInPlace is ApplyBatch for a graph the caller owns outright
and discards on error (a fresh Clone being built up): the same source
validation, the same writes in the same order, the same errors, but
applied onto the receiver instead of onto a swapped-in copy of its
state. On error the receiver is left partially applied and must be
dropped, which the owning caller already does.
================
*/
func (g *Graph) ApplyBatchInPlace(writes []Write) error {
	for _, w := range writes {
		if err := g.checkExternalSource(w.Parameter, w.Source); err != nil {
			return err
		}
	}
	// The same entry headroom ApplyBatch's copy reserves, so applying
	// does not grow the arena in steps.
	g.entries = slices.Grow(g.entries, len(writes)+len(g.topology.dependents))
	for _, w := range writes {
		if _, err := g.apply(g.nodeIndex(w.Parameter), w.Channel, w.Source, w.Value); err != nil {
			return err
		}
	}
	g.dirty = true
	return nil
}

/*
================
NewGraph
================
*/
func NewGraph(definitions []NodeDefinition) (*Graph, error) {
	topo := &graphTopology{
		ids:         make([]uint16, len(definitions)),
		definitions: make([]Definition, len(definitions)),
		keys:        make([]uint32, len(definitions)),
		depStarts:   make([]int32, len(definitions)+1),
		linked:      make([]bool, len(definitions)),
	}
	for i := range topo.indexByID {
		topo.indexByID[i] = -1
	}
	buckets := make([]bucketState, len(definitions)*int(channelCount))
	for i := range buckets {
		buckets[i].head = -1
	}
	for i, d := range definitions {
		if d.ID >= maxGraphNodeID || d.SourceKey == 0 {
			return nil, fmt.Errorf("paramkeeper: invalid node %d identity %d", d.ID, d.SourceKey)
		}
		if topo.indexByID[d.ID] >= 0 {
			return nil, fmt.Errorf("paramkeeper: duplicate node %d", d.ID)
		}
		for prior := 0; prior < i; prior++ {
			if topo.keys[prior] == d.SourceKey {
				return nil, fmt.Errorf("paramkeeper: duplicate node identity %d", d.SourceKey)
			}
		}
		if !validDefinition(d.Definition) {
			return nil, fmt.Errorf("paramkeeper: node %d: invalid definition", d.ID)
		}
		topo.indexByID[d.ID] = int16(i)
		topo.ids[i] = d.ID
		topo.keys[i] = d.SourceKey
		topo.definitions[i] = d.Definition
	}
	topo.reserved = make([]uint32, len(topo.keys))
	copy(topo.reserved, topo.keys)
	sort.Slice(topo.reserved, func(i, j int) bool { return topo.reserved[i] < topo.reserved[j] })
	return &Graph{topology: topo, buckets: buckets}, nil
}

/*
================
Graph.Link

Link registers a target without publishing a value. 4B3060 keys its map by
the target CParam pointer and preserves the first registration, including
its channel. 4B3130 subsequently writes the source's value under its pointer
identity into that target/channel. Invalid cyclic graphs are rejected here.
================
*/
func (g *Graph) Link(source, target uint16, channel Channel) error {
	if source >= maxGraphNodeID || target >= maxGraphNodeID || channel >= channelCount {
		return fmt.Errorf("paramkeeper: invalid dependency %d -> %d channel %d", source, target, channel)
	}
	sIdx := g.nodeIndex(source)
	tIdx := g.nodeIndex(target)
	if sIdx < 0 || tIdx < 0 {
		return fmt.Errorf("paramkeeper: invalid dependency %d -> %d channel %d", source, target, channel)
	}
	for i := g.topology.depStarts[sIdx]; i < g.topology.depStarts[sIdx+1]; i++ {
		if int(g.topology.dependents[i].target) == tIdx {
			return nil
		}
	}
	var seen [maxGraphNodeID]bool
	var pendingBuf [64]int
	pending := append(pendingBuf[:0], tIdx)
	for len(pending) != 0 {
		idx := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if idx == sIdx {
			return fmt.Errorf("paramkeeper: dependency cycle %d -> %d", source, target)
		}
		if !seen[idx] {
			seen[idx] = true
			for i := g.topology.depStarts[idx]; i < g.topology.depStarts[idx+1]; i++ {
				pending = append(pending, int(g.topology.dependents[i].target))
			}
		}
	}
	g.ensureTopologyWritable()
	edge := dependentEdge{target: int16(tIdx), channel: channel, key: g.topology.keys[tIdx]}
	start, end := g.topology.depStarts[sIdx], g.topology.depStarts[sIdx+1]
	pos := end
	for i := start; i < end; i++ {
		if g.topology.dependents[i].key > edge.key {
			pos = i
			break
		}
	}
	deps := append(g.topology.dependents, dependentEdge{})
	copy(deps[pos+1:], deps[pos:])
	deps[pos] = edge
	g.topology.dependents = deps
	for i := sIdx + 1; i < len(g.topology.depStarts); i++ {
		g.topology.depStarts[i]++
	}
	g.topology.linked[tIdx] = true
	return nil
}

/*
================
Graph.Linked

Linked reports whether any parameter feeds id through a Link.
================
*/
func (g *Graph) Linked(id uint16) bool {
	idx := g.nodeIndex(id)
	return idx >= 0 && g.topology.linked[idx]
}

/*
================
Graph.Value
================
*/
func (g *Graph) Value(id uint16) (float32, error) {
	idx := g.nodeIndex(id)
	if idx < 0 {
		return 0, fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	return g.valueAt(idx)
}

/*
================
Graph.Apply
================
*/
func (g *Graph) Apply(id uint16, channel Channel, source uint32, value float32) (bool, error) {
	if err := g.checkExternalSource(id, source); err != nil {
		return false, err
	}
	idx := g.nodeIndex(id)
	if channel >= channelCount || !finite(value) {
		return false, fmt.Errorf("paramkeeper: invalid channel %d or non-finite modifier", channel)
	}
	if value == g.topology.definitions[idx].Ignore {
		return false, nil
	}
	next := g.cloneState(1)
	changed, err := next.apply(idx, channel, source, value)
	if err == nil && changed {
		g.buckets = next.buckets
		g.entries = next.entries
		g.dirty = true
	}
	return changed && err == nil, err
}

/*
================
Graph.Remove
================
*/
func (g *Graph) Remove(id uint16, source uint32) (uint8, error) {
	if err := g.checkExternalSource(id, source); err != nil {
		return 0, err
	}
	idx := g.nodeIndex(id)
	next := g.cloneState(0)
	removed := next.removeSource(idx, source)
	if removed == 0 {
		return 0, nil
	}
	if err := next.propagate(idx); err != nil {
		return 0, err
	}
	g.buckets = next.buckets
	g.entries = next.entries
	g.dirty = true
	return removed, nil
}

/*
================
Graph.Clone

Clone returns an independent value copy sharing the immutable node and link
topology. The bucket state is compacted into fresh slices; a later Link on
either graph copies the shared topology before writing it.
================
*/
func (g *Graph) Clone() *Graph {
	cloned := g.cloneState(0)
	g.topology.shared = true
	return cloned
}

/*
================
Graph.checkExternalSource
================
*/
func (g *Graph) checkExternalSource(id uint16, source uint32) error {
	if g.nodeIndex(id) < 0 {
		return fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	reserved := g.topology.reserved
	pos := sort.Search(len(reserved), func(i int) bool { return reserved[i] >= source })
	if pos < len(reserved) && reserved[pos] == source {
		return fmt.Errorf("paramkeeper: external source %d collides with parameter identity", source)
	}
	return nil
}

/*
================
Graph.nodeIndex
================
*/
func (g *Graph) nodeIndex(id uint16) int {
	if id >= maxGraphNodeID {
		return -1
	}
	return int(g.topology.indexByID[id])
}

/*
================
Graph.apply
================
*/
func (g *Graph) apply(idx int, channel Channel, source uint32, value float32) (bool, error) {
	changed, err := g.applyEntry(idx, channel, source, value)
	if err == nil && changed {
		err = g.propagate(idx)
	}
	return changed, err
}

/*
================
Graph.propagate
================
*/
func (g *Graph) propagate(idx int) error {
	v, err := g.valueAt(idx)
	if err != nil {
		return fmt.Errorf("paramkeeper: evaluating node %d: %w", g.topology.ids[idx], err)
	}
	for i := g.topology.depStarts[idx]; i < g.topology.depStarts[idx+1]; i++ {
		dep := g.topology.dependents[i]
		if _, err = g.apply(int(dep.target), dep.channel, g.topology.keys[idx], v); err != nil {
			return err
		}
	}
	return nil
}

/*
================
Graph.valueAt

The value of one node, following Element.Value's store boundaries: an
unallocated node reads its fallback base unclamped; otherwise each
allocated bucket reduces in ascending source order and the channels
compose through float64 intermediates stored back to float32.
================
*/
func (g *Graph) valueAt(idx int) (float32, error) {
	definition := g.topology.definitions[idx]
	values := [channelCount]float32{definition.Base, 0, 0, 100}
	allocated := false
	for channel := Channel(0); channel < channelCount; channel++ {
		bucket := g.buckets[idx*int(channelCount)+int(channel)]
		if bucket.count&bucketAllocated == 0 {
			continue
		}
		allocated = true
		values[channel] = g.reduceBucket(channel, bucket)
		if !finite(values[channel]) {
			return 0, fmt.Errorf("paramkeeper: channel %d overflow", channel)
		}
	}
	if !allocated {
		return definition.Base, nil
	}
	value := float32((float64(values[PercentSum]) + 100) * float64(values[Flat]) / 100)
	if values[PercentProduct] != 0 {
		if value == 0 {
			value = values[PercentProduct]
		} else {
			value = float32(float64(value) * float64(values[PercentProduct]))
		}
	}
	value = float32((float64(values[FactorProduct]) / 100) * float64(value))
	if !finite(value) {
		return 0, fmt.Errorf("paramkeeper: result overflow")
	}
	if value < definition.Minimum {
		value = definition.Minimum
	} else if value > definition.Maximum {
		value = definition.Maximum
	}
	return value, nil
}

/*
================
Graph.applyEntry

Insert or replace one source in one bucket, keeping entries in ascending
source order. The Ignore sentinel is an insertion no-op: it neither
allocates the bucket nor erases an older entry.
================
*/
func (g *Graph) applyEntry(idx int, channel Channel, source uint32, value float32) (bool, error) {
	if channel >= channelCount || !finite(value) {
		return false, fmt.Errorf("paramkeeper: invalid channel %d or non-finite modifier", channel)
	}
	if value == g.topology.definitions[idx].Ignore {
		return false, nil
	}
	b := idx*int(channelCount) + int(channel)
	count := g.buckets[b].count &^ bucketAllocated
	g.buckets[b].count = bucketAllocated | count
	var prev int32 = -1
	at := g.buckets[b].head
	for at >= 0 {
		entry := g.entries[at]
		if entry.source == source {
			g.entries[at].value = value
			return true, nil
		}
		if entry.source > source {
			break
		}
		prev = at
		at = entry.next
	}
	inserted := int32(len(g.entries))
	g.entries = append(g.entries, arenaEntry{source: source, value: value, next: at})
	if prev < 0 {
		g.buckets[b].head = inserted
	} else {
		g.entries[prev].next = inserted
	}
	g.buckets[b].count = bucketAllocated | (count + 1)
	return true, nil
}

/*
================
Graph.removeSource

Remove one source from every bucket of a node. The return is the native OR
of channel+1 over the buckets that held it; allocated buckets stay
allocated once emptied.
================
*/
func (g *Graph) removeSource(idx int, source uint32) uint8 {
	var removed uint8
	for channel := Channel(0); channel < channelCount; channel++ {
		b := idx*int(channelCount) + int(channel)
		var prev int32 = -1
		at := g.buckets[b].head
		for at >= 0 {
			entry := g.entries[at]
			if entry.source == source {
				if prev < 0 {
					g.buckets[b].head = entry.next
				} else {
					g.entries[prev].next = entry.next
				}
				count := g.buckets[b].count &^ bucketAllocated
				g.buckets[b].count = bucketAllocated | (count - 1)
				removed |= uint8(channel + 1)
				break
			}
			if entry.source > source {
				break
			}
			prev = at
			at = entry.next
		}
	}
	return removed
}

/*
================
Graph.reduceBucket

One bucket's reduction in entry (ascending source) order, with the same
float32 stores as Element's reduce.
================
*/
func (g *Graph) reduceBucket(channel Channel, bucket bucketState) float32 {
	var result float32
	if channel == FactorProduct {
		result = 100
	}
	for at := bucket.head; at >= 0; at = g.entries[at].next {
		value := float64(g.entries[at].value)
		switch channel {
		case Flat, PercentSum:
			result = float32(float64(result) + value)
		case PercentProduct:
			factor := 1 + value/100
			if result != 0 {
				factor *= float64(result)
			}
			result = float32(factor)
		case FactorProduct:
			result = float32(value * float64(result) / 100)
		}
	}
	return result
}

/*
================
Graph.cloneState

A working copy of the value state: fresh bucket metadata and the live
entries compacted into one fresh arena in bucket order, with room for
extra entries a transaction is about to add.
================
*/
func (g *Graph) cloneState(extra int) *Graph {
	buckets := make([]bucketState, len(g.buckets))
	copy(buckets, g.buckets)
	live := 0
	for _, bucket := range g.buckets {
		live += int(bucket.count &^ bucketAllocated)
	}
	entries := make([]arenaEntry, 0, live+extra)
	for i := range buckets {
		count := int(buckets[i].count &^ bucketAllocated)
		if count == 0 {
			buckets[i].head = -1
			continue
		}
		at := g.buckets[i].head
		buckets[i].head = int32(len(entries))
		var prev int32 = -1
		for n := 0; n < count; n++ {
			entry := g.entries[at]
			placed := int32(len(entries))
			entries = append(entries, arenaEntry{source: entry.source, value: entry.value, next: -1})
			if prev >= 0 {
				entries[prev].next = placed
			}
			prev = placed
			at = entry.next
		}
	}
	return &Graph{
		topology: g.topology,
		buckets:  buckets,
		entries:  entries,
		dirty:    g.dirty,
	}
}

/*
================
Graph.ensureTopologyWritable

Link writes the topology; while clones share one, the writer first takes
its own copy so the other graphs keep the topology they cloned with.
================
*/
func (g *Graph) ensureTopologyWritable() {
	if !g.topology.shared {
		return
	}
	topo := *g.topology
	topo.ids = append([]uint16(nil), g.topology.ids...)
	topo.definitions = append([]Definition(nil), g.topology.definitions...)
	topo.keys = append([]uint32(nil), g.topology.keys...)
	topo.reserved = append([]uint32(nil), g.topology.reserved...)
	topo.dependents = append([]dependentEdge(nil), g.topology.dependents...)
	topo.depStarts = append([]int32(nil), g.topology.depStarts...)
	topo.linked = append([]bool(nil), g.topology.linked...)
	topo.shared = false
	g.topology = &topo
}

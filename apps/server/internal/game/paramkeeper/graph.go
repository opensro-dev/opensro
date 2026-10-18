/*
===========================================================================

graph.go - dependency propagation over parameter elements (4B3060 / 4B3130)

Detached, bounded parameter projection with synchronous propagation in native
source-identity order.

===========================================================================
*/

package paramkeeper

import "fmt"

const maxGraphNodeID = 512

// NodeDefinition gives each parameter a distinct source identity for dependency
// propagation. In native this identity is the CParam pointer, not its numeric
// parameter ID. External modifiers must not reuse these reserved identities.
type NodeDefinition struct {
	ID        uint16
	SourceKey uint32
	Definition
}

type dependentEdge struct {
	target  uint16
	channel Channel
	key     uint32
}

type graphTopology struct {
	indexByID  [maxGraphNodeID]int16
	keys       map[uint32]uint16
	dependents [][]dependentEdge
}

type node struct {
	id      uint16
	key     uint32
	element Element
}

// Graph is a detached, bounded parameter projection. It has no effect timers,
// persistence, live character pointers or keeper-specific 23/24 callbacks.
// Successful operations propagate synchronously as in 4B3130; invalid inputs
// and arithmetic errors leave the entire projection unchanged.
type Graph struct {
	topology *graphTopology
	nodes    []node
	pristine *Graph
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

ApplyBatch commits a complete projection update atomically. It clones once
for the whole batch, rather than once per parameter in a character snapshot.
When g is a pristine clone that has not yet been mutated, it applies in-place
and only restores from pristine on error.
================
*/
func (g *Graph) ApplyBatch(writes []Write) error {
	for _, w := range writes {
		if err := g.checkExternalSource(w.Parameter, w.Source); err != nil {
			return err
		}
	}
	if !g.dirty && g.pristine != nil {
		g.dirty = true
		for _, w := range writes {
			if _, err := g.apply(w.Parameter, w.Channel, w.Source, w.Value); err != nil {
				restored := g.pristine.clone()
				g.nodes = restored.nodes
				g.dirty = false
				return err
			}
		}
		return nil
	}
	next := g.clone()
	for _, w := range writes {
		if _, err := next.apply(w.Parameter, w.Channel, w.Source, w.Value); err != nil {
			return err
		}
	}
	g.nodes = next.nodes
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
		keys:       make(map[uint32]uint16, len(definitions)),
		dependents: make([][]dependentEdge, len(definitions)),
	}
	for i := range topo.indexByID {
		topo.indexByID[i] = -1
	}
	nodes := make([]node, len(definitions))
	for i, d := range definitions {
		if d.ID >= maxGraphNodeID || d.SourceKey == 0 {
			return nil, fmt.Errorf("paramkeeper: invalid node %d identity %d", d.ID, d.SourceKey)
		}
		if topo.indexByID[d.ID] >= 0 {
			return nil, fmt.Errorf("paramkeeper: duplicate node %d", d.ID)
		}
		if _, exists := topo.keys[d.SourceKey]; exists {
			return nil, fmt.Errorf("paramkeeper: duplicate node identity %d", d.SourceKey)
		}
		if !validDefinition(d.Definition) {
			return nil, fmt.Errorf("paramkeeper: node %d: invalid definition", d.ID)
		}
		topo.indexByID[d.ID] = int16(i)
		topo.keys[d.SourceKey] = d.ID
		nodes[i] = node{
			id:      d.ID,
			key:     d.SourceKey,
			element: Element{definition: d.Definition},
		}
	}
	return &Graph{topology: topo, nodes: nodes}, nil
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
	sIdx := g.topology.indexByID[source]
	tIdx := g.topology.indexByID[target]
	if sIdx < 0 || tIdx < 0 {
		return fmt.Errorf("paramkeeper: invalid dependency %d -> %d channel %d", source, target, channel)
	}
	deps := g.topology.dependents[sIdx]
	for _, dep := range deps {
		if dep.target == target {
			return nil
		}
	}
	var seen [maxGraphNodeID]bool
	var pendingBuf [64]uint16
	pending := append(pendingBuf[:0], target)
	for len(pending) != 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if id == source {
			return fmt.Errorf("paramkeeper: dependency cycle %d -> %d", source, target)
		}
		if !seen[id] {
			seen[id] = true
			idx := g.topology.indexByID[id]
			for _, next := range g.topology.dependents[idx] {
				pending = append(pending, next.target)
			}
		}
	}
	targetKey := g.nodes[tIdx].key
	edge := dependentEdge{target: target, channel: channel, key: targetKey}
	inserted := false
	for i := range deps {
		if deps[i].key > targetKey {
			deps = append(deps, dependentEdge{})
			copy(deps[i+1:], deps[i:])
			deps[i] = edge
			inserted = true
			break
		}
	}
	if !inserted {
		deps = append(deps, edge)
	}
	g.topology.dependents[sIdx] = deps
	return nil
}

/*
================
Graph.Linked

Linked reports whether any parameter feeds id through a Link.
================
*/
func (g *Graph) Linked(id uint16) bool {
	for _, deps := range g.topology.dependents {
		for _, dep := range deps {
			if dep.target == id {
				return true
			}
		}
	}
	return false
}

/*
================
Graph.Value
================
*/
func (g *Graph) Value(id uint16) (float32, error) {
	if id >= maxGraphNodeID {
		return 0, fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	idx := g.topology.indexByID[id]
	if idx < 0 {
		return 0, fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	return g.nodes[idx].element.Value()
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
	next := g.clone()
	changed, err := next.apply(id, channel, source, value)
	if err == nil && changed {
		g.nodes = next.nodes
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
	next := g.clone()
	idx := next.topology.indexByID[id]
	removed := next.nodes[idx].element.Remove(source)
	if removed == 0 {
		return 0, nil
	}
	if err := next.propagate(id); err != nil {
		return 0, err
	}
	g.nodes = next.nodes
	g.dirty = true
	return removed, nil
}

/*
================
Graph.Clone

Clone returns an independent value copy sharing the immutable node and link
topology.
================
*/
func (g *Graph) Clone() *Graph {
	cloned := g.clone()
	if !g.dirty {
		cloned.pristine = g
	}
	return cloned
}

/*
================
Graph.checkExternalSource
================
*/
func (g *Graph) checkExternalSource(id uint16, source uint32) error {
	if id >= maxGraphNodeID || g.topology.indexByID[id] < 0 {
		return fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	if _, reserved := g.topology.keys[source]; reserved {
		return fmt.Errorf("paramkeeper: external source %d collides with parameter identity", source)
	}
	return nil
}

/*
================
Graph.apply
================
*/
func (g *Graph) apply(id uint16, channel Channel, source uint32, value float32) (bool, error) {
	idx := g.topology.indexByID[id]
	changed, err := g.nodes[idx].element.Apply(channel, source, value)
	if err == nil && changed {
		err = g.propagate(id)
	}
	return changed, err
}

/*
================
Graph.propagate
================
*/
func (g *Graph) propagate(id uint16) error {
	idx := g.topology.indexByID[id]
	n := &g.nodes[idx]
	v, err := n.element.Value()
	if err != nil {
		return fmt.Errorf("paramkeeper: evaluating node %d: %w", id, err)
	}
	for _, dep := range g.topology.dependents[idx] {
		if _, err = g.apply(dep.target, dep.channel, n.key, v); err != nil {
			return err
		}
	}
	return nil
}

/*
================
Graph.clone
================
*/
func (g *Graph) clone() *Graph {
	nodes := make([]node, len(g.nodes))
	for i := range g.nodes {
		nodes[i].id = g.nodes[i].id
		nodes[i].key = g.nodes[i].key
		g.nodes[i].element.cloneInto(&nodes[i].element)
	}
	return &Graph{
		topology: g.topology,
		nodes:    nodes,
		dirty:    g.dirty,
	}
}

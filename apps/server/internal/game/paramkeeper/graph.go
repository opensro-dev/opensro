package paramkeeper

import (
	"fmt"
	"sort"
)

// NodeDefinition gives each parameter a distinct source identity for dependency
// propagation. In native this identity is the CParam pointer, not its numeric
// parameter ID. External modifiers must not reuse these reserved identities.
type NodeDefinition struct {
	ID        uint16
	SourceKey uint32
	Definition
}

type node struct {
	key        uint32
	element    *Element
	dependents map[uint16]Channel
}

// Graph is a detached, bounded parameter projection. It has no effect timers,
// persistence, live character pointers or keeper-specific 23/24 callbacks.
// Successful operations propagate synchronously as in 4B3130; invalid inputs
// and arithmetic errors leave the entire projection unchanged.
type Graph struct {
	nodes map[uint16]*node
	keys  map[uint32]uint16
}

// Write is a source contribution to a single parameter/channel.
type Write struct {
	Parameter uint16
	Channel   Channel
	Source    uint32
	Value     float32
}

// ApplyBatch commits a complete projection update atomically. It clones once
// for the whole batch, rather than once per parameter in a character snapshot.
func (g *Graph) ApplyBatch(writes []Write) error {
	for _, w := range writes {
		if err := g.checkExternalSource(w.Parameter, w.Source); err != nil {
			return err
		}
	}
	next := g.clone()
	for _, w := range writes {
		if _, err := next.apply(w.Parameter, w.Channel, w.Source, w.Value); err != nil {
			return err
		}
	}
	g.nodes = next.nodes
	return nil
}

func NewGraph(definitions []NodeDefinition) (*Graph, error) {
	g := &Graph{nodes: make(map[uint16]*node), keys: make(map[uint32]uint16)}
	for _, d := range definitions {
		if d.ID >= 512 || d.SourceKey == 0 {
			return nil, fmt.Errorf("paramkeeper: invalid node %d identity %d", d.ID, d.SourceKey)
		}
		if _, exists := g.nodes[d.ID]; exists {
			return nil, fmt.Errorf("paramkeeper: duplicate node %d", d.ID)
		}
		if _, exists := g.keys[d.SourceKey]; exists {
			return nil, fmt.Errorf("paramkeeper: duplicate node identity %d", d.SourceKey)
		}
		e, err := New(d.Definition)
		if err != nil {
			return nil, fmt.Errorf("paramkeeper: node %d: %w", d.ID, err)
		}
		g.nodes[d.ID] = &node{key: d.SourceKey, element: e, dependents: make(map[uint16]Channel)}
		g.keys[d.SourceKey] = d.ID
	}
	return g, nil
}

// Link registers a target without publishing a value. 4B3060 keys its map by
// the target CParam pointer and preserves the first registration, including
// its channel. 4B3130 subsequently writes the source's value under its pointer
// identity into that target/channel. Invalid cyclic graphs are rejected here.
func (g *Graph) Link(source, target uint16, channel Channel) error {
	s, sourceOK := g.nodes[source]
	_, targetOK := g.nodes[target]
	if !sourceOK || !targetOK || channel >= channelCount {
		return fmt.Errorf("paramkeeper: invalid dependency %d -> %d channel %d", source, target, channel)
	}
	if _, exists := s.dependents[target]; exists {
		return nil
	}
	seen := make(map[uint16]bool)
	pending := []uint16{target}
	for len(pending) != 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if id == source {
			return fmt.Errorf("paramkeeper: dependency cycle %d -> %d", source, target)
		}
		if !seen[id] {
			seen[id] = true
			for next := range g.nodes[id].dependents {
				pending = append(pending, next)
			}
		}
	}
	s.dependents[target] = channel
	return nil
}

// Linked reports whether any parameter feeds id through a Link.
func (g *Graph) Linked(id uint16) bool {
	for _, n := range g.nodes {
		if _, ok := n.dependents[id]; ok {
			return true
		}
	}
	return false
}

func (g *Graph) Value(id uint16) (float32, error) {
	n, ok := g.nodes[id]
	if !ok {
		return 0, fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	return n.element.Value()
}

func (g *Graph) Apply(id uint16, channel Channel, source uint32, value float32) (bool, error) {
	if err := g.checkExternalSource(id, source); err != nil {
		return false, err
	}
	next := g.clone()
	changed, err := next.apply(id, channel, source, value)
	if err == nil && changed {
		g.nodes = next.nodes
	}
	return changed && err == nil, err
}

func (g *Graph) Remove(id uint16, source uint32) (uint8, error) {
	if err := g.checkExternalSource(id, source); err != nil {
		return 0, err
	}
	next := g.clone()
	removed := next.nodes[id].element.Remove(source)
	if removed == 0 {
		return 0, nil
	}
	if err := next.propagate(id); err != nil {
		return 0, err
	}
	g.nodes = next.nodes
	return removed, nil
}

func (g *Graph) checkExternalSource(id uint16, source uint32) error {
	if _, ok := g.nodes[id]; !ok {
		return fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	if _, reserved := g.keys[source]; reserved {
		return fmt.Errorf("paramkeeper: external source %d collides with parameter identity", source)
	}
	return nil
}

func (g *Graph) apply(id uint16, channel Channel, source uint32, value float32) (bool, error) {
	changed, err := g.nodes[id].element.Apply(channel, source, value)
	if err == nil && changed {
		err = g.propagate(id)
	}
	return changed, err
}

func (g *Graph) propagate(id uint16) error {
	n := g.nodes[id]
	v, err := n.element.Value()
	if err != nil {
		return fmt.Errorf("paramkeeper: evaluating node %d: %w", id, err)
	}
	order := make([]uint16, 0, len(n.dependents))
	for target := range n.dependents {
		order = append(order, target)
	}
	sort.Slice(order, func(i, j int) bool { return g.nodes[order[i]].key < g.nodes[order[j]].key })
	for _, target := range order {
		if _, err = g.apply(target, n.dependents[target], n.key, v); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph) clone() *Graph {
	copy := &Graph{nodes: make(map[uint16]*node, len(g.nodes)), keys: g.keys}
	for id, n := range g.nodes {
		e := &Element{definition: n.element.definition}
		for channel, bucket := range n.element.buckets {
			if bucket != nil {
				e.buckets[channel] = make(map[uint32]float32, len(bucket))
				for source, v := range bucket {
					e.buckets[channel][source] = v
				}
			}
		}
		copy.nodes[id] = &node{key: n.key, element: e, dependents: n.dependents}
	}
	return copy
}

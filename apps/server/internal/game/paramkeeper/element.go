/*
===========================================================================

element.go - single parameter modifier evaluation (the native parameter element)

Package paramkeeper evaluates the shared native parameter modifier algebra.
Elements are local to a stat projection; they are not a second live effect or
character authority. Effect admission, lifetime and persistence stay with
their existing owners.

===========================================================================
*/

package paramkeeper

import (
	"fmt"
	"math"
)

// Channel is the bucket index used by the native element's writes and lookups. In particular,
// Flat replaces the fallback base with the sum of its entries; it is not an
// increment to that base. PercentProduct accepts percentage deltas, whereas
// FactorProduct accepts factors expressed with 100 as the identity.
type Channel uint8

const (
	Flat Channel = iota
	PercentSum
	PercentProduct
	FactorProduct
	channelCount
)

const inlineEntryCap = 2

// Definition corresponds to the native element's +20/+24/+28/+2C initialization.
// Ignore is an insertion no-op sentinel, not a request to erase an older value.
type Definition struct {
	Minimum, Maximum, Base, Ignore float32
}

type bucketEntry struct {
	source uint32
	value  float32
}

// Element must not be copied by assignment after use; use cloneInto. Source
// keys are opaque uint32 identities supplied by the projection owner, ordered
// unsigned as the native source compare does. Native uses source pointers: assigning port
// identities and reconstructing native source order are responsibilities of
// the stat graph owner.
type Element struct {
	definition Definition
	entries    []bucketEntry
	inline     [inlineEntryCap]bucketEntry
	counts     [channelCount]uint8
	allocated  uint8
}

/*
================
New
================
*/
func New(definition Definition) (*Element, error) {
	if !validDefinition(definition) {
		return nil, fmt.Errorf("paramkeeper: invalid definition")
	}
	return &Element{definition: definition}, nil
}

/*
================
validDefinition
================
*/
func validDefinition(definition Definition) bool {
	if !finite(definition.Minimum) || !finite(definition.Maximum) || !finite(definition.Base) || !finite(definition.Ignore) {
		return false
	}
	return definition.Minimum <= definition.Maximum
}

/*
================
Element.channelRange
================
*/
func (p *Element) channelRange(channel Channel) (int, int) {
	start := 0
	for ch := Channel(0); ch < channel; ch++ {
		start += int(p.counts[ch])
	}
	return start, start + int(p.counts[channel])
}

/*
================
Element.Apply

Apply inserts or replaces one source, not the entire channel (the native element's insert).
The result reports whether a write occurred, even if the value is unchanged;
native also dirties and propagates unchanged non-sentinel writes.
================
*/
func (p *Element) Apply(channel Channel, source uint32, value float32) (bool, error) {
	if channel >= channelCount || !finite(value) {
		return false, fmt.Errorf("paramkeeper: invalid channel %d or non-finite modifier", channel)
	}
	if value == p.definition.Ignore { // native sentinel test: ordered equality only
		return false, nil
	}
	p.allocated |= 1 << channel
	if p.entries == nil {
		p.entries = p.inline[:0]
	}
	start, end := p.channelRange(channel)
	insertAt := end
	for i := start; i < end; i++ {
		if p.entries[i].source == source {
			p.entries[i].value = value
			return true, nil
		}
		if p.entries[i].source > source {
			insertAt = i
			break
		}
	}
	p.entries = append(p.entries, bucketEntry{})
	copy(p.entries[insertAt+1:], p.entries[insertAt:])
	p.entries[insertAt] = bucketEntry{source: source, value: value}
	p.counts[channel]++
	return true, nil
}

/*
================
Element.Remove

Remove returns the native OR of channel+1 for every bucket that contained
the source (the native remove), not a channel bitset. A zero return means no change.
Empty buckets remain allocated: deleting the last Flat entry leaves a zero
sum and does not restore Definition.Base.
================
*/
func (p *Element) Remove(source uint32) uint8 {
	var removed uint8
	pos := 0
	for channel := Channel(0); channel < channelCount; channel++ {
		count := int(p.counts[channel])
		end := pos + count
		found := -1
		for i := pos; i < end; i++ {
			if p.entries[i].source == source {
				found = i
				break
			}
			if p.entries[i].source > source {
				break
			}
		}
		if found >= 0 {
			p.entries = append(p.entries[:found], p.entries[found+1:]...)
			p.counts[channel]--
			end--
			removed |= uint8(channel + 1)
		}
		pos = end
	}
	return removed
}

/*
================
Element.Value

Value follows the native value read's store boundaries. Wider intermediates precede
explicit float32 stores; this portable implementation is not an x87 bit-
equivalence certificate. Invalid/overflowing projections fail explicitly.
================
*/
func (p *Element) Value() (float32, error) {
	if p.allocated == 0 { // native: an empty element's fallback bypasses the clamp
		return p.definition.Base, nil
	}
	values := [channelCount]float32{p.definition.Base, 0, 0, 100}
	pos := 0
	for channel := Channel(0); channel < channelCount; channel++ {
		count := int(p.counts[channel])
		end := pos + count
		if p.allocated&(1<<channel) != 0 {
			values[channel] = reduce(channel, p.entries[pos:end])
			if !finite(values[channel]) {
				return 0, fmt.Errorf("paramkeeper: channel %d overflow", channel)
			}
		}
		pos = end
	}
	value := float32((float64(values[PercentSum]) + 100) * float64(values[Flat]) / 100)
	if values[PercentProduct] != 0 {
		if value == 0 { // native zero test: equality, not positivity
			value = values[PercentProduct]
		} else {
			value = float32(float64(value) * float64(values[PercentProduct]))
		}
	}
	value = float32((float64(values[FactorProduct]) / 100) * float64(value))
	if !finite(value) {
		return 0, fmt.Errorf("paramkeeper: result overflow")
	}
	if value < p.definition.Minimum {
		value = p.definition.Minimum
	} else if value > p.definition.Maximum {
		value = p.definition.Maximum
	}
	return value, nil
}

/*
================
Element.cloneInto
================
*/
func (p *Element) cloneInto(dst *Element) {
	dst.definition = p.definition
	dst.counts = p.counts
	dst.allocated = p.allocated
	if len(p.entries) == 0 {
		if p.allocated != 0 {
			dst.entries = dst.inline[:0]
		} else {
			dst.entries = nil
		}
		return
	}
	if len(p.entries) <= inlineEntryCap {
		copy(dst.inline[:], p.entries)
		dst.entries = dst.inline[:len(p.entries)]
	} else {
		dup := make([]bucketEntry, len(p.entries))
		copy(dup, p.entries)
		dst.entries = dup
	}
}

/*
================
reduce
================
*/
func reduce(channel Channel, entries []bucketEntry) float32 {
	var result float32
	if channel == FactorProduct {
		result = 100
	}
	for _, entry := range entries {
		value := float64(entry.value)
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
finite
================
*/
func finite(value float32) bool {
	return !math.IsInf(float64(value), 0) && !math.IsNaN(float64(value))
}

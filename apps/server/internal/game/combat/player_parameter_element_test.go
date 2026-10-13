/*
===========================================================================

player_parameter_element_test.go - the one-element read equals the graph read

PlayerUnlinkedParameter evaluates an unlinked parameter on one keeper
element. graphUnlinkedParameter below is the read it replaced, through the
whole graph; a random walk of effect and abnormal writes, with every write
shape the graph refuses, must give both the same value or both an error.
The one difference is pinned on its own: an evaluation error on another
parameter fails the graph read only.

===========================================================================
*/
package combat

import (
	"math"
	"math/rand"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
graphUnlinkedParameter

The read before the element route, kept as the reference.
================
*/
func graphUnlinkedParameter(c *domain.Character, modifiers []paramkeeper.Write, block *abnormal.Block, id uint16) (float32, error) {
	level, _, _, _, err := playerSnapshotCore(c)
	if err != nil {
		return 0, err
	}
	graph, base, err := playerParameterGraph(level)
	if err != nil {
		return 0, err
	}
	if graph.Linked(id) {
		return 0, errLinkedReference
	}
	var writes []paramkeeper.Write
	for _, w := range base {
		if w.Parameter == id {
			writes = append(writes, w)
		}
	}
	for _, w := range modifiers {
		if w.Source == 0 {
			return 0, errLinkedReference
		}
		if w.Parameter == id {
			writes = append(writes, w)
		}
	}
	if err = graph.ApplyBatch(writes); err != nil {
		return 0, err
	}
	if err = applyAbnormalModifiers(graph, block); err != nil {
		return 0, err
	}
	return graph.Value(id)
}

// errLinkedReference stands for the reference's own refusals.
var errLinkedReference = paramkeeperError("refused")

type paramkeeperError string

func (e paramkeeperError) Error() string { return string(e) }

/*
================
elementTestValue

A write value: ordinary amounts, the Ignore sentinel of 0x8C's definition,
or, rarely, a non-finite or huge one.
================
*/
func elementTestValue(rng *rand.Rand) float32 {
	switch rng.Intn(20) {
	case 0:
		return float32(math.Inf(1))
	case 1:
		return float32(math.NaN())
	case 2:
		return 3e38
	case 3:
		d, _ := paramkeeper.NativeDefinition(unlinkedActionSpeed)
		return d.Ignore
	}
	return float32(rng.Intn(400)) - 50
}

/*
================
elementTestParameter

0x8C most often, other closure parameters, and now and then one outside
the closure.
================
*/
func elementTestParameter(rng *rand.Rand) uint16 {
	switch rng.Intn(4) {
	case 0, 1:
		return unlinkedActionSpeed
	case 2:
		return 600
	}
	return uint16(rng.Intn(0xbd))
}

/*
================
elementTestSource

Effect sources, now and then a reserved parameter identity or source 0.
================
*/
func elementTestSource(rng *rand.Rand, i int) uint32 {
	switch rng.Intn(15) {
	case 0:
		return 0
	case 1:
		return parameterNodeKeyBase + unlinkedActionSpeed
	}
	return unlinkedEffectSource + uint32(i)
}

/*
================
randomElementWrites

Effect writes and an abnormal block that use every shape above, with
frostbite's source-0 8C replacement and slow's 8C factor among them.
================
*/
func randomElementWrites(rng *rand.Rand) ([]paramkeeper.Write, *abnormal.Block) {
	var modifiers []paramkeeper.Write
	for i := 0; i < rng.Intn(5); i++ {
		modifiers = append(modifiers, paramkeeper.Write{Parameter: elementTestParameter(rng),
			Channel: paramkeeper.Channel(rng.Intn(5)), Source: elementTestSource(rng, i), Value: elementTestValue(rng)})
	}
	if rng.Intn(4) == 0 {
		return modifiers, nil
	}
	block := &abnormal.Block{}
	for i := 0; i < rng.Intn(6); i++ {
		m := abnormal.Modifier{Used: rng.Intn(6) != 0, Param: elementTestParameter(rng), Channel: uint8(rng.Intn(5)),
			Source: 5, Value: elementTestValue(rng)}
		switch rng.Intn(7) {
		case 6:
			// Half of strength's overflow pair: with the other half it
			// fails the graph read on parameter 1 only.
			m = abnormal.Modifier{Used: true, Param: 1, Channel: uint8(rng.Intn(2)), Source: 5 + uint32(i), Value: 3e38}
		case 0:
			m = abnormal.Modifier{Used: true, Param: unlinkedActionSpeed, Source: 0, Value: 200}
		case 1:
			m = abnormal.Modifier{Used: true, Param: unlinkedActionSpeed, Channel: 3, Source: 5, Value: 125}
		case 2:
			m.Source = elementTestSource(rng, i)
		}
		block.Modifiers[i] = m
	}
	return modifiers, block
}

/*
================
withoutOtherAbnormalWrites

The block with only its writes to id: what the graph read would see if no
other parameter were written.
================
*/
func withoutOtherAbnormalWrites(block *abnormal.Block, id uint16) *abnormal.Block {
	if block == nil {
		return nil
	}
	only := *block
	for i := range only.Modifiers {
		if only.Modifiers[i].Param != id {
			only.Modifiers[i] = abnormal.Modifier{}
		}
	}
	return &only
}

/*
================
TestUnlinkedElementMatchesTheGraphRead

Same value or both refused, over random writes of every shape. Where only
the graph read fails, it must be the documented case: the same call with
the other parameters' abnormal writes removed succeeds on the graph with
the element's value.
================
*/
func TestUnlinkedElementMatchesTheGraphRead(t *testing.T) {
	rng := rand.New(rand.NewSource(20261013))
	values, refusals, otherFailures := 0, 0, 0
	for sample := 0; sample < 60000; sample++ {
		level := int64(1 + rng.Intn(140))
		c := &domain.Character{Level: &level, Strength: pointer(int64(20 + rng.Intn(400))), Intellect: pointer(int64(20 + rng.Intn(400)))}
		modifiers, block := randomElementWrites(rng)
		want, wantErr := graphUnlinkedParameter(c, modifiers, block, unlinkedActionSpeed)
		got, gotErr := PlayerUnlinkedParameter(c, modifiers, block, unlinkedActionSpeed)
		switch {
		case wantErr == nil && gotErr == nil:
			if math.Float32bits(got) != math.Float32bits(want) {
				t.Fatalf("sample %d: element %v, graph %v\nmodifiers %+v\nblock %+v", sample, got, want, modifiers, block)
			}
			values++
		case wantErr != nil && gotErr != nil:
			refusals++
		case wantErr != nil:
			only, onlyErr := graphUnlinkedParameter(c, modifiers, withoutOtherAbnormalWrites(block, unlinkedActionSpeed), unlinkedActionSpeed)
			if onlyErr != nil || math.Float32bits(only) != math.Float32bits(got) {
				t.Fatalf("sample %d: graph refused (%v) and element read %v, not the documented case\nmodifiers %+v\nblock %+v",
					sample, wantErr, got, modifiers, block)
			}
			otherFailures++
		default:
			t.Fatalf("sample %d: element refused (%v), graph read %v\nmodifiers %+v\nblock %+v", sample, gotErr, want, modifiers, block)
		}
	}
	if values < 15000 || refusals < 1500 || otherFailures < 10 {
		t.Fatalf("the walk proves little: %d values, %d refusals, %d other-parameter failures", values, refusals, otherFailures)
	}
	t.Logf("%d equal values, %d shared refusals, %d other-parameter evaluation failures", values, refusals, otherFailures)
}

/*
================
TestUnlinkedElementIgnoresAnotherParametersEvaluation

The stated contract change: an abnormal write that makes another
parameter's evaluation overflow fails the graph read, while 0x8C, which
does not depend on it, still reads.
================
*/
func TestUnlinkedElementIgnoresAnotherParametersEvaluation(t *testing.T) {
	c := &domain.Character{Level: pointer(int64(40)), Strength: pointer(int64(60)), Intellect: pointer(int64(60))}
	block := &abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: 1, Channel: uint8(paramkeeper.Flat), Source: 5, Value: 3e38}
	block.Modifiers[1] = abnormal.Modifier{Used: true, Param: 1, Channel: uint8(paramkeeper.PercentSum), Source: 5, Value: 3e38}
	block.Modifiers[2] = abnormal.Modifier{Used: true, Param: unlinkedActionSpeed, Channel: 3, Source: 5, Value: 125}
	if _, err := graphUnlinkedParameter(c, nil, block, unlinkedActionSpeed); err == nil {
		t.Fatal("the graph read no longer fails on another parameter's overflow; the fixture proves nothing")
	}
	if got, err := PlayerUnlinkedParameter(c, nil, block, unlinkedActionSpeed); err != nil || got != 125 {
		t.Fatalf("action speed %v (%v), want 125", got, err)
	}
}

/*
================
TestActionSpeedIsUnlinkedInTheBuiltGraph

The element route stands on 0x8C being fed by no link. Asked of the graph
the player closure builds, and the static shape must agree with the graph
for every parameter.
================
*/
func TestActionSpeedIsUnlinkedInTheBuiltGraph(t *testing.T) {
	graph, _, err := playerParameterGraph(1)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Linked(unlinkedActionSpeed) {
		t.Fatal("0x8C is fed by a link; the element route would be wrong")
	}
	shape, err := playerParameterShape()
	if err != nil {
		t.Fatal(err)
	}
	for id := range shape.definitions {
		if graph.Linked(id) != shape.linked[id] {
			t.Fatalf("parameter %d: graph linked %v, shape %v", id, graph.Linked(id), shape.linked[id])
		}
	}
}

/*
================
BenchmarkUnlinkedActionSpeed

One entry action-speed read under a slow and a frostbite: the graph read
it replaced against the element route.
================
*/
func BenchmarkUnlinkedActionSpeed(b *testing.B) {
	c := &domain.Character{Level: pointer(int64(80)), Strength: pointer(int64(300)), Intellect: pointer(int64(120))}
	modifiers := []paramkeeper.Write{{Parameter: unlinkedActionSpeed, Channel: paramkeeper.PercentSum, Source: unlinkedEffectSource, Value: 20}}
	block := &abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: unlinkedActionSpeed, Channel: 3, Source: 5, Value: 125}
	block.Modifiers[1] = abnormal.Modifier{Used: true, Param: 0xb4, Channel: 2, Source: 5, Value: 30}
	b.Run("graph", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = graphUnlinkedParameter(c, modifiers, block, unlinkedActionSpeed)
		}
	})
	b.Run("element", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = PlayerUnlinkedParameter(c, modifiers, block, unlinkedActionSpeed)
		}
	})
}

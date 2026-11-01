package paramkeeper

import (
	"math"
	"testing"
)

func testGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := NewGraph([]NodeDefinition{
		{ID: 1, SourceKey: 1001, Definition: Definition{Maximum: 9999999}},
		{ID: 21, SourceKey: 1021, Definition: Definition{Maximum: 9999999}},
		{ID: 5, SourceKey: 1005, Definition: Definition{Maximum: 9999999}},
		{ID: 50, SourceKey: 1050, Definition: Definition{Maximum: 1000, Base: 100, Ignore: -1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Actual player links: 1 -> 21 -> 5; 50 -> 21 through channel 3.
	// Source: the player graph builder's links, via EBX/EDI/stack channel.
	for _, link := range []struct {
		source, target uint16
		channel        Channel
	}{
		{1, 21, Flat}, {21, 5, Flat}, {50, 21, FactorProduct},
	} {
		if err = g.Link(link.source, link.target, link.channel); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func graphApply(t *testing.T, g *Graph, id uint16, channel Channel, source uint32, v float32) {
	t.Helper()
	if _, err := g.Apply(id, channel, source, v); err != nil {
		t.Fatal(err)
	}
}

func graphValue(t *testing.T, g *Graph, id uint16, want float32) {
	t.Helper()
	got, err := g.Value(id)
	if err != nil || got != want {
		t.Fatalf("param %d: got %v want %v err %v", id, got, want, err)
	}
}
func TestPlayerDefenseDependencySequence(t *testing.T) {
	g := testGraph(t)
	graphApply(t, g, 50, Flat, 0, 19)
	graphApply(t, g, 1, Flat, 0, 100)
	graphValue(t, g, 5, 19)
	graphApply(t, g, 5, Flat, 10, 20) // Direct defense modifier, another owner.
	graphValue(t, g, 5, 39)
	graphApply(t, g, 1, Flat, 11, 100) // STR buff changes downstream defense.
	graphValue(t, g, 5, 58)
	if _, err := g.Remove(1, 11); err != nil {
		t.Fatal(err)
	}
	graphValue(t, g, 5, 39)
	if _, err := g.Remove(5, 10); err != nil {
		t.Fatal(err)
	}
	graphValue(t, g, 5, 19)
	graphApply(t, g, 50, Flat, 0, 30)
	graphValue(t, g, 5, 30)
}

func TestDependencyRegistrationIsNotValuePublicationAndFirstLinkWins(t *testing.T) {
	g := testGraph(t)
	// A fallback value in source 50 is not published merely by Link.
	graphValue(t, g, 21, 0)
	if err := g.Link(1, 21, PercentSum); err != nil {
		t.Fatal(err)
	}
	graphApply(t, g, 1, Flat, 0, 30)
	graphValue(t, g, 21, 30) // Original Flat registration survived.
}

func TestIgnoredDependencyValuePreservesPriorTargetEntry(t *testing.T) {
	g := testGraph(t)
	graphApply(t, g, 1, Flat, 77, 30)
	graphValue(t, g, 5, 30)
	if _, err := g.Remove(1, 77); err != nil {
		t.Fatal(err)
	}
	graphValue(t, g, 1, 0)
	// The native write routes through the element insert, whose zero sentinel is a no-op.
	// It does not turn propagation into RemoveModifier. Preserve this quirk.
	graphValue(t, g, 21, 30)
	graphValue(t, g, 5, 30)
}

func TestGraphRejectsCycleMissingNodeAndIdentityCollision(t *testing.T) {
	g := testGraph(t)
	for _, link := range [][2]uint16{{5, 1}, {1, 1}, {1, 511}} {
		if err := g.Link(link[0], link[1], Flat); err == nil {
			t.Fatalf("invalid link accepted %v", link)
		}
	}
	if _, err := g.Apply(5, Flat, 1001, 10); err == nil {
		t.Fatal("effect identity collided with parameter identity")
	}
	if _, err := g.Remove(5, 1001); err == nil {
		t.Fatal("external source removed dependency")
	}
	if _, err := g.Value(511); err == nil {
		t.Fatal("undefined parameter returned a default")
	}
	for _, defs := range [][]NodeDefinition{
		{{ID: 512, SourceKey: 1}}, {{ID: 0, SourceKey: 0}},
		{{ID: 0, SourceKey: 1}, {ID: 0, SourceKey: 2}},
		{{ID: 0, SourceKey: 1}, {ID: 1, SourceKey: 1}},
	} {
		if _, err := NewGraph(defs); err == nil {
			t.Fatalf("invalid graph accepted: %+v", defs)
		}
	}
}

func TestGraphFailedPropagationRollsBackWholeOperation(t *testing.T) {
	g := testGraph(t)
	graphApply(t, g, 1, Flat, 0, 30)
	// Arrange a downstream overflow without overflowing the input operation.
	graphApply(t, g, 5, FactorProduct, 0, 1e35)
	before, err := g.Value(5)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := g.Apply(1, Flat, 0, 1e10); changed || err == nil {
		t.Fatalf("propagation overflow accepted: changed=%v err=%v", changed, err)
	}
	graphValue(t, g, 1, 30)
	graphValue(t, g, 21, 30)
	graphValue(t, g, 5, before)
}

func TestBatchFailureDoesNotPublishEarlierWrites(t *testing.T) {
	g := testGraph(t)
	graphApply(t, g, 1, Flat, 0, 30)
	err := g.ApplyBatch([]Write{{Parameter: 1, Source: 0, Value: 100}, {Parameter: 5, Channel: Channel(4), Source: 90, Value: 10}})
	if err == nil {
		t.Fatal("invalid batch accepted")
	}
	graphValue(t, g, 1, 30)
	graphValue(t, g, 5, 30)
	if err := g.ApplyBatch([]Write{{Parameter: 1, Source: 0, Value: 100}, {Parameter: 5, Source: 90, Value: 10}}); err != nil {
		t.Fatal(err)
	}
	graphValue(t, g, 5, 110)
}

/*
================
TestApplyBatchInPlaceMatchesApplyBatch

The in-place batch is the fresh-clone build path: same writes, same
order, so every parameter must read back exactly as the atomic batch
left it, and a refused batch must refuse the same way.
================
*/
func TestApplyBatchInPlaceMatchesApplyBatch(t *testing.T) {
	atomic, inPlace := testGraph(t), testGraph(t)
	for _, g := range []*Graph{atomic, inPlace} {
		graphApply(t, g, 1, Flat, 0, 30)
		graphApply(t, g, 50, Flat, 0, 19)
	}
	writes := []Write{
		{Parameter: 1, Source: 0, Value: 100},
		{Parameter: 5, Source: 90, Value: 10},
		{Parameter: 5, Channel: PercentSum, Source: 91, Value: 25},
		{Parameter: 21, Source: 92, Value: 7},
	}
	if err := atomic.ApplyBatch(writes); err != nil {
		t.Fatal(err)
	}
	if err := inPlace.ApplyBatchInPlace(writes); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint16{1, 5, 21, 50} {
		want, err := atomic.Value(id)
		if err != nil {
			t.Fatal(err)
		}
		got, err := inPlace.Value(id)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("param %d: in-place %v, atomic %v", id, got, want)
		}
	}
	bad := []Write{{Parameter: 1, Source: 0, Value: 100}, {Parameter: 5, Channel: Channel(4), Source: 90, Value: 10}}
	if err := atomic.ApplyBatch(bad); err == nil {
		t.Fatal("invalid atomic batch accepted")
	}
	if err := inPlace.ApplyBatchInPlace(bad); err == nil {
		t.Fatal("invalid in-place batch accepted")
	}
}

/*
================
TestApplyInPlaceMatchesApply

The in-place single write is the abnormal-modifier path on the
projection build's own clone: the same writes in the same order
must land on the same values with the same changed answers, and a
refused write must refuse with the same error (its graph is then
dropped, so only the atomic side's rollback is read afterwards).
================
*/
func TestApplyInPlaceMatchesApply(t *testing.T) {
	atomic, inPlace := testGraph(t), testGraph(t)
	steps := []struct {
		id      uint16
		channel Channel
		source  uint32
		value   float32
	}{
		{1, Flat, 0, 30},
		{50, Flat, 0, 19},
		{50, Flat, 7, -1},
		{5, PercentSum, 90, 25},
		{1, Flat, 11, 100},
		{21, FactorProduct, 92, 2},
	}
	for _, step := range steps {
		wantChanged, wantErr := atomic.Apply(step.id, step.channel, step.source, step.value)
		gotChanged, gotErr := inPlace.ApplyInPlace(step.id, step.channel, step.source, step.value)
		if wantChanged != gotChanged || (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("write %+v: atomic (%v, %v), in-place (%v, %v)", step, wantChanged, wantErr, gotChanged, gotErr)
		}
		for _, id := range []uint16{1, 5, 21, 50} {
			want, err := atomic.Value(id)
			if err != nil {
				t.Fatal(err)
			}
			got, err := inPlace.Value(id)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("param %d after %+v: in-place %v, atomic %v", id, step, got, want)
			}
		}
		if atomic.dirty != inPlace.dirty {
			t.Fatalf("dirty after %+v: in-place %v, atomic %v", step, inPlace.dirty, atomic.dirty)
		}
	}
	for _, step := range []struct {
		id      uint16
		channel Channel
		source  uint32
		value   float32
	}{
		{1, Channel(9), 0, 10},
		{1, Flat, 0, float32(math.NaN())},
	} {
		wantChanged, wantErr := atomic.Apply(step.id, step.channel, step.source, step.value)
		gotChanged, gotErr := inPlace.ApplyInPlace(step.id, step.channel, step.source, step.value)
		if wantErr == nil || gotErr == nil || wantErr.Error() != gotErr.Error() || wantChanged || gotChanged {
			t.Fatalf("refused write %+v: atomic (%v, %v), in-place (%v, %v)", step, wantChanged, wantErr, gotChanged, gotErr)
		}
	}
	if _, err := atomic.Apply(5, Flat, 1001, 10); err == nil {
		t.Fatal("atomic accepted a reserved source")
	}
	if _, err := inPlace.ApplyInPlace(5, Flat, 1001, 10); err == nil {
		t.Fatal("in-place accepted a reserved source")
	}
	if _, err := atomic.Apply(511, Flat, 0, 10); err == nil {
		t.Fatal("atomic accepted an undefined parameter")
	}
	if _, err := inPlace.ApplyInPlace(511, Flat, 0, 10); err == nil {
		t.Fatal("in-place accepted an undefined parameter")
	}
}

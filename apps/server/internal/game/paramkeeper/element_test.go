package paramkeeper

import (
	"math"
	"testing"
)

func element(t *testing.T, base float32) *Element {
	t.Helper()
	p, err := New(Definition{Minimum: -1000000000, Maximum: 1000000000, Base: base})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func apply(t *testing.T, p *Element, channel Channel, source uint32, value float32) {
	t.Helper()
	changed, err := p.Apply(channel, source, value)
	if err != nil || !changed {
		t.Fatalf("Apply(%d,%d,%v): changed=%v, err=%v", channel, source, value, changed, err)
	}
}

func value(t *testing.T, p *Element, want float32) {
	t.Helper()
	got, err := p.Value()
	if err != nil || math.Float32bits(got) != math.Float32bits(want) {
		t.Fatalf("Value()=%v (%08x), want %v (%08x), err=%v", got, math.Float32bits(got), want, math.Float32bits(want), err)
	}
}

func TestIndependentSourcesReplaceAndRemove(t *testing.T) {
	p := element(t, 9)
	apply(t, p, Flat, 100, 10)
	apply(t, p, Flat, 200, 20)
	value(t, p, 30) // Does not erase source 100 or add fallback 9.
	apply(t, p, Flat, 100, 15)
	value(t, p, 35)
	apply(t, p, PercentSum, 100, 20)
	value(t, p, 42)
	if got := p.Remove(100); got != 3 { // 1 OR 2, across both channels
		t.Fatalf("remove status=%d", got)
	}
	value(t, p, 20)
	if got := p.Remove(100); got != 0 {
		t.Fatalf("duplicate remove status=%d", got)
	}
	p.Remove(200)
	value(t, p, 0) // Allocated-but-empty Flat bucket survives removal.
}

func TestSentinelIsNoOpNotRemoval(t *testing.T) {
	p := element(t, 11)
	changed, err := p.Apply(Flat, 1, 0)
	if changed || err != nil {
		t.Fatalf("sentinel changed=%v err=%v", changed, err)
	}
	value(t, p, 11) // A sentinel does not allocate an empty bucket.
	apply(t, p, Flat, 1, 7)
	p.Apply(Flat, 1, 0)
	value(t, p, 7) // An old entry survives the ignored write.
	changed, err = p.Apply(Flat, 1, 7)
	if !changed || err != nil {
		t.Fatalf("unchanged non-sentinel must propagate: %v %v", changed, err)
	}
	p, err = New(Definition{Minimum: -100, Maximum: 100, Base: 11, Ignore: -1})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, p, Flat, 1, 0) // Zero is not universally a sentinel.
	value(t, p, 0)
}

func TestFourChannelsComposeAtNativeStoreBoundaries(t *testing.T) {
	p := element(t, 999)
	apply(t, p, Flat, 1, 100)
	apply(t, p, Flat, 2, 20)
	apply(t, p, PercentSum, 1, 10)
	apply(t, p, PercentSum, 2, 15)
	apply(t, p, PercentProduct, 1, 20)
	apply(t, p, PercentProduct, 2, 50)
	apply(t, p, FactorProduct, 1, 50)
	apply(t, p, FactorProduct, 2, 80)
	value(t, p, 108)
	if got := p.Remove(1); got != 7 { // 1|2|3|4 is 7, not the bitmask 15.
		t.Fatalf("native removal return=%d", got)
	}
	value(t, p, 27.6)
}

func TestPercentProductZeroAccumulatorAndNegativeBase(t *testing.T) {
	p := element(t, -10)
	apply(t, p, PercentProduct, 1, 50)
	value(t, p, -15) // Native tests equality to zero, not >0.
	p = element(t, 0)
	apply(t, p, PercentProduct, 1, 50)
	value(t, p, 1.5)
	p = element(t, 10)
	apply(t, p, PercentProduct, 1, -100)
	value(t, p, 10) // Final zero factor is skipped by the native value read.
	apply(t, p, PercentProduct, 2, 50)
	value(t, p, 15) // Reducer restarts when prior aggregate is zero.
	p.Remove(2)
	value(t, p, 10)
	p.Remove(1)
	value(t, p, 10)
}

func TestFactorProductEmptyIdentity(t *testing.T) {
	p := element(t, 100)
	apply(t, p, FactorProduct, 1, 50)
	apply(t, p, FactorProduct, 2, 80)
	value(t, p, 40)
	p.Remove(1)
	value(t, p, 80)
	p.Remove(2)
	value(t, p, 100)
}

func TestUnsignedSourceOrderAndFloat32Accumulation(t *testing.T) {
	// Correct unsigned order: 2^24 + 1 rounds to 2^24, then subtraction is 0.
	// Signed key ordering, insertion ordering or float64-only sum instead give 1.
	for _, order := range [][]uint32{{0xffffffff, 1, 2}, {2, 1, 0xffffffff}, {1, 2, 0xffffffff}} {
		p := element(t, 9)
		v := map[uint32]float32{1: 16777216, 2: 1, 0xffffffff: -16777216}
		for _, source := range order {
			apply(t, p, Flat, source, v[source])
		}
		value(t, p, 0)
	}
}

func TestNoBucketBypassesClampButAllocatedBucketDoesNot(t *testing.T) {
	p, err := New(Definition{Minimum: 5, Maximum: 10, Base: 20})
	if err != nil {
		t.Fatal(err)
	}
	value(t, p, 20)
	apply(t, p, PercentSum, 1, 10)
	value(t, p, 10)
	p.Remove(1)
	value(t, p, 10)
	apply(t, p, Flat, 2, -2)
	value(t, p, 5)
}

func TestInvalidInputsDoNotMutate(t *testing.T) {
	p := element(t, 12)
	for _, test := range []struct {
		channel Channel
		value   float32
	}{
		{4, 10}, {255, 10}, {Flat, float32(math.Inf(1))}, {Flat, float32(math.NaN())},
	} {
		if changed, err := p.Apply(test.channel, 1, test.value); changed || err == nil {
			t.Fatalf("invalid write accepted: %+v", test)
		}
		value(t, p, 12)
	}
	for _, d := range []Definition{{Minimum: 2, Maximum: 1}, {Base: float32(math.NaN())}, {Ignore: float32(math.Inf(1))}} {
		if _, err := New(d); err == nil {
			t.Fatalf("invalid definition accepted: %+v", d)
		}
	}
	apply(t, p, Flat, 1, math.MaxFloat32)
	apply(t, p, Flat, 2, math.MaxFloat32)
	if _, err := p.Value(); err == nil {
		t.Fatal("overflow silently accepted")
	}
}

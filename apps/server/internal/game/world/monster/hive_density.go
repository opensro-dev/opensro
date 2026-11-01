package monster

import "math"

// HiveDensityPolicy is the keep-count input to the reference server's hive
// density computation. Recovered
// enabled rows are all INS_DEFAULT; numeric newer-version world IDs do not
// cross the natural-key projection.
type HiveDensityPolicy struct {
	Kind          uint8
	MonstersPerPC float32
	Step, Maximum uint32
}

// HiveDensity preserves the native unsigned accumulators and initially-zero
// two-slot history. Two 30-second samples update one slot, then both slots
// are averaged. A stalled callback does not manufacture historical samples.
type HiveDensity struct {
	sum, samples, average, cursor uint32
	history                       [2]uint32
}

func (d *HiveDensity) Sample(players, denominator uint32, policy HiveDensityPolicy) float32 {
	d.sum += players
	d.samples++
	if d.samples >= 2 {
		d.history[d.cursor] = d.sum >> 1
		d.cursor = (d.cursor + 1) & 1
		d.average = (d.history[0] + d.history[1]) >> 1
		d.sum, d.samples = 0, 0
	}
	var quotient uint32
	if denominator != 0 {
		quotient = d.average / denominator
	}
	// A zero divisor reaches x87 FISTP's indefinite int64; its low dword
	// is zero, which the native scaling multiplies. IMUL retains only the
	// low dword.
	return min(float32(quotient*policy.Step), float32(policy.Maximum))
}

func (p HiveDensityPolicy) Denominator(total uint32) uint32 {
	if p.MonstersPerPC <= 0 || math.IsNaN(float64(p.MonstersPerPC)) {
		return 0
	}
	v := float64(total) / float64(p.MonstersPerPC)
	if v >= 0x1p63 {
		return 0 // low dword of FISTP indefinite
	}
	return uint32(uint64(v))
}

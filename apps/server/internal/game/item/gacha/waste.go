package gacha

import (
	"fmt"
	"math"
)

type wasteBand struct {
	upper uint32
	prize Prize
}

// The server's LOSE preparation, including the explicit float32 stores
// and fractional redistribution. A raw cumulative sum is not equivalent,
// even when authored weights happen to total 10000.
func prepareWastePool(prizes []Prize) ([]wasteBand, error) {
	var total uint64
	for _, p := range prizes {
		total += uint64(p.ChancePer10000)
	}
	if total == 0 || total > math.MaxUint32 {
		return nil, fmt.Errorf("invalid total weight %d", total)
	}
	ratio := make([]float32, len(prizes))
	scaled := make([]float32, len(prizes))
	fraction := make([]float32, len(prizes))
	carry := make([]float32, len(prizes))
	correction := make([]float32, len(prizes))
	var fractions float32
	remaining := int64(10000)
	for i, p := range prizes {
		ratio[i] = float32(float64(p.ChancePer10000) / float64(total))
		scaled[i] = float32(float64(ratio[i]) * 10000)
		whole := int64(scaled[i])
		remaining -= whole
		fraction[i] = float32(float64(scaled[i]) - float64(whole))
		fractions = float32(float64(fractions) + float64(fraction[i]))
	}
	if remaining < 0 {
		return nil, fmt.Errorf("normalized weights overflow")
	}
	originalRemaining := remaining
	// The native loop revisits rows until its fractional budget is assigned.
	for remaining > 0 {
		progressed := false
		for i := range prizes {
			if remaining == 0 {
				break
			}
			r2 := float32(float64(ratio[i]) * float64(ratio[i]))
			f := float32(float64(fraction[i]) / float64(fractions))
			f2 := float32(float64(f) * float64(f))
			product := float32(float64(r2) * float64(f2))
			root := float32(math.Sqrt(float64(product)))
			increment := float32(float64(root) / float64(fractions) * float64(originalRemaining))
			next := float32(float64(carry[i]) + float64(increment))
			if math.IsNaN(float64(next)) || math.IsInf(float64(next), 0) {
				return nil, fmt.Errorf("invalid fractional weights")
			}
			progressed = progressed || next != carry[i]
			carry[i] = next
			if next >= 1 {
				count := int64(next)
				if count > remaining {
					count = remaining
				}
				carry[i] = float32(float64(next) - float64(count))
				correction[i] = float32(float64(correction[i]) + float64(count))
				remaining -= count
			}
		}
		if !progressed {
			return nil, fmt.Errorf("fractional weights cannot advance")
		}
	}
	out := make([]wasteBand, 0, len(prizes))
	var upper uint32
	for i, p := range prizes {
		width := uint32(float64(scaled[i]) + float64(correction[i]))
		if width == 0 && i > 0 {
			return nil, fmt.Errorf("entry %d has duplicate cumulative boundary", p.EntryID)
		}
		upper += width
		out = append(out, wasteBand{upper, p})
	}
	if upper != 9999 && upper != 10000 {
		return nil, fmt.Errorf("invalid cumulative endpoint %d", upper)
	}
	return out, nil
}

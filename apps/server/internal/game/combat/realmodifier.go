package combat

// RealModifiers projects the server's real-modifier tables. A real
// instruction selects native tables with its first word, supplies a value
// with its second and a key with its third. Contributions belong to
// execution contexts, not skill IDs.
// This container alone does not admit a skill: its consumers and installation
// prerequisites must also be executable before the plan compiler admits it.
type RealModifiers struct {
	tables map[uint32]map[uint32][]RealContribution
}

type RealContribution struct{ Value, Context uint32 }

var realMasks = [...]uint32{0x40, 0x80, 0x100, 0x200, 0x400, 0x800, 0x2000,
	0x4000, 0x8000, 0x10000, 0x20000, 0x40000, 0x80000, 0x100000,
	0x200000, 0x400000, 0x1000000}

func (r *RealModifiers) Update(mask, value, key, context uint32, remove bool) {
	for _, bit := range realMasks {
		if mask&bit == 0 {
			continue
		}
		table := r.tables[bit]
		if remove {
			entries := table[key]
			for i, entry := range entries {
				if entry.Value == value && entry.Context == context {
					entries = append(entries[:i], entries[i+1:]...)
					break
				}
			}
			if len(entries) == 0 {
				delete(table, key)
			} else {
				table[key] = entries
			}
			continue
		}
		if r.tables == nil {
			r.tables = make(map[uint32]map[uint32][]RealContribution)
		}
		if table == nil {
			table = make(map[uint32][]RealContribution)
			r.tables[bit] = table
		}
		entries := table[key]
		// Native multimap preserves equivalent-key insertion order.
		i := 0
		for i < len(entries) && entries[i].Value <= value {
			i++
		}
		entries = append(entries, RealContribution{})
		copy(entries[i+1:], entries[i:])
		entries[i] = RealContribution{value, context}
		table[key] = entries
	}
}

func (r *RealModifiers) Contributions(mask, key uint32) []RealContribution {
	return append([]RealContribution(nil), r.tables[mask][key]...)
}

// Strongest selects the highest unsigned grade, then the highest unsigned
// value within that grade (the server's strongest-entry walk decrements
// end() at both levels).
func (r *RealModifiers) Strongest(mask uint32) (grade, value uint32) {
	for key, entries := range r.tables[mask] {
		if len(entries) != 0 && key >= grade {
			grade, value = key, entries[len(entries)-1].Value
		}
	}
	return
}

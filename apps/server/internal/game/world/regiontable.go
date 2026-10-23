/*
===========================================================================

regiontable.go - one value per region, held as sorted inclusive ranges

The region tables of the derived data (battlefield flags, caravan zones)
share one layout: {"format": ..., "ranges": [[first, last, value], ...]},
sorted and nonoverlapping. This owns the parse, the admission and the
lookup; each table names its file, its format and its value type.

===========================================================================
*/
package world

import (
	"encoding/json"
	"fmt"
	"sort"

	"opensro.online/server/internal/gamedata"
)

/*
================
regionSpan
================
*/
type regionSpan[V any] struct {
	first, last uint16
	value       V
}

/*
================
regionTable

A nil or empty table knows no region.
================
*/
type regionTable[V any] struct {
	spans []regionSpan[V]
}

/*
================
newRegionTable

Refuses inverted, overlapping or unsorted spans: a lookup over them would
answer for the wrong region.
================
*/
func newRegionTable[V any](name string, spans []regionSpan[V]) (*regionTable[V], error) {
	for i, s := range spans {
		if s.first > s.last || i > 0 && s.first <= spans[i-1].last {
			return nil, fmt.Errorf("%s: range %d (%04x-%04x) is inverted or overlaps", name, i, s.first, s.last)
		}
	}
	return &regionTable[V]{spans: spans}, nil
}

/*
================
loadRegionTable

The table file under derivedDir, or nil when there is none.
================
*/
func loadRegionTable[V any](derivedDir, file, format string) (*regionTable[V], error) {
	data, err := gamedata.ReadDerived(derivedDir, file)
	if err != nil || data == nil {
		return nil, err
	}
	var document struct {
		Format string
		Ranges [][3]json.RawMessage
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if document.Format != format {
		return nil, fmt.Errorf("%s: format %q, want %q", file, document.Format, format)
	}
	spans := make([]regionSpan[V], len(document.Ranges))
	for i, row := range document.Ranges {
		s := &spans[i]
		if json.Unmarshal(row[0], &s.first) != nil || json.Unmarshal(row[1], &s.last) != nil ||
			json.Unmarshal(row[2], &s.value) != nil {
			return nil, fmt.Errorf("%s: malformed range %d", file, i)
		}
	}
	return newRegionTable(file, spans)
}

/*
================
lookup
================
*/
func (t *regionTable[V]) lookup(region uint16) (value V, known bool) {
	if t == nil {
		return value, false
	}
	i := sort.Search(len(t.spans), func(i int) bool { return t.spans[i].last >= region })
	if i == len(t.spans) || region < t.spans[i].first {
		return value, false
	}
	return t.spans[i].value, true
}

/*
===========================================================================
itemrefs_lookup.go - native and earned arena item identity lookups.
===========================================================================
*/
package enterworld

import "opensro.online/server/internal/domain"

// ItemRefByCodename implements ItemRefSource.
/*
================
ItemRefByCodename
================
*/
func (t *TextdataItems) ItemRefByCodename(codename string) (*ItemRef, bool) {
	if codename == domain.ArenaSilkScrollCode {
		return ArenaSilkScroll(), true
	}
	t.once.Do(t.load)
	if t.archive != nil {
		key, ok := t.nameKeys[codename]
		if !ok {
			return nil, false
		}
		return t.archive.Get(key)
	}
	row, ok := t.byCodename[codename]
	return row, ok
}

/*
================
ItemRefByID
================
*/
func (t *TextdataItems) ItemRefByID(id uint32) (*ItemRef, bool) {
	if id == domain.ArenaSilkScrollID {
		return ArenaSilkScroll(), true
	}
	t.once.Do(t.load)
	if t.archive != nil {
		key, ok := t.idKeys[id]
		if !ok {
			return nil, false
		}
		return t.archive.Get(key)
	}
	row, ok := t.byID[id]
	return row, ok
}

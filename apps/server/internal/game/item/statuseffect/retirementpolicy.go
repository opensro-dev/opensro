package statuseffect

// RetirementSelected is the server's selected-retirement predicate. The byte
// at descriptor+65 is tested for nonzero, not equality to a named activity.
// This is distinct from voluntary nbuf protection and duration persistence.
// Selection alone does not implement event 6, unregistering, or destruction.
func RetirementSelected(force bool, descriptor65 uint8, current bool, category uint32, slot274, slot2B4, cbuf bool) bool {
	if cbuf {
		return false
	}
	return force || descriptor65 != 0 && (current || category == 3 && (slot274 || slot2B4))
}

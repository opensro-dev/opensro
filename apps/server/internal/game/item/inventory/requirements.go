package inventory

// EquipRequirements is the character-vs-itemdata slice of the equip gates:
// the checks that need data the inventory engine does not hold (character
// level/sex/race, RefItemData requirement columns). The action runtime
// builds one per operation and hangs it on Inventory.Requirements; nil
// means no requirement data (the degraded no-textdata boot) and every item
// passes, exactly like the socket resolver's nil-source behavior.
//
// The split mirrors the NATIVE evaluation order of the client's full-mask
// requirement check (sub_789c60): razed (bit 0x004), STR (0x008), INT (0x010), level
// (0x020), mastery (0x100) and gender (0x040) run BEFORE the clothes/hard
// exclusivity scan (0x080); country (0x200) and the fortress-guild role
// (0x400) run AFTER it - so an item refused by two rules reads back the
// same notice byte retail orders first.
type EquipRequirements interface {
	// PreExclusivity: native bits 0x004 (razed), 0x008 (STR), 0x010 (INT),
	// 0x020 (level), 0x100 (mastery), 0x040 (gender), in that native
	// order - the mastery quad walk runs BEFORE the gender compare.
	// Nil = the item passes.
	PreExclusivity(item Item) *Fault
	// PostExclusivity: native bits 0x200 (country), then 0x400
	// (fortress-guild role: siege weapons only, and SOFT - a character
	// without a member record passes). Nil = the item passes.
	PostExclusivity(item Item) *Fault
}

package monster

// NativeIdleThreshold reconstructs the reference server's wander-or-idle
// roll selectors before the CRT draw. Owner and ownerMount are separate
// relationship identities: the latter is the player's +1D18 mount pointer,
// not this actor's movement/control mode. hasDefaultSkills is RefObjChar
// +400, produced by scanning all ten default skill IDs in the server's
// character-data extraction (+260 span); it is not ride capability at +210.
func NativeIdleThreshold(tid uint16, hasDefaultSkills bool, owner, ownerMount uint32) uint8 {
	cos := tid&0x7fe == 0x1c6
	kind := tid >> 11
	if cos && (kind == 5 || kind == 8) {
		return 101
	}
	if cos && hasDefaultSkills && owner != 0 && ownerMount != 0 {
		return 101
	}
	// v3CC: the independent actor class branch (the mob's packed
	// actor-class predicate).
	if tid&0xfffe == 0x2246 {
		return 101
	}
	return 25
}

// The reference server's controller-spacing maintenance excludes exactly
// the actor currently mounted by a player controller. A player mounted on
// some OTHER actor does not suppress this actor's follow selector. Run this
// only after controller lookup and the native target-admission check.
func NativeControllerMountExcludesFollow(actor uint32, controllerIsPlayer bool, controllerMount uint32) bool {
	return controllerIsPlayer && actor != 0 && controllerMount == actor
}

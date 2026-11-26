/*
===========================================================================

cubestorage.go - top-level persistence carriers for Magic Cube storage

Port-only (#3906). InventoryRow.LinkedStorage stays on the row in memory
(json:"-") and persists at the owning record's top level so schema 61
remains additive inside schema 60's read window (#3090): an older binary
preserves unknown top-level fields on rewrite and refuses unknown nested
fields.

===========================================================================
*/
package domain

// Container tags for CharacterCubeStorage.Container.
const (
	CubeContainerBag       = "bag"
	CubeContainerActiveCOS = "activeCos"
	CubeContainerCOS       = "cos"
)

/*
================
SlotCubeStorage

One Magic Cube's linked storage inside a flat slot container (account or
guild warehouse).
================
*/
type SlotCubeStorage struct {
	Slot    int64         `json:"slot"`
	Storage *COSContainer `json:"storage"`
}

/*
================
CharacterCubeStorage

One Magic Cube's linked storage inside a character record: the player's
bag, the active companion's bag, or a dormant companion's bag carried at
OwnerSlot in MissionInventory.
================
*/
type CharacterCubeStorage struct {
	Container string        `json:"container"`
	OwnerSlot int64         `json:"ownerSlot,omitempty"`
	Slot      int64         `json:"slot"`
	Storage   *COSContainer `json:"storage"`
}

/*
================
CloneCharacterCubeStorages

Deep copy of a character's top-level cube storage slice.
================
*/
func CloneCharacterCubeStorages(source []CharacterCubeStorage) []CharacterCubeStorage {
	if source == nil {
		return nil
	}
	clone := make([]CharacterCubeStorage, len(source))
	for i, entry := range source {
		entry.Storage = CloneCOSContainer(entry.Storage)
		clone[i] = entry
	}
	return clone
}

/*
================
CloneSlotCubeStorages

Deep copy of a warehouse's top-level cube storage slice.
================
*/
func CloneSlotCubeStorages(source []SlotCubeStorage) []SlotCubeStorage {
	if source == nil {
		return nil
	}
	clone := make([]SlotCubeStorage, len(source))
	for i, entry := range source {
		entry.Storage = CloneCOSContainer(entry.Storage)
		clone[i] = entry
	}
	return clone
}

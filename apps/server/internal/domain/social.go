package domain

import "sync"

// characterPublicationMu protects record fields that can be read by one
// session while another session publishes a replacement. The authority-store
// mutation door remains the outer writer boundary.
var characterPublicationMu sync.Mutex

// FriendRecord is one persisted mutual friend edge.
type FriendRecord struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ModelRefID uint32 `json:"modelRefId"`
}

// FriendMaxCount is the roster bound imposed by the native friend panel.
const FriendMaxCount = 0x14

// FriendsView returns a stable copy for cross-session readers.
func FriendsView(character *Character) []FriendRecord {
	if character == nil {
		return nil
	}
	characterPublicationMu.Lock()
	defer characterPublicationMu.Unlock()

	friends := make([]FriendRecord, len(character.Friends))
	copy(friends, character.Friends)
	return friends
}

// SwapFriends publishes a rebuilt friend list. Callers must already be inside
// the authority-store mutation door.
func SwapFriends(character *Character, next []FriendRecord) {
	if character == nil {
		return
	}
	characterPublicationMu.Lock()
	character.Friends = next
	characterPublicationMu.Unlock()
}

// MissionRuntime contains mutable mission state owned by the character.
type MissionRuntime struct {
	EventGuideStateMask          *int64 `json:"eventGuideStateMask,omitempty"`
	EventGuideStateMaskUpdatedAt string `json:"eventGuideStateMaskUpdatedAt,omitempty"`
}

// ResolveEventGuideStateMask returns the persisted mask or zero.
func ResolveEventGuideStateMask(character *Character) uint32 {
	if character == nil {
		return 0
	}
	characterPublicationMu.Lock()
	defer characterPublicationMu.Unlock()
	return resolveEventGuideStateMaskLocked(character)
}

func resolveEventGuideStateMaskLocked(character *Character) uint32 {
	if character.Mission == nil {
		return 0
	}
	return uint32(coerceInt(
		character.Mission.EventGuideStateMask,
		0,
		0xffffffff,
		0,
	))
}

// Snapshot returns the normalized character view used during enter-world.
func (character *Character) Snapshot() *Character {
	return character.snapshotWith(cloneCharacter)
}

/*
================
SnapshotEquipment

Snapshot's normalized view for readers that never leave the worn
equipment and never read the avatar inventory: the mission inventory
keeps only its equipment rows and AvatarInventory is nil. Each caller
must be cleared for the narrow copy root by root - a reader of bag or
avatar rows would silently see them missing. Companion resolution
walks bag rows - a SnapshotEquipment reader must never resolve
companions.
================
*/
func (character *Character) SnapshotEquipment() *Character {
	return character.snapshotWith(cloneCharacterEquipment)
}

/*
================
SnapshotCombat

Snapshot's normalized view for the combat roots (#1943): the mission
inventory keeps only its equipment rows, and the avatar inventory is
copied whole, because the stat projection reads its option rows. Bag
rows are absent, companions included, so a reader must never resolve
companions or read a bag row from it.
================
*/
func (character *Character) SnapshotCombat() *Character {
	return character.snapshotWith(cloneCharacterCombat)
}

/*
================
SnapshotCast

Snapshot's normalized view for the skill press (#1943): the combat
rows plus every companion-carrying bag row, because the press flow
resolves the caster's companions through its copy while no press
reader reads a plain bag row. Each caller must be cleared for the
copy root by root, as for SnapshotCombat.
================
*/
func (character *Character) SnapshotCast() *Character {
	return character.snapshotWith(cloneCharacterCast)
}

/*
================
SnapshotCompanion

Snapshot's normalized pet-owner view for the per-tick pet and brood
passes (#2030): only the fields the pet chain reads, with the companion
rows and the relation slot of the inventory (cloneCharacterCompanion
lists them). Companion resolution walks exactly the companions the full
copy walks. Each caller must be cleared for the view root by root - a
reader of any other field would silently see it zero.
================
*/
func (character *Character) SnapshotCompanion() *Character {
	return character.snapshotWith(cloneCharacterCompanion)
}

/*
================
snapshotWith

The one normalized-copy walk behind Snapshot and SnapshotEquipment:
the clone runs under the publication lock; the mission mask and the
coerced stat points are stamped after it.
================
*/
func (character *Character) snapshotWith(clone func(*Character) *Character) *Character {
	if character == nil {
		return nil
	}

	characterPublicationMu.Lock()
	snapshot := clone(character)
	mask := int64(resolveEventGuideStateMaskLocked(character))
	mission := MissionRuntime{EventGuideStateMask: &mask}
	if character.Mission != nil {
		mission.EventGuideStateMaskUpdatedAt =
			character.Mission.EventGuideStateMaskUpdatedAt
	}
	characterPublicationMu.Unlock()

	statPoints := coerceInt(snapshot.StatPoints, 0, 0xffff, 0)
	snapshot.StatPoints = &statPoints
	snapshot.Mission = &mission
	return snapshot
}

// MutateMission publishes a replacement mission record. Callers that persist
// characters must invoke it inside the authority-store mutation door.
func MutateMission(
	character *Character,
	mutate func(current *MissionRuntime) MissionRuntime,
) {
	if character == nil || mutate == nil {
		return
	}
	characterPublicationMu.Lock()
	next := mutate(character.Mission)
	character.Mission = &next
	characterPublicationMu.Unlock()
}

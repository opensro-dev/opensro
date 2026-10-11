/*
===========================================================================

avataroptions.go - the worn avatar items' magic options in the keeper

An avatar hat, dress or attachment (TID 3.1.13) carries no stats of its own,
only the magic options the smith grants (item/alchemy/avatar.go): the
MATTR_AVATAR_* rows magicoptionassign.txt assigns to TID 3.1.13 (str, int,
hp, mp, hit and evasion rate, HP/MP recovery, the damage and defence
rates). magicOptionWrites already carries their keeper semantics (stra,
inta, hpa, ...); this owner feeds it the worn avatar rows, which live in
their own container, not in equipment sockets 0..12.

===========================================================================
*/

package combat

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	// avatarSourceBase keys each worn avatar's writes to its socket.
	// INFERENCE: the native keeper keys a write by its owning item; the
	// port numbers equipment 1024+socket (stats.go) and passives from 2048,
	// so avatar sockets take 1088+socket, clear of both.
	avatarSourceBase = 1088

	// The avatar item family, TID 3.1.13.
	avatarTypeID1 = 3
	avatarTypeID2 = 1
	avatarTypeID3 = 13
)

/*
================
avatarOptionWrites

The keeper writes of every worn avatar item's magic options. A row whose
reference is missing or is not an avatar is a broken record and refuses
the projection, as an equipped row does.
================
*/
func avatarOptionWrites(character *domain.Character, items enterworld.ItemRefSource, source enterworld.MagicOptionSource) ([]paramkeeper.Write, error) {
	if character == nil || character.AvatarInventory == nil {
		return nil, nil
	}
	var writes []paramkeeper.Write
	for _, row := range character.AvatarInventory.Rows {
		if len(row.MagicOptions) == 0 {
			continue
		}
		ref, ok := items.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || ref.RefObjID != row.RefObjID {
			return nil, fmt.Errorf("combat: worn avatar %q has no matching v1.150 reference row", row.Codename)
		}
		if ref.TypeIDs[0] != avatarTypeID1 || ref.TypeIDs[1] != avatarTypeID2 || ref.TypeIDs[2] != avatarTypeID3 {
			continue
		}
		options, err := resolveMagicOptions(row.Codename, row.MagicOptions, source)
		if err != nil {
			return nil, err
		}
		optionWrites, err := magicOptionWrites(row.Codename, options, uint32(avatarSourceBase+row.Slot))
		if err != nil {
			return nil, err
		}
		writes = append(writes, optionWrites...)
	}
	return writes, nil
}

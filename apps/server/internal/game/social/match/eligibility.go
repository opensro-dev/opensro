package match

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
)

// PartyApplicant is derived from the current authoritative character snapshot.
// Native 5A1890 selects trained masteries; 5BF62E/5BF64A publish them and
// active job class. The latter is not job membership (client CICUser+782).
type PartyApplicant struct {
	Primary, Secondary uint32
	JobClass           uint8
}

func partyApplicant(character *domain.Character) PartyApplicant {
	out := PartyApplicant{JobClass: activePartyJob(character)}
	out.Primary, out.Secondary = domain.TopMasteries(character.Masteries)
	return out
}

// 868D00/5931B0/5931F0: only the equipped 3.1.7.{1,2,3} job suit
// determines trader/thief/hunter class; everything else is ordinary (4).
func activePartyJob(character *domain.Character) uint8 {
	for _, row := range character.MissionInventory {
		if row.Slot != int64(inventory.SocketSpecialDress) {
			continue
		}
		flags := row.TypeFlags
		if flags&0x7fe == 0x3ac {
			job := uint8(flags >> 11)
			if job >= 1 && job <= 3 {
				return job
			}
		}
	}
	return 4
}

// 63C010, GameServer 5BF240 and ShardManager 44F7F0: the same
// job/purpose matrix gates registration, modification and join admission.
// Native automatic search remains an unrestricted local purpose filter.
func partyPurposeAllowed(job, purpose uint8) bool {
	if purpose > 3 {
		return false
	}
	switch job {
	case 1, 3:
		return purpose == 2
	case 2:
		return purpose == 3
	case 4:
		return purpose < 2
	default:
		return false
	}
}

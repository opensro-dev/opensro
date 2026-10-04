/*
===========================================================================

jobsuit.go - which items are job suits

A job suit is item type 3/1/7 with type 4 naming its job (1 trader,
2 thief, 3 hunter): v1.150 ItemTid_IsTraderJobUniformTid (5931B0) and its
thief and hunter siblings. Worn in socket 8 it is job mode.

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/domain"

// JobSuitSlot is the job suit's equipment socket.
const JobSuitSlot = 8

/*
================
JobSuitJob

The job of a packed item type word's suit, or 0 for any other item.
================
*/
func JobSuitJob(typeFlags uint16) uint8 {
	tid1, tid2, tid3, tid4 := typeFlags>>2&7, typeFlags>>5&3, typeFlags>>7&15, typeFlags>>11&31
	if tid1 != 3 || tid2 != 1 || tid3 != 7 || tid4 < uint16(domain.JobTrader) || tid4 > uint16(domain.JobHunter) {
		return 0
	}
	return uint8(tid4)
}

/*
================
DressedJob

The job a character's worn suit puts it in, or 0 outside job mode.
================
*/
func DressedJob(c *Character) uint8 {
	if c == nil {
		return 0
	}
	for _, row := range c.MissionInventory {
		if row.Slot == JobSuitSlot {
			return JobSuitJob(row.TypeFlags)
		}
	}
	return 0
}

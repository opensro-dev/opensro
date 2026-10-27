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

// NoJobClass is the job class of a character outside job mode (856920;
// the server's job-state read agrees).
const NoJobClass uint8 = 4

/*
================
PartyJobClass

The active job class a party compares (client 856920, CICharactor
+0x4F5): the dressed job, or 4 outside job mode.
================
*/
func PartyJobClass(c *Character) uint8 {
	if job := DressedJob(c); job != 0 {
		return job
	}
	return NoJobClass
}

/*
================
PartyMemberName

The name a party roster row carries (the server's party-member snapshot):
the job alias (the server's nickname read) while the character is
dressed in a job suit, its own name otherwise. The native writes the empty
alias of a suited character with no alias and minidumps; the port keeps the
character name instead (port-only, not native: never send an empty name).
================
*/
func PartyMemberName(c *Character) string {
	if c == nil {
		return ""
	}
	if DressedJob(c) != 0 && c.Job.Alias != "" {
		return c.Job.Alias
	}
	return c.Name
}

/*
================
JobsMayParty

The shard manager's job table: traders and hunters (1, 3) party together, thieves
(2) only with thieves, and players outside job mode (4) only with each
other. Its forming and joining checks refuse any other pair.
================
*/
func JobsMayParty(a, b uint8) bool {
	switch a {
	case 1, 3:
		return b == 1 || b == 3
	case 2:
		return b == 2
	case 4:
		return b == 4
	}
	return false
}

/*
===========================================================================

freepvpdress.go - the free-PvP cape's dress bar

Port-only, not native until traced (SRO_FREE_PVP_DRESS_BAR, off by
default): the free-PvP cape (TID 3.1.7.5, ITEM_*_FRPVP_VOUCHER_*) shares
the job suit's socket (slot 8, playerspawnrow.go) but not its rules, so
jobSuitMove never sees it and the cape went on and off at once. With the
flag on, wearing or removing the cape runs the job suit's ten-second
state changer (jobdress.go): the same overhead bar for every observer,
the same cancel when the wearer walks (cancelJobDressOnMove), and the move
carried out when the timer ends. Only the timing is shared: the cape takes
none of the job suit's admission (alias, party, job grade) and changes no
job mode, so the completion checks only that the wearer still lives, and
the ordinary equip step validates the move.

INFERENCE: the v1.150 client draws this bar for any gid
(CICUser_SetActionProgressDurationSeconds, 0x3434) and the cape and the
suits share their socket and record family; the reference server's state
changer for the cape was not traced, so the rule ships behind a flag.

===========================================================================
*/

package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvFreePvpDressBar turns the cape's dress bar on ("on", "1", "true").
const EnvFreePvpDressBar = "SRO_FREE_PVP_DRESS_BAR"

// freePvpCapeTid4 is the free-PvP cape's fourth type id inside the job
// suits' family (3.1.7.*): 1..3 are the trader, thief and hunter suits.
const freePvpCapeTid4 uint16 = 5

/*
================
FreePvpDressBarFromEnv
================
*/
func FreePvpDressBarFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvFreePvpDressBar))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
isFreePvpCape
================
*/
func isFreePvpCape(typeFlags uint16) bool {
	tid1, tid2, tid3, tid4 := typeFlags>>2&7, typeFlags>>5&3, typeFlags>>7&15, typeFlags>>11&31
	return tid1 == 3 && tid2 == 1 && tid3 == 7 && tid4 == freePvpCapeTid4
}

/*
================
freePvpCapeMove

Whether an inventory move wears the cape into the suit socket or takes it
out of the socket.
================
*/
func freePvpCapeMove(c *enterworld.Character, request wire.ItemMoveRequest) bool {
	if request.MovementType != wire.MoveTypeInventory {
		return false
	}
	if request.DestSlot == jobSuitSlot {
		row, ok := inventoryRowAt(c, request.SourceSlot)
		return ok && isFreePvpCape(row.TypeFlags)
	}
	if request.SourceSlot == jobSuitSlot {
		row, ok := inventoryRowAt(c, jobSuitSlot)
		return ok && isFreePvpCape(row.TypeFlags)
	}
	return false
}

/*
================
beginFreePvpDress

Starts the cape's timer and shows its bar; the move's own answer waits
for the timer (advanceJobDresses, completeJobDressInDivision).
================
*/
func (rt *Runtime) beginFreePvpDress(division string, c *enterworld.Character, request wire.ItemMoveRequest) OpResult {
	now := rt.Now().UnixMilli()
	if rt.jobDressPending(division, c.Name) {
		return failureResult(jobWearErrPending)
	}
	key := simulation.WorldKey(division, c.Name)
	rt.jobDresses.Store(key, jobDress{division: division, character: c, request: request,
		due: now + jobDressSeconds*1000, cape: true})
	bar := jobDressBar(c, jobDressSeconds)
	return OpResult{Frames: []wire.Frame{bar}, Broadcast: []wire.Frame{bar}}
}

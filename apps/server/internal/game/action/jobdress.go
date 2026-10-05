/*
===========================================================================

jobdress.go - putting a job suit on and taking it off

A job suit (3/1/7/{1 trader, 2 thief, 3 hunter}) changes job mode, so the
move that wears or removes it is not immediate. v1.188's
CGObjPC_AttachJobSuit (524950) and DetachJobSuit (524B20) admit the move,
remember it, start a ten-second state changer (CGObjPC_SetStateFlag13,
job kind 0xE) and answer nothing yet (0x189B); every observer sees the
overhead dress bar (v1.150 0x3434 [u32 gid][2][2][u8 seconds],
CICUser_SetActionProgressDurationSeconds). When the timer runs out
(AJStateChanger_Advance case 0xE -> 4F0C90) the remembered move is
validated again and carried out, and its 0xB06D answer goes out then.

Wearing admits through CGItemEquip_Equip (497630) and the game rule
(CGameRule_CheckJobSuitWear 5297E0), notice category 1:

  - no dress in progress (0x47), the socket empty (0x46), not riding (0xCA);
  - the suit's job is the player's (0x9E), the player has an alias (0x9F),
    and is in no party (0xA0);
  - the ordinary equip requirements, and the suit's job grade (0xA3).

Taking it off refuses in battle (0x48) or with a full bag (0x07). A
player who dies or travels before the timer ends keeps the old outfit.

INFERENCE: v1.188 tests the suit's job grade from its own record layout;
v1.150 carries it as requirement type 2..4 (the job grades the tooltip
shows), checked here against the member's grade. v1.188 refuses a
mounted wearer with the code v1.150 names WEARLIMIT and gives RIDE to its
cart check; the port answers a mounted wearer with RIDE, the v1.150 text
for that case.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opJobDressBar uint16 = 0x3434

	// jobSuitSlot is the job suit's equipment socket.
	jobSuitSlot uint8 = enterworld.JobSuitSlot
	// jobDressSeconds is SetStateFlag13's 10.0 s state changer.
	jobDressSeconds = 10
	// jobDressKind and jobDressStep are 0x3434's two lead bytes.
	jobDressKind uint8 = 2
	jobDressStep uint8 = 2

	// Category 1 notices (v1.188 0x18xx low bytes).
	jobWearErrBagFull    uint8 = 0x07
	jobWearErrSwap       uint8 = 0x46
	jobWearErrPending    uint8 = 0x47
	jobWearErrBattle     uint8 = 0x48
	jobWearErrNoJob      uint8 = 0x9E
	jobWearErrNoAlias    uint8 = 0x9F
	jobWearErrParty      uint8 = 0xA0
	jobWearErrGradeLimit uint8 = 0xA3
	jobWearErrRide       uint8 = 0xCA
)

/*
================
jobDress

A suit move waiting for its timer: the original request and when it is
carried out.
================
*/
type jobDress struct {
	division  string
	character *enterworld.Character
	request   wire.ItemMoveRequest
	due       int64
}

// errCodeCantActivateCart is UIIT_MSG_STRGERR_YOU_CANT_ACTIVATE_CART.
const errCodeCantActivateCart uint8 = 0x49

/*
================
transportJob

INFERENCE: a trade transport (COS band 2) answers only a trader or thief
in job mode. Neither binary's summoner shows the test this port could
read; v1.150 keeps the refusal text (UIIT_MSG_STRGERR_YOU_CANT_ACTIVATE_CART)
and retail trade animals were job-mode vehicles.
================
*/
func transportJob(c *enterworld.Character) bool {
	job := enterworld.DressedJob(c)
	return job == domain.JobTrader || job == domain.JobThief
}

/*
================
inventoryRowAt
================
*/
func inventoryRowAt(c *enterworld.Character, slot uint8) (enterworld.InventoryRow, bool) {
	for _, row := range c.MissionInventory {
		if row.Slot == int64(slot) {
			return row, true
		}
	}
	return enterworld.InventoryRow{}, false
}

/*
================
jobDressed

True while a job suit is worn: job mode.
================
*/
func (rt *Runtime) jobDressed(c *enterworld.Character) bool {
	return enterworld.DressedJob(c) != 0
}

/*
================
jobDressPending
================
*/
func (rt *Runtime) jobDressPending(division, name string) bool {
	_, ok := rt.jobDresses.Load(simulation.WorldKey(division, name))
	return ok
}

/*
================
jobSuitMove

True when the inventory move wears or removes a job suit.
================
*/
func jobSuitMove(c *enterworld.Character, request wire.ItemMoveRequest) bool {
	if request.MovementType != wire.MoveTypeInventory {
		return false
	}
	if request.DestSlot == jobSuitSlot {
		row, ok := inventoryRowAt(c, request.SourceSlot)
		return ok && enterworld.JobSuitJob(row.TypeFlags) != 0
	}
	if request.SourceSlot == jobSuitSlot {
		row, ok := inventoryRowAt(c, jobSuitSlot)
		return ok && enterworld.JobSuitJob(row.TypeFlags) != 0
	}
	return false
}

/*
================
jobSuitGrade

The job grade a suit requires (its requirement of type 2..4), or 0.
================
*/
func jobSuitGrade(ref *enterworld.ItemRef) int64 {
	grade := int64(0)
	for i, kind := range ref.ReqQuadTypes {
		if kind >= 2 && kind <= 4 {
			grade = max(grade, ref.ReqQuadValues[i])
		}
	}
	return grade
}

/*
================
jobWearRefusal

The refusal for wearing the suit in source, or 0.
================
*/
func (rt *Runtime) jobWearRefusal(division string, c *enterworld.Character, source uint8) uint8 {
	if _, worn := inventoryRowAt(c, jobSuitSlot); worn {
		return jobWearErrSwap
	}
	if mountedOnCOS(c) {
		return jobWearErrRide
	}
	row, _ := inventoryRowAt(c, source)
	switch {
	case enterworld.JobSuitJob(row.TypeFlags) != c.Job.Type:
		return jobWearErrNoJob
	case c.Job.Alias == "":
		return jobWearErrNoAlias
	case len(rt.auraParty(division, c)) != 0:
		return jobWearErrParty
	}
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(row.Codename)
	if !ok || ref == nil {
		return jobWearErrNoJob
	}
	if jobSuitGrade(ref) > int64(c.Job.Grade) {
		return jobWearErrGradeLimit
	}
	// The ordinary equip gates (level, sex, country) on a trial move.
	inv := inventory.New(invItemsFromRows(c.MissionInventory))
	inv.Requirements = equipRequirements(rt.deps.ItemReferences(), c, rt.FortressGuildRole)
	if _, fault := inv.Transfer(source, jobSuitSlot, 1, 1); fault != nil {
		return fault.Code
	}
	return 0
}

/*
================
jobStripRefusal

The refusal for taking the worn suit off, or 0.
================
*/
func (rt *Runtime) jobStripRefusal(c *enterworld.Character, dest uint8, now int64) uint8 {
	if c.BattleUntilMs > now {
		return jobWearErrBattle
	}
	if _, taken := inventoryRowAt(c, dest); taken || !inventory.IsBagSlot(dest) {
		return jobWearErrBagFull
	}
	return 0
}

/*
================
jobDressBar
================
*/
func jobDressBar(c *enterworld.Character, seconds uint8) wire.Frame {
	payload := wire.NewWriter(7).U32(enterworld.ObjectIDForCharacter(c)).U8(jobDressKind).U8(jobDressStep).U8(seconds).Payload()
	return wire.Frame{Opcode: opJobDressBar, Payload: payload}
}

/*
================
beginJobDress

Admits a suit move and starts its timer. The move's own answer waits for
the timer (advanceJobDresses).
================
*/
func (rt *Runtime) beginJobDress(division string, c *enterworld.Character, request wire.ItemMoveRequest) OpResult {
	now := rt.Now().UnixMilli()
	key := simulation.WorldKey(division, c.Name)
	if rt.jobDressPending(division, c.Name) {
		return failureResult(jobWearErrPending)
	}
	var code uint8
	if request.DestSlot == jobSuitSlot {
		code = rt.jobWearRefusal(division, c, request.SourceSlot)
	} else {
		code = rt.jobStripRefusal(c, request.DestSlot, now)
	}
	if code != 0 {
		return failureResult(code)
	}
	rt.jobDresses.Store(key, jobDress{division: division, character: c, request: request, due: now + jobDressSeconds*1000})
	bar := jobDressBar(c, jobDressSeconds)
	return OpResult{Frames: []wire.Frame{bar}, Broadcast: []wire.Frame{bar}}
}

/*
================
advanceJobDresses

Carries out every suit move whose timer ran out: 4F0C90 validates the
remembered move again and answers it.
================
*/
func (rt *Runtime) advanceJobDresses(now int64) {
	var due []jobDress
	rt.jobDresses.Range(func(key, value any) bool {
		dress := value.(jobDress)
		if dress.due <= now {
			rt.jobDresses.Delete(key)
			due = append(due, dress)
		}
		return true
	})
	for _, dress := range due {
		c := dress.character
		if c == nil || rt.findCharacter(dress.division, c.Name) != c {
			continue
		}
		unlock := rt.lockDivision(dress.division)
		// A suit move does not outlive its owner's death.
		code := jobWearErrPending
		switch {
		case !enterworld.CharacterAlive(c):
		case dress.request.DestSlot == jobSuitSlot:
			code = rt.jobWearRefusal(dress.division, c, dress.request.SourceSlot)
		default:
			code = rt.jobStripRefusal(c, dress.request.DestSlot, now)
		}
		result := failureResult(code)
		if code == 0 {
			result = rt.applyInventoryMove(dress.division, c, dress.request)
		}
		unlock()
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(dress.division, c.Name, result.Frames)
		}
	}
}

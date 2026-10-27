/*
===========================================================================

jobdress.go - putting a job suit on and taking it off

A job suit (3/1/7/{1 trader, 2 thief, 3 hunter}) changes job mode, so the
move that wears or removes it is not immediate. The reference server's
suit attach and detach admit the move,
remember it, start a ten-second state changer (state flag 13,
job kind 0xE) and answer nothing yet (0x189B); every observer sees the
overhead dress bar (v1.150 0x3434 [u32 gid][2][2][u8 seconds],
CICUser_SetActionProgressDurationSeconds). When the timer runs out
(state changer case 0xE) the remembered move is
validated again and carried out, and its 0xB06D answer goes out then.

Wearing admits through the native equip step and the game rule
for job suits, notice category 1:

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
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opJobDressBar uint16 = 0x3434

	// jobSuitSlot is the job suit's equipment socket.
	jobSuitSlot uint8 = enterworld.JobSuitSlot
	// jobDressSeconds is the native 10.0 s state changer.
	jobDressSeconds = 10
	// jobActivationMs is +0x2178 = 10, counted down once a second
	// by the native state-timer tick.
	jobActivationMs = 10 * 1000
	// jobDressKind and jobDressStep are 0x3434's two lead bytes.
	jobDressKind uint8 = 2
	jobDressStep uint8 = 2

	// Category 1 notices (v1.188 0x18xx low bytes).
	jobWearErrBagFull uint8 = 0x07
	jobWearErrSwap    uint8 = 0x46
	jobWearErrPending uint8 = 0x47
	jobWearErrBattle  uint8 = 0x48
	// UIIT_MSG_STRGERR_CANT_DETACH_FREEBATTLEITEM_IN_BATTLESTATE.
	capeStripErrBattle   uint8 = 0x77
	jobWearErrCart       uint8 = 0x5A
	jobWearErrNoPosition uint8 = 0xA2
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

// errCodeThiefCartInTown is UIIT_MSG_STRGERR_ROBBER_CANT_SUMMON_CART_IN_TOWN
// (category 1, code 149): "a thief cannot summon a transport in a town".
const errCodeThiefCartInTown uint8 = 0x95

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
	inv := bagOf(c)
	inv.Requirements = equipRequirements(rt.deps.ItemReferences(), c, rt.FortressGuildRole)
	if _, fault := inv.Transfer(source, jobSuitSlot, 1, 1); fault != nil {
		return fault.Code
	}
	return 0
}

/*
================
jobStripRefusal

The refusal for taking the worn suit off, or 0, in the native unequip
check's order: riding (0xCA), battle (0x48), a summoned
cart (0x5A), the job activation wait (0x47),
a party (0xA0), standing where players may fight (0xA2); then the bag.
Native's first test (state +6 == 7, 0x18CD) names a v1.188 state the
v1.150 tables give no text for, so it is not ported.
================
*/
func (rt *Runtime) jobStripRefusal(division string, c *enterworld.Character, dest uint8, now int64) uint8 {
	switch {
	case mountedOnCOS(c):
		return jobWearErrRide
	case c.BattleUntilMs > now:
		return jobWearErrBattle
	case rt.summonedVehicle(c):
		return jobWearErrCart
	case rt.jobActivationPending(division, c, now):
		return jobWearErrPending
	case len(rt.auraParty(division, c)) != 0:
		return jobWearErrParty
	}
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	if battlefield, known := worldgeom.RegionPlayerCombat(at.RegionID); !known || battlefield {
		return jobWearErrNoPosition
	}
	if _, taken := inventoryRowAt(c, dest); taken || !inventory.IsBagSlot(dest, inventory.BagEnd(c)) {
		return jobWearErrBagFull
	}
	return 0
}

/*
================
townRegion

A town is a region where players may not fight (the safe zones).
================
*/
func townRegion(regionID uint16) bool {
	allowed, known := worldgeom.RegionPlayerCombat(regionID)
	return known && !allowed
}

/*
================
ThiefCargoTownBlocked

#721, the movement side of the stolen-goods rule: a thief in job mode
whose transport is summoned may not cross from a battlefield region into a
protected one. The reference server's COS region admission refuses a
COS's region change only when its owner's job state is thief (2), the
region it leaves has IsBattleField set and the one it enters has it clear
(region record +2): moving within a protected region, the Thieves' Den
included, is never refused. Thief town is one isolated protected region,
reached through its gates, so a thief walks it freely to sell; re-testing
the destination alone (#726) refused every step there. Movement asks this
rule for every region change a walk makes (thiefCargoCrossingBlocked), as
native tests each step the transport takes. INFERENCE: native refuses the
transport's own transition; the port refuses the owner's move, which the
transport follows.
================
*/
func (rt *Runtime) ThiefCargoTownBlocked(c *enterworld.Character, fromRegion, toRegion uint16) bool {
	return enterworld.DressedJob(c) == domain.JobThief && rt.summonedVehicle(c) &&
		battlefieldRegion(fromRegion) && townRegion(toRegion)
}

/*
================
battlefieldRegion

A region whose _RefRegion.IsBattleField is set: players may fight there.
================
*/
func battlefieldRegion(regionID uint16) bool {
	allowed, known := worldgeom.RegionPlayerCombat(regionID)
	return known && allowed
}

/*
================
summonedVehicle

Natively a summoned cart or trade animal (vtable +0x2EC).
================
*/
func (rt *Runtime) summonedVehicle(c *enterworld.Character) bool {
	pet := c.ActiveCOS
	if pet == nil || !pet.Summoned {
		return false
	}
	ref, ok := rt.cosReference(pet)
	return ok && isVehicleCOS(ref.TidWord)
}

/*
================
startJobActivation

A worn suit is not active at once: +0x2178 = 10 (when the suit's
equip result 0x3038 goes out) holds the suit's removal (0x47) for ten
state ticks. Job attacks do not wait for it (skillrelations.go).
================
*/
func (rt *Runtime) startJobActivation(division string, c *enterworld.Character, now int64) {
	rt.jobActivations.Store(simulation.WorldKey(division, c.Name), now+jobActivationMs)
}

/*
================
jobActivationPending
================
*/
func (rt *Runtime) jobActivationPending(division string, c *enterworld.Character, now int64) bool {
	end, ok := rt.jobActivations.Load(simulation.WorldKey(division, c.Name))
	return ok && end.(int64) > now
}

/*
================
endJobActivation

Natively a hit taken ends the wait
(entering battle on being attacked). Its 0x30CF broadcast has no v1.150
handler, so nothing is sent.
================
*/
func (rt *Runtime) endJobActivation(division string, c *enterworld.Character) {
	rt.jobActivations.Delete(simulation.WorldKey(division, c.Name))
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
		code = rt.jobStripRefusal(division, c, request.DestSlot, now)
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
reloadJobDress

Re-enters the wearer in place after a suit goes on or comes off, so every
client re-reads the job class and alias.

INFERENCE (no direct call site found): the v1.150 client reads a
character's job class and alias only when it spawns. ItemRecord_GetJobSuitClass
(868D00) has exactly two callers, CICPlayer_OnSpawnInitialize (869160) and
CICUser_DeserializeAppearanceSpawnData (86AFB0), and the equip handler
CharacterDependentData_ApplyEquipItemSlot (596190) only raises event-guide
triggers for a suit. Without a re-spawn, the native wearer would keep the
plain name until the next teleport, which is what players report and not
what they remember from the original: a teleport right after suiting up.
The reference server's dress path (suit attach, suit move completion,
state flag 13) shows no teleport call, so the re-spawn sits behind
an item virtual this audit did not reach. The server's own in-place reload
is the skin change's: its model change sets teleport mode 3 and schedules
the restore after 1.0 s, which this reuses.
================
*/
func (rt *Runtime) reloadJobDress(division string, c *enterworld.Character, now int64, result *OpResult) {
	key := simulation.WorldKey(division, c.Name)
	here := travelPoint{spawn: rt.liveSpawn(key, c, now), world: instance.ID(domain.CharacterWorldInstance(c))}
	var reload OpResult
	if !rt.deps.Update(c, "job-dress-reload", func() bool {
		return rt.startReturnCast(returnCast{division: division, character: c, row: -1, duration: skinReloadMs,
			destination: &here, now: now, mode: skinTeleportMode}, &reload)
	}) {
		return
	}
	result.Frames = append(result.Frames, reload.Frames...)
	result.Broadcast = append(result.Broadcast, reload.Broadcast...)
}

/*
================
advanceJobDresses

Carries out every suit move whose timer ran out: the native completion validates the
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
		result := rt.completeJobDressInDivision(dress, c, now)
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(dress.division, c.Name, result.Frames)
		}
		if len(result.Broadcast) != 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(dress.division, c.Name, result.Broadcast)
		}
	}
}

/*
================
completeJobDressInDivision
================
*/
func (rt *Runtime) completeJobDressInDivision(dress jobDress, c *enterworld.Character, now int64) OpResult {
	unlock := rt.lockDivision(dress.division)
	defer unlock()
	// A suit move does not outlive its owner's death.
	code := jobWearErrPending
	switch {
	case !enterworld.CharacterAlive(c):
	case dress.request.DestSlot == jobSuitSlot:
		code = rt.jobWearRefusal(dress.division, c, dress.request.SourceSlot)
	default:
		code = rt.jobStripRefusal(dress.division, c, dress.request.DestSlot, now)
	}
	result := failureResult(code)
	if code == 0 {
		dressed := rt.jobDressed(c)
		result = rt.applyInventoryMove(dress.division, c, dress.request)
		if dress.request.DestSlot == jobSuitSlot && rt.jobDressed(c) {
			rt.startJobActivation(dress.division, c, now)
		}
		if rt.jobDressed(c) != dressed {
			rt.reloadJobDress(dress.division, c, now, &result)
		}
		// Port-only, not native: taking the suit off clears the streak.
		if dressed && !rt.jobDressed(c) && c.MostWanted != nil {
			var private, public []wire.Frame
			rt.deps.Update(c, "most-wanted-undress", func() bool {
				private, public = rt.clearMostWantedInDoor(dress.division, c, "suit removed", now)
				return true
			})
			result.Frames = append(result.Frames, private...)
			result.Broadcast = append(result.Broadcast, public...)
		}
	}
	return result
}

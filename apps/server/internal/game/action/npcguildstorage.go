/*
===========================================================================

npcguildstorage.go - the guild warehouse at the guild manager

The warehouse row's 0x7338 [npc][0x4000] answers B338 lock 0x4000, which
makes the client ask to open the room (CPSMission_OnNpcInteractionResponse
0xB338 -> 0x7515). One member uses it at a time:

	0x7515 [u32 npc]   open   -> 0xB515 [1] | [2][code] | [2][0x48][str user]
	0x733D [u32 npc]   list   -> 0x34A9 gold, 0x3363 rows, 0xB33D [1]
	0x7428 [u32 npc]   close  -> 0xB428 [1]

then the five guild move types (item/wire/storage.go) move items and gold
through the guild's room in one store commit each. v1.188 is 0x7250 open
(reference server: 0x0D outside a guild, 0x4A below level 2, the master or member
permission 8 else 0x1E, then the lock job 0x24), 0x7252 list (level 2,
the lock's holder, job 5) and 0x7251 close. The holder is runtime state:
it ends with the close or when the holder leaves the world.

===========================================================================
*/

package action

import (
	"errors"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opGuildStorageOpen   uint16 = 0x7515
	opGuildStorageOpened uint16 = 0xb515
	opGuildStorageList   uint16 = 0x733d
	opGuildStorageListed uint16 = 0xb33d
	opGuildStorageClose  uint16 = 0x7428
	opGuildStorageClosed uint16 = 0xb428

	// guildStorageFunction is the warehouse row's 0x7338 mask and B338 lock.
	guildStorageFunction uint32 = 0x4000

	// The warehouse refusals (category 0x10): 0x4A below level 2, 0x48 in
	// another member's hands (B515 then names them).
	guildStorageErrLevel uint8 = 0x4a
	guildStorageErrInUse uint8 = 0x48
)

// guildStorageKey names one guild's warehouse.
type guildStorageKey struct {
	division string
	guild    int64
}

/*
================
guildStorageMember

The acting member's guild and whether they may use its warehouse; code is
the refusal otherwise.
================
*/
func (rt *Runtime) guildStorageMember(division string, c *enterworld.Character) (int64, uint8) {
	store := rt.deps.GuildAuthority()
	if store == nil {
		return 0, guildNpcRefused
	}
	guildID, member := store.GuildOfCharacter(division, c.ID)
	record, members, found := store.Guild(division, guildID)
	if !member || !found {
		return 0, guild.GuildErrNotMember
	}
	if record.Level < domain.GuildStorageMinLevel {
		return 0, guildStorageErrLevel
	}
	for _, row := range members {
		if row.CharID == c.ID && row.Grade != 0 && row.PermMask&guild.PermMaskStorage == 0 {
			return 0, guild.GuildErrPermissionDenied
		}
	}
	return guildID, 0
}

/*
================
guildStorageHolder

Whoever holds a guild's warehouse while still in the world.
================
*/
func (rt *Runtime) guildStorageHolder(key guildStorageKey) (string, bool) {
	value, held := rt.guildStorageUsers.Load(key)
	if !held {
		return "", false
	}
	name := value.(string)
	if _, online := rt.characterAdmissions.Load(simulation.WorldKey(key.division, name)); !online {
		rt.guildStorageUsers.CompareAndDelete(key, name)
		return "", false
	}
	return name, true
}

/*
================
HandleGuildStorageOpen

0x7515: the reference server's admission, then the room's single-user lock.
================
*/
func (rt *Runtime) HandleGuildStorageOpen(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if c == nil || err != nil || r.Done() != nil {
		return guildNpcAnswer(opGuildStorageOpened, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildStorageOpened, guildNpcRefused)
	}
	guildID, code := rt.guildStorageMember(division, c)
	if code != 0 {
		return guildNpcAnswer(opGuildStorageOpened, code)
	}
	key := guildStorageKey{division: division, guild: guildID}
	if holder, held := rt.guildStorageHolder(key); held && holder != c.Name {
		w := wire.NewWriter(4 + len(holder)).U8(2).U8(guildStorageErrInUse).U16(uint16(len(holder)))
		w.Bytes([]byte(holder))
		return OpResult{Frames: []wire.Frame{{Opcode: opGuildStorageOpened, Payload: w.Payload()}}}
	}
	rt.guildStorageUsers.Store(key, c.Name)
	rt.openStorageSession(division, c.Name, gid)
	return guildNpcAnswer(opGuildStorageOpened, 0)
}

/*
================
guildStorageHoldCode

Why the acting member does not hold their guild's warehouse for this
move, as the 0xB06D code the client names (#3568); 0 when they hold
it. Every lost hold within the warehouse flow answers the blocked
code - the warehouse session ended, the manager is gone or is no guild
manager, the membership or its storage permission went, another member
holds the room - except an item move whose keeper stands beyond the
hit range, which answers the personal warehouse's range code. Gold
moves take no range test, as at the personal warehouse
(applyStorageMove). A move with no warehouse session and no guild
behind it is a shape the client cannot compose and keeps the generic
code, as the forge-bound gate's fall-through pins. The
guildStorageMember category codes are the 0xB515 lane's and are not
reused here: on this lane the same bytes would name other texts. The
caller holds the division lock.
================
*/
func (rt *Runtime) guildStorageHoldCode(division string, c *enterworld.Character, gid uint32, itemMove bool) uint8 {
	// The manager the warehouse was opened at, not the current selection:
	// re-selecting it, or anything else, leaves the held room usable.
	if open, ok := rt.storageSession(division, c.Name); !ok || open != gid {
		if _, code := rt.guildStorageMember(division, c); code != 0 {
			return wire.ErrCodeInvalidRequest
		}
		return wire.ErrCodeStorageOperationBlocked
	}
	npc, live := rt.npcForCurrentViewer(division, c, gid)
	if !live || !npc.Services.Has(simulation.NpcServiceGuild) {
		return wire.ErrCodeStorageOperationBlocked
	}
	if itemMove && !rt.npcWithinHitRange(division, c, npc) {
		return hitRangeTooFar
	}
	guildID, code := rt.guildStorageMember(division, c)
	if code != 0 {
		return wire.ErrCodeStorageOperationBlocked
	}
	holder, held := rt.guildStorageHolder(guildStorageKey{division: division, guild: guildID})
	if !held || holder != c.Name {
		return wire.ErrCodeStorageOperationBlocked
	}
	return 0
}

/*
================
holdsGuildStorage

Whether the acting member holds their guild's warehouse at the manager.
The caller holds the division lock.
================
*/
func (rt *Runtime) holdsGuildStorage(division string, c *enterworld.Character, gid uint32) bool {
	return rt.guildStorageHoldCode(division, c, gid, true) == 0
}

/*
================
HandleGuildStorageList

0x733D: the room's gold and rows, then the answer the window waits on.
================
*/
func (rt *Runtime) HandleGuildStorageList(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	store := rt.deps.GuildAuthority()
	if c == nil || err != nil || r.Done() != nil || store == nil {
		return guildNpcAnswer(opGuildStorageListed, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.holdsGuildStorage(division, c, gid) {
		return guildNpcAnswer(opGuildStorageListed, guildStorageErrInUse)
	}
	room, refusal, err := store.TransactGuildStorageAs(division, c.ID, nil)
	if refusal.Refused() || err != nil {
		return guildNpcAnswer(opGuildStorageListed, guildRefusalCode(refusal))
	}
	list, err := rt.storageListPayload(room)
	if err != nil {
		log.Warnf("guild storage: %s list: %v", c.Name, err)
		return guildNpcAnswer(opGuildStorageListed, guildNpcRefused)
	}
	return OpResult{Frames: []wire.Frame{
		rt.commerceReferences(invItemsFromRowsWithin(room.Rows, room.Capacity), nil),
		{Opcode: wire.OpGuildStorageGold, Payload: wire.EncodeStorageGold(uint64(room.Gold))},
		{Opcode: wire.OpGuildStorageList, Payload: list},
		{Opcode: opGuildStorageListed, Payload: []byte{1}},
	}}
}

/*
================
HandleGuildStorageClose

0x7428: the holder lets the room go.
================
*/
func (rt *Runtime) HandleGuildStorageClose(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	if _, err := r.U32(); c == nil || err != nil || r.Done() != nil {
		return guildNpcAnswer(opGuildStorageClosed, guildNpcRefused)
	}
	rt.releaseGuildStorage(division, c.Name)
	return guildNpcAnswer(opGuildStorageClosed, 0)
}

/*
================
releaseGuildStorage

Every warehouse a character holds in the division.
================
*/
func (rt *Runtime) releaseGuildStorage(division, name string) {
	rt.closeStorageSession(division, name)
	rt.guildStorageUsers.Range(func(key, value any) bool {
		if key.(guildStorageKey).division == division && value.(string) == name {
			rt.guildStorageUsers.CompareAndDelete(key, value)
		}
		return true
	})
}

/*
================
applyGuildStorageMove

The five guild move types: the holder's moves at the manager, one store
commit each, answered as the personal warehouse's are - gold takes no
range test, and a member who no longer holds the room reads the
blocked code instead of the silent generic one (#3568).
================
*/
func (rt *Runtime) applyGuildStorageMove(division string, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	store := rt.deps.GuildAuthority()
	personal, _ := wire.PersonalStorageMove(q.MovementType)
	goldMove := personal == wire.MoveTypeStorageGoldDeposit || personal == wire.MoveTypeStorageGoldWithdraw
	gid := q.NpcGID
	if goldMove {
		gid, _ = rt.storageSession(division, c.Name)
	}
	// Port-only (forge.go, issue #1980): a bound forged item never enters
	// the GUILD warehouse - another member's withdraw would move it into
	// the economy. The personal warehouse stays open to it (the owner's
	// own round-trip), so the refusal lives here, not in storableItem,
	// and ahead of the session gates: the caller's own bag is no secret.
	if personal == wire.MoveTypeStorageDeposit {
		if snapshot := rt.characterSnapshot(division, c); snapshot != nil {
			if row, present := bagOf(snapshot).At(q.SourceSlot); present && row.ForgeBound {
				return failureResult(errCodeInvalidStorageTarget)
			}
		}
	}
	if code := rt.guildStorageHoldCode(division, c, gid, !goldMove); code != 0 {
		return failureResult(code)
	}
	if store == nil {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	_, refusal, err := store.TransactGuildStorageAs(division, c.ID, rt.storageMutation(personal, q))
	var moveRefusal storageRefusal
	switch {
	case errors.As(err, &moveRefusal):
		return failureResult(uint8(moveRefusal))
	case err != nil:
		log.Warnf("guild storage: %s move 0x%02X failed: %v", c.Name, q.MovementType, err)
		return failureResult(wire.ErrCodeInvalidRequest)
	case refusal.Refused():
		// The store's own membership answer a breath after the hold
		// check passed: blocked, not malformed.
		return failureResult(wire.ErrCodeStorageOperationBlocked)
	}
	frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodeStorageMoveSuccess(q)}}
	if personal != wire.MoveTypeStorage {
		frames = append(frames, goldFrame(c))
	}
	var questFrames []wire.Frame
	rt.deps.Update(c, "guild-storage-quest-inventory", func() bool {
		questFrames = rt.updateQuestInventory(c)
		return len(questFrames) > 0
	})
	return OpResult{Frames: append(frames, questFrames...)}
}

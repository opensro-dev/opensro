/*
===========================================================================

buff.go - the Academy cumulated buff: earn, show, expire, pass (#2270)

A camp member's saved buff (magic-state slot 7 on the client's board)
and the buffs students pass to the Guardian (slot 8's received rows).
The native rules, read from the reference server's kill-experience
grant and its per-minute buff-time updates:

  - A student below level 40 accumulates points on every monster kill
    the grant path pays: min(30, trunc(10 * exp / monsterExp)) per
    kill. Sub-mentors (kind 1) neither earn nor drain; the master
    earns no points but drains received rows like a student.
  - The saved buff lives in a 24 h (1440-minute) window stamped
    once, by the first-grant show (state 0); the entry show and the
    per-minute refresh read the stamp without moving it. The refresh
    goes out only when the remaining minutes cross a multiple of 10
    or fall to 10 or less. At zero the state-7 send prints the
    client's time-over line and closes the window - the value is
    gone, and the next earned kill is a first grant again.
  - While a received row is held, each kill grant drains 10 points
    off the first row (floor 0) and the drained share feeds the
    grant's bonus: grant * drained / 100, capped at 30 % of the
    grant. A row drained (or found) empty is removed with a mode-5
    list frame; a row whose window runs out is removed the same way.
  - Passing (0x700C, the client's slot-7 confirm) moves the whole
    saved value onto the Guardian as a received row. Refusals the
    native server would compute in its camp manager are modelled on
    the client's own gates (see handlePassBuff).

State lives in the authority store on the member record (schema 38).
This runtime keeps the live copy in a ledger because the kill path
runs inside the fatal-hit character door, where the store door
cannot be taken again; the ledger is flushed through
WriteMemberBuffs at world entry, session close, membership changes
and every minute pulse, so the persisted state is never more than a
minute behind the ledger - the reference server's own posture (its
buff writes are asynchronous camp jobs).

===========================================================================
*/
package mentor

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"

	log "github.com/sirupsen/logrus"
)

const (
	// buffWindowMinutes is the saved and received buffs' lifetime:
	// the reference server's 0x5A0 minutes from the window stamp.
	buffWindowMinutes = 1440
	// buffPointsPerKillCap caps one kill's accumulation (the native
	// 0x1E clamp on the share formula).
	buffPointsPerKillCap = 30
	// buffDrainPerKill is what one kill grant takes off the first
	// received row (the native fixed 0xA).
	buffDrainPerKill = 10
	// buffPassMinRemainingMinutes is the saved window the pass needs:
	// the client offers its confirm only when the slot's accumulated
	// time reaches one hour (0x36EE80 ms), so the server asks the
	// same of the window the member would pass.
	buffPassMinRemainingMinutes = 60
	// buffLevelCap is the student level at which accumulation stops
	// (the native 0x28 test in the grant helper).
	buffLevelCap = 40
)

// buffLedgerKey identifies one member's ledger entry.
type buffLedgerKey struct {
	divisionID string
	campID     int64
	charID     int64
}

// buffCharKey identifies a character for camp resolution in the kill
// path, which has no authority access.
type buffCharKey struct {
	divisionID string
	charID     int64
}

// buffLedgerEntry is the ledger's live copy of one member's buff:
// their kind (fixed at admission) and the durable state.
type buffLedgerEntry struct {
	kind  uint8
	state domain.TrainingCampMemberBuffState
	dirty bool
}

/*
================
buffRemainingMinutes

The whole minutes left in a window stamped at startMs, read at nowMs:
the native remaining is 1440 minus the elapsed minutes, floored at 0.
A stamp in the future (clock skew) reads as a full window.
================
*/
func buffRemainingMinutes(startMs, nowMs int64) int64 {
	if startMs <= 0 {
		return 0
	}
	elapsed := (nowMs - startMs) / 60_000
	if elapsed < 0 {
		elapsed = 0
	}
	remaining := int64(buffWindowMinutes) - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

/*
================
buffCadenceDue

Whether a refresh is due at this remaining time: the native cadence
sends when the remaining minutes stand on a multiple of 10 or at 10
or below (and always at 0, which the expiry send owns - the caller
checks that first).
================
*/
func buffCadenceDue(remaining int64) bool {
	return remaining <= 10 || remaining%10 == 0
}

/*
================
buffKillPoints

One kill's accumulated points: min(30, trunc(10 * exp / monsterExp)),
the native share formula - the member's paid experience over the
monster's total experience, scaled by 100 * 0.1 and truncated. The
denominator crosses float32 as it does natively; the rest is double
(the x87 posture - see AGENTS.md).
================
*/
func buffKillPoints(exp, monsterExp int64) uint32 {
	if exp <= 0 || monsterExp <= 0 {
		return 0
	}
	points := int64(float64(exp) / float64(float32(monsterExp)) * 100.0 * 0.10000000149011612)
	if points > buffPointsPerKillCap {
		points = buffPointsPerKillCap
	}
	if points < 0 {
		points = 0
	}
	return uint32(points)
}

/*
================
buffStateOf

The ledger state form of one stored member record.
================
*/
func buffStateOf(member domain.TrainingCampMemberRecord) domain.TrainingCampMemberBuffState {
	state := domain.TrainingCampMemberBuffState{
		Value:         member.BuffValue,
		WindowStartMs: member.BuffWindowStartMs,
		Live:          member.BuffLive,
	}
	if member.ReservedBuffs != nil {
		state.Reserved = make([]domain.TrainingCampReservedBuff, len(member.ReservedBuffs))
		copy(state.Reserved, member.ReservedBuffs)
	}
	return state
}

/*
================
hydrateBuffCamp

Fills the ledger from a stored camp snapshot (a Camp() read the
caller already made): entries appear for members the ledger lacks,
and clean entries refresh from the store. Dirty entries are the
live truth and are never overwritten. The caller holds no lock.
================
*/
func (r *InviteRuntime) hydrateBuffCamp(divisionID string, campID int64, members []domain.TrainingCampMemberRecord) {
	r.buffMu.Lock()
	defer r.buffMu.Unlock()
	if r.buffLedger == nil {
		r.buffLedger = make(map[buffLedgerKey]*buffLedgerEntry)
		r.buffCampOf = make(map[buffCharKey]int64)
	}
	live := make(map[int64]bool, len(members))
	for _, member := range members {
		live[member.CharID] = true
		key := buffLedgerKey{divisionID: divisionID, campID: campID, charID: member.CharID}
		entry := r.buffLedger[key]
		if entry == nil {
			r.buffLedger[key] = &buffLedgerEntry{kind: member.Kind, state: buffStateOf(member)}
		} else if !entry.dirty {
			entry.kind = member.Kind
			entry.state = buffStateOf(member)
		}
		r.buffCampOf[buffCharKey{divisionID: divisionID, charID: member.CharID}] = campID
	}
	// Members the store no longer lists (graduated, kicked, left while
	// this runtime was not looking) lose their entries.
	for key := range r.buffLedger {
		if key.divisionID == divisionID && key.campID == campID && !live[key.charID] {
			delete(r.buffLedger, key)
			delete(r.buffCampOf, buffCharKey{divisionID: divisionID, charID: key.charID})
		}
	}
}

/*
================
dropBuffMember

Drops one member's ledger entry (membership ended). The durable
state goes away with the member row itself, so nothing flushes.
================
*/
func (r *InviteRuntime) dropBuffMember(divisionID string, campID, charID int64) {
	r.buffMu.Lock()
	defer r.buffMu.Unlock()
	delete(r.buffLedger, buffLedgerKey{divisionID: divisionID, campID: campID, charID: charID})
	delete(r.buffCampOf, buffCharKey{divisionID: divisionID, charID: charID})
}

/*
================
dropBuffCamp

Drops every ledger entry of a camp (it disbanded).
================
*/
func (r *InviteRuntime) dropBuffCamp(divisionID string, campID int64) {
	r.buffMu.Lock()
	defer r.buffMu.Unlock()
	for key := range r.buffLedger {
		if key.divisionID == divisionID && key.campID == campID {
			delete(r.buffLedger, key)
			delete(r.buffCampOf, buffCharKey{divisionID: divisionID, charID: key.charID})
		}
	}
}

/*
================
flushBuffCamp

Lands the camp's dirty ledger entries through the buff door. The
caller holds no lock. The ledger lock is never held across the door
call: the kill path takes the ledger lock while it already holds the
character door (CampKillGrant runs inside UpdateCharactersDeferred,
which holds the store lock), so holding the ledger lock while waiting
for the store lock here would close an AB-BA cycle and stop the store.

The snapshot clears the dirty marks under the ledger lock; a kill that
lands while the write is in flight marks its entry dirty again, and a
refused write re-marks every snapshotted entry, so no change is lost.
buffFlushMu serialises the flushes themselves, so an older snapshot
can never land after a newer one.
================
*/
func (r *InviteRuntime) flushBuffCamp(divisionID string, campID int64) {
	authority := r.deps.TrainingCampAuthority()
	if authority == nil {
		return
	}
	r.buffFlushMu.Lock()
	defer r.buffFlushMu.Unlock()

	r.buffMu.Lock()
	updates := make(map[int64]domain.TrainingCampMemberBuffState)
	var written []*buffLedgerEntry
	for key, entry := range r.buffLedger {
		if key.divisionID != divisionID || key.campID != campID || !entry.dirty {
			continue
		}
		state := entry.state
		if state.Reserved != nil {
			reserved := make([]domain.TrainingCampReservedBuff, len(state.Reserved))
			copy(reserved, state.Reserved)
			state.Reserved = reserved
		}
		updates[key.charID] = state
		written = append(written, entry)
		entry.dirty = false
	}
	r.buffMu.Unlock()
	if len(updates) == 0 {
		return
	}
	if authority.WriteMemberBuffs(divisionID, campID, updates) {
		return
	}
	r.buffMu.Lock()
	for _, entry := range written {
		entry.dirty = true
	}
	r.buffMu.Unlock()
}

/*
================
buffMemberID

The wire member id for a character id. ObjectIDForCharacter derives
it from the id alone, so this is a pure computation - safe inside
the fatal-hit door, where no character-source read may run.
================
*/
func buffMemberID(charID int64) uint32 {
	return enterworld.ObjectIDForCharacter(&enterworld.Character{ID: charID})
}

/*
================
sendBuffToMember

Sends one buff frame to a member's live session, when they have one.
================
*/
func (r *InviteRuntime) sendBuffToMember(divisionID string, charID int64, opcode uint16, payload []byte) {
	character := r.findDivisionCharacterByID(divisionID, charID)
	if character == nil {
		return
	}
	peer, live := r.presence.SessionByName(divisionID, character.Name)
	if !live {
		return
	}
	_ = peer.Send(opcode, payload)
}

/*
================
CampKillGrant

The kill path's academy hook (action.TrainingCampKill): one paid
monster kill for one camp member. It mutates only the ledger - the
fatal-hit door the caller holds forbids the authority door - and
returns the received-buff bonus to fold into the same grant, plus
the earner's own buff frames (the first-grant state 0, a drained
row's removal). exp and skillExp are the grant's paid totals,
monsterExpBase the monster's total experience (the share formula's
denominator). A member with nothing to earn or drain returns zeros.
================
*/
func (r *InviteRuntime) CampKillGrant(divisionID string, character *enterworld.Character, exp, skillExp, monsterExpBase int64, nowMs int64) (bonusExp, bonusSkillExp int64, frames []wire.Frame) {
	if r == nil || character == nil || (exp == 0 && skillExp == 0) {
		return 0, 0, nil
	}
	r.buffMu.Lock()
	campID, joined := r.buffCampOf[buffCharKey{divisionID: divisionID, charID: character.ID}]
	if !joined {
		r.buffMu.Unlock()
		return 0, 0, nil
	}
	entry := r.buffLedger[buffLedgerKey{divisionID: divisionID, campID: campID, charID: character.ID}]
	if entry == nil || entry.kind == MemberKindSubMentor {
		r.buffMu.Unlock()
		return 0, 0, nil
	}
	level := int64(characterLevel(character))
	if entry.kind == MemberKindStudent && level >= buffLevelCap {
		// A student in the graduation band earns and drains nothing;
		// the level transition already cleared the buff.
		r.buffMu.Unlock()
		return 0, 0, nil
	}

	if entry.kind == MemberKindStudent {
		if points := buffKillPoints(exp, monsterExpBase); points > 0 {
			first := !entry.state.Live
			if first {
				entry.state.Live = true
				entry.state.WindowStartMs = nowMs
			}
			entry.state.Value += points
			entry.dirty = true
			if first {
				frames = append(frames, wire.Frame{
					Opcode:  OpTCBuffState,
					Payload: EncodeBuffStateShow3CC1(BuffStateKillRefresh, uint32(buffRemainingMinutes(entry.state.WindowStartMs, nowMs)), entry.state.Value),
				})
			}
		}
	}

	if len(entry.state.Reserved) > 0 {
		row := &entry.state.Reserved[0]
		drained := row.Points
		if drained > buffDrainPerKill {
			drained = buffDrainPerKill
		}
		row.Points -= drained
		entry.dirty = true
		if row.Points == 0 {
			donor := row.DonorCharID
			entry.state.Reserved = entry.state.Reserved[1:]
			frames = append(frames, wire.Frame{
				Opcode:  OpTCBuffList,
				Payload: EncodeBuffListB00C(BuffListRemove, []BuffListRow{{Minutes: 0, MemberID: buffMemberID(donor), Value: 0}}),
			})
		}
		if drained > 0 {
			bonusExp = buffDrainBonus(exp, drained)
			bonusSkillExp = buffDrainBonus(skillExp, drained)
		}
	}
	r.buffMu.Unlock()
	return bonusExp, bonusSkillExp, frames
}

/*
================
buffDrainBonus

The drain's contribution to one grant total: total * drained / 100,
capped at 30 % of the total (the native clamps). At the native drain
of at most 10 the cap cannot bind; it is kept so the formula reads
as read.
================
*/
func buffDrainBonus(total int64, drained uint32) int64 {
	if total <= 0 || drained == 0 {
		return 0
	}
	bonus := total * int64(drained) / 100
	if cap := total * 30 / 100; bonus > cap {
		bonus = cap
	}
	if bonus < 0 {
		return 0
	}
	return bonus
}

/*
================
buffEntrySends

The world-entry buff legs for one member, against the camp snapshot
the entry already read: a live saved buff shows (state 1, or state 7
when the window ran out while offline - the send the client prints
the time-over line for) with the minutes left in the window the
first grant stamped; entry never moves that stamp, so a login
restarts nothing. A window that ran out reports state 7 here and
closes (not live, no value), which keeps the report a one-time
send. Expired received rows drop, and the surviving rows ride as
one silent mode-1 refresh. The caller (WorldBound) holds no lock.
================
*/
func (r *InviteRuntime) buffEntrySends(s *transport.Session, divisionID string, character *enterworld.Character, campID int64, members []domain.TrainingCampMemberRecord) {
	if s == nil || character == nil {
		return
	}
	r.hydrateBuffCamp(divisionID, campID, members)
	nowMs := r.Now().UnixMilli()

	r.buffMu.Lock()
	entry := r.buffLedger[buffLedgerKey{divisionID: divisionID, campID: campID, charID: character.ID}]
	if entry == nil {
		r.buffMu.Unlock()
		return
	}
	var frames []wire.Frame
	if entry.state.Live {
		remaining := buffRemainingMinutes(entry.state.WindowStartMs, nowMs)
		state := BuffStateEntry
		if remaining == 0 {
			state = BuffStateExpired
		}
		frames = append(frames, wire.Frame{Opcode: OpTCBuffState, Payload: EncodeBuffStateShow3CC1(state, uint32(remaining), entry.state.Value)})
		if remaining == 0 {
			// The window ran out: this show is its only report,
			// and the window closes here - live off, value gone,
			// the stamp left where the first grant put it. Closing
			// (not re-stamping) is what stops the next pulse from
			// reporting the same expiry again.
			entry.state.Live = false
			entry.state.Value = 0
			entry.dirty = true
		}
	}
	rows := make([]BuffListRow, 0, len(entry.state.Reserved))
	kept := entry.state.Reserved[:0]
	for _, row := range entry.state.Reserved {
		remaining := buffRemainingMinutes(row.WindowStartMs, nowMs)
		if remaining == 0 {
			entry.dirty = true
			continue
		}
		kept = append(kept, row)
		rows = append(rows, BuffListRow{Minutes: uint32(remaining), MemberID: 0, Value: row.Points})
	}
	entry.state.Reserved = kept
	for i, row := range entry.state.Reserved {
		rows[i].MemberID = buffMemberID(row.DonorCharID)
	}
	if len(rows) > 0 {
		frames = append(frames, wire.Frame{Opcode: OpTCBuffList, Payload: EncodeBuffListB00C(BuffListRefresh, rows)})
	}
	r.buffMu.Unlock()

	for _, frame := range frames {
		_ = s.Send(frame.Opcode, frame.Payload)
	}
	r.flushBuffCamp(divisionID, campID)
}

/*
================
buffExitFlush

The session-exit buff leg: the native server forces one final
remaining-minutes update on exit, but its only durable effect is the
persisted stamp - this port persists every mutation as it happens,
so the exit just lands the ledger.
================
*/
func (r *InviteRuntime) buffExitFlush(divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	r.buffMu.Lock()
	campID, joined := r.buffCampOf[buffCharKey{divisionID: divisionID, charID: character.ID}]
	r.buffMu.Unlock()
	if joined {
		r.flushBuffCamp(divisionID, campID)
	}
}

/*
================
BuffPulse

The minute pass over every ledger entry whose member is online:
the saved buff refreshes at the cadence crossings and reports its
expiry (state 7, which closes the window), received rows refresh
the same way and are pruned at their expiry. Called from the
world tick's minute boundary, outside any character door.
================
*/
func (r *InviteRuntime) BuffPulse(nowMs int64) {
	// The world tick calls far more often than the native minute pulse;
	// evaluate once per wall minute so a cadence refresh cannot repeat.
	r.buffMu.Lock()
	minute := nowMs / 60000
	if minute == r.buffPulseMinute {
		r.buffMu.Unlock()
		return
	}
	r.buffPulseMinute = minute
	keys := make([]buffLedgerKey, 0, len(r.buffLedger))
	for key, entry := range r.buffLedger {
		if entry.state.Live || len(entry.state.Reserved) > 0 {
			keys = append(keys, key)
		}
	}
	r.buffMu.Unlock()

	touched := make(map[buffLedgerKey]bool)
	for _, key := range keys {
		character := r.findDivisionCharacterByID(key.divisionID, key.charID)
		if character == nil {
			continue
		}
		if _, live := r.presence.SessionByName(key.divisionID, character.Name); !live {
			continue
		}
		if r.buffPulseMember(key, character, nowMs) {
			touched[key] = true
		}
	}
	camps := make(map[buffLedgerKey]bool)
	for key := range touched {
		camp := buffLedgerKey{divisionID: key.divisionID, campID: key.campID}
		if !camps[camp] {
			camps[camp] = true
			r.flushBuffCamp(key.divisionID, key.campID)
		}
	}
}

/*
================
buffPulseMember

One online member's minute evaluation. Returns whether the ledger
changed (an expiry close or a pruned row), so the caller flushes the camp.
Frames are composed under the ledger lock and sent after it.
================
*/
func (r *InviteRuntime) buffPulseMember(key buffLedgerKey, character *enterworld.Character, nowMs int64) bool {
	r.buffMu.Lock()
	entry := r.buffLedger[key]
	if entry == nil {
		r.buffMu.Unlock()
		return false
	}
	changed := false
	var frames []wire.Frame
	if entry.state.Live {
		remaining := buffRemainingMinutes(entry.state.WindowStartMs, nowMs)
		switch {
		case remaining == 0:
			frames = append(frames, wire.Frame{Opcode: OpTCBuffState, Payload: EncodeBuffStateShow3CC1(BuffStateExpired, 0, entry.state.Value)})
			// The expiry report closes the window instead of
			// re-stamping it: the stamp keeps the first grant's
			// time and the value is gone, so the report goes out
			// once and no later show can resurrect the buff at a
			// fresh 1440.
			entry.state.Live = false
			entry.state.Value = 0
			entry.dirty = true
			changed = true
		case buffCadenceDue(remaining):
			frames = append(frames, wire.Frame{Opcode: OpTCBuffState, Payload: EncodeBuffStateShow3CC1(BuffStateKillRefresh, uint32(remaining), entry.state.Value)})
		}
	}
	var refresh []BuffListRow
	var removals []BuffListRow
	kept := make([]domain.TrainingCampReservedBuff, 0, len(entry.state.Reserved))
	for _, row := range entry.state.Reserved {
		remaining := buffRemainingMinutes(row.WindowStartMs, nowMs)
		if remaining == 0 {
			removals = append(removals, BuffListRow{Minutes: 0, MemberID: buffMemberID(row.DonorCharID), Value: 0})
			entry.dirty = true
			changed = true
			continue
		}
		kept = append(kept, row)
		if buffCadenceDue(remaining) {
			refresh = append(refresh, BuffListRow{Minutes: uint32(remaining), MemberID: buffMemberID(row.DonorCharID), Value: row.Points})
		}
	}
	entry.state.Reserved = kept
	if len(removals) > 0 {
		frames = append(frames, wire.Frame{Opcode: OpTCBuffList, Payload: EncodeBuffListB00C(BuffListRemove, removals)})
	}
	if len(refresh) > 0 {
		frames = append(frames, wire.Frame{Opcode: OpTCBuffList, Payload: EncodeBuffListB00C(BuffListRefresh, refresh)})
	}
	r.buffMu.Unlock()

	for _, frame := range frames {
		r.sendBuffToMember(key.divisionID, key.charID, frame.Opcode, frame.Payload)
	}
	return changed
}

/*
================
handlePassBuff

The 0x700C pass: move the requester's whole saved value onto their
camp's Guardian (the kind-0 master) as a received row stamped now.
The requester sees their slot 7 clear (state 5); the Guardian, when
online, sees the row arrive (list mode 0, which prints the received
notice for a student's row). Any refusal is the byte-only list
mode 2 (the client's pass-error print):

  - not in a camp, or the camp has no Guardian distinct from the
    requester;
  - no live saved buff, or a value of zero;
  - under an hour of window remaining (the client's own confirm
    gate, asked server-side because a crafted request skips it).

The reference server computes the pass in its camp manager, which
is not in the available binaries; these conditions are modelled
from the client legs and the drain evidence (the issue records
which parts are inference).
================
*/
func (r *InviteRuntime) handlePassBuff(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound || len(payload) != 0 {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending || r.deps.TrainingCampAuthority() == nil {
		return
	}
	refuse := func() {
		_ = s.Send(OpTCBuffList, EncodeBuffListB00C(BuffListPassError, nil))
	}
	campID, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, actor.ID)
	if !joined {
		refuse()
		return
	}
	_, members, ok := r.deps.TrainingCampAuthority().Camp(divisionID, campID)
	if !ok {
		refuse()
		return
	}
	r.hydrateBuffCamp(divisionID, campID, members)
	nowMs := r.Now().UnixMilli()

	var guardian *domain.TrainingCampMemberRecord
	for i := range members {
		if members[i].Kind == MemberKindMaster {
			guardian = &members[i]
			break
		}
	}

	r.buffMu.Lock()
	key := buffLedgerKey{divisionID: divisionID, campID: campID, charID: actor.ID}
	entry := r.buffLedger[key]
	if guardian == nil || guardian.CharID == actor.ID || entry == nil || !entry.state.Live || entry.state.Value == 0 ||
		buffRemainingMinutes(entry.state.WindowStartMs, nowMs) < buffPassMinRemainingMinutes {
		r.buffMu.Unlock()
		refuse()
		return
	}
	points := entry.state.Value
	entry.state.Value = 0
	entry.state.Live = false
	entry.state.WindowStartMs = 0
	entry.dirty = true
	guardianKey := buffLedgerKey{divisionID: divisionID, campID: campID, charID: guardian.CharID}
	guardianEntry := r.buffLedger[guardianKey]
	if guardianEntry == nil {
		guardianEntry = &buffLedgerEntry{kind: MemberKindMaster}
		r.buffLedger[guardianKey] = guardianEntry
	}
	guardianEntry.state.Reserved = append(guardianEntry.state.Reserved, domain.TrainingCampReservedBuff{
		DonorCharID:   actor.ID,
		Points:        points,
		WindowStartMs: nowMs,
	})
	guardianEntry.dirty = true
	guardianID := guardian.CharID
	r.buffMu.Unlock()

	r.flushBuffCamp(divisionID, campID)
	_ = s.Send(OpTCBuffState, EncodeBuffStateClear3CC1(BuffStateRemoval))
	r.sendBuffToMember(divisionID, guardianID, OpTCBuffList, EncodeBuffListB00C(BuffListAdd, []BuffListRow{{
		Minutes:  uint32(buffRemainingMinutes(nowMs, nowMs)),
		MemberID: enterworld.ObjectIDForCharacter(actor),
		Value:    points,
	}}))
	log.Debugf("mentor: %s passed %d academy buff points to guardian %d (camp %d)", actor.Name, points, guardianID, campID)
}

/*
================
clearBuffOnMembershipEnd

Clears one departing member's buff presentation: their saved slot
clears with the given state (5 for leave/kick/disband, 8 for
graduation) and every received row is removed with the matching
list mode. The member record must be the PRE-departure snapshot -
it carries the buff state the client still shows. The ledger entry
is dropped; the durable rows die with the membership, so the clear
sends only and flushes nothing.
================
*/
func (r *InviteRuntime) clearBuffOnMembershipEnd(divisionID string, campID int64, member domain.TrainingCampMemberRecord, state uint8) {
	r.dropBuffMember(divisionID, campID, member.CharID)
	if !member.BuffLive && len(member.ReservedBuffs) == 0 {
		return
	}
	mode := BuffListRemove
	if state == BuffStateGraduated {
		mode = BuffListRemoveGraduate
	}
	if member.BuffLive {
		r.sendBuffToMember(divisionID, member.CharID, OpTCBuffState, EncodeBuffStateClear3CC1(state))
	}
	if len(member.ReservedBuffs) > 0 {
		rows := make([]BuffListRow, 0, len(member.ReservedBuffs))
		for _, row := range member.ReservedBuffs {
			rows = append(rows, BuffListRow{Minutes: 0, MemberID: buffMemberID(row.DonorCharID), Value: 0})
		}
		r.sendBuffToMember(divisionID, member.CharID, OpTCBuffList, EncodeBuffListB00C(mode, rows))
	}
}

/*
================
clearBuffOnLevelCap

The level-transition clear (the native graduation-level clear): a
student who reaches the graduation band while still a member loses
the saved buff (state 8) and every received row (mode 8), whether or
not the graduation itself has run. Called from NotifyMemberLevel
after the level broadcast; the ledger entry is zeroed and flushed.
================
*/
func (r *InviteRuntime) clearBuffOnLevelCap(divisionID string, character *enterworld.Character) {
	if character == nil {
		return
	}
	r.buffMu.Lock()
	campID, joined := r.buffCampOf[buffCharKey{divisionID: divisionID, charID: character.ID}]
	if !joined {
		r.buffMu.Unlock()
		return
	}
	key := buffLedgerKey{divisionID: divisionID, campID: campID, charID: character.ID}
	entry := r.buffLedger[key]
	if entry == nil || entry.kind != MemberKindStudent {
		r.buffMu.Unlock()
		return
	}
	hadSaved := entry.state.Live
	reserved := entry.state.Reserved
	if !hadSaved && len(reserved) == 0 {
		r.buffMu.Unlock()
		return
	}
	entry.state.Value = 0
	entry.state.Live = false
	entry.state.WindowStartMs = 0
	entry.state.Reserved = nil
	entry.dirty = true
	r.buffMu.Unlock()

	r.flushBuffCamp(divisionID, campID)
	if hadSaved {
		r.sendBuffToMember(divisionID, character.ID, OpTCBuffState, EncodeBuffStateClear3CC1(BuffStateGraduated))
	}
	if len(reserved) > 0 {
		rows := make([]BuffListRow, 0, len(reserved))
		for _, row := range reserved {
			rows = append(rows, BuffListRow{Minutes: 0, MemberID: buffMemberID(row.DonorCharID), Value: 0})
		}
		r.sendBuffToMember(divisionID, character.ID, OpTCBuffList, EncodeBuffListB00C(BuffListRemoveGraduate, rows))
	}
}

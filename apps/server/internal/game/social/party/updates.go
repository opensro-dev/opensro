/*
===========================================================================

updates.go - party position and vital publication beyond visual interest

Party membership, not object visibility, owns these native 3E58 deltas.
The world ticker supplies detached live positions; the party runtime owns
its last-published rows and retires them with the corresponding party.

===========================================================================
*/
package party

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// Inferred publication cadence: one second bounds map/vital delay without
// sending an eight-member roster on every 100 ms simulation tick.
const partyUpdateIntervalMs int64 = 1000
const partyUpdateMember uint8 = 6

/*
================
memberUpdateKey
================
*/
type memberUpdateKey struct {
	division string
	party    uint64
	member   uint32
}

/*
================
memberUpdateState

Only the coordinated simulation hook accesses this state.
================
*/
type memberUpdateState struct {
	nextAtMs int64
	rows     map[memberUpdateKey]MemberRow
}

/*
================
encodeMemberUpdate

Retail 0x75DB30 reads the masked row. 0x761870 type 6 applies position
through 0x822110, including the packed world even outside entity interest.
================
*/
func encodeMemberUpdate(row MemberRow) []byte {
	mask := MemberMaskLevel | MemberMaskStatus | MemberMaskPosition
	if row.Masteries {
		mask |= MemberMaskMastery
	}
	writer := wire.NewWriter(28).U8(partyUpdateMember).U32(row.MemberID).U8(mask).
		U8(row.Level).U8(row.StatusNibbles).U16(row.Region).
		U16(uint16(row.PosX)).U16(uint16(row.PosY)).U16(uint16(row.PosZ)).U32(row.War)
	if row.Masteries {
		writer.U32(row.PrimaryMastery).U32(row.SecondaryMastery)
	}
	return writer.Payload()
}

/*
================
MemberUpdates

The ticker routes returned batches to exact characters, independently of
spawn interest. Unchanged rows stay quiet; the first sweep seeds the cache.
================
*/
func (r *Runtime) MemberUpdates(sessions []simulation.SessionSnapshot, nowMs int64) []simulation.DivisionFrames {
	state := &r.updates
	if nowMs < state.nextAtMs {
		return nil
	}
	state.nextAtMs = nowMs + partyUpdateIntervalMs
	byDivision := make(map[string]map[uint32]simulation.SessionSnapshot)
	for _, session := range sessions {
		if byDivision[session.DivisionID] == nil {
			byDivision[session.DivisionID] = make(map[uint32]simulation.SessionSnapshot)
		}
		byDivision[session.DivisionID][simulation.PlayerObjectID(session.CharacterID)] = session
	}
	next := make(map[memberUpdateKey]MemberRow)
	var deliveries []simulation.DivisionFrames
	for division, online := range byDivision {
		for _, party := range r.registry.RewardSnapshots(division) {
			var frames []simulation.Frame
			for _, member := range party.Members {
				session, present := online[member.MemberID]
				if !present {
					continue
				}
				character := findCharacterByName(r.deps, division, member.Name)
				if character == nil {
					continue
				}
				row := r.memberRowFor(division, character)
				pose := session.World.LiveSpawnAt(nowMs)
				row.Region, row.PosX, row.PosY, row.PosZ, row.War = pose.RegionID, int16(pose.X), int16(pose.Y), int16(pose.Z), session.WorldInstance
				key := memberUpdateKey{division, party.ObjectOrder, member.MemberID}
				next[key] = row
				if previous, found := state.rows[key]; found && previous == row {
					continue
				}
				frames = append(frames, simulation.Frame{Opcode: OpPartyUpdate, Payload: encodeMemberUpdate(row)})
			}
			if len(frames) == 0 {
				continue
			}
			for _, member := range party.Members {
				if session, present := online[member.MemberID]; present {
					deliveries = append(deliveries, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: session.CharacterID, Frames: frames})
				}
			}
		}
	}
	state.rows = next
	return deliveries
}

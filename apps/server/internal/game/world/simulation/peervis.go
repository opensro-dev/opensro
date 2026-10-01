/*
===========================================================================

peervis.go - peer visibility and native appearance publication.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
)

// Peer visibility: the character-appearance / object-list lane that makes a
// second connected character SPAWN as a CICUser on other clients.
//
// Movement was already broadcast (the 0x30E3 / 0xB2F5 tick legs), but a gid
// with no spawn row parks in the client's object-track table (the native
// 0x30E3 behavior for an unresolved gid) - so no peer was ever visible. This
// leg pushes one 0x30D7 single-object spawn per (viewer, peer) pair when the
// peer first becomes visible, and one 0x36AB despawn when the peer's session
// leaves the division. The row bytes are the wire.PlayerSpawnRow layout,
// which the client's REAL sub_86afb0 CICUser_DeserializeFull fold chain
// parses field for field (proven end-to-end by the client parity harness).

// PeerAppearance is the wire-visible appearance of one connected character,
// value-copied out of the bound enterworld.Character at snapshot time (the
// SessionSnapshot concurrency contract). Every field traces to persisted
// character state - no placeholder rows.
/*
================
PeerAppearance
================
*/
type PeerAppearance struct {
	PVPState  uint8
	EventTeam *uint8
	Walk, Run float32
	// RefObjID is the character-model RefObjData id (character.ModelRef,
	// the same record the local player resolves through sub_850c60).
	RefObjID uint32
	// Name is the character name (-> CICUser +0x108 via sub_4b1710).
	Name string
	// BodyShapeByte rides the sub_869110 lead byte into +0x758.
	BodyShapeByte uint8
	VisualFlags   uint8
	// Skin is the transform the player wears (RefObjID 0: none).
	Skin wire.TransformSkin
	// Equipment is the WORN set (equipment-band inventory slots), in slot
	// order, for the sub_86afb0 equip loop.
	Equipment []wire.PlayerEquipItem
	// GuildName/GuildID/GuildGrantName/CrestParam are the guild half of
	// the sub_869df0 non-local tail, resolved from the store's guild door
	// (the same rows the 0x32C4 seed encodes) for guild members and left
	// zero otherwise. A NON-EMPTY GuildName is what arms the client's
	// sub_869810 BindGuild leg (@0x0086a242 tests the +0x7a8 wstring's
	// size field at +0x7bc), so crests and banner labels can resolve for
	// peers at all; CrestParam is the wire crestParamA - the
	// G{prefix}_{guildId}_{crestParamA}.crb filename parameter
	// (sub_833d40 @0x833e42). The alliance params B/C stay 0 (no alliance
	// state exists - the client-global default).
	GuildName      string
	GuildID        uint32
	GuildGrantName string
	CrestParam     uint32
	// FortSiegeAuthority is the guild sub-block's trailing team byte
	// (client sub_869df0 @0x0086a1b2 -> sub_869940 -> CICPlayer+0x7e0),
	// sourced from the member row's FortressRole - the SAME byte the
	// 0x32C4 member loop carries at +0x5c for the local player. The
	// six-mark fortress-war gate needs it in {1 Commander, 2 Deputy}.
	FortSiegeAuthority uint8
	// SpawnSkills is the peer's active effect list (85FB20), so a buff the
	// peer already had shows when it comes into view.
	SpawnSkills []wire.SpawnSkillEntry
}

// PeerScaleDenom is the +0x4d8 scale denominator for a player row: an
// IEEE-754 FLOAT 100.0 on the wire (fld dword @0x0085fba5), which the client
// turns into the neutral 100.0/100.0 = 1.0 reciprocal at +0x4dc - the same
// value the NPC create row ships.
const PeerScaleDenom float32 = 100

// BuildPeerSpawnRow encodes one 0x30D7 CICUser spawn row for a peer at the
// given pose (callers pass the LIVE interpolated position, never the move
// goal - the bug D plane rule). Walk/run ride the char-data 20/50 pair
// (+0x24c/+0x250 sources), and the appear tail is the drop-in presentation
// byte 1 (the grounditem 0x30D7 convention).
/*
================
BuildPeerSpawnRow
================
*/
func BuildPeerSpawnRow(appearance PeerAppearance, gid uint32, pose Spawn) []byte {
	walk, run := appearance.Walk, appearance.Run
	if walk <= 0 {
		walk = WalkSpeed
	}
	if run <= 0 {
		run = RunSpeed
	}
	row := wire.PlayerSpawnRow{
		RefObjID:      appearance.RefObjID,
		BodyShapeByte: appearance.BodyShapeByte,
		Skin:          appearance.Skin,
		VisualFlags:   appearance.VisualFlags,
		Equipment:     appearance.Equipment,
		Gid:           gid,
		Position: wire.Position{
			RegionID: pose.RegionID,
			X:        float32(pose.X),
			Y:        float32(pose.Y),
			Z:        float32(pose.Z),
			Heading:  pose.Angle,
		},
		WalkSpeed:          walk,
		RunSpeed:           run,
		ScaleDenom:         PeerScaleDenom,
		Name:               appearance.Name,
		PVPState:           appearance.PVPState,
		EventTeam:          appearance.EventTeam,
		GuildName:          appearance.GuildName,
		GuildID:            appearance.GuildID,
		GuildGrantName:     appearance.GuildGrantName,
		CrestParamA:        appearance.CrestParam,
		FortSiegeAuthority: appearance.FortSiegeAuthority,
		SpawnSkills:        appearance.SpawnSkills,
		WithAppearTail:     true,
		AppearFlag:         1,
	}
	return row.Encode()
}

// runPeerVisibility is the per-tick peer spawn/despawn leg. For every viewer
// it shows other same-instance characters in interest exactly once
// (0x30D7 at the peer's live pose), and despawns gids whose scope ended
// (0x36AB, the same 4-byte gid wire the pickup path sends). The local player
// is never pushed to itself - the client renders self on the local plane and
// the remote view must never carry it (the wave-3 negative pin).
/*
================
runPeerVisibility
================
*/
func (t *Ticker) runPeerVisibility(state *divisionTickState, nowMs int64, sessions []SessionSnapshot, live map[string]bool) {
	if !state.peerVisibilityChanged(sessions, nowMs, live) {
		return
	}
	// A recovered delivery panic must not certify a partial visibility pass.
	state.peerVisibilityValid = false
	index := buildPeerInterestIndex(sessions, nowMs, false)
	var candidates []int
	liveGids := make(map[uint32]bool)
	for _, viewer := range sessions {
		shown := state.shownPeers[viewer.SessionID]
		if shown == nil {
			shown = make(map[uint32]bool)
			state.shownPeers[viewer.SessionID] = shown
		}

		clear(liveGids)
		candidates = index.candidates(&viewer, viewer.World.LiveSpawnAt(nowMs), candidates)
		for _, peerIndex := range candidates {
			peer := &sessions[peerIndex]
			if peer.SessionID == viewer.SessionID {
				continue
			}
			gid := PlayerObjectID(peer.CharacterID)
			liveGids[gid] = true
			if shown[gid] {
				continue
			}
			pose := index.poses[peerIndex]
			appearance := *peer.Appearance
			appearance.Walk, appearance.Run = peer.World.MovementSpeeds()
			frames := []Frame{{
				ScopeGID: gid, ScopeVisible: true,
				Opcode:  wire.OpSingleObjectSpawn,
				Payload: BuildPeerSpawnRow(appearance, gid, pose),
			}}
			if peer.NativeBodyStatus != 0 {
				frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelBody, Value: peer.NativeBodyStatus}).Encode()})
			}
			if segment := peer.World.MoveSegment; segment.Valid() && nowMs < segment.ArrivesAtMs {
				source := MovementSourceFromSpawn(pose)
				goal := peer.World.Spawn
				frames = append(frames,
					Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{Gid: gid, StateType: wire.StateChannelMove, Value: peer.World.MovementMode}).Encode()},
					Frame{Opcode: OpMovementAck, Payload: BuildMovementAckPayload(gid, MovementRequest{Mode: MovementAckDestinationMode, RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}, &source)},
				)
			}
			t.Push.PushToSession(viewer.SessionID, frames)
			state.rememberPeerPath(viewer.SessionID, gid, peer.World)
			shown[gid] = true
		}

		for gid := range shown {
			if liveGids[gid] {
				continue
			}
			despawn := wire.ObjectDespawn{Gid: gid}
			t.Push.PushToSession(viewer.SessionID, []Frame{{
				ScopeGID: gid,
				Opcode:   wire.OpObjectDespawn,
				Payload:  despawn.Encode(),
			}})
			delete(shown, gid)
			delete(state.peerPaths[viewer.SessionID], gid)
		}
	}

	// Departed viewers must not leak show bookkeeping (the settled-map rule).
	for sessionID := range state.shownPeers {
		if !live[sessionID] {
			delete(state.shownPeers, sessionID)
			delete(state.peerPaths, sessionID)
		}
	}
	state.peerVisibilityValid = true
}

/*
================
peerPositionVisible
================
*/
func peerPositionVisible(viewer, object Spawn) bool {
	return worldgeom.InterestVisible(
		worldgeom.RegionXZ{RegionID: viewer.RegionID, X: viewer.X, Z: viewer.Z},
		worldgeom.RegionXZ{RegionID: object.RegionID, X: object.X, Z: object.Z},
	)
}

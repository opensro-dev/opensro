/*
===========================================================================

resume.go - re-pushing current mover state to a resumed viewer

Non-player movers publish on the lossy lane (bridge.go): a position frame
handed to a socket that then dies is never re-sent, by design for superseded
state. Without a re-push, a monster that walked somewhere and stopped just
before a drop stays rendered at its old position for the resumed client
until it moves again (#1818). Player movement carries a Current anchor and
replays from the ordered ring; this pass is the non-player equivalent.

The transport bridge records resumed sessions (worldsession.Bridge); the
ticker drains the registry in prepareTick and, on that tick, sends each
resumed viewer one ordered position frame per object it already holds:
monsters it has been shown, patrol NPCs in its publication set, companions
it has been shown and its own. Nothing is spawned or despawned here - the
objects are already on the client - and nothing reaches any other viewer.
The frames carry a delivery-time admission (resumeFrameCurrent) so the
bridge routes them on the ordered lane, behind the replayed stream.

===========================================================================
*/

package simulation

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
ResumeSource

ResumeSource is the optional SessionSource face the ticker drains at the
start of a tick: the scene identities of the sessions that resumed since
the last tick. The transport bridge implements it; a source without it
simply never re-pushes.
================
*/
type ResumeSource interface {
	DrainResumedSessions() []string
}

/*
================
resumeFrameCurrent

resumeFrameCurrent admits a re-push frame at delivery time. The frame is
built from this tick's live state, so it is current by construction; the
predicate's presence is what routes the batch onto the ordered lane
(worldsession bridge delivery), behind everything the resume replayed.
================
*/
func resumeFrameCurrent() bool { return true }

/*
================
repushResumedViewer

repushResumedViewer sends one resumed viewer the current position of every
non-player mover it already holds. It runs after the monster leg, so the
shown sets it reads are this tick's. Bounded by the visible set: a viewer
is never told about an object it was never shown.
================
*/
func (t *Ticker) repushResumedViewer(work divisionTickWork, session SessionSnapshot, tick, nowMs int64) {
	var frames []Frame
	frames = append(frames, t.resumedMonsterFrames(work.state, session, nowMs)...)
	frames = append(frames, resumedNpcFrames(t.Roster, session, tick)...)
	frames = append(frames, resumedCompanionFrames(work, session, nowMs)...)
	if len(frames) == 0 {
		return
	}
	for i := range frames {
		frames[i].Current = resumeFrameCurrent
	}
	t.Push.PushToSession(session.SessionID, frames)
}

/*
================
resumedMonsterFrames

resumedMonsterFrames projects every monster the viewer has been shown at
its live pose: the in-flight snapshot (channel plus current goal) while a
segment runs, one source correction once it has settled. Dead monsters
keep their death presentation; no position is re-pushed for them.
================
*/
func (t *Ticker) resumedMonsterFrames(state *divisionTickState, session SessionSnapshot, nowMs int64) []Frame {
	ops := state.monsters
	if ops == nil || ops.Monsters == nil {
		return nil
	}
	shown := ops.shownMonsters[session.SessionID]
	if len(shown) == 0 {
		return nil
	}
	gids := make([]uint32, 0, len(shown))
	for gid := range shown {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	var frames []Frame
	for _, gid := range gids {
		instance, ok := ops.Monsters.Get(session.DivisionID, gid)
		if !ok || instance.CurrentHP == 0 {
			continue
		}
		mover, ok := ops.Monsters.Mover(session.DivisionID, gid)
		if !ok {
			continue
		}
		pose := mover.LivePoseAt(nowMs, ops.GroundHeight)
		if mover.InFlight(nowMs) {
			source := &MovementSource{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			frames = append(frames, monsterInFlightSnapshotFrames(instance, mover, source)...)
			continue
		}
		frames = append(frames, correctionFrame(gid, pose))
	}
	return frames
}

/*
================
resumedNpcFrames

resumedNpcFrames corrects every patrolling roster NPC in the viewer's
publication set onto the canonical patrol track (NpcCorrectionFrames,
restricted to what this viewer holds). Static NPCs never move, so they
need no correction.
================
*/
func resumedNpcFrames(roster []NpcDef, session SessionSnapshot, tick int64) []Frame {
	if !session.NpcsEnabled || len(roster) == 0 || session.PublishedObjects == nil {
		return nil
	}
	shown := make(map[uint32]bool, len(session.PublishedObjects))
	for _, gid := range session.PublishedObjects {
		if gid > domain.NPCGIDBase && gid < domain.GroundItemGIDBase {
			shown[gid] = true
		}
	}
	if len(shown) == 0 {
		return nil
	}
	var frames []Frame
	for index := range roster {
		npc := &roster[index]
		if !npc.Patrol || !shown[npc.ObjectID] {
			continue
		}
		anchor := session.NpcAnchor
		anchor.X += npc.SpawnOffsetX
		anchor.Z += npc.SpawnOffsetZ
		state := ComputeNpcPatrolState(anchor, tick)
		correction := wire.ObjectSourceCorrection{Gid: npc.ObjectID, Position: npcPatrolPose(state)}
		frames = append(frames, Frame{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode()})
	}
	return frames
}

/*
================
resumedCompanionFrames

resumedCompanionFrames re-projects the companions the viewer already
holds: the ones the peer lane has shown it, plus its own (the owner never
enters its own shown set, but its pet's movement reaches it on the same
lossy lane). PeerCOS.frames without a spawn is exactly this projection:
the in-flight goal while a segment runs, one correction once settled.
================
*/
func resumedCompanionFrames(work divisionTickWork, session SessionSnapshot, nowMs int64) []Frame {
	pets := make(map[uint32]*PeerCOS)
	for i := range work.sessions {
		owner := &work.sessions[i]
		if owner.COS != nil {
			pets[owner.COS.Row.Gid] = owner.COS
		}
		for _, pet := range owner.Companions {
			if pet != nil {
				pets[pet.Row.Gid] = pet
			}
		}
	}
	var gids []uint32
	if shown := work.state.shownCOS[session.SessionID]; len(shown) != 0 {
		for gid := range shown {
			if pets[gid] != nil {
				gids = append(gids, gid)
			}
		}
	}
	own := session.Companions
	if own == nil && session.COS != nil {
		own = []*PeerCOS{session.COS}
	}
	for _, pet := range own {
		if pet != nil {
			gids = append(gids, pet.Row.Gid)
		}
	}
	if len(gids) == 0 {
		return nil
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	var frames []Frame
	for _, gid := range gids {
		if pet := pets[gid]; pet != nil {
			frames = append(frames, pet.frames(nowMs, false)...)
		}
	}
	return frames
}

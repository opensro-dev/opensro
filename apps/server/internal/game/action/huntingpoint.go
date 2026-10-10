/*
===========================================================================

huntingpoint.go - a marked player's position follows its hunter (hntp)

The Rogue's Tag Point and Hunting Point install an lnks pair carrying
hntp. SkillCombat_EngageSkill (593757) then attaches the recipient's
action records to a task of the source (CActionTargetContext_SetCoordinates
4F9A90), so the source keeps receiving the recipient's 0x30E3 moves after
it has left sight. The v1.150 client registers the subject from the
private 0xB5ED (CSkillRunTimeManager_RegisterTrackedEntity 84F130), moves
its marker on each 0x30E3 for that GID (84F360) and draws it on the world
map (CIFWorldMap_DrawHuntingPointMarkers 57B550).

A recipient the source can see already reaches it through the ordinary
peer movement, so this owner reports only a recipient outside the
source's published set, and only when its position changed. Inferred:
the native task forwards each movement record; the port samples the
recipient's live position on the mission tick instead, which places the
marker the same way.

===========================================================================
*/

package action

import (
	"slices"
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
huntingReports

The last position reported for each hunt link, by its source token.
================
*/
type huntingReports struct {
	mu   sync.Mutex
	last map[uint32]wire.Position
}

/*
================
AdvanceHuntingPoints

The mission-tick hook: one 0x30E3 to each hunter whose marked recipient
is out of its sight and has moved since the last report.
================
*/
func (rt *Runtime) AdvanceHuntingPoints(nowMs int64, sessions []simulation.SessionSnapshot) []simulation.DivisionFrames {
	rt.huntingReports.mu.Lock()
	defer rt.huntingReports.mu.Unlock()
	if rt.huntingReports.last == nil {
		rt.huntingReports.last = make(map[uint32]wire.Position)
	}
	live := make(map[uint32]bool)
	var out []simulation.DivisionFrames
	for _, link := range rt.effects.HuntLinks(nowMs) {
		live[link.SourceToken] = true
		frame, ok := rt.huntingReport(link.DivisionID, link.SourceName, link.TargetName, link.TargetGID, link.SourceToken, nowMs, sessions)
		if ok {
			out = append(out, frame)
		}
	}
	for token := range rt.huntingReports.last {
		if !live[token] {
			delete(rt.huntingReports.last, token)
		}
	}
	return out
}

/*
================
huntingReport

The recipient's move for its hunter, when the hunter's session cannot see
it and it moved. Seeing it again forgets the last report, so leaving
sight reports at once.
================
*/
func (rt *Runtime) huntingReport(division, sourceName, targetName string, targetGID, token uint32, nowMs int64, sessions []simulation.SessionSnapshot) (simulation.DivisionFrames, bool) {
	unlock := rt.lockDivision(division)
	defer unlock()
	source := rt.findCharacter(division, sourceName)
	target := rt.characterSnapshot(division, rt.findCharacter(division, targetName))
	if source == nil || target == nil || enterworld.ObjectIDForCharacter(target) != targetGID {
		return simulation.DivisionFrames{}, false
	}
	index := slices.IndexFunc(sessions, func(s simulation.SessionSnapshot) bool {
		return s.DivisionID == division && s.CharacterID == source.ID
	})
	if index < 0 || sessions[index].PublishedObjects == nil {
		return simulation.DivisionFrames{}, false
	}
	if slices.Contains(sessions[index].PublishedObjects, targetGID) {
		delete(rt.huntingReports.last, token)
		return simulation.DivisionFrames{}, false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, target.Name), target, nowMs)
	position := wire.Position{RegionID: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle}
	if previous, reported := rt.huntingReports.last[token]; reported && previous == position {
		return simulation.DivisionFrames{}, false
	}
	rt.huntingReports.last[token] = position
	move := wire.ObjectSourceMove{Position: position, Gid: targetGID}
	return simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: source.ID,
		Frames: []simulation.Frame{{Opcode: wire.OpObjectSourceMove, Payload: move.Encode()}}}, true
}

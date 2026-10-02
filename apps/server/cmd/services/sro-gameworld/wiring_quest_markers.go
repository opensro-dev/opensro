package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/simulation"
)

// A single tick owner publishes each viewer's quest registry. Derivation uses
// the quest authority read door; transport and world rosters remain root-owned.
func (game *gameplayPlane) questMarkerTick() simulation.TickHook {
	publications := make(map[uint64]*quest.MarkerPublication)
	anchors := make(map[string]simulation.NpcDef)
	for _, npc := range game.items.NpcRoster {
		if npc.AuthoredSpawn {
			if _, exists := anchors[npc.Codename]; !exists {
				anchors[npc.Codename] = npc
			}
		}
	}
	return func(nowMs int64) []simulation.DivisionFrames {
		if game.quests == nil {
			return nil
		}
		live := make(map[uint64]bool)
		for _, session := range game.hub.Sessions() {
			if session.Evicted() {
				continue
			}
			if _, ok := session.WorldSnapshot(); !ok {
				continue
			}
			c, division, ok := enterworld.SessionCharacter(game.deps, session)
			if !ok {
				continue
			}
			live[session.ID] = true
			var states map[uint32]quest.NpcMarker
			game.deps.Read(division, func() { states = quest.MarkersByNpc(game.quests.MarkerStates(c)) })
			rows := make(map[uint32][18]byte, len(states))
			for id, state := range states {
				npc, ok := anchors[state.Codename]
				if !ok {
					continue
				}
				s := npc.Spawn
				rows[id] = quest.EncodeNpcMarker(id, npc.ObjectID, state.State, s.RegionID, int16(s.X), int16(s.Y), int16(s.Z))
			}
			publication := publications[session.ID]
			if publication == nil {
				publication = &quest.MarkerPublication{}
				publications[session.ID] = publication
			}
			for _, frame := range publication.Update(rows) {
				if err := session.Send(frame.Opcode, frame.Payload); err != nil {
					delete(publications, session.ID)
					log.Debugf("quest marker publication failed for session %d: %v", session.ID, err)
					break
				}
			}
		}
		for id := range publications {
			if !live[id] {
				delete(publications, id)
			}
		}
		return nil
	}
}

func (game *gameplayPlane) validateQuestMarkerRoster() error {
	counts := map[string]int{}
	refs := map[string]uint32{}
	for _, npc := range game.items.NpcRoster {
		if npc.AuthoredSpawn {
			counts[npc.Codename]++
			refs[npc.Codename] = npc.RefObjID
		}
	}
	for _, def := range game.quests.Defs.All() {
		specs := []quest.QuestSpec{def.QuestSpec}
		for _, stage := range def.Stages {
			specs = append(specs, stage.QuestSpec)
		}
		for _, spec := range specs {
			for _, code := range []string{spec.StartNpcCodename, spec.EndNpcCodename, spec.DeliveryNpcCodename} {
				if code != "" && counts[code] != 1 {
					return fmt.Errorf("quest %s marker NPC %s has %d authored placements; resolve its identity before publication", def.Codename, code, counts[code])
				}
			}
		}
	}
	// The journal target list names these same placements (SQuestInfo 0x40).
	return game.quests.Defs.ResolveJournalNpcs(func(code string) (uint32, bool) {
		ref, ok := refs[code]
		return ref, ok
	})
}

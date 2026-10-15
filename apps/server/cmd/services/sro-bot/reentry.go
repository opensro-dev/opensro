package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// sceneEntry consumes the same reset, character, and finalize edges as admission.
type sceneEntry struct {
	pending       bool
	positionReady bool
	region        uint16
	started       time.Time
}

func (e *sceneEntry) handle(frame transport.Frame, world *botWorld, brain *bot.Brain, socket *bot.Socket) (bool, error) {
	switch frame.Opcode {
	case bot.OpcodeSceneReset:
		if len(frame.Payload) < 2 {
			return true, fmt.Errorf("short scene reset")
		}
		e.region = binary.LittleEndian.Uint16(frame.Payload[:2])
		e.pending, e.positionReady, e.started = true, false, time.Now()
		world.beginScene(e.region)
		brain.ResetScene()
		return true, nil
	case enterworld.OpcodeMyCharacterChunk:
		if !e.pending {
			return false, nil
		}
		region, x, y, z, ok := bot.ParseCharacterPosition(frame.Payload, world.Self().GID, e.region)
		if !ok {
			return true, fmt.Errorf("scene re-entry position missing")
		}
		world.setPosition(region, x, y, z)
		if level, ok := bot.ParseCharacterLevel(frame.Payload); ok {
			world.setLevel(level)
		}
		e.positionReady = true
		return true, nil
	case bot.OpcodeObjectListFinalize:
		if !e.pending {
			return false, nil
		}
		if !e.positionReady {
			return true, fmt.Errorf("scene finalized without character position")
		}
		if err := socket.Write(enterworld.OpcodeGameReady, nil); err != nil {
			return true, err
		}
		e.pending = false
		return true, nil
	}
	return false, nil
}

func (w *botWorld) beginScene(region uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.monsters = map[uint32]bot.Actor{}
	w.groundItems = map[uint32]bot.GroundItem{}
	w.self.Region = region
	w.self.PositionKnown = false
	w.frameSet = false
	w.moveGoal = nil
	w.pendingMove = false
}

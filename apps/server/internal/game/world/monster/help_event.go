package monster

import (
	"encoding/binary"
	"fmt"
	"math"
)

// HelpEvent is native AI event 1, not a client packet. The reference
// server's help-event writer writes exactly these 14 bytes; its receiver
// reads them in this order. SenderParameter is the
// result of slot +124, not a guessed LIFE, rank, or urgency field.
type HelpEvent struct {
	RequestType                uint8
	SenderGID, SenderTacticsID uint32
	SenderParameter            uint8
	TargetGID                  uint32
}

// One pending event, not a queue. The reference server's AI message
// receiver releases the previous event and replaces it; its pending-message
// processing consumes even a rejected event. Stored by value so a
// detached Instance cannot mutate the live inbox through a shared pointer.
type HelpInbox = CommandInbox

func DecodeHelpEvent(payload []byte) (HelpEvent, error) {
	if len(payload) != 14 {
		return HelpEvent{}, fmt.Errorf("AI help event length %d, want 14", len(payload))
	}
	return HelpEvent{payload[0], binary.LittleEndian.Uint32(payload[1:5]), binary.LittleEndian.Uint32(payload[5:9]), payload[9], binary.LittleEndian.Uint32(payload[10:14])}, nil
}

// HelpReceiverHeader is the part of the reference server's help receiver
// before lookup/hostility/navigation.
// Even rejected events consume the pending-event slot and the current AI tick.
func (c TacticsControls) HelpReceiverHeader(event HelpEvent, battle bool, currentHP, maximumHP uint32) bool {
	if c.HelpResponse == 2 || (event.RequestType == 0 && event.SenderTacticsID != c.ID) || (event.SenderParameter != 1 && battle) {
		return false
	}
	// Native getters feed signed FILD, then spill the percent to float32.
	percent := float32(float64(int32(currentHP)) * 100 / float64(int32(maximumHP)))
	return percent >= 80 && !math.IsNaN(float64(percent))
}

// The reference server's help trace uses a 3D target distance but a PLANAR
// offset from the stored home.
// Its no-nest branch is strict; the adjusted nest branch admits equality.
func (c TacticsControls) HelpWithinTrace(distance, storedHomeDistance, storedHomeRadius float32, hasNest bool) bool {
	if c.TraceBoundary == 0 || c.TraceBoundary == 2 || c.TraceData == 0 {
		return true
	}
	limit := float32(c.TraceData)
	if !hasNest {
		return distance < limit
	}
	overshoot := float32(storedHomeDistance - float32(float64(storedHomeRadius)*1.5))
	if overshoot > 0 {
		limit -= overshoot
	}
	return distance <= limit
}

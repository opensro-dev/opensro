/*
===========================================================================

quickslot.go - native hotbar configuration wire and persistence

===========================================================================
*/
package enterworld

import (
	"encoding/binary"
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/transport"
)

// OpcodeQuickSlotBinding is the native client-config quickslot opcode. Save
// frames are exactly {u8 mode=1,u8 slot,u8 kind,u32 payload}.
const OpcodeQuickSlotBinding uint16 = 0x7541

const quickSlotSaveMode uint8 = 1

// The binding stores a bag-relative index; inventory move slots are bytes.
// 572E00 checks the current bag capacity, not a fixed 45-slot inventory.
const quickSlotBagPayloadLimit = (1 << 8) - 13

// Native CGInterface_OnMissionLoadingRevealed (client sub_683b40) can emit
// the exact two-byte body {0,7} when its world-entry quickslot block was not
// mode 7. It is a reveal/request marker, not a slot mutation. Our normal
// enter-world writer always supplies mode 7, but accepting this sister frame
// keeps the opcode contract complete and prevents it from falling into the
// seven-byte save decoder.
var quickSlotMissionRevealRequest = [...]byte{0, 7}

/*
================
QuickSlotSaveRequest
================
*/
type QuickSlotSaveRequest struct {
	Slot    uint8
	Kind    uint8
	Payload uint32
}

/*
================
DecodeQuickSlotSaveRequest
================
*/
func DecodeQuickSlotSaveRequest(payload []byte) (QuickSlotSaveRequest, error) {
	if len(payload) != 7 {
		return QuickSlotSaveRequest{}, fmt.Errorf("quickslot payload length %d, want 7", len(payload))
	}
	if payload[0] != quickSlotSaveMode {
		return QuickSlotSaveRequest{}, fmt.Errorf("quickslot mode %d, want save mode 1", payload[0])
	}
	request := QuickSlotSaveRequest{
		Slot:    payload[1],
		Kind:    payload[2],
		Payload: binary.LittleEndian.Uint32(payload[3:7]),
	}
	if int(request.Slot) >= domain.QuickSlotCount {
		return QuickSlotSaveRequest{}, fmt.Errorf("quickslot index %d outside 0..50", request.Slot)
	}
	if !domain.QuickSlotKindValid(request.Kind) {
		return QuickSlotSaveRequest{}, fmt.Errorf("quickslot kind 0x%02X is not native", request.Kind)
	}
	// These payloads are slot indices in the client serializer. Clamp at the
	// trust boundary too; skill/action ids are validated only when executed by
	// their gameplay lanes and remain opaque client configuration here.
	switch request.Kind {
	case 0x46:
		if request.Payload >= quickSlotBagPayloadLimit {
			return QuickSlotSaveRequest{}, fmt.Errorf("inventory quickslot payload %d exceeds the inventory wire range", request.Payload)
		}
	case 0x47:
		if request.Payload >= 0x0d {
			return QuickSlotSaveRequest{}, fmt.Errorf("equipment quickslot payload %d outside 0..12", request.Payload)
		}
	case 0x4e:
		if request.Payload >= 4 {
			return QuickSlotSaveRequest{}, fmt.Errorf("COS quickslot payload %d outside 0..3", request.Payload)
		}
	}
	if request.Kind == 0 {
		request.Payload = 0
	}
	return request, nil
}

// HandleQuickSlotSave persists one binding with copy-then-swap semantics.
// Kind zero deletes the row because CIFUnderBar OnCreate already owns the
// empty default; enter-world therefore emits only meaningful state records.
/*
================
HandleQuickSlotSave
================
*/
func HandleQuickSlotSave(deps *Deps, character *Character, payload []byte) (bool, error) {
	request, err := DecodeQuickSlotSaveRequest(payload)
	if err != nil {
		return false, err
	}
	if character == nil {
		return false, fmt.Errorf("character not found")
	}
	changed := deps.Update(character, "quickslot-save", func() bool {
		if character.DeletePending {
			return false
		}
		next := make([]QuickSlotBinding, 0, len(character.QuickSlots)+1)
		matchingRows := 0
		alreadyEqual := false
		for _, row := range character.QuickSlots {
			if row.Slot != request.Slot {
				next = append(next, row)
				continue
			}
			matchingRows++
			alreadyEqual = row.Kind == request.Kind && row.Payload == request.Payload
		}
		if request.Kind != 0 {
			next = append(next, QuickSlotBinding(request))
		}
		if request.Kind != 0 && matchingRows == 1 && alreadyEqual {
			return false
		}
		if request.Kind == 0 && matchingRows == 0 {
			return false
		}
		sort.Slice(next, func(i, j int) bool { return next[i].Slot < next[j].Slot })
		character.QuickSlots = next
		return true
	})
	return changed, nil
}

// HandleQuickSlotMessage owns the complete client->server 0x7541 shape:
// mission-reveal marker {0,7}, mode-1 binding save, or mode-2 auto-potion save. The
// marker is deliberately side-effect free; the authoritative mode-7 state is
// already carried by the enter-world payload.
/*
================
HandleQuickSlotMessage
================
*/
func HandleQuickSlotMessage(deps *Deps, character *Character, payload []byte) (bool, error) {
	if len(payload) > 0 && payload[0] == 2 {
		return handleAutoPotionSave(deps, character, payload)
	}
	if len(payload) == len(quickSlotMissionRevealRequest) &&
		payload[0] == quickSlotMissionRevealRequest[0] &&
		payload[1] == quickSlotMissionRevealRequest[1] {
		if character == nil {
			return false, fmt.Errorf("character not found")
		}
		return false, nil
	}
	return HandleQuickSlotSave(deps, character, payload)
}

/*
================
RegisterQuickSlotBindings
================
*/
func RegisterQuickSlotBindings(hub *transport.Hub, deps *Deps) {
	hub.Handle(OpcodeQuickSlotBinding, func(s *transport.Session, opcode uint16, payload []byte) {
		character, _, bound := SessionCharacter(deps.Characters, s)
		if !bound {
			log.Debugf("bootstrap: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		if _, err := HandleQuickSlotMessage(deps, character, payload); err != nil {
			log.Debugf("bootstrap: quickslot save from session %d refused: %v", s.ID, err)
		}
	})
}

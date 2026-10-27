package alchemy

import (
	"fmt"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const OpReinforce uint16 = 0x7373
const OpReinforceResult uint16 = 0xb373
const OpStone uint16 = 0x7651
const OpStoneResult uint16 = 0xb651

func DecodeStone(p []byte) ([]uint8, bool, error) {
	if len(p) != 4 || (p[0] != 2 && p[0] != 3) || p[1] != 2 {
		return nil, false, fmt.Errorf("alchemy: invalid stone request")
	}
	return append([]uint8(nil), p[2:]...), p[0] == 3, nil
}

func DecodeReinforce(p []byte) ([]uint8, error) {
	if len(p) < 3 || int(p[0])+1 != len(p) || p[0] > 3 {
		return nil, fmt.Errorf("alchemy: invalid reinforcement request")
	}
	return append([]uint8(nil), p[1:]...), nil
}

// ReinforceFrames uses the v1.150 client's grammar, not v1.188 B150's extra
// mode byte. Material deltas precede the result so presentation sees the
// committed inventory. The target body/destruction belongs to B373 alone.
func ReinforceFrames(before []inventory.Item, result Outcome) []wire.Frame {
	return ResultFrames(OpReinforceResult, before, result)
}

func ResultFrames(op uint16, before []inventory.Item, result Outcome) []wire.Frame {
	frames := []wire.Frame{}
	for _, old := range before {
		if old.Slot == result.Target {
			continue
		}
		quantity := uint16(0)
		for _, row := range result.Items {
			if row.Slot == old.Slot {
				quantity = row.Quantity
				break
			}
		}
		if quantity != old.Quantity {
			frames = append(frames, wire.Frame{Opcode: 0x3645, Payload: wire.NewWriter(4).U8(old.Slot).U8(8).U16(quantity).Payload()})
		}
	}
	status := uint8(0)
	if result.Success {
		status = 1
	}
	// Natively, an unsuccessful stone attempt consumes the stone and
	// returns 0x5423, not a synthetic unchanged equipment body.
	if op == OpStoneResult && !result.Success {
		return append(frames, wire.Frame{Opcode: op, Payload: []byte{2, 0x23}})
	}
	w := wire.NewWriter(4).U8(1).U8(status).U8(result.Target)
	if !result.Success && op == OpReinforceResult {
		destroy := uint8(0)
		if result.Destroyed {
			destroy = 1
		}
		w.U8(destroy)
	}
	p := w.Payload()
	if !result.Destroyed {
		for _, row := range result.Items {
			if row.Slot == result.Target {
				p = append(p, row.Body().Encode()...)
				break
			}
		}
	}
	return append(frames, wire.Frame{Opcode: op, Payload: p})
}

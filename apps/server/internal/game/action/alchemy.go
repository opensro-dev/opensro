package action

import (
	"crypto/rand"
	"errors"
	log "github.com/sirupsen/logrus"
	"math/big"
	"reflect"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

func (rt *Runtime) ConfigureAlchemy(dir string) error {
	c, err := alchemy.LoadCatalog(dir, rt.deps.ItemReferences())
	if err != nil {
		return err
	}
	rt.Alchemy = c
	return nil
}

func (rt *Runtime) AlchemyMagicOptionIDs() []uint32 {
	if rt.Alchemy == nil {
		return nil
	}
	ids := make([]uint32, 0, len(rt.Alchemy.Magic))
	for id := range rt.Alchemy.Magic {
		ids = append(ids, uint32(id))
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func secureAlchemyRoll() (uint32, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(32768))
	if err != nil {
		return 0, err
	}
	return uint32(n.Uint64()), nil
}

func (rt *Runtime) registerAlchemy(hub *transport.Hub) {
	for _, opcode := range []uint16{alchemy.OpCompound, alchemy.OpDissolve} {
		hub.Handle(opcode, func(session *transport.Session, opcode uint16, payload []byte) {
			character, division, bound := enterworld.SessionCharacter(rt.deps, session)
			if bound {
				sendFrames(session, rt.HandleAlchemyProcess(division, character, opcode, payload))
			}
		})
	}
	hub.Handle(alchemy.OpReinforce, func(session *transport.Session, opcode uint16, payload []byte) {
		character, division, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			return
		}
		sendFrames(session, rt.HandleAlchemyReinforce(division, character, payload))
	})
	hub.Handle(alchemy.OpStone, func(session *transport.Session, opcode uint16, payload []byte) {
		character, division, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			return
		}
		sendFrames(session, rt.HandleAlchemyStone(division, character, payload))
	})
}

func alchemyFailure(op uint16, err error) []wire.Frame {
	var refusal alchemy.Refusal
	if errors.As(err, &refusal) {
		return []wire.Frame{{Opcode: op, Payload: []byte{2, byte(refusal)}}}
	}
	if err != nil {
		log.Warnf("alchemy: opcode 0x%04X refused: %v", op, err)
	}
	return []wire.Frame{{Opcode: op, Payload: []byte{0}}}
}

func (rt *Runtime) alchemyOutputCodenames() []string {
	var names []string
	if rt.Alchemy != nil {
		for name, ref := range rt.Alchemy.Items {
			// Potion-tablet products need references before their first B06D
			// grant just as stones/elements do; otherwise the bag decoder has
			// no identity for an item the server successfully manufactured.
			if ref.Flags&0x7fe == 0x5ec || ref.Flags == wire.PackTypeFlags(3, 3, 13, 1) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

// HandleAlchemyReinforce shares the same division lock and durable character
// door as item moves, purchases and Gacha. No live row is modified until the
// complete detached outcome and ordered reply have been constructed.
func (rt *Runtime) HandleAlchemyReinforce(division string, character *enterworld.Character, p []byte) []wire.Frame {
	slots, err := alchemy.DecodeReinforce(p)
	if err != nil {
		return alchemyFailure(alchemy.OpReinforceResult, err)
	}
	return rt.applyAlchemy(division, character, alchemy.OpReinforceResult, slots, false)
}

func (rt *Runtime) HandleAlchemyStone(division string, character *enterworld.Character, p []byte) []wire.Frame {
	slots, magic, err := alchemy.DecodeStone(p)
	if err != nil {
		return alchemyFailure(alchemy.OpStoneResult, err)
	}
	return rt.applyAlchemy(division, character, alchemy.OpStoneResult, slots, magic)
}

func (rt *Runtime) applyAlchemy(division string, character *enterworld.Character, op uint16, slots []uint8, magic bool) []wire.Frame {
	if character == nil || rt.Alchemy == nil {
		return alchemyFailure(op, nil)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	var frames []wire.Frame
	var err error
	committed := rt.deps.Update(character, "alchemy", func() bool {
		// Reinforcement and stones share the same living-character admission
		// as compound/dissolve. Keep this inside the inventory transaction so
		// a death between UI admission and the request cannot spend materials.
		if character.DeletePending || !enterworld.CharacterAlive(character) {
			return false
		}
		before := invItemsFromBag(character)
		reinforceBonus, stoneBonus := rt.alchemyBonuses(division, character)
		var result alchemy.Outcome
		if op == alchemy.OpReinforceResult {
			result, err = rt.Alchemy.Reinforce(before, slots, reinforceBonus, rt.AlchemyRoll)
		} else {
			result, err = rt.Alchemy.Stone(before, slots, magic, stoneBonus, rt.AlchemyRoll)
		}
		if err != nil {
			return false
		}
		frames = alchemy.ResultFrames(op, before, result)
		character.MissionInventory = alchemyRows(character.MissionInventory, before, result.Items)
		if rt.UpdateQuestInventory != nil {
			quest, _ := rt.UpdateQuestInventory(character)
			frames = append(frames, quest...)
		}
		return true
	})
	if !committed {
		return alchemyFailure(op, err)
	}
	return frames
}

// Preserve the exact persisted rows outside the changed slots. Re-arming an
// entire bag would otherwise normalize unrelated legacy quantity/variance
// fields merely because the character performed Alchemy.
func alchemyRows(rows []enterworld.InventoryRow, before, after []inventory.Item) []enterworld.InventoryRow {
	previous := map[int64]inventory.Item{}
	next := map[int64]inventory.Item{}
	for _, item := range before {
		previous[int64(item.Slot)] = item
	}
	for _, item := range after {
		next[int64(item.Slot)] = item
	}
	out := make([]enterworld.InventoryRow, 0, len(rows))
	for _, row := range rows {
		old, armed := previous[row.Slot]
		if !armed {
			out = append(out, row)
			continue
		}
		item, exists := next[row.Slot]
		if !exists {
			continue
		}
		if reflect.DeepEqual(old, item) {
			out = append(out, row)
		} else {
			out = append(out, rowsFromInvItems([]inventory.Item{item})[0])
		}
	}
	for _, item := range after {
		if _, exists := previous[int64(item.Slot)]; !exists {
			out = append(out, rowsFromInvItems([]inventory.Item{item})[0])
		}
	}
	return out
}

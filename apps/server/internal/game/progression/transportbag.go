/*
===========================================================================

transportbag.go - a transport's cargo bag grows with its owner

The level transition's tail: when a character gains a level, an active
transport's cargo bag grows to the owner's new native size
(enterworld.NativeTransportBagCapacity, #4490 U11-01) and the owner is
told with a fresh 0x3158 COS record, the record that carries the
capacity byte. A bag never shrinks: the native size only ever raises
it, and a bag the pet-inventory-pages deviation raised further is
already past the native size and stays.

===========================================================================
*/

package progression

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// transportBagBand is the characterdata TID band of the transport
// vehicles: the band-2 family whose 0x3158 record carries a cargo bag
// (the riding and pet bands author none this rule sizes).
const transportBagBand = 2

/*
================
growTransportBag

Grows the active transport's bag to the owner's post-grant level size.
Returns the 0x3158 record to publish and whether the bag grew. Runs
after the level commit, on the live character; a record the builder
refuses leaves the old capacity in place.
================
*/
func (rt *Runtime) growTransportBag(character *enterworld.Character) (wire.Frame, bool) {
	pet := character.ActiveCOS
	if pet == nil || pet.Container == nil {
		return wire.Frame{}, false
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return wire.Frame{}, false
	}
	ref, ok := refs.CharacterRefByCodename(pet.Codename)
	if !ok || ref == nil || ref.TidWord>>11 != transportBagBand || ref.InventoryCapacity == 0 {
		return wire.Frame{}, false
	}
	levels, ok := rt.deps.LevelData().(interface {
		WithdrawalGoldBasis(level int64) (int64, bool)
	})
	if !ok {
		return wire.Frame{}, false
	}
	gold, found := levels.WithdrawalGoldBasis(characterLevel(character))
	if !found {
		return wire.Frame{}, false
	}
	places := enterworld.NativeTransportBagCapacity(ref, gold)
	if places <= pet.Container.Capacity {
		return wire.Frame{}, false
	}
	old := pet.Container.Capacity
	pet.Container.Capacity = places
	record, err := enterworld.BuildCOSRecord(pet, ref, rt.deps.ItemReferences())
	if err != nil {
		pet.Container.Capacity = old
		log.Warnf("progression: transport bag growth record refused - %v", err)
		return wire.Frame{}, false
	}
	return wire.Frame{Opcode: wire.OpCosRecordCreate, Payload: record}, true
}

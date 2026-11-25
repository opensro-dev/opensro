/*
===========================================================================

reentry.go - the teleport re-entry rebuild

A teleport-style move (return scroll, portal, relocation) sends the
client the same reset/character/object-list sequence a retail region
reset does, rebuilt here from the live record after the caller's world
mutation. The rebuild runs under the caller's publication-gate hold, so
a slow one names its parts (#2802).

===========================================================================
*/
package enterworld

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// ReentryPackets rebuilds the native reset/character/object-list
// sequence for an already-bound mission session. Teleport-style operations
// use this after their authoritative world mutation so the client enters the
// same loading transition as a retail region reset instead of receiving only
// an in-world position correction.
/*
================
ReentryPackets
================
*/
func (d *Deps) ReentryPackets(divisionID, characterName string) ([]Packet, bool) {
	projection := *d
	projection.RestoreEntryEffects = nil // Caller already owns the live actor transaction.
	projection.PrepareEntry = nil
	projection.AdoptEntrySpawn = nil
	// The bootstrap's own writes (the seed, the starter kit, the encoded
	// inventory adoption below) ride the deferred door (#1820): a
	// re-entry built on the tick joins the tick's one batched commit
	// instead of committing per door under the division lock. A deferred
	// door called with no batch open commits at once (store
	// tickbatch.go), so a re-entry built by a handler behaves exactly as
	// before. Login never passes through here and keeps its doors.
	if d.MutateCharacterDeferred != nil {
		projection.MutateCharacter = d.MutateCharacterDeferred
	}
	// A re-entry runs under its caller's publication-gate hold (the
	// return-scroll arrival's returnArrival.reentry part, #2802): a slow one
	// names its own parts as reentry.<part> beside it.
	hold := simulation.NewHoldBreakdown("reentry", d.clock)
	result := Build(&projection, BootstrapRequest{
		DivisionID:    divisionID,
		CharacterName: characterName,
		hold:          &hold,
	})
	packets, ok := projection.encodeReentry(result)
	hold.Mark("encode")
	hold.Report(d.PublicationHold)
	return packets, ok
}

// PreparedReentry couples the packets to the resolved placement they encode.
// The action owner commits this placement only after preparation succeeds.
/*
================
PreparedReentry
================
*/
type PreparedReentry struct {
	Packets []Packet
	Spawn   simulation.Spawn
	// InventorySize is the capacity the packets present; the committing
	// owner adopts it on the live record (domain.AdoptInventorySize).
	InventorySize uint8
	// InventoryBase is the inventory-pages floor the packets present
	// (zero native); the committing owner raises the live record to it.
	InventoryBase uint8
}

/*
================
PrepareReentry
================
*/
func (d *Deps) PrepareReentry(divisionID string, character *Character) (PreparedReentry, bool) {
	if character == nil || character.DeletePending || character.MissionInventory == nil {
		return PreparedReentry{}, false
	}
	snapshot := character.Snapshot()
	if d.NormalizeEntryQuests != nil {
		if err := d.NormalizeEntryQuests(snapshot); err != nil {
			return PreparedReentry{}, false
		}
	}
	projection := *d
	// The caller holds the division lock and commits the returned stand only
	// after preparation succeeds. Login adoption would re-enter that lock.
	projection.AdoptEntrySpawn = nil
	snapshot.PresentInventoryExpansion()
	var inventoryBase uint8
	if d.InventoryPages {
		snapshot.RaiseInventoryFloor(domain.InventoryPagesBase)
		inventoryBase = domain.InventoryPagesBase
	}
	result := buildCharacterProjection(&projection, divisionID, snapshot, nil, "")
	packets, ok := d.encodeReentry(result)
	if !ok {
		return PreparedReentry{}, false
	}
	spawn := result.LocalPlayerEntry.StartProfile
	return PreparedReentry{Packets: packets, Spawn: simulation.Spawn{
		RegionID: uint16(spawn.RegionID), X: spawn.X, Y: spawn.Y, Z: spawn.Z, Angle: uint16(spawn.Angle),
	}, InventorySize: snapshot.InventoryCapacity(), InventoryBase: inventoryBase}, true
}

/*
================
encodeReentry
================
*/
func (d *Deps) encodeReentry(result *BootstrapResult) ([]Packet, bool) {
	if result == nil || result.NativeResult != nativeResultSuccess {
		return nil, false
	}
	if len(result.Packets) == 0 || result.Packets[0].NativeOpcode != OpcodeResetClient {
		return nil, false
	}
	payload, err := d.entryPayload(result)
	if err != nil {
		return nil, false
	}
	// Reset opens the existing session's re-entry admission. Replace the browser
	// projection before its GID latch: retaining the login DTO here resurrects
	// stale HP and coordinates even though the native stream is already fresh.
	packets := make([]Packet, 0, len(result.Packets)+1)
	packets = append(packets, result.Packets[0], NewPacket(transport.OpEnterWorldResult, payload))
	packets = append(packets, result.Packets[1:]...)
	d.adoptEncodedInventory(result)
	return packets, true
}

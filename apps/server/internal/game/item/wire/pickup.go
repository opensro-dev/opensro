package wire

import "fmt"

// PickupAnim is the 0x35C7 pickup-animation trigger.
//
// Handler sub_7780f0 reads the two fields (@0x007780ff a 4-byte gid,
// @0x0077810d a 1-byte heading), resolves the gid, skips the entity when it is
// riding, sets its yaw from the heading and calls SetMotionState(0x0A) - the
// only SetMotionState(10) call site in the binary, which plays motion 0x26
// (ANI_PICK). Local and remote clients run the same handler, so the picker's
// scoop is visible to the whole division.
type PickupAnim struct {
	// Gid is the picking entity, not the item being picked up.
	Gid uint32
	// Heading is the picker's yaw scaled to 0..255 over a full circle. Build
	// it from a u16 angle with HeadingByteFromAngle.
	Heading uint8
}

// PickupAnimSize is the encoded size of a 0x35C7 payload.
const PickupAnimSize = 5

// Encode returns the 0x35C7 payload: [u32 gid][u8 heading].
func (p PickupAnim) Encode() []byte {
	return NewWriter(PickupAnimSize).U32(p.Gid).U8(p.Heading).Payload()
}

// DecodePickupAnim parses a 0x35C7 payload.
func DecodePickupAnim(payload []byte) (PickupAnim, error) {
	var out PickupAnim
	r := NewReader(payload)

	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	heading, err := r.U8()
	if err != nil {
		return out, err
	}
	if err := r.Done(); err != nil {
		return out, err
	}

	out.Gid = gid
	out.Heading = heading
	return out, nil
}

// ObjectDespawn is the 0x36AB single-entity despawn.
//
// Handler sub_777310 performs exactly one 4-byte read (@0x0077731c), resolves
// the gid through CEntityManager_ResolveGidObjectOrAssert and tail-jumps the
// resolved object's vtable +0x4c despawn slot. There is no second field.
type ObjectDespawn struct {
	Gid uint32
}

// ObjectDespawnSize is the encoded size of a 0x36AB payload.
const ObjectDespawnSize = 4

// Encode returns the 0x36AB payload: [u32 gid].
func (d ObjectDespawn) Encode() []byte {
	return NewWriter(ObjectDespawnSize).U32(d.Gid).Payload()
}

// DecodeObjectDespawn parses a 0x36AB payload.
func DecodeObjectDespawn(payload []byte) (ObjectDespawn, error) {
	var out ObjectDespawn
	r := NewReader(payload)

	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	if err := r.Done(); err != nil {
		return out, err
	}

	out.Gid = gid
	return out, nil
}

/*
================
GoldRefresh

The character's gold balance: 0x30B3 type 1 (CPSMission_OnPointUpdate30B3
case 0) [u8 1][u64 balance][u8 notify]. With notify set and the balance
grown, the client prints UIIT_MSG_STATE_GAIN_GOLD with the difference before
it stores the balance. Gold rides OpPointsUpdate (statwire.go); 0x3126 is
the warehouse's gold (CIFStorageRoom, storage.go).
================
*/
type GoldRefresh struct {
	Balance uint64
	Notify  bool
}

// GoldRefreshSize is the encoded size of a gold refresh payload.
const GoldRefreshSize = 10

/*
================
GoldRefresh.Encode
================
*/
func (g GoldRefresh) Encode() []byte {
	notify := uint8(0)
	if g.Notify {
		notify = 1
	}
	return NewWriter(GoldRefreshSize).U8(PointsTypeGold).U64(g.Balance).U8(notify).Payload()
}

/*
================
DecodeGoldRefresh
================
*/
func DecodeGoldRefresh(payload []byte) (GoldRefresh, error) {
	var out GoldRefresh
	r := NewReader(payload)
	kind, err := r.U8()
	if err != nil {
		return out, err
	}
	if kind != PointsTypeGold {
		return out, fmt.Errorf("gold refresh: subtype %d", kind)
	}
	if out.Balance, err = r.U64(); err != nil {
		return out, err
	}
	notify, err := r.U8()
	if err != nil {
		return out, err
	}
	out.Notify = notify != 0
	return out, r.Done()
}

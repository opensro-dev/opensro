/*
===========================================================================

cosride.go - the COS ride toggle request (0x74B5) and its refusal

The COS window's ride button (CICCos_ExecuteActionCommand action 1,
6A24F1/6A2506 -> CNetProcess_SendCosRideToggle74B5 6FFB40) sends
[u8 rideState][u32 cosGid]: 1 rides the summoned horse or transport, 0
gets off. The server answers on the ride-state opcode 0xB4B5 (v1.188
0xB0CB, CGObjPC_HandleMountToggleRequest 5119B0): mode 1 on success
(EncodeCosRideState) or mode 2 [u8 2][u8 code] on refusal, which the
client shows as system notice category 0xE (CPSMission_OnCOSRideStateB4B5
777F60).

===========================================================================
*/

package wire

import "fmt"

// OpCosRideToggleRequest is the client's ride/get-off request (0x74B5).
const OpCosRideToggleRequest uint16 = 0x74B5

// Ride refusal codes (CCOSManager_TryBindRideActor 4FC4D0 and the request
// handler). The client prints 4, 6, 7, 8, 0xA and 0xB; the rest are silent.
const (
	CosRideRefusedRequest  uint8 = 0x02 // malformed state; also bind without an actor body
	CosRideRefusedNoTarget uint8 = 0x03 // no vehicle GID / nothing ridden
	CosRideTooFar          uint8 = 0x04 // UIIT_MSG_CMSERR_TOO_FAR (over 30 units)
	CosRideRefusedState    uint8 = 0x05 // busy motion, already riding, or not riding
	CosRideNotMyCOS        uint8 = 0x07 // UIIT_MSG_CMSERR_ITS_NOT_MY_COS_OBJ
	CosRideCannotRide      uint8 = 0x08 // UIIT_MSG_CMSERR_CANT_RIDE_ON_IT
	CosRideRefusedLife     uint8 = 0x09 // the rider is not alive
	CosRideInBattle        uint8 = 0x0B // UIIT_MSG_COS_CAN_NOT_RIDE_BATTLE
)

/*
================
CosRideToggle
================
*/
type CosRideToggle struct {
	RideState uint8
	CosGid    uint32
}

/*
================
DecodeCosRideToggle
================
*/
func DecodeCosRideToggle(payload []byte) (CosRideToggle, error) {
	if len(payload) != 5 {
		return CosRideToggle{}, fmt.Errorf("cos ride toggle: payload is %d bytes, want 5", len(payload))
	}
	r := NewReader(payload)
	state, err := r.U8()
	if err != nil {
		return CosRideToggle{}, err
	}
	gid, err := r.U32()
	if err != nil {
		return CosRideToggle{}, err
	}
	return CosRideToggle{RideState: state, CosGid: gid}, nil
}

/*
================
EncodeCosRideRefusal

The mode-2 0xB4B5 body: [u8 2][u8 code].
================
*/
func EncodeCosRideRefusal(code uint8) []byte {
	return NewWriter(2).U8(2).U8(code).Payload()
}

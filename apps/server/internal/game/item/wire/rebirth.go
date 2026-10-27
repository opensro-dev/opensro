package wire

import "fmt"

const (
	RebirthAtSpecifiedPoint uint8 = 1
	RebirthAtPresentPoint   uint8 = 2
)

// DecodeLocalRebirthRequest pins sub_6971f0's 0x32DC body: exactly one
// choice byte. The level gate for choice 2 is authority policy, not wire
// decoding, and therefore lives in action.
func DecodeLocalRebirthRequest(payload []byte) (uint8, error) {
	if len(payload) != 1 {
		return 0, fmt.Errorf("local rebirth payload length %d, want 1", len(payload))
	}
	choice := payload[0]
	if choice != RebirthAtSpecifiedPoint && choice != RebirthAtPresentPoint {
		return 0, fmt.Errorf("local rebirth choice %d is outside 1..2", choice)
	}
	return choice, nil
}

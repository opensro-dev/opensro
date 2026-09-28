/*
===========================================================================

withdrawal.go - v1.150 skill and mastery restoration packets

Client senders 701D60/701E40 carry the potion reference, learned identity,
and desired rank. The rank is absolute, not a number of levels to remove.
Handlers 75BCE0/75BDB0 consume a one-byte error or the committed identity.

===========================================================================
*/
package wire

const (
	OpSkillWithdrawalRequest    uint16 = 0x74D6
	OpMasteryWithdrawalRequest  uint16 = 0x7606
	OpSkillWithdrawalResponse   uint16 = 0xB4D6
	OpMasteryWithdrawalResponse uint16 = 0xB606
)

/*
================
WithdrawalRequest
================
*/
type WithdrawalRequest struct {
	PotionID  uint32
	LearnedID uint32
	Rank      uint8
}

/*
================
DecodeWithdrawalRequest
================
*/
func DecodeWithdrawalRequest(payload []byte) (WithdrawalRequest, error) {
	r := NewReader(payload)
	item, err := r.U32()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	id, err := r.U32()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	rank, err := r.U8()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if err = r.Done(); err != nil {
		return WithdrawalRequest{}, err
	}
	return WithdrawalRequest{PotionID: item, LearnedID: id, Rank: rank}, nil
}

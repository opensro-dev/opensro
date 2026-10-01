/*
===========================================================================

itemmall.go - account currencies consumed by native Item Mall purchases

Funding is outside the game contract. Purchases debit an account and persist
the resulting character inventory as one authority operation.

===========================================================================
*/
package domain

/*
================
MallBalance
================
*/
type MallBalance struct {
	Silk     uint32 `json:"silk"`
	GiftSilk uint32 `json:"giftSilk"`
	Points   uint32 `json:"points"`
}

/*
================
MallInsufficientCurrency

Preserves the native refusal category without leaking database errors.
================
*/
type MallInsufficientCurrency struct{}

/*
================
Error
================
*/
func (MallInsufficientCurrency) Error() string {
	return "mall: insufficient currency"
}

/*
================
MallAuthority

The grant callback runs with the account and character locked. It receives a
detached inventory and must return a complete replacement without reentering
the authority. A failed grant or durable commit changes neither plane.
================
*/
type MallAuthority interface {
	MallBalance(character *Character) (MallBalance, error)
	PurchaseMall(character *Character, cost MallBalance, grant func([]InventoryRow) ([]InventoryRow, error)) (MallBalance, error)
}

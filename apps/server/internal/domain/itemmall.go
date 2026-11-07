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
SilkHistoryRow

One recorded silk use: when it happened (unix milliseconds) and how much
silk it charged (the purchase's silk and gift silk together; points are a
separate currency and never part of the charge). Port-only bookkeeping:
the v1.150 client ships the history window but records
nothing, so the store keeps the rows the window lists.
================
*/
type SilkHistoryRow struct {
	AtMs    int64
	Charged uint32
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
	// SilkHistory lists the account's recorded silk uses inside the period
	// (newest first, at most limit rows) with the period's total charge.
	SilkHistory(character *Character, periodDays int, limit int) ([]SilkHistoryRow, uint32, error)
}

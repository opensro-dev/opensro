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

The account's mall wallet. Silk is two balances since #4133 (port-only):
EarnedSilk is what active play credited (the beta silk hour), and Silk
is granted silk (the starter, referral rewards, operator and GM grants,
market sale proceeds). Every balance a wallet held before the split is
granted. A purchase draws earned first; what it drew decides the bought
item's bound flag (see MallAuthority.PurchaseMall).
================
*/
type MallBalance struct {
	Silk       uint32 `json:"silk"`
	EarnedSilk uint32 `json:"earnedSilk"`
	GiftSilk   uint32 `json:"giftSilk"`
	Points     uint32 `json:"points"`
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
separate currency and never part of the charge). EarnedSilk is the part
of the silk charge the earned balance paid (#4133: the history keeps the
split; uses recorded before it keep zero, so they read as all granted).
Port-only bookkeeping: the v1.150 client ships the history window but
records nothing, so the store keeps the rows the window lists.
================
*/
type SilkHistoryRow struct {
	AtMs       int64
	Charged    uint32
	EarnedSilk uint32
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
	// The grant callback learns whether the purchase is paid wholly
	// from earned silk: the price is silk alone and the earned balance
	// covers all of it. Since #4133 that one fact sets the delivered
	// items' bound flag (earned-bought items trade, anything else is
	// account-bound), so the store decides it inside the debit, where
	// the live balances are.
	PurchaseMall(character *Character, cost MallBalance, grant func(rows []InventoryRow, earnedOnly bool) ([]InventoryRow, error)) (MallBalance, error)
	// SilkHistory lists the account's recorded silk uses inside the period
	// (newest first, at most limit rows) with the period's total charge.
	SilkHistory(character *Character, periodDays int, limit int) ([]SilkHistoryRow, uint32, error)
}

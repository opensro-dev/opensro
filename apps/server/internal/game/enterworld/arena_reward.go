/*
===========================================================================
arena_reward.go - port-only earned silk voucher; already earned items stay usable.
===========================================================================
*/
package enterworld

import "opensro.online/server/internal/domain"

/*
================
ArenaSilkScroll
================
*/
func ArenaSilkScroll() *ItemRef {
	return &ItemRef{RefObjID: domain.ArenaSilkScrollID, Codename: domain.ArenaSilkScrollCode, Name: "Silk scroll (10 silk)", Icon: "item/etc/scroll_return_01.ddj", TypeIDs: [4]int64{3, 3, 3, 0}, Country: 3, NativeFields: NewNativeFields(map[string]float64{"canUse": 1, "maxStack": domain.ArenaSilkScrollMaxStack, "sellPrice": 0})}
}

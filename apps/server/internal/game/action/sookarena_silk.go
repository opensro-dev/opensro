/*
===========================================================================
sookarena_silk.go - earned vouchers use the account/inventory transaction door.
===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

type arenaSilkRedeemer interface {
	RedeemArenaSilk(*domain.Character, uint8) (domain.MallBalance, error)
}

/*
================
useArenaSilk
================
*/
func (rt *Runtime) useArenaSilk(c *enterworld.Character, q wire.ItemUseRequest, tail []byte) (OpResult, bool) {
	found := false
	for _, row := range c.MissionInventory {
		if row.Slot == int64(q.Slot) && row.RefObjID == domain.ArenaSilkScrollID {
			found = true
		}
	}
	if !found {
		return OpResult{}, false
	}
	authority, ok := rt.mallAuthority.(arenaSilkRedeemer)
	if rt.sookArena == nil || !ok || len(tail) != 0 || q.TypeWord != enterworld.ArenaSilkScroll().TypeFlags() || !enterworld.CharacterAlive(c) {
		return itemUseFailure(wire.ErrCodeInvalidRequest), true
	}
	balance, err := authority.RedeemArenaSilk(c, q.Slot)
	if err != nil {
		return itemUseFailure(wire.ErrCodeInvalidRequest), true
	}
	var remaining uint16
	for _, row := range c.MissionInventory {
		if row.Slot == int64(q.Slot) && row.RefObjID == domain.ArenaSilkScrollID {
			remaining = uint16(row.StackCount)
			break
		}
	}
	payload, _ := json.Marshal(balance)
	return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(q.Slot, remaining, q.TypeWord)}, {Opcode: opMallBalance, Payload: payload}, sookBanner("Silk scroll redeemed: +10 silk.")}}, true
}

/*
================
RedeemArenaSilk - preserve beta wallet observation around the same atomic door.
================
*/
func (b *BetaSilk) RedeemArenaSilk(c *domain.Character, slot uint8) (domain.MallBalance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c == nil {
		return domain.MallBalance{}, fmt.Errorf("voucher owner unavailable")
	}
	if _, err := b.wallet.GrantBetaSilkStarter(c.AccountID, betaSilkStarter); err != nil {
		return domain.MallBalance{}, err
	}
	a, ok := b.wallet.(arenaSilkRedeemer)
	if !ok {
		return domain.MallBalance{}, fmt.Errorf("voucher wallet unavailable")
	}
	balance, err := a.RedeemArenaSilk(c, slot)
	if err == nil {
		b.observeBalance(c.AccountID, balance)
	}
	return balance, err
}

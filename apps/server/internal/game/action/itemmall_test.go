/*
===========================================================================

itemmall_test.go - mall handler admission and atomic delivery boundaries

Persistence concurrency and SQL binding are tested in store. These tests
exercise the actual 706D dispatch and catalogue-address trust boundary.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

/*
================
mallTestAuthority
================
*/
type mallTestAuthority struct {
	balance domain.MallBalance
	calls   int
	fail    bool
}

/*
================
MallBalance
================
*/
func (a *mallTestAuthority) MallBalance(*domain.Character) (domain.MallBalance, error) {
	return a.balance, nil
}

/*
================
PurchaseMall
================
*/
func (a *mallTestAuthority) PurchaseMall(c *domain.Character, cost domain.MallBalance, grant func([]domain.InventoryRow) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	a.calls++
	if cost.Silk > a.balance.Silk || cost.GiftSilk > a.balance.GiftSilk || cost.Points > a.balance.Points {
		return a.balance, domain.MallInsufficientCurrency{}
	}
	rows, err := grant(c.Snapshot().MissionInventory)
	if err != nil {
		return a.balance, err
	}
	if a.fail {
		return a.balance, errors.New("durable commit failed")
	}
	a.balance.Silk -= cost.Silk
	a.balance.GiftSilk -= cost.GiftSilk
	a.balance.Points -= cost.Points
	c.MissionInventory = rows
	return a.balance, nil
}

/*
================
mallFixture
================
*/
func mallFixture() (*Runtime, *enterworld.Character, *mallTestAuthority, wire.MallPurchase) {
	c := testCharacter()
	refs := testItems()
	rt, _ := newTestRuntime(c, refs)
	authority := &mallTestAuthority{balance: domain.MallBalance{Silk: 100, GiftSilk: 20, Points: 10}}
	rt.mallAuthority = authority
	rt.mallCatalog = &commerce.MallCatalog{Offers: []commerce.MallPackage{{MallAddress: commerce.MallAddress{Group: 852, Shop: 2, Tab: 3, Slot: 4}, PackageID: 100, CurrencyMask: 22, Silk: 30, GiftSilk: 2, AllowsPoints: true, PurchaseLimit: 5, Contents: []commerce.Content{{Ref: refs["ITEM_ETC_HP_POTION_01"], Stack: 50, Data: 10}}}}}
	q := wire.MallPurchase{Group: 852, Shop: 2, Tab: 3, Slot: 4, Package: 100, Quantity: 2, Points: 5}
	return rt, c, authority, q
}

/*
================
TestMallNativePurchaseDispatch
================
*/
func TestMallNativePurchaseDispatch(t *testing.T) {
	rt, c, authority, q := mallFixture()
	payload, err := q.Encode()
	if err != nil {
		t.Fatal(err)
	}
	result := rt.HandleItemMove(testDivision, c, payload)
	if len(result.Frames) != 3 || result.Frames[0].Opcode != 14 || result.Frames[1].Opcode != opMallCatalog || result.Frames[2].Opcode != wire.OpItemMoveResponse {
		t.Fatalf("unexpected purchase frames: %+v", result)
	}
	want := domain.MallBalance{Silk: 45, GiftSilk: 16, Points: 5}
	if authority.balance != want {
		t.Fatalf("wrong debit: %+v", authority.balance)
	}
	var projection mallProjection
	if err := json.Unmarshal(result.Frames[1].Payload, &projection); err != nil {
		t.Fatal(err)
	}
	if projection.MallBalance != want || len(projection.Items) != 1 || projection.Items[0].Slot != 13 {
		t.Fatalf("wrong committed projection: %+v", projection)
	}
	item, ok := inventory.New(invItemsFromBag(c), domain.DefaultInventorySize).At(13)
	if !ok || item.Quantity != 20 {
		t.Fatalf("lost authored package quantity: %+v", item)
	}
	wantReply := []byte{1, 0x18, 0x54, 3, 2, 3, 4, 1, 13, 2, 0}
	if !bytes.Equal(result.Frames[2].Payload, wantReply) {
		t.Fatalf("wrong native ack: %x", result.Frames[2].Payload)
	}
}

/*
================
TestMallForgedPurchasesCannotMutate
================
*/
func TestMallForgedPurchasesCannotMutate(t *testing.T) {
	for _, kind := range []string{"group", "shop", "tab", "slot", "package", "quantity", "points", "point-policy", "silk-overflow", "gift-overflow", "funds", "full", "commit", "extra-bytes", "truncated", "zero-quantity", "deleted", "unbound"} {
		t.Run(kind, func(t *testing.T) {
			rt, c, authority, q := mallFixture()
			switch kind {
			case "group":
				q.Group++
			case "shop":
				q.Shop++
			case "tab":
				q.Tab++
			case "slot":
				q.Slot++
			case "package":
				q.Package++
			case "quantity":
				q.Quantity = math.MaxUint16
			case "points":
				q.Points = math.MaxUint32
			case "point-policy":
				rt.mallCatalog.Offers[0].AllowsPoints = false
			case "silk-overflow":
				rt.mallCatalog.Offers[0].Silk = math.MaxUint32
			case "gift-overflow":
				rt.mallCatalog.Offers[0].GiftSilk = math.MaxUint32
			case "funds":
				authority.balance.Silk = 1
			case "commit":
				authority.fail = true
			case "full":
				for slot := int64(inventory.EquipmentSlotEnd); slot < int64(domain.DefaultInventorySize); slot++ {
					if slot == 20 {
						continue
					}
					row := c.MissionInventory[0]
					row.Slot = slot
					c.MissionInventory = append(c.MissionInventory, row)
				}
			case "deleted":
				c.DeletePending = true
			}
			before := c.Snapshot()
			balance := authority.balance
			payload, err := q.Encode()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "extra-bytes":
				payload = append(payload, 0)
			case "truncated":
				payload = payload[:len(payload)-1]
			case "zero-quantity":
				payload[6] = 0
				payload[7] = 0
			case "unbound":
				c = nil
			}
			result := rt.HandleItemMove(testDivision, c, payload)
			if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
				t.Fatalf("forged purchase accepted: %+v", result)
			}
			if kind == "full" && result.Frames[0].Payload[1] != wire.ErrCodeStorageFull {
				t.Fatal("full bag lost its native refusal code")
			}
			if kind == "funds" && result.Frames[0].Payload[1] != wire.ErrCodeMallInsufficientCurrency {
				t.Fatal("insufficient currency lost its native refusal code")
			}
			if authority.balance != balance || c != nil && !reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
				t.Fatal("refusal mutated currency or inventory")
			}
		})
	}
}

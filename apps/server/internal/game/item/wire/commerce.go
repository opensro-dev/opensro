package wire

import "fmt"

const (
	MoveTypeCosInventory uint8 = 0x10
	MoveTypeCosPickup    uint8 = 0x11
	MoveTypeCosDrop      uint8 = 0x12
	MoveTypeCosToPlayer  uint8 = 0x1a
	MoveTypePlayerToCos  uint8 = 0x1b
	MoveTypeShopBuy      uint8 = 8
	MoveTypeShopSell     uint8 = 9
	MoveTypeCosShopBuy   uint8 = 0x13
	MoveTypeCosShopSell  uint8 = 0x14
)

// The client's ItemMoveRequest_Serialize (6980D3 / 6982C0), outgoing
// direction; client CGInterface_RequestItemMove binds the selected NPC
// GID. Decoding a layout does not authorize a transaction.
// Action commerce authorizes supported player transactions; COS storage
// transactions still require a COS inventory authority.
func (q ItemMoveRequest) encodeCommerce(w *Writer) error {
	if q.NpcGID == 0 || q.Quantity == 0 {
		return fmt.Errorf("wire: invalid commerce identity or quantity")
	}
	if q.MovementType == MoveTypeCosShopBuy || q.MovementType == MoveTypeCosShopSell {
		if q.CosGID == 0 {
			return fmt.Errorf("wire: absent commerce COS")
		}
		w.U32(q.CosGID)
	}
	if q.MovementType == MoveTypeShopBuy || q.MovementType == MoveTypeCosShopBuy {
		w.U8(q.ShopTab).U8(q.ShopSlot)
	} else {
		w.U8(q.SourceSlot)
	}
	w.U16(q.Quantity).U32(q.NpcGID)
	return nil
}
func (q *ItemMoveRequest) decodeCommerce(r *Reader) (err error) {
	if q.MovementType == MoveTypeCosShopBuy || q.MovementType == MoveTypeCosShopSell {
		if q.CosGID, err = r.U32(); err != nil {
			return err
		}
		if q.CosGID == 0 {
			return fmt.Errorf("wire: absent commerce COS")
		}
	}
	if q.MovementType == MoveTypeShopBuy || q.MovementType == MoveTypeCosShopBuy {
		if q.ShopTab, err = r.U8(); err != nil {
			return err
		}
		if q.ShopSlot, err = r.U8(); err != nil {
			return err
		}
	} else {
		if q.SourceSlot, err = r.U8(); err != nil {
			return err
		}
	}
	if q.Quantity, err = r.U16(); err != nil {
		return err
	}
	if q.NpcGID, err = r.U32(); err != nil {
		return err
	}
	if q.Quantity == 0 || q.NpcGID == 0 {
		return fmt.Errorf("wire: invalid commerce identity or quantity")
	}
	return nil
}

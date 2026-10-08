package alchemy

import (
	"encoding/binary"
	"fmt"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"sort"
)

const OpCompound uint16 = 0x716f
const OpCompoundResult uint16 = 0xb16f
const OpDissolve uint16 = 0x7549
const OpDissolveResult uint16 = 0xb549

type ProcessRequest struct {
	Mode     uint8
	Quantity uint32
	Slots    []uint8
	Cancel   bool
	// BagEnd is the requesting character's capacity byte (inventory.BagEnd),
	// set by the action owner: products land inside that bag.
	BagEnd uint8
}

func DecodeProcess(op uint16, p []byte) (ProcessRequest, error) {
	var r ProcessRequest
	if op == OpCompound && len(p) == 1 && p[0] == 1 {
		r.Cancel = true
		return r, nil
	}
	if op == OpDissolve {
		if len(p) != 3 || (p[0] != 1 && p[0] != 2) || (p[0] == 1 && p[2] != 12) {
			return r, Refusal(0x10)
		}
		r.Mode = 3
		r.Quantity = 1
		r.Slots = append([]uint8(nil), p[1:1+int(p[0])]...)
	} else {
		if op != OpCompound || len(p) < 8 || p[0] != 2 || (p[1] != 1 && p[1] != 2) {
			return r, Refusal(0x10)
		}
		r.Mode = p[1]
		r.Quantity = binary.LittleEndian.Uint32(p[2:6])
		count := int(p[6])
		if r.Quantity == 0 || count < 1 || count > 9 {
			return r, Refusal(0x10)
		}
		if r.Mode == 1 {
			if len(p) != 7+count {
				return r, Refusal(0x10)
			}
		} else {
			if count > 5 || len(p) != 12 {
				return r, Refusal(0x10)
			}
			for _, slot := range p[7+count:] {
				if slot != 12 {
					return r, Refusal(0x10)
				}
			}
		}
		r.Slots = append([]uint8(nil), p[7:7+count]...)
	}
	seen := map[uint8]bool{}
	for _, slot := range r.Slots {
		if slot < inventory.EquipmentSlotEnd || slot >= inventory.MaxBagEnd || seen[slot] {
			return ProcessRequest{}, Refusal(0x10)
		}
		seen[slot] = true
	}
	return r, nil
}

type Product struct {
	Reference Reference
	Quantity  uint32
	Plus      uint8
}
type ProcessOutcome struct {
	Items     []inventory.Item
	Completed uint32
}

func (c *Catalog) processInputs(items []inventory.Item, r ProcessRequest) (map[uint8]inventory.Item, error) {
	rows := map[uint8]inventory.Item{}
	for _, i := range items {
		if _, exists := rows[i.Slot]; exists {
			return nil, Refusal(0x10)
		}
		rows[i.Slot] = i
	}
	seen := map[uint8]bool{}
	for _, slot := range r.Slots {
		// items holds the character's bag only: a slot past its capacity
		// has no row and is refused below.
		if slot < inventory.EquipmentSlotEnd || slot >= inventory.MaxBagEnd || seen[slot] {
			return nil, Refusal(0x10)
		}
		seen[slot] = true
		i, ok := rows[slot]
		ref, known := c.Items[i.Codename]
		if !ok || !known || ref.ID != i.RefObjID || ref.Flags != i.TypeFlags || i.Quantity == 0 {
			return nil, Refusal(0x10)
		}
	}
	return rows, nil
}

func (c *Catalog) Compound(items []inventory.Item, r ProcessRequest, roll Roll) (ProcessOutcome, error) {
	var out ProcessOutcome
	if r.Cancel || (r.Mode != 1 && r.Mode != 2) || r.Quantity == 0 || len(r.Slots) == 0 || len(r.Slots) > 9 {
		return out, Refusal(0x10)
	}
	rows, err := c.processInputs(items, r)
	if err != nil {
		return out, err
	}
	consume := map[uint8]uint32{}
	products := []Product{}
	if r.Mode == 1 {
		// Batch ownership lives in action. This planner commits one native
		// material step; a total must never be multiplied across selected rows.
		if len(r.Slots) != 1 {
			return out, Refusal(0x10)
		}
		// 508BB0 resolves the four authored output/quantity pairs; 508750
		// charges (reference price / 1000 + 1) Rondo per material unit.
		var rondo uint64
		for _, slot := range r.Slots {
			i := rows[slot]
			ref := c.Items[i.Codename]
			if ref.Flags&0xfffe != 0x25ec || uint32(i.Quantity) < r.Quantity {
				return out, Refusal(8)
			}
			consume[slot] = r.Quantity
			rondo += uint64(ref.Price/1000+1) * uint64(r.Quantity)
			for k := 0; k < 4; k++ {
				product, ok := c.Items[ref.Descriptions[k]]
				if !ok || product.Flags&0xfffe != 0x2dec || ref.Params[k] == 0 || ref.Params[k] == 0xffffffff {
					return out, Refusal(8)
				}
				n := uint64(ref.Params[k]) * uint64(r.Quantity)
				if n > 0xffffffff {
					return out, Refusal(8)
				}
				products = append(products, Product{Reference: product, Quantity: uint32(n)})
			}
		}
		if err = c.consumeRondo(rows, consume, "ITEM_ETC_ARCHEMY_RONDO_01", rondo); err != nil {
			return out, err
		}
		out.Completed = r.Quantity
	} else {
		// 509450/509D00: one tablet produces one stone, with the four
		// exact element grades/amounts named by its reference row.
		if len(r.Slots) != 5 || r.Quantity != 1 {
			return out, Refusal(0x10)
		}
		tablet := rows[r.Slots[0]]
		ref := c.Items[tablet.Codename]
		if ref.Flags&0xfffe != 0x1dec {
			return out, Refusal(8)
		}
		consume[tablet.Slot] = 1
		for k := 0; k < 4; k++ {
			found := false
			for _, slot := range r.Slots[1:] {
				i := rows[slot]
				if i.Codename == ref.Descriptions[k] && ref.Params[k] > 0 && uint32(i.Quantity) >= ref.Params[k] {
					consume[i.Slot] = ref.Params[k]
					found = true
					break
				}
			}
			if !found {
				return out, Refusal(9)
			}
		}
		product, ok := c.Items[ref.Descriptions[4]]
		stone := product.Flags&0xfffe == 0x0dec || product.Flags&0xfffe == 0x15ec || product.Flags&0xfffe == 0x3dec
		potion := product.Flags == wire.PackTypeFlags(3, 3, 13, 1)
		if !ok || (!stone && !potion) {
			return out, Refusal(8)
		}
		// 509E4B..509E6A initializes Plus=0 and rolls assimilation only for
		// stones; the manufacture call at 509EB8 grants ONE output. Potion
		// tablets use the same five inputs but carry a skill/degree parameter,
		// not an assimilation distribution or manufactured quantity.
		var plus uint8
		if stone {
			var e error
			plus, e = stoneAssimilation(product, roll)
			if e != nil {
				return out, e
			}
		}
		products = append(products, Product{Reference: product, Quantity: 1, Plus: plus})
		out.Completed = 1
	}
	out.Items, err = allocateProducts(items, consume, products, r.BagEnd)
	return out, err
}

func stoneAssimilation(ref Reference, roll Roll) (uint8, error) {
	// Tablet manufacture, 509BC0: parameter 2 only, nonempty distribution.
	values := assimilationValues(ref.Params[1])
	if len(values) == 0 {
		return 0, fmt.Errorf("alchemy: missing assimilation distribution %s", ref.Name)
	}
	n, err := draw(roll, uint32(len(values)))
	if err != nil {
		return 0, err
	}
	return values[n], nil
}

func assimilationValues(params ...uint32) []uint8 {
	values := []uint8{}
	for _, param := range params {
		for shift := uint(0); shift < 32; shift += 8 {
			if v := uint8(param >> shift); v != 0 {
				values = append(values, v)
			}
		}
	}
	return values
}

func (c *Catalog) consumeRondo(rows map[uint8]inventory.Item, consume map[uint8]uint32, name string, amount uint64) error {
	for slot := inventory.EquipmentSlotEnd; slot < inventory.MaxBagEnd && amount > 0; slot++ {
		i, ok := rows[slot]
		if !ok || i.Codename != name {
			continue
		}
		ref, known := c.Items[name]
		if !known || ref.ID != i.RefObjID || ref.Flags != i.TypeFlags || consume[slot] > uint32(i.Quantity) {
			return Refusal(0x10)
		}
		n := min(uint64(uint32(i.Quantity)-consume[slot]), amount)
		consume[slot] += uint32(n)
		amount -= n
	}
	if amount != 0 {
		return Refusal(7)
	}
	return nil
}

func allocateProducts(items []inventory.Item, consume map[uint8]uint32, products []Product, bagEnd uint8) ([]inventory.Item, error) {
	if bagEnd <= inventory.EquipmentSlotEnd {
		return nil, fmt.Errorf("alchemy: the request names no bag (BagEnd %d)", bagEnd)
	}
	remaining := make([]inventory.Item, 0, len(items))
	for _, i := range items {
		n := consume[i.Slot]
		if n > uint32(i.Quantity) {
			return nil, Refusal(7)
		}
		i.Quantity -= uint16(n)
		if i.Quantity > 0 {
			remaining = append(remaining, i)
		}
	}
	inv := inventory.New(remaining, bagEnd)
	for _, p := range products {
		if p.Reference.Stack == 0 || p.Quantity == 0 {
			return nil, Refusal(8)
		}
		for left := p.Quantity; left > 0; {
			i := inventory.Item{RefObjID: p.Reference.ID, Codename: p.Reference.Name, TypeFlags: p.Reference.Flags, Quantity: uint16(min(left, 65535)), Plus: p.Plus}
			var granted uint32
			if p.Reference.Stack == 1 {
				i.Quantity = 1
				if _, err := inv.Grant(i); err != nil {
					return nil, Refusal(8)
				}
				granted = 1
			} else {
				g, err := inv.GrantStack(i, p.Reference.Stack)
				if err != nil {
					return nil, Refusal(8)
				}
				granted = uint32(i.Quantity - g.GroundRemainder)
			}
			if granted == 0 {
				return nil, Refusal(8)
			}
			left -= granted
		}
	}
	return inv.Items(), nil
}

// Material deltas/removals precede grants. New rows use the native inventory
// grant packet; 3645 cannot create a row at a previously absent slot.
func ProcessFrames(op uint16, before []inventory.Item, out ProcessOutcome) []wire.Frame {
	frames := []wire.Frame{}
	next := map[uint8]inventory.Item{}
	old := map[uint8]inventory.Item{}
	for _, i := range out.Items {
		next[i.Slot] = i
	}
	for _, i := range before {
		old[i.Slot] = i
	}
	for _, i := range before {
		n, ok := next[i.Slot]
		quantity := uint16(0)
		if ok && n.RefObjID == i.RefObjID && n.Plus == i.Plus {
			quantity = n.Quantity
		}
		if quantity != i.Quantity {
			frames = append(frames, wire.Frame{Opcode: 0x3645, Payload: wire.NewWriter(4).U8(i.Slot).U8(8).U16(quantity).Payload()})
		}
	}
	ordered := append([]inventory.Item(nil), out.Items...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Slot < ordered[j].Slot })
	for _, i := range ordered {
		p, ok := old[i.Slot]
		if !ok || p.RefObjID != i.RefObjID || p.Plus != i.Plus {
			frames = append(frames, wire.Frame{Opcode: 0xb06d, Payload: wire.EncodePickupItemResult(i.Slot, i.Body())})
		}
	}
	p := []byte{1}
	if op == OpCompoundResult {
		p = wire.NewWriter(6).U8(1).U8(2).U32(out.Completed).Payload()
	}
	return append(frames, wire.Frame{Opcode: op, Payload: p})
}

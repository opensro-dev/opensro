package alchemy

import (
	"fmt"
	"opensro.online/server/internal/game/item/inventory"
)

type WeightedStone struct {
	Codename string
	Weight   uint32
}
type DissolvePool struct{ Attribute, Magic []WeightedStone }

// Dissolve keeps the reward plan detached until every draw, reference lookup,
// Rondo requirement and output slot has been admitted. Native 50B4E0 requires
// both selectors even when the eventual count of bonus stones is zero.
func (c *Catalog) Dissolve(items []inventory.Item, r ProcessRequest, roll Roll) (ProcessOutcome, error) {
	var out ProcessOutcome
	if r.Mode != 3 || r.Quantity != 1 || len(r.Slots) < 1 || len(r.Slots) > 2 {
		return out, Refusal(0x12)
	}
	rows, err := c.processInputs(items, r)
	if err != nil {
		return out, err
	}
	// v1.150's form sends the destruction Rondo first, then equipment.
	// The later server also admits a lone equipment selection and finds Rondo.
	if len(r.Slots) == 2 && rows[r.Slots[0]].Codename != "ITEM_ETC_ARCHEMY_RONDO_02" {
		return out, Refusal(0x12)
	}
	target := rows[r.Slots[len(r.Slots)-1]]
	ref := c.Items[target.Codename]
	degree := ref.Degree()
	// 50C070/50C160: job equipment and either repair restriction refuse
	// dissolution. Presence is the gate, even when the encoded value is zero.
	if ref.Flags&0x7fe == 0x72c {
		return out, Refusal(0x17)
	}
	for _, value := range target.MagicOptions {
		option, known := c.Magic[uint16(value)]
		if !known {
			return out, Refusal(6)
		}
		if option.Tag == 0x6e726570 {
			return out, Refusal(0x13)
		}
		if option.Tag == 0x00726570 {
			return out, Refusal(0x15)
		}
	}
	if category(ref.Flags) == "" || ref.Flags&2 != 0 || degree < 1 || degree > 12 || target.Quantity != 1 || len(target.MagicOptions) > 12 {
		return out, Refusal(6)
	}
	pool, ok := c.DissolveDrops[degree]
	if !ok || len(pool.Attribute) == 0 || len(pool.Magic) == 0 {
		return out, fmt.Errorf("alchemy: no admitted dissolution assignment pools for degree %d", degree)
	}
	consume := map[uint8]uint32{target.Slot: 1}
	if err = c.consumeRondo(rows, consume, "ITEM_ETC_ARCHEMY_RONDO_02", uint64(ref.Price/20000+1)); err != nil {
		return out, err
	}
	// 50B520/50B580 constants; 50B6F9 and 50B731 choose two ordered,
	// distinct element pairs, then two amounts per pair.
	divisors := [12]float64{75, 140, 215, 290, 365, 458, 551, 660, 776, 903, 1035, 1180}
	stoneDivisors := [12]float64{18750, 34950, 53700, 72340, 91150, 114280, 137690, 164830, 193830, 225240, 258180, 294706}
	pairs := [12][2]int{{0, 1}, {0, 2}, {0, 3}, {1, 0}, {1, 2}, {1, 3}, {2, 0}, {2, 1}, {2, 3}, {3, 0}, {3, 1}, {3, 2}}
	a, err := draw(roll, 12)
	if err != nil {
		return out, err
	}
	b, err := draw(roll, 12)
	if err != nil {
		return out, err
	}
	products := []Product{}
	names := [4]string{"EARTH", "WATER", "FIRE", "WIND"}
	price := float64(float32(ref.Price))
	base := price / divisors[degree-1]
	for i := 0; i < 2; i++ {
		x, e := draw(roll, 6)
		if e != nil {
			return out, e
		}
		y, e := draw(roll, 6)
		if e != nil {
			return out, e
		}
		counts := [2]uint32{uint32((float64(x+15)/100+float64(float32(0.025))*float64(target.Plus))*base) + 1, uint32((float64(y+10)/100+float64(float32(0.02))*float64(target.Plus))*base) + 1}
		for k, kind := range []int{pairs[a][i], pairs[b][i]} {
			name := fmt.Sprintf("ITEM_ETC_ARCHEMY_ELEMENT_%s_%02d", names[kind], degree)
			product, known := c.Items[name]
			if !known {
				return out, fmt.Errorf("alchemy: missing element %s", name)
			}
			products = append(products, Product{Reference: product, Quantity: counts[k]})
		}
	}
	for kind, choices := range [][]WeightedStone{pool.Attribute, pool.Magic} {
		x, e := draw(roll, 6)
		if e != nil {
			return out, e
		}
		factor := uint32(1)
		if kind == 1 {
			factor = uint32(len(target.MagicOptions))
		}
		count := min(uint32(float64(x+5)*float64(factor)*price/100/stoneDivisors[degree-1]), 2)
		for i := uint32(0); i < 2; i++ {
			product, e := c.selectDissolveStone(choices, degree, kind, roll)
			if e != nil {
				return out, e
			}
			plus, e := dissolveAssimilation(product, roll)
			if e != nil {
				return out, e
			}
			if i < count {
				products = append(products, Product{Reference: product, Quantity: 1, Plus: plus})
			}
		}
	}
	out.Items, err = allocateProducts(items, consume, products, r.BagEnd)
	out.Completed = 1
	return out, err
}

// 50AE10 reads parameter 2 AND 3, in low-byte-first order, and returns zero
// without drawing when both are empty (e.g. Solid/Luck). Manufacture differs.
func dissolveAssimilation(ref Reference, roll Roll) (uint8, error) {
	values := assimilationValues(ref.Params[1], ref.Params[2])
	if len(values) == 0 {
		return 0, nil
	}
	n, err := draw(roll, uint32(len(values)))
	if err != nil {
		return 0, err
	}
	return values[n], nil
}

func (c *Catalog) dissolveWeightTotal(choices []WeightedStone, degree, kind int) (uint32, error) {
	var total uint32
	for _, choice := range choices {
		r, ok := c.Items[choice.Codename]
		flags := r.Flags & 0xfffe
		if !ok || r.Degree() != degree || choice.Weight == 0 || choice.Weight > 32767-total || (kind == 0 && flags != 0x15ec) || (kind == 1 && flags != 0x0dec && flags != 0x3dec) {
			return 0, fmt.Errorf("alchemy: invalid dissolution selector")
		}
		total += choice.Weight
	}
	if total == 0 {
		return 0, fmt.Errorf("alchemy: empty dissolution selector")
	}
	return total, nil
}

func (c *Catalog) selectDissolveStone(choices []WeightedStone, degree, kind int, roll Roll) (Reference, error) {
	total, err := c.dissolveWeightTotal(choices, degree, kind)
	if err != nil {
		return Reference{}, err
	}
	x, err := draw(roll, total)
	if err != nil {
		return Reference{}, err
	}
	for _, choice := range choices {
		if x < choice.Weight {
			return c.Items[choice.Codename], nil
		}
		x -= choice.Weight
	}
	return Reference{}, fmt.Errorf("alchemy: invalid dissolution weight total")
}

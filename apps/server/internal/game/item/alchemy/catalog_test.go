package alchemy

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestPublishedCatalog(t *testing.T) {
	dir := os.Getenv("SRO_ALCHEMY_TEXTDATA")
	if dir == "" {
		t.Skip("set SRO_ALCHEMY_TEXTDATA to validate the published media")
	}
	c, err := LoadCatalog(filepath.Clean(dir), enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Items) < 1000 || len(c.Magic) < 100 {
		t.Fatal("incomplete published reference tables")
	}
	r := c.Items["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B"]
	if probability(r, 0) != 50 || probability(r, 11) != 12 {
		t.Fatalf("published elixir parameters %+v", r)
	}
	// Exercise the activated profile with real reference prices, flags, grades,
	// element stack limits and stone assimilation metadata for every category/grade.
	rondo := c.Items["ITEM_ETC_ARCHEMY_RONDO_02"]
	seen := map[string]bool{}
	refs := make([]Reference, 0, len(c.Items))
	for _, ref := range c.Items {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	for _, ref := range refs {
		cat, degree := category(ref.Flags), ref.Degree()
		key := fmt.Sprintf("%s/%d", cat, degree)
		if cat == "" || degree < 1 || degree > 12 || seen[key] {
			continue
		}
		seen[key] = true
		for _, rollValue := range []uint32{0, 32767} {
			items := []inventory.Item{
				{Slot: 13, RefObjID: ref.ID, Codename: ref.Name, TypeFlags: ref.Flags, Quantity: 1},
				{Slot: 14, RefObjID: rondo.ID, Codename: rondo.Name, TypeFlags: rondo.Flags, Quantity: 65535},
			}
			out, err := c.Dissolve(items, ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{14, 13}}, func() (uint32, error) { return rollValue, nil })
			if err != nil {
				t.Fatalf("%s %s: %v", key, ref.Name, err)
			}
			if out.Completed != 1 {
				t.Fatal(out)
			}
			for _, item := range out.Items {
				if item.Codename == ref.Name {
					t.Fatal("equipment survived", ref.Name)
				}
				if item.Codename != rondo.Name && c.Items[item.Codename].Degree() != degree {
					t.Fatal("cross-grade reward", item)
				}
			}
		}
	}
	for _, cat := range []string{"weapon", "armor", "shield", "accessory"} {
		for degree := 1; degree <= 9; degree++ {
			if !seen[fmt.Sprintf("%s/%d", cat, degree)] {
				t.Fatal("missing published category/grade", cat, degree)
			}
		}
	}
	t.Logf("dissolved %d published category/grade representatives with both entropy boundaries", len(seen))
}

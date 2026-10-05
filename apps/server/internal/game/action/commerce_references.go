/*
===========================================================================

commerce_references.go - item reference deltas and immutable merchandise seeds

Reference metadata is shared by bootstrap and live transactions. It never
contains inventory ownership or quantities.

===========================================================================
*/
package action

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"sort"
)

// A public reference delta contains no item owner, slot, quantity or modifiers.
// Publish before new equipment can appear in a peer's native equip/spawn packet.
const opCommerceItemReferences uint16 = 14

/*
================
commerceReferences
================
*/
func (rt *Runtime) commerceReferences(items, before []inventory.Item) wire.Frame {
	rows := []inventory.Item{}
	for _, item := range items {
		same := false
		for _, old := range before {
			if reflect.DeepEqual(item, old) {
				same = true
				break
			}
		}
		if !same {
			rows = append(rows, item)
		}
	}
	type ref struct {
		DescriptionSymbol string                  `json:"descriptionSymbol,omitempty"`
		ID                uint32                  `json:"refObjId"`
		Type              uint16                  `json:"typeFlags"`
		Name              string                  `json:"name"`
		MaxStack          uint16                  `json:"maxStack"`
		Icon              string                  `json:"icon,omitempty"`
		NativeFields      enterworld.NativeFields `json:"nativeFields,omitempty"`
	}
	refs := []ref{}
	seen := map[uint32]bool{}
	for _, row := range rows {
		if !seen[row.RefObjID] {
			seen[row.RefObjID] = true
			name := row.Codename
			icon := ""
			descriptionSymbol := ""
			var nativeFields enterworld.NativeFields
			if reference, ok := rt.deps.ItemReferences().ItemRefByCodename(row.Codename); ok && reference != nil {
				name = reference.Name
				icon = reference.Icon
				descriptionSymbol = reference.DescriptionSymbol
				nativeFields = enterworld.ItemUseNativeFields(reference, rt.deps.SkillData())
			}
			refs = append(refs, ref{descriptionSymbol, row.RefObjID, row.TypeFlags, name, rt.maxStackFor(row.TypeFlags, row.Codename), icon, nativeFields})
		}
	}
	b, _ := json.Marshal(struct {
		Version int   `json:"version"`
		Items   []ref `json:"items"`
	}{1, refs})
	return wire.Frame{Opcode: opCommerceItemReferences, Payload: b}
}

// The immutable merchandise dictionary is retained compressed and decoded into
// detached frames at world readiness; its wire encoding is unchanged.
// It closes the bootstrap/purchase race without forwarding inventory contents.
/*
================
CommerceReferenceSeed
================
*/
func (rt *Runtime) CommerceReferenceSeed() []wire.Frame {
	frames := make([]wire.Frame, len(rt.commerceReferenceSeed))
	for i, f := range rt.commerceReferenceSeed {
		reader, err := gzip.NewReader(bytes.NewReader(f.Payload))
		if err != nil {
			panic(err)
		}
		payload, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			panic(err)
		}
		frames[i] = wire.Frame{Opcode: f.Opcode, Payload: payload}
	}
	return frames
}

/*
================
prepareCommerceReferences
================
*/
func (rt *Runtime) prepareCommerceReferences() {
	rt.commerceReferenceSeed = nil
	seen := map[uint32]bool{}
	items := []inventory.Item{}
	for _, offers := range rt.Commerce.Tabs {
		for _, offer := range offers {
			for _, content := range offer.Contents {
				ref := content.Ref
				if seen[ref.RefObjID] {
					continue
				}
				seen[ref.RefObjID] = true
				items = append(items, inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags()})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RefObjID < items[j].RefObjID })
	for len(items) > 0 {
		n := len(items)
		if n > 64 {
			n = 64
		}
		frame := rt.commerceReferences(items[:n], nil)
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(frame.Payload); err != nil {
			panic(err)
		}
		if err := writer.Close(); err != nil {
			panic(err)
		}
		frame.Payload = compressed.Bytes()
		rt.commerceReferenceSeed = append(rt.commerceReferenceSeed, frame)
		items = items[n:]
	}
}

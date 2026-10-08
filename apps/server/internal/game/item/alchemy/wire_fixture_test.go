package alchemy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
)

// This corpus is also ingested by client-next's production inventory owner.
// UPDATE_ALCHEMY_WIRE_FIXTURE=1 regenerates it from the Go rule/packet owners.
func TestWireFixture(t *testing.T) {
	type item struct {
		Slot     uint8  `json:"slot"`
		ID       uint32 `json:"refObjId"`
		Flags    uint16 `json:"typeFlags"`
		Body     []int  `json:"body"`
		Plus     uint8  `json:"plus"`
		Quantity uint16 `json:"quantity"`
	}
	type frame struct {
		Opcode  uint16 `json:"opcode"`
		Payload []int  `json:"payload"`
	}
	type scenario struct {
		Name    string  `json:"name"`
		Mode    string  `json:"mode"`
		Before  []item  `json:"before"`
		After   []item  `json:"after"`
		Frames  []frame `json:"frames"`
		Success bool    `json:"success"`
		Slots   []int   `json:"slots,omitempty"`
	}
	ints := func(p []byte) []int {
		out := make([]int, len(p))
		for i, v := range p {
			out[i] = int(v)
		}
		return out
	}
	pack := func(rows []inventory.Item) []item {
		out := []item{}
		for _, r := range rows {
			out = append(out, item{r.Slot, r.RefObjID, r.TypeFlags, ints(r.Body().Encode()), r.Plus, r.Quantity})
		}
		return out
	}
	corpus := []scenario{}
	for _, name := range []string{"reinforce-win", "reinforce-destroy", "magic-win", "magic-failure", "attribute-win"} {
		c, items := fixture()
		items = items[:2]
		op := OpReinforceResult
		mode := "reinforce"
		var result Outcome
		var err error
		switch name {
		case "reinforce-win":
			result, err = c.Reinforce(items, []uint8{13, 14}, 0, sequence(t, 0))
		case "reinforce-destroy":
			items[0].Plus = 5
			result, err = c.Reinforce(items, []uint8{13, 14}, 0, sequence(t, 99, 0))
		default:
			magic := name != "attribute-win"
			c, items = stoneFixture(magic)
			op = OpStoneResult
			mode = "magic"
			if !magic {
				mode = "attribute"
			}
			draws := []uint32{0, 1, 100}
			if name == "magic-failure" {
				draws = []uint32{99}
			}
			if !magic {
				draws = []uint32{0, 2, 3, 4, 5, 100}
			}
			result, err = c.Stone(items, []uint8{13, 14}, magic, 0, sequence(t, draws...))
		}
		if err != nil {
			t.Fatal(err)
		}
		s := scenario{Name: name, Mode: mode, Before: pack(items), After: pack(result.Items), Success: result.Success}
		for _, f := range ResultFrames(op, items, result) {
			s.Frames = append(s.Frames, frame{f.Opcode, ints(f.Payload)})
		}
		corpus = append(corpus, s)
	}
	c, inputs := processFixture()
	request := ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 2, Quantity: 1, Slots: []uint8{13, 14, 15, 16, 17}}
	result, err := c.Compound(inputs, request, sequence(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	s := scenario{Name: "tablet-manufacture", Mode: "advanced", Before: pack(inputs), After: pack(result.Items), Success: true, Slots: ints(request.Slots)}
	for _, f := range ProcessFrames(OpCompoundResult, inputs, result) {
		s.Frames = append(s.Frames, frame{f.Opcode, ints(f.Payload)})
	}
	corpus = append(corpus, s)
	c, inputs = dissolveProfileFixture(t)
	request = ProcessRequest{BagEnd: domain.DefaultInventorySize, Mode: 3, Quantity: 1, Slots: []uint8{14, 13}}
	result, err = c.Dissolve(inputs, request, func() (uint32, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	s = scenario{Name: "dissolve-inferred-rewards", Mode: "dissolve", Before: pack(inputs), After: pack(result.Items), Success: true, Slots: ints(request.Slots)}
	for _, f := range ProcessFrames(OpDissolveResult, inputs, result) {
		s.Frames = append(s.Frames, frame{f.Opcode, ints(f.Payload)})
	}
	corpus = append(corpus, s)
	body, err := json.MarshalIndent(corpus, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	path := filepath.Join("testdata", "alchemy_wire.json")
	if os.Getenv("UPDATE_ALCHEMY_WIRE_FIXTURE") == "1" {
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, body) {
		t.Fatal("Alchemy cross-client wire corpus drifted")
	}
}

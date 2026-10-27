package community

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// whisperBlockPayload composes the sub_704a00 wire body the client's REAL
// send chain builds (re-harness communitySendersParity: {u8 mode, u16
// len, ANSI name}).
func whisperBlockPayload(mode uint8, name string) []byte {
	writer := wire.NewWriter(3 + len(name))
	writer.U8(mode)
	writer.U16(uint16(len(name)))
	writer.Bytes([]byte(name))
	return writer.Payload()
}

func TestDecodeWhisperBlockRequest(t *testing.T) {
	cases := []struct {
		label   string
		payload []byte
		want    WhisperBlockRequest
		wantErr string
	}{
		{
			label:   "register",
			payload: whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			want:    WhisperBlockRequest{Mode: 1, Name: "Berk"},
		},
		{
			label:   "cancel",
			payload: whisperBlockPayload(WhisperBlockModeCancel, "Berk"),
			want:    WhisperBlockRequest{Mode: 2, Name: "Berk"},
		},
		{
			label:   "unknown mode",
			payload: whisperBlockPayload(3, "Berk"),
			wantErr: "not register(1)/cancel(2)",
		},
		{
			label:   "empty name",
			payload: whisperBlockPayload(WhisperBlockModeRegister, ""),
			wantErr: "name is empty",
		},
		{
			label:   "short name bytes",
			payload: []byte{0x01, 0x08, 0x00, 'B'},
			wantErr: "shorter than the layout",
		},
		{
			label:   "empty payload",
			payload: nil,
			wantErr: "shorter than the layout",
		},
		{
			label:   "trailing bytes",
			payload: append(whisperBlockPayload(WhisperBlockModeRegister, "Berk"), 0x00),
			wantErr: "trailing",
		},
		{
			label:   "name just under the 0x80 GameServer bound",
			payload: whisperBlockPayload(WhisperBlockModeRegister, strings.Repeat("B", 0x7F)),
			want:    WhisperBlockRequest{Mode: 1, Name: strings.Repeat("B", 0x7F)},
		},
		{
			label:   "name at the 0x80 GameServer bound",
			payload: whisperBlockPayload(WhisperBlockModeRegister, strings.Repeat("B", 0x80)),
			wantErr: "0x80",
		},
	}
	for _, c := range cases {
		got, err := DecodeWhisperBlockRequest(c.payload)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want containing %q", c.label, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected err %v", c.label, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: decoded %+v, want %+v", c.label, got, c.want)
		}
	}
}

func TestHandleWhisperBlockMutations(t *testing.T) {
	fullList := make([]string, WhisperBlockMaxCount)
	for i := range fullList {
		fullList[i] = "Name" + string(rune('A'+i))
	}
	cases := []struct {
		label       string
		before      []string
		payload     []byte
		wantApplied bool
		wantRefusal string
		wantAfter   []string
	}{
		{
			label:       "register onto empty list",
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantApplied: true,
			wantAfter:   []string{"Berk"},
		},
		{
			label:       "register appends in order",
			before:      []string{"Alder"},
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantApplied: true,
			wantAfter:   []string{"Alder", "Berk"},
		},
		{
			label:       "register duplicate refused",
			before:      []string{"Berk"},
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantRefusal: "already blocked",
			wantAfter:   []string{"Berk"},
		},
		{
			label:       "register case-folded duplicate refused",
			before:      []string{"Berk"},
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "berk"),
			wantRefusal: "already blocked",
			wantAfter:   []string{"Berk"},
		},
		{
			label:       "register over the 0x14 cap refused",
			before:      fullList,
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantRefusal: "block list full",
			wantAfter:   fullList,
		},
		{
			// The actor is "asd2": the native self-compare
			// (wire 2 through the block handler's remap) refuses.
			label:       "register self refused",
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "asd2"),
			wantRefusal: "cannot block yourself",
			wantAfter:   nil,
		},
		{
			// The native quote scan (0x27 / 0x22):
			// the list never changes and the {1,4} no-op ack answers.
			label:       "register quoted name refused",
			payload:     whisperBlockPayload(WhisperBlockModeRegister, "O'Brien"),
			wantRefusal: "quote",
			wantAfter:   nil,
		},
		{
			label:       "cancel removes",
			before:      []string{"Alder", "Berk", "Cale"},
			payload:     whisperBlockPayload(WhisperBlockModeCancel, "Berk"),
			wantApplied: true,
			wantAfter:   []string{"Alder", "Cale"},
		},
		{
			label:       "cancel matches case-insensitively",
			before:      []string{"Alder", "Berk", "Cale"},
			payload:     whisperBlockPayload(WhisperBlockModeCancel, "BERK"),
			wantApplied: true,
			wantAfter:   []string{"Alder", "Cale"},
		},
		{
			label:       "cancel miss refused",
			before:      []string{"Alder"},
			payload:     whisperBlockPayload(WhisperBlockModeCancel, "Berk"),
			wantRefusal: "not on the block list",
			wantAfter:   []string{"Alder"},
		},
		{
			label:       "malformed payload refused",
			before:      []string{"Alder"},
			payload:     []byte{0x01},
			wantRefusal: "shorter than the layout",
			wantAfter:   []string{"Alder"},
		},
	}
	for _, c := range cases {
		character := &enterworld.Character{ID: 1, Name: "asd2", BlockedWhisperers: c.before}
		var labels []string
		deps := &enterworld.Deps{
			// Register now requires the target to EXIST (retail
			// _CharNameList); "Berk" is the only name these cases add.
			Characters: enterworld.StaticCharacterSource{
				"global-official": {character, {ID: 2, Name: "Berk"}},
			},
			MutateCharacter: func(mc *enterworld.Character, label string, fn func()) {
				labels = append(labels, label)
				fn()
			},
		}
		outcome := HandleWhisperBlock(deps, "global-official", character, c.payload)
		if outcome.Applied != c.wantApplied {
			t.Errorf("%s: applied = %v, want %v (refusal %q)", c.label, outcome.Applied, c.wantApplied, outcome.Refusal)
		}
		if c.wantRefusal != "" && !strings.Contains(outcome.Refusal, c.wantRefusal) {
			t.Errorf("%s: refusal = %q, want containing %q", c.label, outcome.Refusal, c.wantRefusal)
		}
		if c.wantRefusal == "" && outcome.Refusal != "" {
			t.Errorf("%s: unexpected refusal %q", c.label, outcome.Refusal)
		}
		wantAfter := c.wantAfter
		if wantAfter == nil {
			wantAfter = []string{}
		}
		gotAfter := character.BlockedWhisperers
		if gotAfter == nil {
			gotAfter = []string{}
		}
		if !reflect.DeepEqual(gotAfter, wantAfter) {
			t.Errorf("%s: list after = %v, want %v", c.label, gotAfter, wantAfter)
		}
		if outcome.Applied {
			if len(labels) == 0 || labels[len(labels)-1] != "whisper-block" {
				t.Errorf("%s: mutation did not commit through the door with label whisper-block (labels %v)", c.label, labels)
			}
		}
	}
}

// TestHandleWhisperBlockCopyThenSwap pins the snapshot-safety convention:
// a slice alias taken before the mutation (a character snapshot) must
// keep its view - the handler installs a NEW slice rather than writing
// through the old backing array.
func TestHandleWhisperBlockCopyThenSwap(t *testing.T) {
	character := &enterworld.Character{ID: 1, Name: "asd2", BlockedWhisperers: []string{"Alder"}}
	snapshot := character.BlockedWhisperers
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{
			"global-official": {character, {ID: 2, Name: "Berk"}},
		},
	}
	outcome := HandleWhisperBlock(deps, "global-official", character, whisperBlockPayload(WhisperBlockModeRegister, "Berk"))
	if !outcome.Applied {
		t.Fatalf("register refused: %q", outcome.Refusal)
	}
	if !reflect.DeepEqual(snapshot, []string{"Alder"}) {
		t.Fatalf("pre-mutation alias changed to %v", snapshot)
	}
	if !reflect.DeepEqual(character.BlockedWhisperers, []string{"Alder", "Berk"}) {
		t.Fatalf("list after = %v", character.BlockedWhisperers)
	}
}

func TestHandleWhisperBlockIdentityGuards(t *testing.T) {
	deps := &enterworld.Deps{}
	if outcome := HandleWhisperBlock(deps, "global-official", nil, whisperBlockPayload(1, "Berk")); outcome.Applied || outcome.Refusal != "characterNotFound" {
		t.Fatalf("nil character: %+v", outcome)
	}
	pending := &enterworld.Character{Name: "asd2", DeletePending: true}
	if outcome := HandleWhisperBlock(deps, "global-official", pending, whisperBlockPayload(1, "Berk")); outcome.Applied || outcome.Refusal != "deletePending" {
		t.Fatalf("delete pending: %+v", outcome)
	}
}

// whisperBlockAckDeps builds the ack-contract fixture: the actor "asd2"
// with the given starting list, plus the blockable target "Berk". No
// bracketed record: store.CharacterNameShapeValid (^[A-Za-z0-9_]+$,
// length 2..12) makes a "[GM]..." name uncreatable, so no division can
// ever carry one - which is exactly why the old [GM] prefix gate was
// dead code and was deleted (see HandleWhisperBlock).
func whisperBlockAckDeps(before []string) (*enterworld.Deps, *enterworld.Character) {
	actor := &enterworld.Character{ID: 1, Name: "asd2", BlockedWhisperers: before}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{
			"global-official": {actor, {ID: 2, Name: "Berk"}},
		},
	}
	return deps, actor
}

// TestWhisperBlockAckContract pins the 0xB66F ack RAW BYTES per arm -
// the wire contract the client's sub_771550 reads: {u8 mode, u8 result},
// result 0 appending {u16 len, ANSI name}. Result codes: 0 success
// (panel applies live), 1 "Name already exists.", 2 "User does not
// exist.", 3 "Cannot register any more users to block list.", 4 the
// no-op arm. A nil ack is SILENCE - no 0xB66F frame leaves at all. The
// rule ORDER is retail's in-memory ADD gate (the reference server):
// duplicate, capacity, self-compare,
// quote scan, then existence (enforced by the SQL proc after
// the enqueue), then persist.
func TestWhisperBlockAckContract(t *testing.T) {
	fullList := make([]string, WhisperBlockMaxCount)
	for i := range fullList {
		fullList[i] = "Name" + string(rune('A'+i))
	}
	fullListWithBerk := append(append([]string{}, fullList[:WhisperBlockMaxCount-1]...), "Berk")
	cases := []struct {
		label     string
		before    []string
		payload   []byte
		wantAck   []byte
		wantAfter []string
	}{
		{
			label:     "register success acks {1,0,name}",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantAck:   []byte{0x01, 0x00, 0x04, 0x00, 'B', 'e', 'r', 'k'},
			wantAfter: []string{"Berk"},
		},
		{
			label:     "register duplicate acks {1,1}, no double-persist",
			before:    []string{"Berk"},
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantAck:   []byte{0x01, 0x01},
			wantAfter: []string{"Berk"},
		},
		{
			label:     "register onto a full list acks {1,3}",
			before:    fullList,
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantAck:   []byte{0x01, 0x03},
			wantAfter: fullList,
		},
		{
			// The native gate runs the duplicate walk FIRST (a match
			// -> internal 2 -> wire 1 through the block handler's case 2)
			// and the 0x14 capacity check second
			// (-> internal 1 -> wire 3), so a name that is both
			// duplicate and over-cap acks {1,1}.
			label:     "duplicate wins over full list (native rule order)",
			before:    fullListWithBerk,
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "Berk"),
			wantAck:   []byte{0x01, 0x01},
			wantAfter: fullListWithBerk,
		},
		{
			// Duplicate precedes existence (SQL-side, after
			// the enqueue), so an already-listed name acks {1,1} even
			// though no character carries it.
			label:     "duplicate wins over no-such-user (native rule order)",
			before:    []string{"Cale"},
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "Cale"),
			wantAck:   []byte{0x01, 0x01},
			wantAfter: []string{"Cale"},
		},
		{
			// SELF-BLOCK: the native gate compares the requested name against
			// the owner's own CharName (a CompareStringA
			// call); EQUAL -> internal 3, and the
			// block handler remaps 3 -> wire 2 - the
			// "User does not exist." msgbox. The actor is "asd2".
			label:     "register self acks {1,2} (native self-compare)",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "asd2"),
			wantAck:   []byte{0x01, 0x02},
			wantAfter: nil,
		},
		{
			// Retail's CompareStringA runs with flags 0 (case-SENSITIVE);
			// we fold instead - names are CI-unique
			// (idx_characters_name on name_lower), so "ASD2" resolves to
			// the same character and a sensitive compare would let the
			// self-block through on a casing technicality. Deliberate
			// divergence, documented on the handler arm.
			label:     "register self with folded casing acks {1,2} (EqualFold divergence)",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "ASD2"),
			wantAck:   []byte{0x01, 0x02},
			wantAfter: nil,
		},
		{
			// Capacity (step 2) precedes the self-compare
			// (step 3): self onto a full list acks {1,3}.
			label:     "full list wins over self (native rule order)",
			before:    fullList,
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "asd2"),
			wantAck:   []byte{0x01, 0x03},
			wantAfter: fullList,
		},
		{
			// QUOTE SCAN: the native scan walks the name with
			// CharNextA and returns 1 on 0x27 '\'' ("quotation");
			// the gate then lands internal 4 and
			// the caller maps every result > 3 to wire 4 - the
			// client's sub_771550 no-op arm (no msgbox, no panel change).
			// NOT silence: retail does send the {1,4} frame.
			label:     "register single-quoted name acks {1,4} (native quote scan)",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "O'Brien"),
			wantAck:   []byte{0x01, 0x04},
			wantAfter: nil,
		},
		{
			// The 0x22 '"' arm of the same scan ("dbl quotation")
			// -> internal 4 -> wire 4.
			label:     "register double-quoted name acks {1,4} (native quote scan)",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, `a"b`),
			wantAck:   []byte{0x01, 0x04},
			wantAfter: nil,
		},
		{
			// Quote scan (step 4) runs BEFORE existence
			// (SQL-side): a quoted unknown name acks {1,4}, not {1,2}.
			label:     "quote scan wins over no-such-user (native rule order)",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "no'body"),
			wantAck:   []byte{0x01, 0x04},
			wantAfter: nil,
		},
		{
			// Capacity (step 2) precedes the quote scan (step 4): a
			// quoted name onto a full list acks {1,3}.
			label:     "full list wins over quote scan (native rule order)",
			before:    fullList,
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "O'Brien"),
			wantAck:   []byte{0x01, 0x03},
			wantAfter: fullList,
		},
		{
			label:     "register unknown target acks {1,2}, not persisted",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "Cale"),
			wantAck:   []byte{0x01, 0x02},
			wantAfter: nil,
		},
		{
			// The [GM] prefix gate is GONE (dead code): a bracketed name
			// fails store.CharacterNameShapeValid (^[A-Za-z0-9_]+$), so
			// no such character can exist and the EXISTENCE check refuses
			// it as {1,2} - the identical wire outcome the gate produced
			// (even the v1.188 shard's SQL return 3 remapped to wire 2).
			label:     "register [GM]-shaped name acks {1,2} via existence (gate deleted)",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "[GM]Berk"),
			wantAck:   []byte{0x01, 0x02},
			wantAfter: nil,
		},
		{
			// Without the gate, a bracketed name follows the normal rule
			// order: list-full is checked before existence, so it acks
			// {1,3} like any other name onto a full list.
			label:     "full list wins over [GM]-shaped non-existence (normal rule order)",
			before:    fullList,
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "[GM]Berk"),
			wantAck:   []byte{0x01, 0x03},
			wantAfter: fullList,
		},
		{
			label:     "register junk charset acks {1,2} via existence",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "!!!"),
			wantAck:   []byte{0x01, 0x02},
			wantAfter: nil,
		},
		{
			label:     "register folds case for existence, persists and echoes AS SENT",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, "berk"),
			wantAck:   []byte{0x01, 0x00, 0x04, 0x00, 'b', 'e', 'r', 'k'},
			wantAfter: []string{"berk"},
		},
		{
			label:     "register name at the 0x80 bound refuses SILENTLY",
			payload:   whisperBlockPayload(WhisperBlockModeRegister, strings.Repeat("B", 0x80)),
			wantAck:   nil,
			wantAfter: nil,
		},
		{
			label:     "cancel hit acks {2,0,name} and removes",
			before:    []string{"Alder", "Berk"},
			payload:   whisperBlockPayload(WhisperBlockModeCancel, "Berk"),
			wantAck:   []byte{0x02, 0x00, 0x04, 0x00, 'B', 'e', 'r', 'k'},
			wantAfter: []string{"Alder"},
		},
		{
			// The find folds case but the echo must carry the STORED
			// casing: the client's erase-by-name is case-sensitive
			// (the client's raw UTF-16 compare), so
			// echoing "berk" as sent would miss the displayed "Berk" row
			// and leave a phantom until the next enter-world reseed.
			label:     "cancel with folded casing echoes the STORED name",
			before:    []string{"Berk"},
			payload:   whisperBlockPayload(WhisperBlockModeCancel, "berk"),
			wantAck:   []byte{0x02, 0x00, 0x04, 0x00, 'B', 'e', 'r', 'k'},
			wantAfter: nil,
		},
		{
			// Cancel miss acks {2,2} - the reference server's evidence
			// (SQL 1 -> wire 2; the handler's (result!=3)*2+2),
			// MEDIUM confidence; the full note sits on the handler arm.
			label:     "cancel miss acks {2,2}, store unchanged",
			before:    []string{"Alder"},
			payload:   whisperBlockPayload(WhisperBlockModeCancel, "Berk"),
			wantAck:   []byte{0x02, 0x02},
			wantAfter: []string{"Alder"},
		},
	}
	for _, c := range cases {
		deps, actor := whisperBlockAckDeps(c.before)
		outcome := HandleWhisperBlock(deps, "global-official", actor, c.payload)
		if !bytes.Equal(outcome.Ack, c.wantAck) {
			t.Errorf("%s: ack = % X, want % X", c.label, outcome.Ack, c.wantAck)
		}
		wantAfter := c.wantAfter
		if wantAfter == nil {
			wantAfter = []string{}
		}
		gotAfter := actor.BlockedWhisperers
		if gotAfter == nil {
			gotAfter = []string{}
		}
		if !reflect.DeepEqual(gotAfter, wantAfter) {
			t.Errorf("%s: list after = %v, want %v", c.label, gotAfter, wantAfter)
		}
	}
}

package community

// Letter-lane table tests: the packed receive time against a mirror of
// the client's CIFLetterSlot_SetRecord bit decode, the 0x7261 strict decode, the
// pinned answer encoders byte for byte, and the three handlers over a
// fake letter door (refusal arms silent, mutation arms copy-then-swap).

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

// decodeReceiveTime mirrors the client CIFLetterSlot_SetRecord receive
// time decode:
// bits 0..5 year, 6..9 month, 10..14 day, 15..19 hour, 20..25 minute.
func decodeReceiveTime(packed uint32) (year, month, day, hour, minute uint32) {
	return packed & 0x3f, (packed >> 6) & 0xf, (packed >> 10) & 0x1f, (packed >> 15) & 0x1f, (packed >> 20) & 0x3f
}

func TestPackLetterReceiveTimeMatchesClientDecode(t *testing.T) {
	cases := []time.Time{
		time.Date(2026, 7, 28, 21, 43, 12, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2063, 6, 15, 12, 30, 0, 0, time.UTC),
	}
	for _, moment := range cases {
		year, month, day, hour, minute := decodeReceiveTime(PackLetterReceiveTime(moment))
		if int(year) != moment.Year()%100 || int(month) != int(moment.Month()) || int(day) != moment.Day() ||
			int(hour) != moment.Hour() || int(minute) != moment.Minute() {
			t.Errorf("pack(%v) decodes to %02d-%02d-%02d %02d:%02d", moment, year, month, day, hour, minute)
		}
	}
}

func TestDecodeLetterSendRequest(t *testing.T) {
	payload := []byte{0x04, 0x00, 'B', 'e', 'r', 'k', 0x05, 0x00, 'h', 'e', 'l', 'l', 'o'}
	request, err := DecodeLetterSendRequest(payload)
	if err != nil || request.Receiver != "Berk" || request.Body != "hello" {
		t.Fatalf("decode = %+v, %v", request, err)
	}
	if _, err := DecodeLetterSendRequest(payload[:8]); err == nil {
		t.Error("truncated body accepted")
	}
	if _, err := DecodeLetterSendRequest(append(append([]byte{}, payload...), 0x00)); err == nil {
		t.Error("trailing byte accepted")
	}
	if _, err := DecodeLetterSendRequest(nil); err == nil {
		t.Error("empty payload accepted")
	}
	// A letter replays to its reader at every world entry: only valid UTF-8
	// is stored. Valid multi-byte text still passes.
	if _, err := DecodeLetterSendRequest([]byte{0x04, 0x00, 'B', 'e', 'r', 'k', 0x02, 0x00, 0xc3, 0x28}); err == nil {
		t.Error("invalid UTF-8 body accepted")
	}
	if _, err := DecodeLetterSendRequest([]byte{0x02, 0x00, 0xff, 0xfe, 0x01, 0x00, 'x'}); err == nil {
		t.Error("invalid UTF-8 receiver accepted")
	}
	if got, err := DecodeLetterSendRequest([]byte{0x04, 0x00, 'B', 'e', 'r', 'k', 0x02, 0x00, 0xc3, 0xa9}); err != nil || got.Body != "é" {
		t.Errorf("valid UTF-8 body = %+v, %v", got, err)
	}
}

func TestLetterAnswerEncoders(t *testing.T) {
	if got := EncodeLetterSendAckB261(); !bytes.Equal(got, []byte{0x01}) {
		t.Errorf("0xB261 ack = % X", got)
	}
	if got := EncodeLetterDeleteAckB0CC(3); !bytes.Equal(got, []byte{0x01, 0x03}) {
		t.Errorf("0xB0CC ack = % X", got)
	}
	if got := EncodeLetterReadBodyB3F2(2, "hi"); !bytes.Equal(got, []byte{0x01, 0x02, 0x02, 0x00, 'h', 'i'}) {
		t.Errorf("0xB3F2 body = % X", got)
	}
	want3F9A := []byte{
		0x08,
		0x03, 0x00, 'A', 's', 'h',
		0x73, 0x07, 0x00, 0x00,
		0x78, 0x56, 0x34, 0x12,
	}
	if got := EncodeLetterReceivedEvent3F9A("Ash", 1907, 0x12345678); !bytes.Equal(got, want3F9A) {
		t.Errorf("0x3F9A case 8 = % X, want % X", got, want3F9A)
	}
}

// fakeLetterStore is a map-backed enterworld.LetterStore twin of the
// authority store's memos door.
type fakeLetterStore struct {
	boxes  map[string]map[int64][]enterworld.LetterRecord
	labels []string
}

func newFakeLetterStore() *fakeLetterStore {
	return &fakeLetterStore{boxes: map[string]map[int64][]enterworld.LetterRecord{}}
}

func (f *fakeLetterStore) Mailbox(divisionID string, characterID int64) []enterworld.LetterRecord {
	live := f.boxes[divisionID][characterID]
	out := make([]enterworld.LetterRecord, len(live))
	copy(out, live)
	return out
}

func (f *fakeLetterStore) DeliverLetter(divisionID string, _, receiverID int64, maxCount int, letter enterworld.LetterRecord) bool {
	if len(f.Mailbox(divisionID, receiverID)) >= maxCount {
		return false
	}
	return f.UpdateMailbox(divisionID, receiverID, "letter-send", func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool) {
		return append(mailbox, letter), true
	})
}

func (f *fakeLetterStore) UpdateMailbox(divisionID string, characterID int64, label string, fn func(mailbox []enterworld.LetterRecord) ([]enterworld.LetterRecord, bool)) bool {
	f.labels = append(f.labels, label)
	snapshot := f.Mailbox(divisionID, characterID)
	next, changed := fn(snapshot)
	if !changed {
		return false
	}
	if f.boxes[divisionID] == nil {
		f.boxes[divisionID] = map[int64][]enterworld.LetterRecord{}
	}
	f.boxes[divisionID][characterID] = next
	return true
}

// fakeCharacterSource answers one fixed division roster.
type fakeCharacterSource struct {
	divisionID string
	characters []*enterworld.Character
}

func (f fakeCharacterSource) CharactersForDivision(divisionID string) []*enterworld.Character {
	if divisionID != f.divisionID {
		return nil
	}
	return f.characters
}

const letterTestDivision = "global-official"

func letterTestInt64(v int64) *int64 { return &v }

func letterTestDeps(letters enterworld.LetterStore, characters ...*enterworld.Character) *enterworld.Deps {
	return &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: fakeCharacterSource{divisionID: letterTestDivision, characters: characters},
		Letters:    letters,
	}
}

func letterTestSender() *enterworld.Character {
	return &enterworld.Character{
		ID:            1,
		Name:          "Ash",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     letterTestInt64(enterworld.RaceChina),
		Gender:        letterTestInt64(enterworld.GenderMale),
	}
}

func letterTestReceiver() *enterworld.Character {
	return &enterworld.Character{
		ID:            2,
		Name:          "Berk",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     letterTestInt64(enterworld.RaceChina),
		Gender:        letterTestInt64(enterworld.GenderMale),
	}
}

func letterSendPayload(receiver, body string) []byte {
	out := []byte{byte(len(receiver)), byte(len(receiver) >> 8)}
	out = append(out, []byte(receiver)...)
	out = append(out, byte(len(body)), byte(len(body)>>8))
	return append(out, []byte(body)...)
}

func TestHandleLetterSendDelivers(t *testing.T) {
	letters := newFakeLetterStore()
	sender := letterTestSender()
	receiver := letterTestReceiver()
	deps := letterTestDeps(letters, sender, receiver)
	moment := time.Date(2026, 7, 28, 21, 43, 0, 0, time.UTC)

	// Receiver resolution is case-insensitive, the CreateCharacter
	// uniqueness rule's read twin.
	outcome := HandleLetterSend(deps, letterTestDivision, sender, letterSendPayload("berk", "meet me"), moment)
	if outcome.Refusal != "" {
		t.Fatalf("send refused: %s", outcome.Refusal)
	}
	if !bytes.Equal(outcome.AckPayload, []byte{0x01}) {
		t.Fatalf("ack payload = % X", outcome.AckPayload)
	}
	if outcome.RecipientName != "Berk" {
		t.Fatalf("recipient = %q", outcome.RecipientName)
	}
	wantPush := EncodeLetterReceivedEvent3F9A("Ash", enterworld.CharacterModelRef(sender, deps.Roster), PackLetterReceiveTime(moment))
	if !bytes.Equal(outcome.RecipientPushPayload, wantPush) {
		t.Fatalf("push payload = % X, want % X", outcome.RecipientPushPayload, wantPush)
	}
	mailbox := letters.Mailbox(letterTestDivision, receiver.ID)
	if len(mailbox) != 1 {
		t.Fatalf("mailbox = %d letter(s), want 1", len(mailbox))
	}
	letter := mailbox[0]
	if letter.Sender != "Ash" || letter.Body != "meet me" || letter.ReadFlag != 0 ||
		letter.PackedReceiveTime != PackLetterReceiveTime(moment) {
		t.Fatalf("stored letter = %+v", letter)
	}
	if len(letters.labels) != 1 || letters.labels[0] != "letter-send" {
		t.Fatalf("door labels = %v", letters.labels)
	}
}

func TestHandleLetterSendRefusals(t *testing.T) {
	receiverAtCap := string(bytes.Repeat([]byte{'a'}, LetterReceiverMaxBytes+1))
	bodyAtCap := string(bytes.Repeat([]byte{'b'}, LetterBodyMaxBytes+1))
	cases := []struct {
		name    string
		payload []byte
	}{
		{"malformed", []byte{0x04}},
		{"empty receiver", letterSendPayload("", "hello")},
		{"empty body", letterSendPayload("Berk", "")},
		{"job alias", letterSendPayload("*Trader", "hello")},
		{"receiver over cap", letterSendPayload(receiverAtCap, "hello")},
		{"body over cap", letterSendPayload("Berk", bodyAtCap)},
		{"unknown receiver", letterSendPayload("Cale", "hello")},
	}
	for _, tc := range cases {
		letters := newFakeLetterStore()
		sender := letterTestSender()
		deps := letterTestDeps(letters, sender, letterTestReceiver())
		outcome := HandleLetterSend(deps, letterTestDivision, sender, tc.payload, time.Now())
		if outcome.Refusal == "" || outcome.AckPayload != nil || outcome.RecipientPushPayload != nil {
			t.Errorf("%s: outcome = %+v, want a silent refusal", tc.name, outcome)
		}
		if got := letters.Mailbox(letterTestDivision, 2); len(got) != 0 {
			t.Errorf("%s: mailbox mutated to %d letter(s)", tc.name, len(got))
		}
	}
}

func TestHandleLetterSendMailboxCap(t *testing.T) {
	letters := newFakeLetterStore()
	sender := letterTestSender()
	receiver := letterTestReceiver()
	deps := letterTestDeps(letters, sender, receiver)
	full := make([]enterworld.LetterRecord, LetterMailboxMaxCount)
	for i := range full {
		full[i] = enterworld.LetterRecord{Sender: "Ash", Body: fmt.Sprintf("letter %d", i)}
	}
	letters.boxes[letterTestDivision] = map[int64][]enterworld.LetterRecord{receiver.ID: full}

	outcome := HandleLetterSend(deps, letterTestDivision, sender, letterSendPayload("Berk", "one too many"), time.Now())
	if outcome.Refusal == "" || outcome.AckPayload != nil {
		t.Fatalf("outcome = %+v, want the mailbox-full refusal", outcome)
	}
	if got := letters.Mailbox(letterTestDivision, receiver.ID); len(got) != LetterMailboxMaxCount {
		t.Fatalf("mailbox = %d letter(s), want the cap %d", len(got), LetterMailboxMaxCount)
	}
}

func TestHandleLetterSendNilStore(t *testing.T) {
	sender := letterTestSender()
	deps := letterTestDeps(nil, sender, letterTestReceiver())
	outcome := HandleLetterSend(deps, letterTestDivision, sender, letterSendPayload("Berk", "hello"), time.Now())
	if outcome.Refusal == "" {
		t.Fatal("nil letter store accepted")
	}
}

func TestHandleLetterReadFetchFlipsOnce(t *testing.T) {
	letters := newFakeLetterStore()
	receiver := letterTestReceiver()
	deps := letterTestDeps(letters, receiver)
	letters.boxes[letterTestDivision] = map[int64][]enterworld.LetterRecord{receiver.ID: {
		{Sender: "Ash", Body: "first", ReadFlag: 0},
		{Sender: "Ash", Body: "second", ReadFlag: 1},
	}}

	outcome := HandleLetterReadFetch(deps, letterTestDivision, receiver, []byte{0x00})
	if outcome.Refusal != "" {
		t.Fatalf("read refused: %s", outcome.Refusal)
	}
	if want := EncodeLetterReadBodyB3F2(0, "first"); !bytes.Equal(outcome.AnswerPayload, want) {
		t.Fatalf("answer = % X, want % X", outcome.AnswerPayload, want)
	}
	if got := letters.Mailbox(letterTestDivision, receiver.ID); got[0].ReadFlag != 1 {
		t.Fatalf("read flag = %d after fetch, want 1", got[0].ReadFlag)
	}

	// An already-read letter still answers (the client re-opens rows)
	// and stays read.
	again := HandleLetterReadFetch(deps, letterTestDivision, receiver, []byte{0x01})
	if again.Refusal != "" || !bytes.Equal(again.AnswerPayload, EncodeLetterReadBodyB3F2(1, "second")) {
		t.Fatalf("re-read outcome = %+v", again)
	}

	// Out of range: silent.
	if out := HandleLetterReadFetch(deps, letterTestDivision, receiver, []byte{0x07}); out.Refusal == "" || out.AnswerPayload != nil {
		t.Fatalf("out-of-range read outcome = %+v", out)
	}
}

func TestHandleLetterDeleteCompacts(t *testing.T) {
	letters := newFakeLetterStore()
	receiver := letterTestReceiver()
	deps := letterTestDeps(letters, receiver)
	letters.boxes[letterTestDivision] = map[int64][]enterworld.LetterRecord{receiver.ID: {
		{Sender: "Ash", Body: "first"},
		{Sender: "Ash", Body: "second"},
		{Sender: "Ash", Body: "third"},
	}}

	outcome := HandleLetterDelete(deps, letterTestDivision, receiver, []byte{0x01})
	if outcome.Refusal != "" || !bytes.Equal(outcome.AnswerPayload, []byte{0x01, 0x01}) {
		t.Fatalf("delete outcome = %+v", outcome)
	}
	mailbox := letters.Mailbox(letterTestDivision, receiver.ID)
	if len(mailbox) != 2 || mailbox[0].Body != "first" || mailbox[1].Body != "third" {
		t.Fatalf("mailbox after delete = %+v", mailbox)
	}

	// Out of range: silent, nothing changes.
	if out := HandleLetterDelete(deps, letterTestDivision, receiver, []byte{0x05}); out.Refusal == "" || out.AnswerPayload != nil {
		t.Fatalf("out-of-range delete outcome = %+v", out)
	}
	if got := letters.Mailbox(letterTestDivision, receiver.ID); len(got) != 2 {
		t.Fatalf("mailbox mutated by the refused delete: %d letter(s)", len(got))
	}
}

func TestSeedFramesEncodeMailbox(t *testing.T) {
	letters := newFakeLetterStore()
	receiver := letterTestReceiver()
	letters.boxes[letterTestDivision] = map[int64][]enterworld.LetterRecord{receiver.ID: {
		{Sender: "Ash", SenderModelRefID: 1907, PackedReceiveTime: 0x12345678, ReadFlag: 1, Body: "kept server-side"},
	}}

	frames := SeedFramesFunc(nil, letters)(letterTestDivision, receiver)
	if len(frames) != 2 || frames[0].NativeOpcode != OpFriendRosterPush || frames[1].NativeOpcode != OpLetterListAnswer {
		t.Fatalf("seed frames = %+v", frames)
	}
	want := EncodeLetterListB3CD([]LetterListEntry{{
		Sender: "Ash", SenderModelRefID: 1907, PackedReceiveTime: 0x12345678, ReadFlag: 1,
	}})
	got := make([]byte, len(frames[1].Payload))
	for i, v := range frames[1].Payload {
		got[i] = byte(v)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("letter seed payload = % X, want % X", got, want)
	}
}

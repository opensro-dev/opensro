package domain

// LetterRecord is one persisted letter (memo) row: the per-recipient
// mailbox entry the community letter lane serves. The wire-facing fields
// mirror the client's ced160+0x24c record (CharacterDependentData_AppendLetter insert: sender
// wstring +0x00, sender model ref +0x1c, packed receive time +0x3c, read
// flag +0x40) plus the body the 0xB3F2 read-fetch fills lazily
// (client 8277D0 stores it at +0x20). The retail shard shape is
// _Memo(ID64, CharID, FromCharName, Message, Date, Status, RefObjID) -
// schema reference only.
//
// Letters deliberately do NOT live on the Character record: bodies are up
// to 256 bytes each and a mailbox holds up to 20, which would bloat every
// unrelated character commit. They persist in the authority store's memos
// table, reached through the LetterStore door below.
type LetterRecord struct {
	Sender            string `json:"sender"`
	SenderModelRefID  uint32 `json:"senderModelRefId"`
	PackedReceiveTime uint32 `json:"packedReceiveTime"`
	ReadFlag          uint8  `json:"readFlag"`
	Body              string `json:"body"`
	// UnknownFields are the stored letter's top-level fields this
	// binary does not know, a newer release's (store/json.go, #3090).
	// A letter has no identity but its place in the mailbox, so the
	// letter carries them itself and the store writes them back with
	// it. Only the store sets them; game code copies letters whole.
	UnknownFields string `json:"-"`
}

// LetterDelivery is DeliverLetter's verdict. The letter lane answers
// the sender from it (a full mailbox names a different client line
// than any other refusal), so the one distinction the wire needs is
// made here, on the store owner's single locked read - never by a
// second mailbox lookup outside the door.
type LetterDelivery uint8

const (
	// LetterDeliveryRefused: no commit - a character is unavailable or
	// delete-pending under the lock, or the record is outside the
	// persisted bounds.
	LetterDeliveryRefused LetterDelivery = iota
	LetterDeliveryDone
	// LetterDeliveryMailboxFull: no commit - the receiver's mailbox
	// already holds the cap, counted on the delivery read itself.
	LetterDeliveryMailboxFull
)

// LetterStore is the authority store's letter-mailbox door. A mailbox is the ordered letter list of
// one character; the SLICE ORDER is the wire order - the 0xB3CD list, the
// u8 index in 0x73F2/0x70CC and the client's ced160+0x24c list all agree
// on it by construction (seeds emit slice order, sends append at the
// tail on both sides, deletes compact both sides at the same index).
type LetterStore interface {
	// Mailbox returns a copy of the character's persisted mailbox in
	// list order. A character with no rows answers an empty list.
	Mailbox(divisionID string, characterID int64) []LetterRecord
	// DeliverLetter validates both live characters, the receiver's deletion
	// state, and the mailbox cap under one lock before appending and
	// committing. A refusal commits nothing.
	DeliverLetter(divisionID string, senderID, receiverID int64, maxCount int, letter LetterRecord) LetterDelivery
	// UpdateMailbox runs a conditional copy-then-swap update. The callback's
	// bool reports whether state changed; false means no commit.
	UpdateMailbox(divisionID string, characterID int64, label string, fn func(mailbox []LetterRecord) ([]LetterRecord, bool)) bool
}

// Package quest is the quest lane of the v1.150 gateway: the curated
// quest DEFINITIONS (shipped questdata/questcontentsdata joined with the
// authored objective/reward table - definitions.go), the per-character
// quest-state mutations through the bootstrap Mutate door (runtime.go),
// the 0x31ED quest-update emitters, and the two inbound handlers the
// client composes - 0x71EB give-up and 0x729A reward-select
// (register.go).
//
// Every opcode number and byte layout here is pinned from the v1.150
// CLIENT (the community package rule): the S->C 0x31ED grammar is the
// registered handler CPSMission_OnNetProcessInLog0x31ED plus the
// SQuestInfo deserializer SQuestInfo_Deserialize it drives; the two
// C->S bodies are the composers NetClient_Send71EB (0x71EB) and
// NetClient_Send729A (0x729A), each a bare 4-byte refId append
// (CMsgStreamBuffer_AppendBytes(&refId, 4)). v1.188/
// DuckSoup opcode numbers never cross the boundary - their 0x7515 is a
// different packet entirely in v1.150 (the hosted interaction sender).
package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// C->S opcodes, pinned from the client composers.
const (
	// OpQuestGiveUpRequest is the give-up composer NetClient_Send71EB
	// (opcode 0x71eb, then the 4-byte refId append): reached from the
	// CIFQuestReward agreement box's Yes arm (CIFQuestReward_ConfirmGiveUp,
	// both confirm "types" 0x64/0x65).
	OpQuestGiveUpRequest uint16 = 0x71EB
	// OpQuestRewardRequest is the reward-select composer
	// NetClient_Send729A (opcode 0x729a, then the 4-byte refId append):
	// reached from the CIFQuestReward action button's kind-2 arm
	// (CIFQuestReward_ConfirmReward).
	OpQuestRewardRequest uint16 = 0x729A
)

// OpQuestUpdate is the S->C mid-session quest channel 0x31ED (client
// handler CPSMission_OnNetProcessInLog0x31ED): [u8 op][u32 questRefId],
// then per op below.
const OpQuestUpdate uint16 = 0x31ED

// The CPSMission_OnNetProcessInLog0x31ED op bytes (its
// `switch (op - 1)`; op 0 and op > 4 fall to the plain return - never
// compose them).
const (
	// QuestUpdateOpInsert (op 1): a full SQuestInfo body follows; the
	// client ctor-defaults then REAL-deserializes it and inserts
	// registry record + quest-window row (CGInterface_OnQuestInserted).
	QuestUpdateOpInsert uint8 = 1
	// QuestUpdateOpUpdate (op 2): the same body, re-deserialized IN
	// PLACE over the registry record CQuestDataManager_FindQuestInfo
	// finds by refId. A miss is a native null-deref - never emit op 2
	// for a quest this server has not inserted (or seeded through the
	// 0x32B3 section 2).
	QuestUpdateOpUpdate uint8 = 2
	// QuestUpdateOpComplete / QuestUpdateOpAbandon (ops 3 and 4) land on
	// the SAME client leg: row remove + tracked-id clear +
	// registry erase + completed-list append. The split is OUR declared
	// choice (see runtime.go): op 3 is emitted for a reward turn-in,
	// op 4 for a give-up - client-identical, but the byte preserves the
	// semantic split for wire logs and future captures.
	QuestUpdateOpComplete uint8 = 3
	QuestUpdateOpAbandon  uint8 = 4
)

// DecodeQuestRefRequest strict-decodes the shared 0x71EB/0x729A body:
// exactly [u32 questRefId] (both composers append nothing else). Short
// and oversized payloads refuse like every sibling decoder.
func DecodeQuestRefRequest(payload []byte) (uint32, error) {
	r := wire.NewReader(payload)
	refID, err := r.U32()
	if err != nil {
		return 0, fmt.Errorf("quest ref request: %w", err)
	}
	if err := r.Done(); err != nil {
		return 0, fmt.Errorf("quest ref request: %w", err)
	}
	return refID, nil
}

// appendWireString appends the NativePacketWriter.string layout the
// SQuestContents description field uses: u16 length + narrow bytes (the
// bootstrap writeWireString twin - kept local so this package never
// reaches into bootstrap's unexported wire helpers).
func appendWireString(w *wire.Writer, value string) {
	raw := []byte(value)
	w.U16(uint16(len(raw)))
	w.Bytes(raw)
}

// AppendQuestInfoBody appends one SQuestInfo wire body (client
// deserializer SQuestInfo_Deserialize) - the EXACT emission
// bootstrap/wire.go runs
// for the 0x32B3 section 2, kept rule-for-rule in sync (the Go test
// TestQuestInfoBodyMatchesEnterStreamSectionTwo cross-pins the two):
//
//	[u8 u08][u8 u09][u8 flags]
//	flags&0x04 -> [u32 progress]  (absent keeps the ctor 0xffffffff)
//	flags&0x08 -> [u8 u10]        (the CIFQuestReward kind byte)
//	flags&0x10 -> [u8 count] x {[u8 tag][u8 kind][wire string]
//	              [u8 objectiveCount | 0xFF sentinel][count x u32]}
//	flags&0x40 -> [u8 n] x [u32 targetId]
//
// EMISSION IS FLAG-DRIVEN: a field is written exactly when its flag bit
// is set - the client consumes by flags alone and a mismatch desyncs
// the stream (the ActiveQuestRecord field doc).
func AppendQuestInfoBody(w *wire.Writer, record enterworld.ActiveQuestRecord) {
	w.U8(record.U08)
	w.U8(record.U09)
	w.U8(record.Flags)
	if record.Flags&0x04 != 0 {
		w.U32(record.Progress)
	}
	if record.Flags&0x08 != 0 {
		w.U8(record.U10)
	}
	if record.Flags&0x10 != 0 {
		contents := record.Contents
		if len(contents) > 0xff {
			contents = contents[:0xff]
		}
		w.U8(uint8(len(contents)))
		for _, node := range contents {
			w.U8(node.Tag)
			w.U8(node.Kind)
			appendWireString(w, node.Description)
			if node.ObjectiveSentinel {
				// The 0xFF count sentinel: NO value array bytes
				// (SQuestContents_Deserialize wrap).
				w.U8(0xff)
				continue
			}
			values := node.ObjectiveValues
			// 0xFF would read as the sentinel; a well-formed record
			// never approaches it.
			if len(values) > 0xfe {
				values = values[:0xfe]
			}
			w.U8(uint8(len(values)))
			for _, value := range values {
				w.U32(value)
			}
		}
	}
	if record.Flags&0x40 != 0 {
		targets := record.TargetIds
		if len(targets) > 0xff {
			targets = targets[:0xff]
		}
		w.U8(uint8(len(targets)))
		for _, targetID := range targets {
			w.U32(targetID)
		}
	}
}

// EncodeQuestUpdateInsert composes the 0x31ED op-1 payload:
// [1][refId u32][SQuestInfo body].
func EncodeQuestUpdateInsert(record enterworld.ActiveQuestRecord) []byte {
	w := wire.NewWriter(64)
	w.U8(QuestUpdateOpInsert)
	w.U32(record.RefID)
	AppendQuestInfoBody(w, record)
	return w.Payload()
}

// EncodeQuestUpdateUpdate composes the 0x31ED op-2 payload:
// [2][refId u32][SQuestInfo body] - the in-place re-deserialize form.
func EncodeQuestUpdateUpdate(record enterworld.ActiveQuestRecord) []byte {
	w := wire.NewWriter(64)
	w.U8(QuestUpdateOpUpdate)
	w.U32(record.RefID)
	AppendQuestInfoBody(w, record)
	return w.Payload()
}

// EncodeQuestUpdateComplete composes the 0x31ED op-3 payload:
// [3][refId u32] - nothing further is read on the remove leg.
func EncodeQuestUpdateComplete(refID uint32) []byte {
	return wire.NewWriter(5).U8(QuestUpdateOpComplete).U32(refID).Payload()
}

// EncodeQuestUpdateAbandon composes the 0x31ED op-4 payload:
// [4][refId u32] - the client leg is identical to op 3 (see the op
// constants' declared-choice note).
func EncodeQuestUpdateAbandon(refID uint32) []byte {
	return wire.NewWriter(5).U8(QuestUpdateOpAbandon).U32(refID).Payload()
}

package community

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// WhisperBlockOutcome is one handled 0x766F request. Applied reports
// whether the list changed; Refusal carries the log-only reason when it
// did not. Ack is the 0xB66F body to answer with
// (CPSMission_OnAliasActionResponse0xB66F - the client applies a
// result-0 add/remove to the blocking panel LIVE and
// msgboxes results 1/2/3); a nil Ack stays SILENT on the wire (decode
// refusals and identity guards only). The enter-world chunk C
// (client CPSMission_ReadBlockedWhispererList) remains the reseed
// channel.
type WhisperBlockOutcome struct {
	Applied      bool
	BlockedCount int
	Refusal      string
	Ack          []byte
}

func refusedWhisperBlock(reason string) WhisperBlockOutcome {
	return WhisperBlockOutcome{Refusal: reason}
}

// HandleWhisperBlock applies one decoded 0x766F request to the bound
// character and composes the 0xB66F ack. Register follows the RETAIL
// rule order of the reference server's in-memory ADD gate (v1.188 GameServer
// dump, wire codes via its caller's remap): the duplicate
// walk {1,1} (internal 2 -> wire 1), the 0x14
// cap {1,3} (-> wire 3), the self-name compare
// {1,2} (CompareStringA against the owner's own CharName ->
// internal 3 -> wire 2), the quote scan {1,4}
// (-> internal 4 -> wire 4, the client's no-op
// arm), then target existence {1,2} (retail enforces it in the SQL
// proc AFTER the enqueue, so it comes last), then persist + {1,0,name}.
// Cancel removes ({2,0,name}, echoing the STORED casing); a miss acks
// {2,2} (see the cancel arm's evidence note). The list swap runs inside
// the Mutate door as one unit (label
// "whisper-block"; the door's store lock serializes it against every
// other record mutation), copy-then-swap so character snapshots taken
// concurrently keep their view - the eventGuide MissionRuntime
// convention.
//
// NOT enforced here on purpose: charset validation. Retail's charset
// gate is CLIENT-side (client CStringCheck_IsAllowedName); the server
// relies on existence -
// junk that no character carries fails as "does not exist" {1,2}.
func HandleWhisperBlock(deps Dependencies, divisionID string, character *enterworld.Character, payload []byte) WhisperBlockOutcome {
	if character == nil {
		return refusedWhisperBlock("characterNotFound")
	}
	request, err := DecodeWhisperBlockRequest(payload)
	if err != nil {
		return refusedWhisperBlock(err.Error())
	}

	targetExists := false
	if request.Mode == WhisperBlockModeRegister {
		// The existence lookup runs OUTSIDE the door, the friend-add
		// precedent: retail requires the target in _CharNameList, and
		// the EqualFold matches its CI collation (CreateCharacter
		// refuses case-insensitive duplicates, so the fold is
		// unambiguous). No [GM]-prefix gate: our create-name rule
		// (store.CharacterNameShapeValid, ^[A-Za-z0-9_]+$ length 2..12)
		// makes a bracketed name uncreatable, so this existence check
		// already refuses every "[GM]..." request as {1,2} - the v1.188
		// shard's prefix gate (whose SQL return remapped to the same
		// wire 2) could never change an observable outcome, and the
		// v1.150 client dump has no "[GM]" name-prefix rule at all
		// (its "[GM]" is UIIT_STT_GM_MARK, a render-time display mark
		// in the client).
		targetExists = findCharacterByName(deps, divisionID, request.Name) != nil
	}

	outcome := WhisperBlockOutcome{}
	changed := deps.Update(character, "whisper-block", func() bool {
		if character.DeletePending {
			outcome.Refusal = "deletePending"
			return false
		}
		blocked := character.BlockedWhisperers
		outcome.BlockedCount = len(blocked)
		// The match folds case like every other name path (character
		// names are case-insensitively unique, presence/party/match keys
		// lowercase, the friend and letter lookups EqualFold), so "Bob"
		// cannot be blocked twice as "bob" and a cancel matches whatever
		// casing the client sends. The STORED string stays the register
		// request's verbatim casing - the entered chunk-C reseed echoes
		// it back unchanged.
		at := -1
		for index, name := range blocked {
			if strings.EqualFold(name, request.Name) {
				at = index
				break
			}
		}
		switch request.Mode {
		case WhisperBlockModeRegister:
			if at >= 0 {
				outcome.Refusal = fmt.Sprintf("%q already blocked", request.Name)
				outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeRegister, WhisperBlockResultAlreadyExists, "")
				return false
			}
			if len(blocked) >= WhisperBlockMaxCount {
				outcome.Refusal = fmt.Sprintf("block list full (%d/%d)", len(blocked), WhisperBlockMaxCount)
				outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeRegister, WhisperBlockResultListFull, "")
				return false
			}
			if strings.EqualFold(request.Name, character.Name) {
				// SELF-BLOCK, the reference ADD gate's third gate: the requested
				// name against the owner's OWN CharName (the +0xec
				// vcall on *(mgr+0x14), then CompareStringA);
				// equal -> internal 3 -> wire 2 ("User
				// does not exist."). Retail
				// compares case-SENSITIVELY (flags 0) - we fold on
				// purpose: names are CI-unique (idx_characters_name on
				// name_lower) and every other name path folds, so
				// "ASD2" resolves to the same character and a sensitive
				// compare would wave the self-block through on a casing
				// technicality (retail's own quirk: its CI SQL
				// collation stores the block anyway). Not preserved.
				outcome.Refusal = "cannot block yourself"
				outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeRegister, WhisperBlockResultNoSuchUser, "")
				return false
			}
			if strings.ContainsAny(request.Name, `'"`) {
				// QUOTE SCAN, the reference ADD gate's fourth gate: its text
				// scan walks the name with CharNextA and returns 1
				// on 0x27 '\'' ("quotation") or 0x22 '"'
				// ("dbl quotation") - SQL-injection armor for
				// the "{?=CALL %s (%d, '%s')}" job string.
				// The hit lands internal 4 -> wire 4
				// (the client's no-op arm) - an ack IS sent,
				// not silence. The scan also substring-scans a
				// config-loaded ban-word vector; no such config exists
				// here, so only the quote half is ported.
				outcome.Refusal = fmt.Sprintf("%q carries a quote character", request.Name)
				outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeRegister, WhisperBlockResultNoEffect, "")
				return false
			}
			if !targetExists {
				outcome.Refusal = fmt.Sprintf("%q does not exist in division %s", request.Name, divisionID)
				outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeRegister, WhisperBlockResultNoSuchUser, "")
				return false
			}
			next := make([]string, 0, len(blocked)+1)
			next = append(next, blocked...)
			next = append(next, request.Name)
			character.BlockedWhisperers = next
			outcome.Applied = true
			outcome.BlockedCount = len(next)
			outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeRegister, WhisperBlockResultSuccess, request.Name)
			return true
		case WhisperBlockModeCancel:
			if at < 0 {
				// Cancel miss acks {2,2} - JUDGEMENT CALL, MEDIUM
				// confidence, recorded 2026-07-29 so a future
				// evidence-finder can overturn it cheaply. Evidence: the
				// v1.188 GameServer lands a remove miss on wire result 2
				// via TWO independent paths (the SQL remap
				// maps SQL 1 "not on list" -> 2; the
				// synchronous path's miss -> 3, then the caller
				// computes (result != 3)*2+2 -> 2). That
				// binary emits opcode 0xb30d, not our 0xb66f, so the
				// evidence is version-skewed - but its ADD-side remap
				// (its add remap) reproduces exactly the result semantics
				// the v1.150 client's CPSMission_OnAliasActionResponse0xB66F decodes (1 duplicate /
				// 2 no-such-user / 3 list-full), so the result-code
				// contract survived the renumbering. The v1.150 client
				// shows "User does not exist." for {x,2} - odd for an
				// unblock, but the retail UI cannot normally produce a
				// cancel miss (the cancel leg sends the SELECTED row's
				// text; only a crafted packet or a race lands here).
				// SILENCE was the alternative and has zero positive
				// evidence.
				outcome.Refusal = fmt.Sprintf("%q not on the block list", request.Name)
				outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeCancel, WhisperBlockResultNoSuchUser, "")
				return false
			}
			// Echo the STORED string, not the request's casing: the
			// client's erase-by-name is CASE-SENSITIVE (the client's
			// erase compares raw UTF-16 code units, no _wcsicmp; the
			// mode-1 duplicate walk std_wstring_compare_substring ->
			// std_char_traits_wchar_compare is likewise raw), so the ack must
			// carry the exact casing the panel row displays or the
			// erase misses and a phantom row survives until the next
			// enter-world reseed.
			stored := blocked[at]
			next := make([]string, 0, len(blocked)-1)
			next = append(next, blocked[:at]...)
			next = append(next, blocked[at+1:]...)
			character.BlockedWhisperers = next
			outcome.Applied = true
			outcome.BlockedCount = len(next)
			outcome.Ack = EncodeWhisperBlockAckB66F(WhisperBlockModeCancel, WhisperBlockResultSuccess, stored)
			return true
		}
		return false
	})
	if !changed && outcome.Refusal == "" {
		outcome.Refusal = "character is no longer authoritative"
	}
	return outcome
}

/*
===========================================================================
history.go - composition adapter from transport facts to durable history
===========================================================================
*/
package main

import (
	"strconv"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/transport"
)

/*
================
historyObserver
================
*/
type historyObserver struct{ *history.Journal }

/*
================
RecordLifecycle
================
*/
func (observer historyObserver) RecordLifecycle(e transport.LifecycleEvent) {
	event := history.Event{Session: e.Session, Account: e.Account, Shard: e.Shard, Character: e.Character,
		Kind: e.Kind, Category: e.Category, Code: e.Code, Message: e.Message, Lifecycle: true, Attached: e.Attached, InWorld: e.InWorld}
	// How the session connected (#1449): the journal's session row picks
	// these two fields up; everything else about the event is unchanged.
	if e.Transport != "" || e.Fallback != "" {
		event.Fields = map[string]string{}
		if e.Transport != "" {
			event.Fields["transport"] = e.Transport
		}
		if e.Fallback != "" {
			event.Fields["fallback"] = e.Fallback
		}
	}
	observer.Record(event)
}

/*
================
silkGrantEvent

One GM or operator silk grant as an operator history event: who granted,
to whom, how much, and the wallet it left.
================
*/
func silkGrantEvent(grant action.SilkGrant) history.Event {
	return history.Event{Kind: "silk_grant", Category: "operator", Code: grant.Source, Shard: grant.Division,
		Account: grant.Account, Character: grant.Character,
		Message: grant.Grantor + " granted " + strconv.FormatUint(uint64(grant.Amount), 10) + " silk",
		Fields: map[string]string{"grantor": grant.Grantor, "amount": strconv.FormatUint(uint64(grant.Amount), 10),
			"balance": strconv.FormatUint(uint64(grant.Balance), 10)}}
}

/*
================
crestChangeEvent

One committed crest change as an audit event (#2282, owner
contract): the character, the guild, and the old and new payload
hashes - an upload by the crest's master, or a staff removal
(Code "operator").
================
*/
func crestChangeEvent(change action.CrestChange) history.Event {
	message := change.Character + " registered the " + change.Kind + " crest at revision " +
		strconv.FormatUint(uint64(change.NewRevision), 10)
	if change.Source == "operator" {
		message = change.Operator + " removed the " + change.Kind + " crest"
	}
	return history.Event{Kind: "crest_change", Category: "operator", Code: change.Source, Shard: change.Division,
		Character: change.Character, Message: message,
		Fields: map[string]string{"guild": strconv.FormatInt(change.GuildID, 10),
			"alliance": strconv.FormatInt(change.AllianceID, 10),
			"oldHash":  change.OldHash, "newHash": change.NewHash,
			"oldRevision": strconv.FormatUint(uint64(change.OldRevision), 10),
			"newRevision": strconv.FormatUint(uint64(change.NewRevision), 10)}}
}

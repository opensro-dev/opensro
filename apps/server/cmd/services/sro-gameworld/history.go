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

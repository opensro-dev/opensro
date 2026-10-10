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
	observer.Record(history.Event{Session: e.Session, Account: e.Account, Shard: e.Shard, Character: e.Character,
		Kind: e.Kind, Category: e.Category, Code: e.Code, Message: e.Message, Lifecycle: true, Attached: e.Attached, InWorld: e.InWorld})
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

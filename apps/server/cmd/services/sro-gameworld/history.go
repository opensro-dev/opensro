/*
===========================================================================
history.go - composition adapter from transport facts to durable history
===========================================================================
*/
package main

import (
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

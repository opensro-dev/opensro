/*
===========================================================================

wiring_census.go - the connection journal's install (opensro#2716)

Port-only, not native (SRO_CONNECTION_JOURNAL): with the flag on, the
hub's lifecycle stream also feeds a connection journal in the shard's
state directory - one row per entered session, its address held only
as keyed hashes under a key minted once into the same directory
(internal/platform/connjournal). Off (default) nothing is opened,
nothing is installed, and the transport runs exactly as native. The
journal is the GameWorld's own file: the character store's
compatibility number does not move.

===========================================================================
*/
package main

import (
	"fmt"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/platform/census"
	"opensro.online/server/internal/platform/connjournal"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/transport"
)

/*
================
censusObserver

The transport's census half: entered sessions open a journal row with
the attachment's client facts, ended sessions close it. Session names
come from the history journal, so the two journals join on them.
================
*/
type censusObserver struct {
	journal *connjournal.Journal
	named   *history.Journal
}

/*
================
SessionID
================
*/
func (observer censusObserver) SessionID(id string) string { return observer.named.SessionID(id) }

/*
================
RecordLifecycle
================
*/
func (observer censusObserver) RecordLifecycle(event transport.LifecycleEvent) {
	switch event.Kind {
	case "world_entered":
		observer.journal.RecordEntered(connjournal.Entry{Session: event.Session, Account: event.Account,
			Character: event.Character, Shard: event.Shard, Transport: event.Transport,
			Address: event.ClientAddr, UserAgent: event.UserAgent})
	case "ended":
		observer.journal.CloseSession(event.Session)
	}
}

/*
================
installConnectionJournal

Opens the census beside the history journal and installs its
observer. The flag off installs nothing and opens nothing.
================
*/
func installConnectionJournal(application *gameWorldApplication, ts *transport.Server, named *history.Journal, ownedShardID string) error {
	if !connjournal.Enabled() {
		return nil
	}
	directory := store.DirForShardFromEnv(ownedShardID)
	key, err := connjournal.LoadOrCreateKey(filepath.Join(directory, "connections.key"))
	if err != nil {
		return fmt.Errorf("connection journal key: %w", err)
	}
	journal, err := connjournal.Open(filepath.Join(directory, "connections.sqlite"), ownedShardID, key)
	if err != nil {
		return fmt.Errorf("connection journal: %w", err)
	}
	application.connJournal = journal
	ts.Hub.Census = censusObserver{journal: journal, named: named}
	log.Infof("connection journal: entered sessions recorded with keyed address hashes (%s, port-only, not native)",
		connjournal.EnvConnectionJournal)
	return nil
}

/*
================
installCensusReads

Part 2 of the census (opensro#2717): the staff console's reads over
the journal - online people, uniques, clusters with evidence, the
person view and bot signals (internal/platform/census). There is
nothing to read while the journal is off, so the routes install only
when installConnectionJournal opened one.
================
*/
func installCensusReads(application *gameWorldApplication, ownedShardID string) error {
	journal := application.connJournal
	if journal == nil {
		return nil
	}
	directory := store.DirForShardFromEnv(ownedShardID)
	service := census.NewService(census.JournalRows(journal), census.ChatLogCounts(directory), time.Now)
	// The public status answer's people counts read this same
	// service (opensro-web#78); installPublicAPI runs after us.
	application.census = service
	handler, err := census.Handler(service, filepath.Join(directory, "operator-token"))
	if err != nil {
		return fmt.Errorf("census reads: %w", err)
	}
	application.controlAPI.InstallCensus(census.Path, handler)
	log.Infof("census: staff reads on %s (people, clusters with evidence, bot signals; port-only, not native)", census.Path)
	return nil
}

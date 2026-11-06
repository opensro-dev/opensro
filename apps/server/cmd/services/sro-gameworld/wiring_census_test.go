/*
===========================================================================

wiring_census_test.go - the census installs only behind its flag

SRO_CONNECTION_JOURNAL off (the native posture) means no observer on
the hub, no journal on the application and no files in the shard
directory; on means all three (#2716, both settings tested).
===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/platform/connjournal"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/transport"
)

/*
================
installCensusForTest
================
*/
func installCensusForTest(t *testing.T) (*gameWorldApplication, *transport.Server, *history.Journal, string) {
	t.Helper()
	directory := t.TempDir()
	t.Setenv(store.EnvStateDir, directory)
	named, err := history.Open(filepath.Join(directory, "history.sqlite"), "gameworld", "global-official", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = named.Close() })
	application := &gameWorldApplication{}
	server := &transport.Server{Hub: &transport.Hub{}}
	return application, server, named, directory
}

/*
================
TestConnectionJournalOffInstallsNothing
================
*/
func TestConnectionJournalOffInstallsNothing(t *testing.T) {
	t.Setenv(connjournal.EnvConnectionJournal, "")
	application, server, named, directory := installCensusForTest(t)
	if err := installConnectionJournal(application, server, named, "global-official"); err != nil {
		t.Fatal(err)
	}
	if server.Hub.Census != nil || application.connJournal != nil {
		t.Fatal("the census installed with its flag off")
	}
	for _, name := range []string{"connections.sqlite", "connections.key"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("%s exists with the census off", name)
		}
	}
}

/*
================
TestConnectionJournalOnInstallsTheObserver
================
*/
func TestConnectionJournalOnInstallsTheObserver(t *testing.T) {
	t.Setenv(connjournal.EnvConnectionJournal, "on")
	application, server, named, directory := installCensusForTest(t)
	if err := installConnectionJournal(application, server, named, "global-official"); err != nil {
		t.Fatal(err)
	}
	if server.Hub.Census == nil || application.connJournal == nil {
		t.Fatal("the census did not install with its flag on")
	}
	for _, name := range []string{"connections.sqlite", "connections.key"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("%s missing with the census on: %v", name, err)
		}
	}
	// The observer names sessions exactly as history does, so the two
	// journals join on them.
	if got, want := server.Hub.Census.SessionID("7"), named.SessionID("7"); got != want {
		t.Fatalf("census session name = %q, history names it %q", got, want)
	}
	if err := application.connJournal.Close(); err != nil {
		t.Fatal(err)
	}
}

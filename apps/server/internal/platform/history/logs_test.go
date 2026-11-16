package history_test

/*
===========================================================================

logs_test.go - the log hook's contract (#3186)

Fire never hands an error back to logrus. A fatal entry fired after
the journal closed (a last shutdown line is exactly that) used to
return "history queue unavailable"; logrus printed "Failed to fire
hook" and the noise stood in for the real reason the process exited.
===========================================================================
*/

import (
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/history"
)

/*
================
TestFireAfterCloseStaysQuiet

Open a journal, close it, then fire a fatal entry: Fire returns nil
and the process-side result of the fatal log stays in the caller's
hands alone.
================
*/
func TestFireAfterCloseStaysQuiet(t *testing.T) {
	journal, err := history.Open(filepath.Join(t.TempDir(), "history.sqlite"), "test", "shard", "build")
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	entry := &log.Entry{Level: log.FatalLevel, Message: "shutdown: complete", Time: time.Now()}
	if err := journal.Fire(entry); err != nil {
		t.Fatalf("Fire on a closed journal returned %v; the hook must stay silent and let the entry's own fate rule", err)
	}
}

/*
================
TestFireSkipsPerSessionInfoLines

#3307: a per-session info line is transport chatter - Fire keeps it
out of the journal, where one looping session otherwise writes the
whole raw window. A warning from the same session and an info line
naming no session are still stored.
================
*/
func TestFireSkipsPerSessionInfoLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	journal, err := history.Open(path, "test", "shard", "build")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, entry := range []*log.Entry{
		{Level: log.InfoLevel, Message: "transport: scene live", Time: now, Data: log.Fields{"session": "abc"}},
		{Level: log.WarnLevel, Message: "transport: fell back", Time: now, Data: log.Fields{"session": "abc"}},
		{Level: log.InfoLevel, Message: "agentapi: mint enabled", Time: now, Data: log.Fields{}},
	} {
		if err := journal.Fire(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = history.Open(path, "test", "shard", "build")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	data, err := journal.Query(history.Filter{Kind: "log"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range data["events"].([]history.EventRow) {
		seen[row.Message] = true
	}
	if seen["transport: scene live"] {
		t.Fatal("per-session info line was stored as a history event")
	}
	if !seen["transport: fell back"] || !seen["agentapi: mint enabled"] {
		t.Fatalf("kept log lines missing: %v", seen)
	}
}

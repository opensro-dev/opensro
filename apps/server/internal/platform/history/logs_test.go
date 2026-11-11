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

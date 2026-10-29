/*
===========================================================================

chatlog_test.go - the moderation log's file contract

===========================================================================
*/
package chatlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
================
readLines
================
*/
func readLines(t *testing.T, path string) []line {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []line
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if raw == "" {
			continue
		}
		var entry line
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			t.Fatalf("line %q is not the log's JSON: %v", raw, err)
		}
		out = append(out, entry)
	}
	return out
}

/*
================
TestRecordWritesTheLineWithAllFields

An accepted line lands in its UTC day's file with every moderation
field, muted and target included.
================
*/
func TestRecordWritesTheLineWithAllFields(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 28, 16, 22, 51, 0, time.UTC)
	l := Open(dir, 30)
	l.Record(Entry{At: at, AccountID: "spammer", Character: "BadActor", Channel: "all", Text: "buy gold fast"})
	l.Record(Entry{At: at, AccountID: "spammer", Character: "BadActor", Channel: "whisper", Target: "Victim", Muted: true, Text: "psst"})
	l.Close()

	lines := readLines(t, filepath.Join(dir, "chat-2026-10-28.log"))
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2", len(lines))
	}
	if lines[0] != (line{TS: "2026-10-28T16:22:51Z", Account: "spammer", Character: "BadActor", Channel: "all", Text: "buy gold fast"}) {
		t.Fatalf("public line = %+v", lines[0])
	}
	if lines[1] != (line{TS: "2026-10-28T16:22:51Z", Account: "spammer", Character: "BadActor", Channel: "whisper", Target: "Victim", Muted: true, Text: "psst"}) {
		t.Fatalf("muted whisper = %+v", lines[1])
	}
	if got := l.Dropped(); got != 0 {
		t.Fatalf("dropped %d lines on a healthy disk", got)
	}
	var none *Log
	none.Record(Entry{Text: "nowhere"})
	none.Close()
}

/*
================
TestDailyRotationSplitsAtTheUTCDate

Lines on two UTC days land in two daily files, each named for its day.
================
*/
func TestDailyRotationSplitsAtTheUTCDate(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir, 30)
	l.Record(Entry{At: time.Date(2026, 10, 28, 23, 59, 59, 0, time.UTC), AccountID: "a", Character: "A", Channel: "all", Text: "before"})
	l.Record(Entry{At: time.Date(2026, 10, 29, 0, 0, 1, 0, time.UTC), AccountID: "a", Character: "A", Channel: "all", Text: "after"})
	l.Close()

	if lines := readLines(t, filepath.Join(dir, "chat-2026-10-28.log")); len(lines) != 1 || lines[0].Text != "before" {
		t.Fatalf("first day = %+v", lines)
	}
	if lines := readLines(t, filepath.Join(dir, "chat-2026-10-29.log")); len(lines) != 1 || lines[0].Text != "after" {
		t.Fatalf("second day = %+v", lines)
	}
}

/*
================
TestRetentionPrunesOnlyExpiredDailyFiles

With 30 days kept, a file from 30 days before today goes at open and
one from 29 days before stays; foreign files - including the
shadow-mute list beside the log - are never touched.
================
*/
func TestRetentionPrunesOnlyExpiredDailyFiles(t *testing.T) {
	dir := t.TempDir()
	today := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	expired := fileName(today.AddDate(0, 0, -30))
	boundary := fileName(today.AddDate(0, 0, -29))
	current := fileName(today)
	for _, name := range []string{expired, boundary, current, "chat-not-a-day.log", "chat-shadow-mutes.txt", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l := &Log{dir: dir, retainDays: 30, now: func() time.Time { return today }}
	l.prune("")

	if _, err := os.Stat(filepath.Join(dir, expired)); !os.IsNotExist(err) {
		t.Fatalf("%s survived the retention sweep", expired)
	}
	for _, kept := range []string{boundary, current, "chat-not-a-day.log", "chat-shadow-mutes.txt", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Fatalf("%s was removed or lost: %v", kept, err)
		}
	}
}

/*
================
TestRetentionKeepsTheOpenDailyFile

An expired day that is the file being written stays: the writer's own
file is never pruned out from under it, whatever the clock says.
================
*/
func TestRetentionKeepsTheOpenDailyFile(t *testing.T) {
	dir := t.TempDir()
	today := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	old := today.AddDate(0, 0, -40)
	name := fileName(old)
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := &Log{dir: dir, retainDays: 30, now: func() time.Time { return today }}
	l.prune(old.UTC().Format(dateLayout))
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("the open daily file %s was pruned: %v", name, err)
	}
	l.prune("")
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatalf("%s survived once it was no longer open", name)
	}
}

/*
================
TestRecordNeverBlocksOnAFullQueue

A caller whose writer has stopped draining drops lines instead of
waiting: Record returns at once every time.
================
*/
func TestRecordNeverBlocksOnAFullQueue(t *testing.T) {
	l := &Log{queue: make(chan Entry, 2), now: time.Now}
	for range 10 {
		l.Record(Entry{Text: "spam"})
	}
	if got := l.Dropped(); got != 8 {
		t.Fatalf("dropped = %d, want 8 of 10 unqueued", got)
	}
}

/*
================
TestWriteErrorNeverStallsTheCaller

The daily path is a directory, so every open fails: Record still
returns, the writer keeps count of what it lost, and Close comes back.
================
*/
func TestWriteErrorNeverStallsTheCaller(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if err := os.Mkdir(filepath.Join(dir, fileName(at)), 0o755); err != nil {
		t.Fatal(err)
	}
	l := Open(dir, 30)
	for range 5 {
		l.Record(Entry{At: at, AccountID: "a", Character: "A", Channel: "all", Text: "lost"})
	}
	l.Close()
	if got := l.Dropped(); got != 5 {
		t.Fatalf("dropped = %d, want every unwritable line counted", got)
	}
}

/*
================
TestRetainDaysFromEnv
================
*/
func TestRetainDaysFromEnv(t *testing.T) {
	t.Setenv(RetainDaysEnv, "")
	if got := RetainDaysFromEnv(); got != DefaultRetainDays {
		t.Fatalf("unset = %d, want the default %d", got, DefaultRetainDays)
	}
	t.Setenv(RetainDaysEnv, "7")
	if got := RetainDaysFromEnv(); got != 7 {
		t.Fatalf("set = %d, want 7", got)
	}
	for _, bad := range []string{"junk", "0", "-3"} {
		t.Setenv(RetainDaysEnv, bad)
		if got := RetainDaysFromEnv(); got != DefaultRetainDays {
			t.Fatalf("%q = %d, want the default %d", bad, got, DefaultRetainDays)
		}
	}
	l := Open(t.TempDir(), 0)
	defer l.Close()
	if l.retainDays != DefaultRetainDays {
		t.Fatalf("Open(0) kept = %d, want the default %d", l.retainDays, DefaultRetainDays)
	}
}

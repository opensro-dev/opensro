/*
===========================================================================

contributor_test.go - the Contributor title registry and roster frame

Drives the registry through its two files in a temporary directory and
reads the frames it would push: who carries the title, how an opt-out
persists, and who is sent the roster.

===========================================================================
*/
package contributor

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

/*
================
writeFile

The test's stand-in for store.WriteFileAtomic.
================
*/
func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o600)
}

/*
================
writeMembers

Writes the sync's file and moves its time forward, so a rewrite of the
same size inside one clock tick still reads as a change.
================
*/
func writeMembers(t *testing.T, dir, body string, at time.Time) {
	t.Helper()
	path := filepath.Join(dir, MembersFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

/*
================
decodeRoster
================
*/
func decodeRoster(t *testing.T, p []byte) []string {
	t.Helper()
	if len(p) < 2 {
		t.Fatalf("roster %x too short", p)
	}
	n, o := int(binary.LittleEndian.Uint16(p)), 2
	out := []string{}
	for i := 0; i < n; i++ {
		size := int(binary.LittleEndian.Uint16(p[o:]))
		o += 2
		out = append(out, string(p[o:o+size]))
		o += size
	}
	if o != len(p) {
		t.Fatalf("roster %x has %d trailing bytes", p, len(p)-o)
	}
	return out
}

func TestEnabledReadsTheShardFlag(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, " ON ": true, "1": true, "true": true, "yes": false} {
		if got := Enabled(func(string) string { return value }); got != want {
			t.Errorf("Enabled(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestEncodeRosterIsSortedDistinctAndBounded(t *testing.T) {
	got := decodeRoster(t, EncodeRoster([]string{"Zed", "Amy", "", "Amy", strings.Repeat("x", maxNameBytes+1)}))
	if !reflect.DeepEqual(got, []string{"Amy", "Zed"}) {
		t.Fatalf("roster = %v", got)
	}
	if got := EncodeRoster(nil); !reflect.DeepEqual(got, []byte{0, 0}) {
		t.Fatalf("empty roster = %x", got)
	}
	many := make([]string, RosterMax+5)
	for i := range many {
		many[i] = strings.Repeat("a", 1+i%60) + string(rune('A'+i%26)) + strings.Repeat("b", i/1560)
	}
	if n := len(decodeRoster(t, EncodeRoster(many))); n > RosterMax {
		t.Fatalf("roster carried %d names, limit %d", n, RosterMax)
	}
}

func TestMembersFileDecidesWhoIsShown(t *testing.T) {
	dir, at := t.TempDir(), time.Unix(1_700_000_000, 0)
	r, err := Open(dir, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if r.Shown("a1") {
		t.Fatal("no members file still showed a title")
	}
	writeMembers(t, dir, `{"accounts":["a1"," a2 ",""]}`, at)
	r.reloadLocked()
	if !r.Shown("a1") || !r.Shown("a2") || r.Shown("") || r.Shown("a3") {
		t.Fatal("members file not applied")
	}
	// A corrupt rewrite keeps the last good set.
	writeMembers(t, dir, `{"accounts":[`, at.Add(time.Second))
	r.reloadLocked()
	if !r.Shown("a1") {
		t.Fatal("corrupt members file stripped the titles")
	}
	// A removed file is an empty role.
	if err := os.Remove(filepath.Join(dir, MembersFileName)); err != nil {
		t.Fatal(err)
	}
	r.reloadLocked()
	if r.Shown("a1") {
		t.Fatal("removed members file kept a title")
	}
}

func TestCommandHidesAndShowsAndPersists(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, handled := r.Command("a1", "hello /contributor"); handled {
		t.Fatal("ordinary chat taken as the command")
	}
	if answer, handled := r.Command("a9", "/contributor off"); !handled || answer != answerNotMember {
		t.Fatalf("non-member: %q %v", answer, handled)
	}
	if answer, _ := r.Command("a1", "/contributor"); answer != answerShown {
		t.Fatalf("status = %q", answer)
	}
	if answer, _ := r.Command("a1", "/contributor maybe"); answer != answerUsage {
		t.Fatalf("usage = %q", answer)
	}
	if answer, _ := r.Command("a1", "/Contributor OFF"); answer != answerHidden || r.Shown("a1") {
		t.Fatalf("off = %q, shown %v", answer, r.Shown("a1"))
	}
	// The choice survives a restart.
	again, err := Open(dir, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if again.Shown("a1") {
		t.Fatal("opt-out lost across a reopen")
	}
	if answer, _ := again.Command("a1", "/contributor on"); answer != answerShown || !again.Shown("a1") {
		t.Fatalf("on = %q", answer)
	}
	if third, _ := Open(dir, writeFile); !third.Shown("a1") {
		t.Fatal("opt-in lost across a reopen")
	}
}

func TestCommandSaveFailureKeepsTheOldChoice(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, func(string, []byte) error { return errors.New("disk full") })
	if err != nil {
		t.Fatal(err)
	}
	if answer, _ := r.Command("a1", "/contributor off"); answer != answerBusy || !r.Shown("a1") {
		t.Fatalf("failed save = %q, shown %v", answer, r.Shown("a1"))
	}
}

func TestCorruptOptOutFileRefusesTheBoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, HiddenFileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, writeFile); err == nil {
		t.Fatal("corrupt opt-out file opened")
	}
}

func TestPushSendsChangesToAllAndTheRosterToNewArrivals(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	amy := Online{SessionID: "s1", AccountID: "a1", Name: "Amy"}
	bob := Online{SessionID: "s2", AccountID: "a2", Name: "Bob"}
	now := int64(10_000)

	payload, targets, all := r.Push([]Online{amy, bob}, now)
	if !all || targets != nil || !reflect.DeepEqual(decodeRoster(t, payload), []string{"Amy"}) {
		t.Fatalf("first push: all=%v targets=%v roster=%x", all, targets, payload)
	}
	// Off the pace nothing is sent.
	if payload, _, _ := r.Push([]Online{amy, bob}, now+1); payload != nil {
		t.Fatal("pushed off the pace")
	}
	// Same roster: only the arrival is sent it.
	carl := Online{SessionID: "s3", AccountID: "a3", Name: "Carl"}
	now += pushIntervalMs
	payload, targets, all = r.Push([]Online{amy, bob, carl}, now)
	if all || !reflect.DeepEqual(targets, []string{"s3"}) || payload == nil {
		t.Fatalf("arrival push: all=%v targets=%v", all, targets)
	}
	now += pushIntervalMs
	if payload, _, _ := r.Push([]Online{amy, bob, carl}, now); payload != nil {
		t.Fatal("unchanged roster sent again")
	}
	// A contributor hides the title: the next pass is due at once and
	// everyone gets the empty roster.
	r.Command("a1", "/contributor off")
	payload, _, all = r.Push([]Online{amy, bob, carl}, now+1)
	if !all || len(decodeRoster(t, payload)) != 0 {
		t.Fatalf("hide push: all=%v roster=%v", all, decodeRoster(t, payload))
	}
	// A session that left and came back is sent the roster again.
	now += 2 * pushIntervalMs
	r.Push([]Online{amy, bob}, now)
	now += pushIntervalMs
	if _, targets, _ := r.Push([]Online{amy, bob, carl}, now); !reflect.DeepEqual(targets, []string{"s3"}) {
		t.Fatalf("re-entry targets = %v", targets)
	}
}

func TestPushRereadsTheMembersFileOnItsPace(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	amy := Online{SessionID: "s1", AccountID: "a1", Name: "Amy"}
	now := int64(1)
	r.Push([]Online{amy}, now)
	writeMembers(t, dir, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	now += pushIntervalMs
	if payload, _, _ := r.Push([]Online{amy}, now); payload != nil {
		t.Fatal("members file re-read before its pace")
	}
	now += reloadIntervalMs
	payload, _, all := r.Push([]Online{amy}, now)
	if !all || !reflect.DeepEqual(decodeRoster(t, payload), []string{"Amy"}) {
		t.Fatalf("reload push: all=%v roster=%x", all, payload)
	}
}

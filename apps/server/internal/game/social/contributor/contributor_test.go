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

func TestMembersPathIsTheSyncFileOrTheShardDefault(t *testing.T) {
	env := map[string]string{}
	getenv := func(name string) string { return env[name] }
	dir := t.TempDir()
	if got, err := MembersPath(getenv, dir); err != nil || got != filepath.Join(dir, MembersFileName) {
		t.Fatalf("default = %q, %v", got, err)
	}
	abs := filepath.Join(t.TempDir(), "elsewhere.json")
	env[EnvContributorsFile] = " " + abs + " "
	if got, err := MembersPath(getenv, dir); err != nil || got != abs {
		t.Fatalf("absolute = %q, %v", got, err)
	}
	env[EnvContributorsFile] = "relative/contributors.json"
	if _, err := MembersPath(getenv, dir); err == nil {
		t.Fatal("a relative path was accepted")
	}
}

func TestMembersFileOutsideTheShardDirectory(t *testing.T) {
	shard, elsewhere := t.TempDir(), t.TempDir()
	writeMembers(t, elsewhere, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(shard, filepath.Join(elsewhere, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Shown("a1") {
		t.Fatal("members file outside the shard directory not read")
	}
	if answer, _ := r.Command("a1", "/contributor off"); answer != answerHidden {
		t.Fatalf("off = %q", answer)
	}
	if _, err := os.Stat(filepath.Join(shard, HiddenFileName)); err != nil {
		t.Fatalf("opt-out not kept in the shard directory: %v", err)
	}
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
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
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
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
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
	again, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if again.Shown("a1") {
		t.Fatal("opt-out lost across a reopen")
	}
	if answer, _ := again.Command("a1", "/contributor on"); answer != answerShown || !again.Shown("a1") {
		t.Fatalf("on = %q", answer)
	}
	if third, _ := Open(dir, filepath.Join(dir, MembersFileName), writeFile); !third.Shown("a1") {
		t.Fatal("opt-in lost across a reopen")
	}
}

func TestCommandSaveFailureKeepsTheOldChoice(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), func(string, []byte) error { return errors.New("disk full") })
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
	if _, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile); err == nil {
		t.Fatal("corrupt opt-out file opened")
	}
}

func TestPushSendsChangesToAllAndTheRosterToNewArrivals(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, `{"accounts":["a1"]}`, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
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
	// The refresh bound resends the unchanged roster to everyone.
	now += refreshIntervalMs + pushIntervalMs
	if _, _, all := r.Push([]Online{amy, bob, carl}, now); !all {
		t.Fatal("refresh did not resend the roster to everyone")
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
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
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

// grouped is a sync roster: member m1 links a1 (oldest), a2 and a3;
// member m2 links b1. The flat list is each member's oldest account.
const grouped = `{"members":[{"member":"m1","accounts":["a1","a2","a3"]},` +
	`{"member":"m2","accounts":["b1"]}],"accounts":["a1","b1"]}`

func TestOneMemberTitlesOnlyItsFirstAccount(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, grouped, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Shown("a1") || r.Shown("a2") || r.Shown("a3") || !r.Shown("b1") {
		t.Fatalf("shown a1 %v a2 %v a3 %v b1 %v", r.Shown("a1"), r.Shown("a2"), r.Shown("a3"), r.Shown("b1"))
	}
	if answer, _ := r.Command("a2", "/contributor"); answer != answerElsewhere {
		t.Fatalf("status on a linked untitled account = %q", answer)
	}
	if answer, _ := r.Command("a2", "/contributor off"); answer != answerElsewhere || !r.Shown("a1") {
		t.Fatalf("off on an untitled account = %q, a1 shown %v", answer, r.Shown("a1"))
	}
	if answer, _ := r.Command("c1", "/contributor here"); answer != answerNotMember {
		t.Fatalf("here from an unlinked account = %q", answer)
	}
}

func TestHereMovesTheTitleAndPersists(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, grouped, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	r.Push([]Online{{SessionID: "s1", AccountID: "a1", Name: "First"}}, 0)
	if answer, _ := r.Command("a3", "/contributor here"); answer != answerMoved+" "+answerShown {
		t.Fatalf("here = %q", answer)
	}
	if r.Shown("a1") || !r.Shown("a3") || !r.Shown("b1") {
		t.Fatalf("after here: a1 %v a3 %v b1 %v", r.Shown("a1"), r.Shown("a3"), r.Shown("b1"))
	}
	// Every viewer is sent the moved roster on the next pass.
	payload, _, all := r.Push([]Online{
		{SessionID: "s1", AccountID: "a1", Name: "First"},
		{SessionID: "s3", AccountID: "a3", Name: "Third"},
	}, 1)
	if !all || !reflect.DeepEqual(decodeRoster(t, payload), []string{"Third"}) {
		t.Fatalf("moved roster = %v (all %v)", decodeRoster(t, payload), all)
	}
	if answer, _ := r.Command("a3", "/contributor here"); answer != answerShown {
		t.Fatalf("here on the titled account = %q", answer)
	}
	// The move survives a restart.
	again, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if again.Shown("a1") || !again.Shown("a3") {
		t.Fatal("move lost across a reopen")
	}
	// The member unlinks a3: the title falls back to its first account.
	writeMembers(t, dir, `{"members":[{"member":"m1","accounts":["a1","a2"]}],"accounts":["a1"]}`,
		time.Unix(1_700_000_100, 0))
	again.reloadLocked()
	if !again.Shown("a1") || again.Shown("a3") {
		t.Fatal("an unlinked choice kept the title")
	}
}

func TestHereCarriesTheAccountsOwnOptOut(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, grouped, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), writeFile)
	if err != nil {
		t.Fatal(err)
	}
	r.Command("a1", "/contributor off")
	r.Command("a2", "/contributor here")
	if !r.Shown("a2") {
		t.Fatal("a2 does not show the moved title")
	}
	if answer, _ := r.Command("a1", "/contributor here"); answer != answerMoved+" "+answerHidden || r.Shown("a1") {
		t.Fatalf("back to the hidden account = %q, shown %v", answer, r.Shown("a1"))
	}
}

func TestHereSaveFailureKeepsTheTitleWhereItWas(t *testing.T) {
	dir := t.TempDir()
	writeMembers(t, dir, grouped, time.Unix(1_700_000_000, 0))
	r, err := Open(dir, filepath.Join(dir, MembersFileName), func(string, []byte) error { return errors.New("disk full") })
	if err != nil {
		t.Fatal(err)
	}
	if answer, _ := r.Command("a2", "/contributor here"); answer != answerBusy || !r.Shown("a1") || r.Shown("a2") {
		t.Fatalf("failed move = %q, a1 %v a2 %v", answer, r.Shown("a1"), r.Shown("a2"))
	}
}

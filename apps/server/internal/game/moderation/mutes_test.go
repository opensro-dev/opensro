/*
===========================================================================

mutes_test.go - the mute book survives restarts, ends on time, never lies

===========================================================================
*/
package moderation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

type fakeHub struct {
	set map[string][8]uint16
}

func newFakeHub() *fakeHub { return &fakeHub{set: map[string][8]uint16{}} }

func (h *fakeHub) SetCharacterRestriction(division, name string, kind uint8, until [8]uint16) bool {
	if kind != transport.CommandRestrictionChat {
		panic("only chat mutes are installed")
	}
	h.set[division+":"+strings.ToLower(name)] = until
	return true
}

func (h *fakeHub) ClearCharacterRestriction(division, name string, kind uint8) bool {
	delete(h.set, division+":"+strings.ToLower(name))
	return true
}

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func writeFile(path string, payload []byte) error { return os.WriteFile(path, payload, 0o600) }

func sample(name string) Mute {
	return Mute{Division: "DIV", Character: name, Operator: "staff", Clause: "C2", Reason: "slur in all chat"}
}

/*
================
TestMuteSurvivesARestartAndEndsOnTime
================
*/
func TestMuteSurvivesARestartAndEndsOnTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), MuteFileName)
	c := &clock{at: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)}
	hub := newFakeHub()
	book, err := OpenBook(path, hub, c.now, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	mute, err := book.Mute(sample("Loud"), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	want := [8]uint16{2026, 11, 6, 7, 10, 0, 0, 0} // Saturday the 7th, 10:00 UTC
	if hub.set["DIV:loud"] != want {
		t.Fatalf("installed %v, want %v", hub.set["DIV:loud"], want)
	}

	// A restart: a fresh hub and book read the file back.
	c.at = c.at.Add(time.Hour)
	reborn := newFakeHub()
	book, err = OpenBook(path, reborn, c.now, writeFile)
	if err != nil {
		t.Fatal(err)
	}
	if reborn.set["DIV:loud"] != want {
		t.Fatal("the mute did not survive the restart")
	}
	if got, ok := book.Active("DIV", "LOUD"); !ok || got.UntilMs != mute.UntilMs || got.Clause != "C2" {
		t.Fatalf("active after restart = %+v %v", got, ok)
	}

	// Not yet: nothing reaped one minute early.
	c.at = time.UnixMilli(mute.UntilMs).Add(-time.Minute)
	if ended, _ := book.Reap(); len(ended) != 0 {
		t.Fatal("reaped early")
	}
	c.at = time.UnixMilli(mute.UntilMs)
	ended, err := book.Reap()
	if err != nil || len(ended) != 1 || ended[0].Character != "Loud" {
		t.Fatalf("reap = %v %v", ended, err)
	}
	if _, ok := reborn.set["DIV:loud"]; ok {
		t.Fatal("reap did not clear the hub")
	}
	// The file no longer holds it either.
	again, _ := OpenBook(path, newFakeHub(), c.now, writeFile)
	if _, ok := again.Active("DIV", "Loud"); ok {
		t.Fatal("an ended mute came back from disk")
	}
}

/*
================
TestAMuteThatEndedWhileDownIsNotReinstalled
================
*/
func TestAMuteThatEndedWhileDownIsNotReinstalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), MuteFileName)
	c := &clock{at: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)}
	book, _ := OpenBook(path, newFakeHub(), c.now, writeFile)
	if _, err := book.Mute(sample("Gone"), time.Hour); err != nil {
		t.Fatal(err)
	}
	c.at = c.at.Add(2 * time.Hour)
	hub := newFakeHub()
	if _, err := OpenBook(path, hub, c.now, writeFile); err != nil {
		t.Fatal(err)
	}
	if len(hub.set) != 0 {
		t.Fatal("an expired mute was installed at boot")
	}
}

/*
================
TestUnmuteAndReplace
================
*/
func TestUnmuteAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), MuteFileName)
	c := &clock{at: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)}
	hub := newFakeHub()
	book, _ := OpenBook(path, hub, c.now, writeFile)
	book.Mute(sample("Loud"), time.Hour)
	longer, _ := book.Mute(sample("loud"), 7*24*time.Hour)
	if got, _ := book.Active("DIV", "Loud"); got.UntilMs != longer.UntilMs {
		t.Fatal("a second mute did not replace the first")
	}
	if _, err := book.Unmute("DIV", "LOUD"); err != nil {
		t.Fatal(err)
	}
	if len(hub.set) != 0 {
		t.Fatal("unmute left the hub record")
	}
	if _, err := book.Unmute("DIV", "Loud"); !errors.Is(err, ErrNoMute) {
		t.Fatalf("second unmute = %v", err)
	}
}

/*
================
TestAMuteIsRefusedWithoutClauseReasonOrSaneLength
================
*/
func TestAMuteIsRefusedWithoutClauseReasonOrSaneLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), MuteFileName)
	c := &clock{at: time.Now()}
	hub := newFakeHub()
	book, _ := OpenBook(path, hub, c.now, writeFile)
	bad := []struct {
		edit     func(*Mute)
		duration time.Duration
	}{
		{func(m *Mute) { m.Clause = "" }, time.Hour},
		{func(m *Mute) { m.Clause = "C 2" }, time.Hour},
		{func(m *Mute) { m.Reason = "bad" }, time.Hour},
		{func(m *Mute) { m.Operator = "" }, time.Hour},
		{func(m *Mute) { m.Character = " " }, time.Hour},
		{func(m *Mute) {}, 30 * time.Second},
		{func(m *Mute) {}, MaxMuteDuration + time.Hour},
	}
	for i, b := range bad {
		m := sample("Loud")
		b.edit(&m)
		if _, err := book.Mute(m, b.duration); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	if len(hub.set) != 0 {
		t.Fatal("a refused mute reached the hub")
	}
}

/*
================
TestAFailedSaveInstallsNothing

The book never announces a mute the disk does not hold.
================
*/
func TestAFailedSaveInstallsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), MuteFileName)
	fail := false
	write := func(p string, b []byte) error {
		if fail {
			return errors.New("disk full")
		}
		return writeFile(p, b)
	}
	hub := newFakeHub()
	book, _ := OpenBook(path, hub, time.Now, write)
	fail = true
	if _, err := book.Mute(sample("Loud"), time.Hour); err == nil {
		t.Fatal("mute succeeded on a failed save")
	}
	if len(hub.set) != 0 {
		t.Fatal("hub muted without a saved record")
	}
	if _, ok := book.Active("DIV", "Loud"); ok {
		t.Fatal("book holds a mute it could not save")
	}
}

/*
================
TestACorruptBookRefusesToStart
================
*/
func TestACorruptBookRefusesToStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), MuteFileName)
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := OpenBook(path, newFakeHub(), time.Now, writeFile); err == nil {
		t.Fatal("a corrupt book opened")
	}
}

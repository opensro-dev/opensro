/*
===========================================================================

book_test.go - the staff book's contract and the capability table

===========================================================================
*/
package staff

import (
	"errors"
	"math/bits"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

func openTestBook(t *testing.T, dir string) *Book {
	t.Helper()
	write := func(path string, payload []byte) error { return os.WriteFile(path, payload, 0o600) }
	book, err := OpenBook(filepath.Join(dir, FileName), func() time.Time { return time.UnixMilli(1_790_000_000_000) }, write)
	if err != nil {
		t.Fatal(err)
	}
	return book
}

/*
================
TestBookSurvivesARestart

A grant and an edited role are on disk after each change and come back
on the next boot; a revoke is gone after it.
================
*/
func TestBookSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	book := openTestBook(t, dir)
	if _, err := book.SetRole(Role{ID: "mod", Tag: "[MOD]", NameColor: "#7ad8ff", ChatColor: "#c0f0ff"}); err != nil {
		t.Fatal(err)
	}
	grant, mask, err := book.Grant(Request{Division: "global-official", Character: "Hundred", Role: "mod",
		Capabilities: []string{"teleport", "find_user"}, Operator: "staff-a"})
	if err != nil {
		t.Fatal(err)
	}
	if mask != TeleportSelf|FindUser || len(grant.Capabilities) != 2 {
		t.Fatalf("grant = %+v mask %b", grant, mask)
	}

	again := openTestBook(t, dir)
	got, ok := again.Lookup("global-official", "HUNDRED")
	if !ok || got.Role != "mod" || got.Operator != "staff-a" {
		t.Fatalf("lookup after restart = %+v %v", got, ok)
	}
	roster := again.Roster("global-official", []string{"HostGM"})
	if len(roster) != 2 || roster[0].Character != "HostGM" || roster[0].Tag != "[GM]" || roster[0].NameColor != 0xFFFFD87A ||
		roster[0].ChatColor != 0xFFFFAEC3 || roster[1].Tag != "[MOD]" || roster[1].NameColor != 0xFF7AD8FF {
		t.Fatalf("roster = %+v", roster)
	}
	if again.Roster("other-division", nil) != nil {
		t.Fatal("another division's roster holds grants")
	}

	if _, err := again.Revoke("global-official", "hundred"); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Revoke("global-official", "hundred"); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("second revoke = %v", err)
	}
	if _, ok := openTestBook(t, dir).Lookup("global-official", "Hundred"); ok {
		t.Fatal("revoked grant came back after a restart")
	}
}

/*
================
TestOnlyAnApproverAddsAnElevatedPower

Make item is elevated: an operator cannot add it, may keep it or drop it,
and an approver may add it. The rule is the book's, not the console's.
================
*/
func TestOnlyAnApproverAddsAnElevatedPower(t *testing.T) {
	book := openTestBook(t, t.TempDir())
	request := Request{Division: "d", Character: "Hundred", Role: GMRole, Capabilities: []string{"make_item", "teleport"}, Operator: "op"}
	if _, _, err := book.Grant(request); !errors.Is(err, ErrElevated) {
		t.Fatalf("operator added make_item: %v", err)
	}
	if _, ok := book.Lookup("d", "Hundred"); ok {
		t.Fatal("a refused grant was stored")
	}
	request.Approver = true
	if _, _, err := book.Grant(request); err != nil {
		t.Fatal(err)
	}
	request.Approver = false
	if _, _, err := book.Grant(request); err != nil {
		t.Fatalf("operator could not keep make_item: %v", err)
	}
	request.Capabilities = []string{"teleport"}
	if _, mask, err := book.Grant(request); err != nil || mask != TeleportSelf {
		t.Fatalf("operator could not drop make_item: %v %b", err, mask)
	}
	request.Capabilities = []string{"make_item"}
	if _, _, err := book.Grant(request); !errors.Is(err, ErrElevated) {
		t.Fatalf("operator re-added a dropped make_item: %v", err)
	}
}

/*
================
TestBookRefusesBadInput
================
*/
func TestBookRefusesBadInput(t *testing.T) {
	dir := t.TempDir()
	book := openTestBook(t, dir)
	for _, role := range []Role{
		{ID: "Bad Id", Tag: "[X]", NameColor: "#FFFFFF", ChatColor: "#FFFFFF"},
		{ID: "x", Tag: "", NameColor: "#FFFFFF", ChatColor: "#FFFFFF"},
		{ID: "x", Tag: "[A]\n[GM]B", NameColor: "#FFFFFF", ChatColor: "#FFFFFF"},
		{ID: "x", Tag: "[TOO-LONG-TAG]", NameColor: "#FFFFFF", ChatColor: "#FFFFFF"},
		{ID: "x", Tag: "[X]", NameColor: "red", ChatColor: "#FFFFFF"},
	} {
		if _, err := book.SetRole(role); err == nil {
			t.Fatalf("role %+v accepted", role)
		}
	}
	for _, request := range []Request{
		{Division: "d", Character: "A", Role: "nope", Operator: "op"},
		{Division: "d", Character: "A", Role: GMRole, Capabilities: []string{"fly"}, Operator: "op"},
		{Division: "d", Character: "A", Role: GMRole},
	} {
		if _, _, err := book.Grant(request); err == nil {
			t.Fatalf("grant %+v accepted", request)
		}
	}
	// A corrupt book refuses the boot rather than forgetting every grant.
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(string, []byte) error { return nil }
	if _, err := OpenBook(filepath.Join(dir, FileName), time.Now, write); err == nil {
		t.Fatal("corrupt book opened")
	}
}

/*
================
TestAllowsReadsTheProjectedBits
================
*/
func TestAllowsReadsTheProjectedBits(t *testing.T) {
	c := &domain.Character{GMPrivilege: true, GMCapabilities: uint32(TeleportSelf | Ban)}
	if !Allows(c, TeleportSelf) || !Allows(c, Ban) || Allows(c, MakeItem) || Allows(c, 0) {
		t.Fatal("capability bits misread")
	}
	c.GMPrivilege = false
	if Allows(c, TeleportSelf) || Allows(nil, TeleportSelf) {
		t.Fatal("a non-GM was allowed")
	}
	if Capability(All) != capabilityEnd-1 || len(Rows) != bits.OnesCount32(uint32(All)) {
		t.Fatalf("table and All disagree: %d rows", len(Rows))
	}
	var seen Capability
	for _, row := range Rows {
		if row.Capability == 0 || seen&row.Capability != 0 {
			t.Fatalf("row %s repeats or lacks a bit", row.ID)
		}
		seen |= row.Capability
	}
	if seen != All {
		t.Fatalf("a capability has no row: %b of %b", seen, All)
	}
}

/*
===========================================================================

minters_test.go - the minting accounts over a real store's snapshot

Gold is moved through the store's own doors (a pickup and a merchant
sale land in the character hour buckets, a quest reward and the
creation seed as exact rows), and one minting character is deleted and
reaped into the archive before the snapshot: the route reads what the
store writes, and a deletion keeps the gold on its account.

===========================================================================
*/
package economy

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
)

/*
================
mintingCharacter

A character of account in the test division, created through the store.
================
*/
func mintingCharacter(t *testing.T, authority *store.Store, account, name string) *domain.Character {
	t.Helper()
	gold := int64(0)
	c := &domain.Character{Name: name, Gold: &gold}
	if err := authority.CreateCharacter("d1", account, c); err != nil {
		t.Fatal(err)
	}
	return c
}

/*
================
goldThrough

One door that leaves c holding balance.
================
*/
func goldThrough(authority *store.Store, c *domain.Character, door string, balance int64) {
	authority.UpdateCharacter(c, door, func() bool {
		c.Gold = &balance
		return true
	})
}

/*
================
TestMintersRouteRanksAccountsByTheirBusiestHour
================
*/
func TestMintersRouteRanksAccountsByTheirBusiestHour(t *testing.T) {
	// One fixed mid-hour instant for the store and the route, so every
	// change lands in one hour bucket. Creation needs a racial base
	// skill set; any one id satisfies it.
	now := time.Unix(dayStart+10*3600+1800, 0)
	authority, err := store.Open(t.TempDir(), store.Options{
		Now:           func() time.Time { return now },
		DefaultSkills: func(string, []uint32) ([]uint32, error) { return []uint32{1}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)

	farmer := mintingCharacter(t, authority, "farm", "Farmer")
	goldThrough(authority, farmer, "pickup-gold", 5000)        // monster_gold: a bucket
	goldThrough(authority, farmer, "shop-transaction", 9000)   // npc_sale: a bucket
	goldThrough(authority, farmer, "shop-transaction", 10000)  // a second sale
	goldThrough(authority, farmer, "shop-transaction", 9500)   // a buy burns: no mint
	alt := mintingCharacter(t, authority, "farm", "FarmerAlt") // deleted below
	goldThrough(authority, alt, "quest-reward", 2000)          // an exact row
	player := mintingCharacter(t, authority, "player", "Player")
	goldThrough(authority, player, "quest-reward", 300)
	goldThrough(authority, player, "storage-transfer", 1300) // a withdrawal moves gold, mints none

	if !authority.ReserveCharacterDeletion(alt, now.Add(-8*24*time.Hour).UTC().Format(time.RFC3339)) {
		t.Fatal("deletion not reserved")
	}
	if reaped := authority.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v", reaped)
	}

	dir := t.TempDir()
	service, err := Open(dir, authority, time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	token := filepath.Join(dir, TokenFile)
	if err := os.WriteFile(token, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := service.Handler(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, handler, Path+"minters?from="+strconv.FormatInt(dayStart, 10)+"&to="+strconv.FormatInt(now.Unix(), 10), true)
	if code != http.StatusOK || body["truncated"] != false {
		t.Fatalf("minters = %d %v", code, body)
	}
	minters, _ := body["minters"].([]any)
	if len(minters) != 2 {
		t.Fatalf("minters %v", minters)
	}
	first, second := minters[0].(map[string]any), minters[1].(map[string]any)
	hour := float64(now.Unix() / 3600 * 3600)
	// Farmer: 5000 picked up and 5000 sold, FarmerAlt's 2000 reward
	// after its deletion, all in this hour; two sales; the buy mints nothing.
	if first["account"] != "farm" || first["total"] != float64(12000) || first["peakTotal"] != float64(12000) ||
		first["peakHour"] != hour || first["characters"] != float64(2) || first["npcSales"] != float64(2) {
		t.Fatalf("farm %v", first)
	}
	if second["account"] != "player" || second["total"] != float64(300) || second["npcSales"] != float64(0) {
		t.Fatalf("player %v", second)
	}

	if code, _ := get(t, handler, Path+"minters?from=10&to=5", true); code != http.StatusBadRequest {
		t.Fatalf("a reversed window answered %d", code)
	}
}

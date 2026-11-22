/*
===========================================================================

grants_test.go - the free grants route over a real store's snapshot

The rows come from the producer itself (store.GrantBetaSilkStarter and
CreditBetaSilk writing grant_account_days), copied by the store's own
SnapshotTo: the route reads the table the store writes, not a fixture's
idea of it.

===========================================================================
*/
package economy

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
)

/*
================
TestGrantsRouteAnswersPerAccountFromTheSnapshot
================
*/
func TestGrantsRouteAnswersPerAccountFromTheSnapshot(t *testing.T) {
	authority, err := store.Open(t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	for _, account := range []string{"farm-1", "farm-2"} {
		if _, err := authority.GrantBetaSilkStarter(account, 100); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := authority.CreditBetaSilk("farm-1", 7, 1000); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	now := time.Now()
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
	if code, body := get(t, handler, Path+"grants", true); code != http.StatusServiceUnavailable || errorCode(body) != "no_snapshot" {
		t.Fatalf("before a snapshot: %d %v", code, body)
	}
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, handler, Path+"grants?account=farm-1", true)
	if code != http.StatusOK {
		t.Fatalf("grants = %d %v", code, body)
	}
	grants, _ := body["grants"].([]any)
	if len(grants) != 2 || body["truncated"] != false {
		t.Fatalf("farm-1 grants %v", body)
	}
	sources := map[string]float64{}
	for _, entry := range grants {
		grant := entry.(map[string]any)
		if grant["account"] != "farm-1" || grant["item"] != "silk" {
			t.Fatalf("grant %v", grant)
		}
		sources[grant["source"].(string)] = grant["total"].(float64)
	}
	if sources["starter-silk"] != 100 || sources["beta-silk"] != 7 {
		t.Fatalf("farm-1 sources %v", sources)
	}
	if _, all := get(t, handler, Path+"grants", true); len(all["grants"].([]any)) != 3 {
		t.Fatalf("every account: %v", all)
	}
}

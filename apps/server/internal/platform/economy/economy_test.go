/*
===========================================================================

economy_test.go - the aggregates a snapshot answers, and who may ask

The tests build a snapshot database by hand (the store's tables the
aggregates read) and hand it over through a fake snapshotter, so every
number below is known.

===========================================================================
*/
package economy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

// testToken is the operator token the fixture writes.
const testToken = "0123456789abcdef0123456789abcdef-economy"

// dayStart is a fixed day boundary (2026-10-01 00:00 UTC).
const dayStart = int64(1790812800)

/*
================
ledgerFixture
================
*/
type ledgerFixture struct {
	gold    int64
	reason  string
	atS     int64
	charID  int64
	balance int64
}

/*
================
fileSnapshotter

Copies a prepared database to the snapshot path, as the store would.
================
*/
type fileSnapshotter struct {
	source string
}

/*
================
fileSnapshotter.SnapshotTo
================
*/
func (f fileSnapshotter) SnapshotTo(path string) error {
	bytes, err := os.ReadFile(f.source)
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o600)
}

/*
================
writeSnapshotSource

A database holding the given ledger rows, characters (division, record)
and warehouses (account, record).
================
*/
func writeSnapshotSource(t *testing.T, ledger []ledgerFixture, characters [][2]string, warehouses [][2]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE economy_ledger (seq INTEGER PRIMARY KEY, at_ms INTEGER NOT NULL, division TEXT NOT NULL,
  char_id INTEGER NOT NULL, reason TEXT NOT NULL, door TEXT NOT NULL, gold INTEGER NOT NULL, balance INTEGER NOT NULL);
CREATE TABLE characters (division TEXT NOT NULL, id INTEGER NOT NULL, name_lower TEXT NOT NULL, record TEXT NOT NULL);
CREATE TABLE account_storage (account_id TEXT PRIMARY KEY, record TEXT NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	for _, row := range ledger {
		if _, err := db.Exec("INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance) VALUES (?, 'd1', ?, ?, 'door', ?, ?)",
			row.atS*1000, row.charID, row.reason, row.gold, row.balance); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range characters {
		if _, err := db.Exec("INSERT INTO characters (division, id, name_lower, record) VALUES (?, ?, ?, ?)", c[0], i+1, fmt.Sprint("c", i), c[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range warehouses {
		if _, err := db.Exec("INSERT INTO account_storage (account_id, record) VALUES (?, ?)", w[0], w[1]); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

/*
================
newTestService

A service over source whose clock reads now, its token file written.
================
*/
func newTestService(t *testing.T, source string, now time.Time) (*Service, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	service, err := Open(dir, fileSnapshotter{source: source}, time.Hour, func() time.Time { return now })
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
	return service, handler
}

/*
================
get

An authorised local GET; returns the status and the decoded JSON body
(nil for a plain answer such as a 404).
================
*/
func get(t *testing.T, handler http.Handler, target string, authorised bool) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8791"+target, nil)
	r.RemoteAddr = "127.0.0.1:50000"
	if authorised {
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("X-SRO-Local-Diagnostics", "1")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var body map[string]any
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v: %s", target, err, w.Body.String())
		}
	}
	return w.Code, body
}

/*
================
errorCode
================
*/
func errorCode(body map[string]any) string {
	failure, _ := body["error"].(map[string]any)
	code, _ := failure["code"].(string)
	return code
}

/*
================
TestSeriesFoldMintBurnNetAndReason

A mint sums mint gains, a burn burn losses (as a magnitude), net the two;
a transfer and an unattributed change count in neither; a reason series
sums that reason alone.
================
*/
func TestSeriesFoldMintBurnNetAndReason(t *testing.T) {
	source := writeSnapshotSource(t, []ledgerFixture{
		{gold: 100, reason: "npc_sale", atS: dayStart + 10},
		{gold: -30, reason: "shop_buy", atS: dayStart + 20},
		{gold: -50, reason: "exchange", atS: dayStart + 30},
		{gold: 50, reason: "exchange", atS: dayStart + 30},
		{gold: -25, reason: "storage_deposit", atS: dayStart + 35},
		{gold: -200, reason: "guild_storage", atS: dayStart + 36},
		{gold: 150, reason: "guild_storage", atS: dayStart + 37},
		{gold: 7, reason: "unattributed", atS: dayStart + 40},
		{gold: 20, reason: "monster_gold", atS: dayStart + 3600 + 5},
		{gold: -1000, reason: "stall_sale", atS: dayStart + 3600 + 10},
		{gold: 990, reason: "stall_sale", atS: dayStart + 3600 + 10},
		{gold: -50, reason: "repair", atS: dayStart + 2*3600 + 5},
	}, nil, nil)
	service, handler := newTestService(t, source, time.Unix(dayStart+3*3600, 0))
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		metric string
		sums   []string
	}{
		{"gold_minted", []string{"100", "20"}},
		{"gold_burned", []string{"30", "10", "50"}},
		{"gold_net", []string{"70", "10", "-50"}},
		{"reason:shop_buy", []string{"30"}},
		{"reason:repair", []string{"50"}},
		{"reason:exchange", []string{"50"}},
		{"reason:stall_sale", []string{"990"}},
		{"reason:storage_deposit", []string{"25"}},
		{"reason:guild_storage", []string{"350"}},
	}
	for _, tc := range cases {
		target := fmt.Sprintf("/economy/v1/series?metric=%s&bucket=1h&from=%d&to=%d", tc.metric, dayStart, dayStart+3*3600)
		status, body := get(t, handler, target, true)
		points, _ := body["points"].([]any)
		if status != http.StatusOK || len(points) != len(tc.sums) {
			t.Fatalf("%s: %d %v", tc.metric, status, body)
		}
		for i, sum := range tc.sums {
			if point := points[i].(map[string]any); point["sum"] != sum {
				t.Fatalf("%s point %d = %v, want sum %s", tc.metric, i, point, sum)
			}
		}
	}
}

/*
================
TestFlowsAlignEveryBucket

Every reason's array has one entry per bucket of the window, as a positive
magnitude, and "0" where it moved nothing.
================
*/
func TestFlowsAlignEveryBucket(t *testing.T) {
	source := writeSnapshotSource(t, []ledgerFixture{
		{gold: 100, reason: "npc_sale", atS: dayStart + 10},
		{gold: -50, reason: "exchange", atS: dayStart + 20},
		{gold: 50, reason: "exchange", atS: dayStart + 20},
		{gold: -25, reason: "storage_deposit", atS: dayStart + 3600 + 10},
		{gold: -1000, reason: "stall_sale", atS: dayStart + 3600 + 20},
		{gold: 990, reason: "stall_sale", atS: dayStart + 3600 + 20},
		{gold: -200, reason: "guild_storage", atS: dayStart + 2*3600},
		{gold: 150, reason: "guild_storage", atS: dayStart + 2*3600},
		{gold: -40, reason: "repair", atS: dayStart + 2*3600},
	}, nil, nil)
	service, handler := newTestService(t, source, time.Unix(dayStart+3*3600, 0))
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	status, body := get(t, handler, fmt.Sprintf("/economy/v1/flows?bucket=1h&from=%d&to=%d", dayStart, dayStart+2*3600), true)
	buckets, _ := body["t"].([]any)
	reasons, _ := body["reasons"].(map[string]any)
	if status != http.StatusOK || len(buckets) != 3 {
		t.Fatalf("flows %d %v", status, body)
	}
	if got := fmt.Sprint(reasons["npc_sale"]); got != "[100 0 0]" {
		t.Fatalf("npc_sale %s", got)
	}
	if got := fmt.Sprint(reasons["exchange"]); got != "[50 0 0]" {
		t.Fatalf("exchange %s", got)
	}
	if got := fmt.Sprint(reasons["storage_deposit"]); got != "[0 25 0]" {
		t.Fatalf("storage_deposit %s", got)
	}
	if got := fmt.Sprint(reasons["stall_sale"]); got != "[0 990 0]" {
		t.Fatalf("stall_sale %s", got)
	}
	if got := fmt.Sprint(reasons["guild_storage"]); got != "[0 0 350]" {
		t.Fatalf("guild_storage %s", got)
	}
	if got := fmt.Sprint(reasons["repair"]); got != "[0 0 40]" {
		t.Fatalf("repair %s", got)
	}
}

/*
================
TestDistributionCountsGoldGearAndActivity

Characters' gold and the warehouse's, the 14-day active accounts, plus
levels of equipment only, and seals by degree and letter.
================
*/
func TestDistributionCountsGoldGearAndActivity(t *testing.T) {
	now := time.Unix(dayStart, 0)
	recent, stale := now.Add(-time.Hour).UnixMilli(), now.Add(-30*24*time.Hour).UnixMilli()
	equipment := int(wire.PackTypeFlags(3, 1, 1, 0)) // an equipment-band row
	etc := int(wire.PackTypeFlags(3, 3, 1, 0))       // an ETC-band row
	character := func(account string, gold, login int64, items string) string {
		return fmt.Sprintf(`{"accountId":%q,"gold":%d,"lastLoginAtMs":%d,"missionInventory":[%s]}`, account, gold, login, items)
	}
	source := writeSnapshotSource(t, nil, [][2]string{
		{"d1", character("a", 10, recent, fmt.Sprintf(`{"codename":"ITEM_CH_BLADE_08_C_RARE","typeFlags":%d,"plus":7},{"codename":"ITEM_ETC_HP_POTION_01","typeFlags":%d,"plus":0}`, equipment, etc))},
		{"d1", character("a", 90, stale, fmt.Sprintf(`{"codename":"ITEM_CH_CLOTHES_02_FA_A_RARE","typeFlags":%d,"plus":0}`, equipment))},
		{"d1", character("b", 0, stale, "")},
	}, [][2]string{{"a", `{"capacity":150,"gold":900,"rows":[]}`}})
	service, handler := newTestService(t, source, now)
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	status, body := get(t, handler, "/economy/v1/distribution", true)
	gold, _ := body["gold"].(map[string]any)
	if status != http.StatusOK || gold["total"] != "1000" || gold["characters"] != float64(3) || gold["accounts14d"] != float64(1) ||
		gold["max"] != "90" || gold["p50"] != "10" {
		t.Fatalf("distribution %d %v", status, body)
	}
	if plus := fmt.Sprint(body["plus"]); plus != "map[0:1 7:1]" {
		t.Fatalf("plus %s", plus)
	}
	if sox := fmt.Sprint(body["sox"]); sox != "[map[degree:2 moon:0 star:1 sun:0] map[degree:8 moon:0 star:0 sun:1]]" {
		t.Fatalf("sox %s", sox)
	}
	if active := fmt.Sprint(body["soxAccounts14d"]); active != "[map[degree:2 moon:0 star:1 sun:0] map[degree:8 moon:0 star:0 sun:1]]" {
		t.Fatalf("active sox %s", active)
	}
	status, body = get(t, handler, "/economy/v1/distribution?division=d1", true)
	if gold, _ := body["gold"].(map[string]any); status != http.StatusOK || gold["total"] != "100" {
		t.Fatalf("division block %d %v", status, body)
	}
}

/*
================
TestGiniShareAndPercentile
================
*/
func TestGiniShareAndPercentile(t *testing.T) {
	if g := gini([]int64{5, 5, 5, 5}); g != 0 {
		t.Fatalf("equal holdings gini %v", g)
	}
	if g := gini([]int64{0, 0, 0, 100}); g != 0.75 {
		t.Fatalf("one holder gini %v", g)
	}
	if share := topShare([]int64{1, 1, 1, 7}, 0.10); share != 0.7 {
		t.Fatalf("top share %v", share)
	}
	if p := percentile([]int64{1, 2, 3, 4}, 0.5); p != 2 {
		t.Fatalf("p50 %d", p)
	}
}

/*
================
TestNetMintFlagRaisesOnASpike

Seven quiet days, then a day minting far more: one flag on that day.
================
*/
func TestNetMintFlagRaisesOnASpike(t *testing.T) {
	var ledger []ledgerFixture
	for day := int64(0); day < 8; day++ {
		ledger = append(ledger, ledgerFixture{gold: 10, reason: "monster_gold", atS: dayStart + day*86400 + 60})
	}
	ledger = append(ledger, ledgerFixture{gold: 5000, reason: "monster_gold", atS: dayStart + 8*86400 + 60})
	service, handler := newTestService(t, writeSnapshotSource(t, ledger, nil, nil), time.Unix(dayStart+9*86400, 0))
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	status, body := get(t, handler, fmt.Sprintf("/economy/v1/flags?from=%d&to=%d", dayStart+7*86400, dayStart+9*86400-1), true)
	flags, _ := body["flags"].([]any)
	if status != http.StatusOK || len(flags) != 1 {
		t.Fatalf("flags %d %v", status, body)
	}
	if flag := flags[0].(map[string]any); flag["rule"] != "net_mint_p95" || flag["value"] != "5000" || flag["at"] != float64(dayStart+8*86400) {
		t.Fatalf("flag %v", flag)
	}
}

/*
================
TestHandlerGuardAndContractErrors

Only an authorised local GET is answered; before the first snapshot meta
says so and the aggregates answer no_snapshot; bad parameters answer
their contract codes; without a token file every route is 404.
================
*/
func TestHandlerGuardAndContractErrors(t *testing.T) {
	source := writeSnapshotSource(t, nil, nil, nil)
	service, handler := newTestService(t, source, time.Unix(dayStart, 0))
	if status, body := get(t, handler, "/economy/v1/meta", false); status != http.StatusForbidden || errorCode(body) != "unauthorized" {
		t.Fatalf("unauthorised meta %d %v", status, body)
	}
	if status, body := get(t, handler, "/economy/v1/meta", true); status != http.StatusOK || body["snapshotAt"] != nil || body["contract"] != float64(1) {
		t.Fatalf("meta before a snapshot %d %v", status, body)
	}
	if status, body := get(t, handler, "/economy/v1/series?metric=gold_net", true); status != http.StatusServiceUnavailable || errorCode(body) != "no_snapshot" {
		t.Fatalf("series before a snapshot %d %v", status, body)
	}
	if err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	for target, want := range map[string]string{
		"/economy/v1/series?metric=gold_net&bucket=1w":                                     "bad_bucket",
		"/economy/v1/series?metric=gold_gross":                                             "bad_metric",
		fmt.Sprintf("/economy/v1/series?metric=gold_net&bucket=1h&from=0&to=%d", dayStart): "range_too_wide",
	} {
		if status, body := get(t, handler, target, true); status != http.StatusBadRequest || errorCode(body) != want {
			t.Fatalf("%s: %d %v, want %s", target, status, body, want)
		}
	}
	post := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8791/economy/v1/meta", strings.NewReader("{}"))
	post.RemoteAddr = "127.0.0.1:50000"
	post.Header.Set("Authorization", "Bearer "+testToken)
	post.Header.Set("X-SRO-Local-Diagnostics", "1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, post)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST answered %d", w.Code)
	}
	closed, err := service.Handler(filepath.Join(t.TempDir(), "absent-token"))
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := get(t, closed, "/economy/v1/meta", true); status != http.StatusNotFound {
		t.Fatalf("without a token file meta answered %d", status)
	}
}

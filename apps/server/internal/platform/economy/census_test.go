/*
===========================================================================

census_test.go - the staff seal census (SRO_SEAL_CENSUS, #1691)

The census reads one seeded snapshot copy; every number below is
known. A (degree, tier) bucket with fewer than MinSealCensusHolders
distinct holders is withheld whole; the route is 404 while the flag
is off, and a second run while one is in flight is refused.
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
	"sync"
	"testing"
	"time"
)

/*
================
writeCensusSource

A snapshot database with the four tables the census reads, seeded so
every count in TestSealCensusCountsEveryContainer is known by hand.
================
*/
func writeCensusSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE characters (division TEXT NOT NULL, id INTEGER NOT NULL, name_lower TEXT NOT NULL, record TEXT NOT NULL);
CREATE TABLE account_storage (account_id TEXT PRIMARY KEY, record TEXT NOT NULL);
CREATE TABLE guilds (division TEXT NOT NULL, guild_id INTEGER NOT NULL, record TEXT NOT NULL);
CREATE TABLE ground_items (division TEXT NOT NULL, gid INTEGER NOT NULL, record TEXT NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	characters := []string{
		// Sun and Moon in the bag, a Star in the wolf's bag.
		`{"accountId":"ac1","missionInventory":[
			{"codename":"ITEM_CH_SWORD_09_C_RARE"},{"codename":"ITEM_CH_SWORD_09_B_RARE"}],
		 "avatarInventory":{"capacity":4,"rows":[{"codename":"ITEM_AVATAR_W"}]},
		 "activeCos":{"codename":"COS_P_WOLF","container":{"capacity":5,"rows":[
			{"codename":"ITEM_CH_SWORD_09_A_RARE"}]}}}`,
		// Sun and Moon in the bag, a Star in the avatar, a Star in the
		// transport's bag.
		`{"accountId":"ac2","missionInventory":[
			{"codename":"ITEM_CH_SWORD_09_C_RARE"},{"codename":"ITEM_CH_SWORD_09_B_RARE"}],
		 "avatarInventory":{"capacity":4,"rows":[{"codename":"ITEM_CH_SWORD_09_A_RARE"}]},
		 "mercenaries":[{"codename":"COS_T_CAMEL","container":{"capacity":14,"rows":[
			{"codename":"ITEM_CH_SWORD_09_A_RARE"}]}}]}`,
		// Sun and Star in the bag.
		`{"accountId":"ac3","missionInventory":[
			{"codename":"ITEM_CH_SWORD_09_C_RARE"},{"codename":"ITEM_CH_SWORD_09_A_RARE"}]}`,
		// One Moon; a summon item whose pet bag holds a degree-8 Star.
		`{"accountId":"ac4","missionInventory":[
			{"codename":"ITEM_CH_SWORD_09_B_RARE"},
			{"codename":"ITEM_COS_P_WOLF_SUMMON","summon":{"codename":"COS_P_WOLF",
			 "container":{"capacity":5,"rows":[{"codename":"ITEM_CH_SWORD_08_A_RARE"}]}}}]}`,
	}
	for i, record := range characters {
		if _, err := db.Exec("INSERT INTO characters (division, id, name_lower, record) VALUES ('d1', ?, ?, ?)",
			i+1, fmt.Sprint("c", i), record); err != nil {
			t.Fatal(err)
		}
	}
	warehouses := [][2]string{
		{"ac1", `{"rows":[{"codename":"ITEM_CH_SWORD_09_C_RARE"}]}`},
		{"ac2", `{"rows":[{"codename":"ITEM_CH_SWORD_09_A_RARE"}]}`},
	}
	for _, w := range warehouses {
		if _, err := db.Exec("INSERT INTO account_storage (account_id, record) VALUES (?, ?)", w[0], w[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO guilds (division, guild_id, record) VALUES ('d1', 7, ?)",
		`{"storage":{"rows":[{"codename":"ITEM_CH_SWORD_09_C_RARE"}]}}`); err != nil {
		t.Fatal(err)
	}
	ground := []string{
		`{"codename":"ITEM_CH_SWORD_09_A_RARE"}`,
		`{"codename":"ITEM_COS_T_CAMEL_SUMMON","summon":{"codename":"COS_T_CAMEL",
		 "container":{"capacity":14,"rows":[{"codename":"ITEM_CH_SWORD_09_C_RARE"}]}}}`,
	}
	for i, record := range ground {
		if _, err := db.Exec("INSERT INTO ground_items (division, gid, record) VALUES ('d1', ?, ?)", i+1, record); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

/*
================
TestSealCensusCountsEveryContainer

Degree 9: Star 6 items over 4 holders (bag, avatar, pet bag, transport
bag, warehouse, ground), Moon 3 over exactly the floor's 3 holders,
Sun 6 over 4 holders (the guild and ground-transport Suns count toward
what exists, never toward a holder). Degree-8 Star has one holder and
is withheld whole.
================
*/
func TestSealCensusCountsEveryContainer(t *testing.T) {
	service, _ := censusService(t, fileSnapshotter{source: writeCensusSource(t)})
	report, err := service.RunSealCensus()
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalItems != 16 || report.CharactersHolding != 4 || report.AccountsHolding != 2 {
		t.Fatalf("totals = %+v, want 16 items, 4 characters, 2 accounts", report)
	}
	if report.Locations != (SealCensusLocations{Inventory: 7, Avatar: 1, Pet: 2, Transport: 2, Storage: 2, GuildStorage: 1, Ground: 1}) {
		t.Fatalf("locations = %+v", report.Locations)
	}
	if report.SuppressedCells != 1 {
		t.Fatalf("suppressed = %d, want 1 (the one-holder degree-8 Star)", report.SuppressedCells)
	}
	if len(report.Rows) != 1 || report.Rows[0].Degree != 9 {
		t.Fatalf("rows = %+v, want the degree-9 row only", report.Rows)
	}
	row := report.Rows[0]
	for name, cell := range map[string]*SealCensusCell{"star": row.Star, "moon": row.Moon, "sun": row.Sun} {
		if cell == nil {
			t.Fatalf("%s withheld, want served", name)
		}
	}
	if *row.Star != (SealCensusCell{Items: 6, Holders: 4}) || *row.Moon != (SealCensusCell{Items: 3, Holders: 3}) ||
		*row.Sun != (SealCensusCell{Items: 6, Holders: 4}) {
		t.Fatalf("row = %+v", row)
	}
	// The copy is discarded; the report is the newest answer.
	if _, err := os.Stat(filepath.Join(service.dir, censusFile)); !os.IsNotExist(err) {
		t.Fatalf("census copy left behind: %v", err)
	}
	if service.censusLatest != report {
		t.Fatal("the run did not become the newest census")
	}
}

/*
================
censusService
================
*/
func censusService(t *testing.T, snapshotter Snapshotter) (*Service, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	service, err := Open(dir, snapshotter, time.Hour, func() time.Time { return time.Unix(dayStart, 0) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	tokenPath := filepath.Join(dir, TokenFile)
	if err := os.WriteFile(tokenPath, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := service.Handler(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	return service, handler
}

/*
================
callCensus

One authorised (or not) census request; returns status and body.
================
*/
func callCensus(t *testing.T, handler http.Handler, method, target string, auth bool) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, "http://127.0.0.1:8791"+target, nil)
	request.RemoteAddr = "127.0.0.1:50000"
	if auth {
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := map[string]any{}
	if recorder.Body.Len() > 0 {
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	}
	return recorder.Code, body
}

/*
================
TestSealCensusRouteOffAndOn

Off (the default) the route refuses outright; on, GET answers
no_census until a POST runs one, and the operator token still guards.
================
*/
func TestSealCensusRouteOffAndOn(t *testing.T) {
	service, handler := censusService(t, fileSnapshotter{source: writeCensusSource(t)})
	if status, _ := callCensus(t, handler, http.MethodGet, "/economy/v1/sealcensus", true); status != http.StatusNotFound {
		t.Fatalf("off GET: status %d, want 404", status)
	}
	if status, _ := callCensus(t, handler, http.MethodPost, "/economy/v1/sealcensus", true); status != http.StatusMethodNotAllowed {
		t.Fatalf("off POST: status %d, want 405", status)
	}
	service.SetSealCensusEnabled(true)
	if status, _ := callCensus(t, handler, http.MethodPost, "/economy/v1/sealcensus", false); status != http.StatusForbidden {
		t.Fatalf("no token: status %d, want 403", status)
	}
	if status, body := callCensus(t, handler, http.MethodGet, "/economy/v1/sealcensus", true); status != http.StatusServiceUnavailable ||
		errorCode(body) != "no_census" {
		t.Fatalf("before a run: status %d, body %v", status, body)
	}
	status, body := callCensus(t, handler, http.MethodPost, "/economy/v1/sealcensus", true)
	if status != http.StatusOK {
		t.Fatalf("run: status %d, body %v", status, body)
	}
	if body["totalItems"] != float64(16) || body["minHolders"] != float64(MinSealCensusHolders) {
		t.Fatalf("report = %v", body)
	}
	if status, body := callCensus(t, handler, http.MethodGet, "/economy/v1/sealcensus", true); status != http.StatusOK ||
		body["totalItems"] != float64(16) {
		t.Fatalf("after a run: status %d, body %v", status, body)
	}
}

// gatedSnapshotter holds its one copy until released, so a second run
// overlaps the first.
type gatedSnapshotter struct {
	source  string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

/*
================
gatedSnapshotter.SnapshotTo
================
*/
func (g *gatedSnapshotter) SnapshotTo(path string) error {
	g.once.Do(func() { close(g.started) })
	<-g.release
	bytes, err := os.ReadFile(g.source)
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o600)
}

/*
================
TestSealCensusRefusesASecondRun
================
*/
func TestSealCensusRefusesASecondRun(t *testing.T) {
	gated := &gatedSnapshotter{source: writeCensusSource(t), started: make(chan struct{}), release: make(chan struct{})}
	service, handler := censusService(t, gated)
	service.SetSealCensusEnabled(true)
	done := make(chan int, 1)
	go func() {
		status, _ := callCensus(t, handler, http.MethodPost, "/economy/v1/sealcensus", true)
		done <- status
	}()
	<-gated.started
	status, body := callCensus(t, handler, http.MethodPost, "/economy/v1/sealcensus", true)
	if status != http.StatusConflict || errorCode(body) != "census_running" {
		t.Fatalf("second run: status %d, body %v, want 409 census_running", status, body)
	}
	close(gated.release)
	if status := <-done; status != http.StatusOK {
		t.Fatalf("first run: status %d, want 200", status)
	}
}

/*
================
TestSealCensusFromEnv

Explicit on values only; a typo keeps the census off.
================
*/
func TestSealCensusFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"on": true, "1": true, "true": true, "ON": true, "off": false, "": false, "enabled": false} {
		t.Setenv(EnvSealCensus, value)
		if got := SealCensusFromEnv(); got != want {
			t.Fatalf("SealCensusFromEnv(%q) = %v, want %v", value, got, want)
		}
	}
}

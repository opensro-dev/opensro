/*
===========================================================================

killstats_test.go - the served kill and EXP rates (SRO_KILL_STATS)

Rows are name-free (day, band) counts. A band with fewer than
MinKillStatCharacters distinct characters is withheld whole; the
route is 404 until the shard counts, and the operator token still
guards every answer.

===========================================================================
*/
package economy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func killStatsService(t *testing.T) (*Service, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	service, err := Open(dir, fileSnapshotter{}, time.Hour, func() time.Time { return time.Unix(dayStart, 0) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
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

func getKillStats(t *testing.T, handler http.Handler, target string, auth bool) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8791"+target, nil)
	request.RemoteAddr = "127.0.0.1:50000"
	if auth {
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := map[string]any{}
	if recorder.Body.Len() > 0 && strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body is not JSON: %v", target, err)
		}
	}
	return recorder.Code, body
}

/*
================
TestKillStatsRouteIs404WhileOffAndAnswersWhenOn

Off (the default) the route refuses outright; on, flushed rows serve
with their rates, flushes accumulate the additive columns, and the
character count is the day's distinct count, not a sum of flushes.
================
*/
func TestKillStatsRouteIs404WhileOffAndAnswersWhenOn(t *testing.T) {
	service, handler := killStatsService(t)
	target := fmt.Sprintf("/economy/v1/killstats?from=%d&to=%d", dayStart, dayStart+3600)
	if status, _ := getKillStats(t, handler, target, true); status != http.StatusNotFound {
		t.Fatalf("off: status %d, want 404", status)
	}
	service.SetKillStatsEnabled(true)
	if status, _ := getKillStats(t, handler, target, false); status != http.StatusForbidden {
		t.Fatalf("no token: status %d, want 403", status)
	}
	row := KillStatRow{DayS: dayStart, Band: 8, Kills: 50, Exp: 9000, SkillExp: 3000, OnlineMs: 2 * 3600000, Characters: 4}
	if err := service.RecordKillStats([]KillStatRow{row}); err != nil {
		t.Fatal(err)
	}
	row.Kills, row.Exp, row.SkillExp, row.OnlineMs = 10, 1000, 500, 3600000
	if err := service.RecordKillStats([]KillStatRow{row}); err != nil {
		t.Fatal(err)
	}
	status, body := getKillStats(t, handler, target, true)
	if status != http.StatusOK {
		t.Fatalf("on: status %d, body %v", status, body)
	}
	if body["minCharacters"] != float64(MinKillStatCharacters) || body["suppressed"] != float64(0) {
		t.Fatalf("meta = %v", body)
	}
	days := body["days"].([]any)
	if len(days) != 1 {
		t.Fatalf("days = %v, want one", days)
	}
	bands := days[0].(map[string]any)["bands"].([]any)
	if len(bands) != 1 {
		t.Fatalf("bands = %v, want one", bands)
	}
	band := bands[0].(map[string]any)
	if band["label"] != "81-90" || band["kills"] != float64(60) || band["exp"] != float64(10000) ||
		band["skillExp"] != float64(3500) || band["characters"] != float64(4) {
		t.Fatalf("band = %v", band)
	}
	if band["onlineHours"] != float64(3) || band["killsPerOnlineHour"] != float64(20) {
		t.Fatalf("rates = %v, want 3 online hours and 20 kills/h", band)
	}
}

/*
================
TestKillStatsSuppressesBandsUnderThePrivacyFloor

A two-character band could name a player's hours: it is withheld
whole and only counted in "suppressed".
================
*/
func TestKillStatsSuppressesBandsUnderThePrivacyFloor(t *testing.T) {
	service, handler := killStatsService(t)
	service.SetKillStatsEnabled(true)
	rows := []KillStatRow{
		{DayS: dayStart, Band: 9, Kills: 5, Exp: 500, OnlineMs: 3600000, Characters: 2},
		{DayS: dayStart, Band: 8, Kills: 60, Exp: 6000, OnlineMs: 3600000, Characters: 3},
	}
	if err := service.RecordKillStats(rows); err != nil {
		t.Fatal(err)
	}
	status, body := getKillStats(t, handler, fmt.Sprintf("/economy/v1/killstats?from=%d&to=%d", dayStart, dayStart+3600), true)
	if status != http.StatusOK {
		t.Fatalf("status %d, body %v", status, body)
	}
	if body["suppressed"] != float64(1) {
		t.Fatalf("suppressed = %v, want 1", body["suppressed"])
	}
	days := body["days"].([]any)
	bands := days[0].(map[string]any)["bands"].([]any)
	if len(bands) != 1 || bands[0].(map[string]any)["band"] != float64(8) {
		t.Fatalf("bands = %v, want only the three-character band", bands)
	}
}

/*
================
TestKillStatsRefusesBadRanges
================
*/
func TestKillStatsRefusesBadRanges(t *testing.T) {
	service, handler := killStatsService(t)
	service.SetKillStatsEnabled(true)
	for target, code := range map[string]string{
		"/economy/v1/killstats?from=x": "bad_bucket",
		"/economy/v1/killstats?to=-1":  "bad_bucket",
		fmt.Sprintf("/economy/v1/killstats?from=%d&to=%d", dayStart+86400, dayStart):     "bad_bucket",
		fmt.Sprintf("/economy/v1/killstats?from=%d&to=%d", dayStart, dayStart+400*86400): "range_too_wide",
	} {
		status, body := getKillStats(t, handler, target, true)
		if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != code {
			t.Fatalf("%s: status %d body %v, want 400 %s", target, status, body, code)
		}
	}
}

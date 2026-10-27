/*
===========================================================================

sealpitystats_test.go - the served seal pity rollup (SRO_SEAL_PITY, #1690)

Rows are (day, degree, tier) aggregates and carry no character
dimension, so nothing is withheld: the calibration column is natural
drops over debt accrued. The route is 404 until the shard counts, and
the operator token still guards every answer.

===========================================================================
*/
package economy

import (
	"fmt"
	"net/http"
	"testing"
)

/*
================
TestSealPityRouteIs404WhileOffAndAnswersWhenOn
================
*/
func TestSealPityRouteIs404WhileOffAndAnswersWhenOn(t *testing.T) {
	service, handler := killStatsService(t)
	target := fmt.Sprintf("/economy/v1/sealpity?from=%d&to=%d", dayStart, dayStart+3600)
	if status, _ := getKillStats(t, handler, target, true); status != http.StatusNotFound {
		t.Fatalf("off: status %d, want 404", status)
	}
	service.SetSealPityStatsEnabled(true)
	if status, _ := getKillStats(t, handler, target, false); status != http.StatusForbidden {
		t.Fatalf("no token: status %d, want 403", status)
	}
	rows := []SealPityStatRow{
		{DayS: dayStart, Degree: 9, Tier: 1, DebtAccrued: 10, NaturalDrops: 4, ForcedDrops: 1, DailyCapHits: 2, OverThreshold: 0},
		{DayS: dayStart, Degree: 9, Tier: 2, DebtAccrued: 5, NaturalDrops: 6, OverThreshold: 1},
	}
	for range 2 {
		if err := service.RecordSealPityStats(rows); err != nil {
			t.Fatal(err)
		}
	}
	status, body := getKillStats(t, handler, target, true)
	if status != http.StatusOK {
		t.Fatalf("on: status %d", status)
	}
	days, ok := body["days"].([]any)
	if !ok || len(days) != 1 {
		t.Fatalf("days %v", body["days"])
	}
	got := days[0].(map[string]any)["rows"].([]any)
	if len(got) != 2 {
		t.Fatalf("rows %v", got)
	}
	moon := got[0].(map[string]any)
	if moon["degree"] != float64(9) || moon["tier"] != float64(1) || moon["debtAccrued"] != float64(20) ||
		moon["naturalDrops"] != float64(8) || moon["forcedDrops"] != float64(2) ||
		moon["dailyCapHits"] != float64(4) || moon["overThreshold"] != float64(0) || moon["calibration"] != 0.4 {
		t.Fatalf("moon row %+v", moon)
	}
	sun := got[1].(map[string]any)
	if sun["tier"] != float64(2) || sun["overThreshold"] != float64(2) || sun["calibration"] != 1.2 {
		t.Fatalf("sun row %+v", sun)
	}
}

/*
===========================================================================

metrics_tick_test.go - the tick-only metrics view over HTTP

The view exists so an edge login can reach tick numbers without the rest
of /transport/metrics. The test that matters is the key set: a field
added to the view by accident (or a field of Metrics leaking through an
embed) is the defect, so the document's top-level keys are compared
with the exact allowed list.

===========================================================================
*/

package transport_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

// tickViewKeys is every top-level key the tick view carries in this
// test. Of the omitempty keys, monster_navigation and tick_zones appear
// because the test installs their providers; tick_spikes stays absent
// because the spike journal is not enabled here.
var tickViewKeys = []string{
	"captured_at",
	"live_sessions",
	"monster_navigation",
	"publication_holds",
	"runtime",
	"slow_divisions",
	"slow_hooks",
	"tick_commit",
	"tick_count",
	"tick_last_ms",
	"tick_max_ms",
	"tick_mean_ms",
	"tick_ms_histogram",
	"tick_overruns",
	"tick_phase_ms",
	"tick_zones",
}

/*
================
TestTransportMetricsTickEndpoint
================
*/
func TestTransportMetricsTickEndpoint(t *testing.T) {
	srv := startServer(t)
	srv.Hub.SetNavigationMetrics(func() map[string]uint64 { return map[string]uint64{"plan.ai.ok": 3} })
	srv.Hub.SetTickZoneMetrics(func() map[string]transport.TickZone {
		return map[string]transport.TickZone{"divisions": {Count: 2, TotalMs: 84}}
	})
	srv.Hub.SetEconomyMetrics(func() map[string]uint64 { return map[string]uint64{"gold.shop.gain": 9} })
	srv.Hub.RecordTickDuration(40*time.Millisecond, 100*time.Millisecond)
	srv.Hub.RecordTickDuration(44*time.Millisecond, 100*time.Millisecond)

	client := http.Client{Timeout: smokeIOTimeout}
	url := fmt.Sprintf("http://%s%s", srv.WSAddr(), transport.PathMetricsTick)
	before := time.Now().UTC()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, tickViewKeys) {
		t.Fatalf("tick view keys =\n  %s\nwant\n  %s", strings.Join(keys, " "), strings.Join(tickViewKeys, " "))
	}

	var doc transport.TickMetrics
	body, _ := json.Marshal(raw)
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.TickCount != 2 || doc.TickMeanMs != 42 || doc.TickMaxMs != 44 {
		t.Fatalf("tick count/mean/max = %d/%v/%v, want 2/42/44", doc.TickCount, doc.TickMeanMs, doc.TickMaxMs)
	}
	if zone := doc.TickZones["divisions"]; zone.Count != 2 || zone.TotalMs != 84 {
		t.Fatalf("tick_zones.divisions = %+v, want count 2 total 84", zone)
	}
	if doc.MonsterNavigation["plan.ai.ok"] != 3 {
		t.Fatalf("monster_navigation = %v, want plan.ai.ok 3", doc.MonsterNavigation)
	}
	captured, err := time.Parse(time.RFC3339Nano, doc.CapturedAt)
	if err != nil || captured.Before(before.Add(-time.Second)) {
		t.Fatalf("captured_at = %q (%v), want a time at or after %v", doc.CapturedAt, err, before)
	}
}

/*
================
TestTransportMetricsTickRefusesWrites
================
*/
func TestTransportMetricsTickRefusesWrites(t *testing.T) {
	srv := startServer(t)
	client := http.Client{Timeout: smokeIOTimeout}
	url := fmt.Sprintf("http://%s%s", srv.WSAddr(), transport.PathMetricsTick)
	resp, err := client.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", resp.StatusCode)
	}
}

/*
===========================================================================

metrics_navigation_test.go - the monster_navigation block on /transport/metrics

===========================================================================
*/
package transport

import (
	"encoding/json"
	"strings"
	"testing"
)

/*
================
TestNavigationMetricsBlockFollowsItsProvider
================
*/
func TestNavigationMetricsBlockFollowsItsProvider(t *testing.T) {
	hub := newHub(testCfg())
	plain, err := json.Marshal(hub.Metrics())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "monster_navigation") {
		t.Fatal("the block appeared without a provider")
	}
	hub.SetNavigationMetrics(func() map[string]uint64 { return map[string]uint64{"plan.chase.ready_direct": 3} })
	var decoded struct {
		Navigation map[string]uint64 `json:"monster_navigation"`
	}
	raw, err := json.Marshal(hub.Metrics())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Navigation["plan.chase.ready_direct"] != 3 {
		t.Fatalf("monster_navigation = %v, want the provider's counters", decoded.Navigation)
	}
}

/*
================
TestMonsterLegMetricsBlockFollowsItsProvider

The monster_leg census (#3119) publishes only when its provider is
installed, exactly as its own block beside monster_navigation.
================
*/
func TestMonsterLegMetricsBlockFollowsItsProvider(t *testing.T) {
	hub := newHub(testCfg())
	plain, err := json.Marshal(hub.Metrics())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "monster_leg") {
		t.Fatal("the block appeared without a provider")
	}
	hub.SetMonsterLegMetrics(func() map[string]uint64 { return map[string]uint64{"steps": 7, "mode.idle": 5} })
	var decoded struct {
		Leg map[string]uint64 `json:"monster_leg"`
	}
	raw, err := json.Marshal(hub.Metrics())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Leg["steps"] != 7 || decoded.Leg["mode.idle"] != 5 {
		t.Fatalf("monster_leg = %v, want the provider's counters", decoded.Leg)
	}
}

/*
===========================================================================

uituning_test.go - the client tuning reaches the bootstrap only when tuned

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"strings"
	"testing"

	"opensro.online/server/internal/game/tunables"
)

/*
================
TestUiTuningIsSentOnlyWhenTuned

At every default the bootstrap carries no uiTuning (the blob an older
build sent); a changed value sends all three, with the snapshot's values.
================
*/
func TestUiTuningIsSentOnlyWhenTuned(t *testing.T) {
	defaults, err := tunables.Parse(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := UiTuningFrom(defaults); got != nil {
		t.Fatalf("defaults sent %+v", got)
	}
	tuned, err := tunables.Parse(func(name string) string {
		if name == "SRO_SHORT_NOTICE_SHOW_MS" {
			return "4000"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	got := UiTuningFrom(tuned)
	if got == nil || *got != (UiTuning{ShortNoticeShowMs: 4000, ShortNoticeFadeMs: 1000, ChatAckTimeoutMs: 10000}) {
		t.Fatalf("tuned sent %+v", got)
	}
	for _, c := range []struct {
		tuning *UiTuning
		want   string
	}{
		{nil, ""},
		{got, `"uiTuning":{"shortNoticeShowMs":4000,"shortNoticeFadeMs":1000,"chatAckTimeoutMs":10000}`},
	} {
		blob, err := json.Marshal(&BootstrapResult{
			NativeResult: nativeResultSuccess, Character: &Character{Name: "keeper", AccountID: "a"},
			LocalPlayerEntry: &LocalPlayerEntry{RaceKey: RaceKeyChina},
			ChatMessages:     []string{}, SystemMessages: []string{}, Packets: []Packet{},
			UiTuning: c.tuning,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if c.want == "" && strings.Contains(string(blob), "uiTuning") {
			t.Fatal("an untuned shard named uiTuning in the blob")
		}
		if c.want != "" && !strings.Contains(string(blob), c.want) {
			t.Fatalf("blob lacks %s: %s", c.want, blob)
		}
	}
}

/*
================
TestUiTuningBoundsMatchTheClient

The registry's bounds are the client's (ui-tuning.ts BOUNDS): the server
must never send a value the client would replace with its default.
================
*/
func TestUiTuningBoundsMatchTheClient(t *testing.T) {
	want := map[string][2]float64{
		tunables.ShortNoticeShowMs: {3000, 7000},
		tunables.ShortNoticeFadeMs: {500, 2000},
		tunables.ChatAckTimeoutMs:  {5000, 30000},
	}
	for key, bounds := range want {
		s, ok := tunables.Lookup(key)
		if !ok || s.Min != bounds[0] || s.Max != bounds[1] || s.Class != tunables.Entry || s.Kind != tunables.Int {
			t.Fatalf("%s: %+v, want %v, Entry, Int", key, s, bounds)
		}
	}
}

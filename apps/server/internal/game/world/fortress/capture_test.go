package fortress

import "testing"

/*
================
TestTheStoneFallsOnlyAfterItsGuardsAndTheCountdown

The fort stone refuses while towers stand (0x3040) and for three minutes
after the last falls (0x3046); its capture hands the fortress to the
breaker until the war ends, with the gates shut to attackers for five
minutes, and the war's end makes the holder the occupier.
================
*/
func TestTheStoneFallsOnlyAfterItsGuardsAndTheCountdown(t *testing.T) {
	a := New([]Catalog{{ID: 1, CodeName: "FORTRESS_JANGAN"}})
	if record, _ := a.Get("a", 1); !record.EntryOpen {
		t.Fatal("a fresh fortress world shuts its gates (600C60 sets +0x84)")
	}
	a.BeginWar("a", 1, true)
	if code := a.StoneRefusal("a", 1, 0); code != StoneRefusedTowersStand {
		t.Fatalf("stone with towers standing = %#x", code)
	}
	if !a.TowersFallen("a", 1, 1000) || a.TowersFallen("a", 1, 1000) {
		t.Fatal("the countdown must start once")
	}
	if code := a.StoneRefusal("a", 1, 1000+CountdownMs-1); code != StoneRefusedCountdown {
		t.Fatalf("stone during the countdown = %#x", code)
	}
	if code := a.StoneRefusal("a", 1, 1000+CountdownMs); code != 0 {
		t.Fatalf("stone after the countdown = %#x", code)
	}
	if !a.Capture("a", 1, 7, 5000) {
		t.Fatal("capture refused")
	}
	record, _ := a.Get("a", 1)
	if record.Holder() != 7 || record.GuildID != 0 || record.EntryOpen {
		t.Fatalf("after the capture %+v", record)
	}
	a.Advance(5000 + CaptureWaitMs - 1)
	if record, _ = a.Get("a", 1); record.EntryOpen {
		t.Fatal("the gates reopened early")
	}
	a.Advance(5000 + CaptureWaitMs)
	if record, _ = a.Get("a", 1); !record.EntryOpen {
		t.Fatal("the gates stayed shut")
	}
	if owner, changed := a.FinishWar("a", 1); owner != 7 || !changed {
		t.Fatalf("war end = %d %v", owner, changed)
	}
	if record, _ = a.Get("a", 1); record.GuildID != 7 || record.TempGuildID != 0 || record.TowersStanding {
		t.Fatalf("after the war %+v", record)
	}
	a.BeginWar("a", 1, false)
	if code := a.StoneRefusal("a", 1, 0); code != 0 {
		t.Fatalf("an unguarded stone = %#x", code)
	}
}

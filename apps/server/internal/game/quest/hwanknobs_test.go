/*
===========================================================================

hwanknobs_test.go - the Blue Berserk operator knobs, unset and set

===========================================================================
*/
package quest

import "testing"

func envOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestHwanKnobsUnsetKeepTheOriginalRolls(t *testing.T) {
	knobs, err := HwanKnobsFromEnv(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	// Niya: below 2 normal, 2 to 29 flutter, 30 and up nothing, over 101.
	for roll, want := range map[uint32]NiyaOutcome{0: NiyaNormalSpirit, 1: NiyaNormalSpirit, 2: NiyaFlutterSpirit,
		29: NiyaFlutterSpirit, 30: NiyaNothing, 100: NiyaNothing, 101: NiyaNormalSpirit, 102: NiyaNormalSpirit, 103: NiyaFlutterSpirit} {
		if got := knobs.NiyaCapture(roll); got != want {
			t.Errorf("unset niya roll %d = %d, want %d", roll, got, want)
		}
	}
	// Relic: 0 to 10 wins, 11 loses, over 101.
	for roll, want := range map[uint32]bool{0: true, 10: true, 11: false, 100: false, 101: true, 112: false} {
		if got := knobs.SunRelicFound(roll); got != want {
			t.Errorf("unset relic roll %d = %v, want %v", roll, got, want)
		}
	}
	if knobs.OstraconDropCount() != 1 {
		t.Errorf("unset fast drop = %d, want 1", knobs.OstraconDropCount())
	}
}

func TestHwanKnobsAcceptWholePercentages(t *testing.T) {
	knobs, err := HwanKnobsFromEnv(envOf(map[string]string{
		EnvHwanNiyaCaptureRates: " 50, 30,20 ", EnvHwanSunRelic: " 25 ", EnvHwan3FastDrop: "On"}))
	if err != nil {
		t.Fatal(err)
	}
	for roll, want := range map[uint32]NiyaOutcome{0: NiyaNormalSpirit, 19: NiyaNormalSpirit, 20: NiyaFlutterSpirit,
		49: NiyaFlutterSpirit, 50: NiyaNothing, 99: NiyaNothing, 100: NiyaNormalSpirit} {
		if got := knobs.NiyaCapture(roll); got != want {
			t.Errorf("niya 50,30,20 roll %d = %d, want %d", roll, got, want)
		}
	}
	for roll, want := range map[uint32]bool{0: true, 24: true, 25: false, 99: false} {
		if got := knobs.SunRelicFound(roll); got != want {
			t.Errorf("relic 25 roll %d = %v, want %v", roll, got, want)
		}
	}
	if knobs.OstraconDropCount() != FastDropCount {
		t.Errorf("fast drop on = %d, want %d", knobs.OstraconDropCount(), FastDropCount)
	}
}

func TestHwanKnobsEdges(t *testing.T) {
	always, err := HwanKnobsFromEnv(envOf(map[string]string{EnvHwanNiyaCaptureRates: "0,0,100", EnvHwanSunRelic: "100"}))
	if err != nil {
		t.Fatal(err)
	}
	never, err := HwanKnobsFromEnv(envOf(map[string]string{EnvHwanNiyaCaptureRates: "100,0,0", EnvHwanSunRelic: "0"}))
	if err != nil {
		t.Fatal(err)
	}
	for roll := uint32(0); roll < 250; roll++ {
		if always.NiyaCapture(roll) != NiyaNormalSpirit || !always.SunRelicFound(roll) {
			t.Fatalf("0,0,100 / 100 must always win (roll %d)", roll)
		}
		if never.NiyaCapture(roll) != NiyaNothing || never.SunRelicFound(roll) {
			t.Fatalf("100,0,0 / 0 must never win (roll %d)", roll)
		}
	}
}

func TestHwanKnobsRefuseMalformedValues(t *testing.T) {
	for _, value := range []string{"25.6", "-1", "101", "abc", "11%", "+11", "0x10", "1e1", "1 1"} {
		if _, err := HwanKnobsFromEnv(envOf(map[string]string{EnvHwanSunRelic: value})); err == nil {
			t.Errorf("%s=%q must be refused", EnvHwanSunRelic, value)
		}
	}
	for _, value := range []string{"70,28", "70,28,2,0", "70,28,3", "70,28,-2", "70.0,28,2", "a,b,c", "70,,30", "101,0,0"} {
		if _, err := HwanKnobsFromEnv(envOf(map[string]string{EnvHwanNiyaCaptureRates: value})); err == nil {
			t.Errorf("%s=%q must be refused", EnvHwanNiyaCaptureRates, value)
		}
	}
	if _, err := HwanKnobsFromEnv(envOf(map[string]string{EnvHwan3FastDrop: "maybe"})); err == nil {
		t.Errorf("%s=maybe must be refused", EnvHwan3FastDrop)
	}
	// Empty and whitespace-only are unset, not an error.
	if _, err := HwanKnobsFromEnv(envOf(map[string]string{EnvHwanSunRelic: "  ", EnvHwanNiyaCaptureRates: "", EnvHwan3FastDrop: " "})); err != nil {
		t.Errorf("empty knobs must be unset: %v", err)
	}
}

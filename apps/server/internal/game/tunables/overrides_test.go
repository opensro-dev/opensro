/*
===========================================================================

overrides_test.go - validation, stored overrides and the live swap

===========================================================================
*/
package tunables

import (
	"strings"
	"sync"
	"testing"
)

/*
================
TestValidateUsesTheBootRules
================
*/
func TestValidateUsesTheBootRules(t *testing.T) {
	if v, err := Validate(JobKillNoticesPerMinute, "12"); err != nil || v.Number != 12 {
		t.Fatalf("12: %v %v", v, err)
	}
	for key, raw := range map[string]string{JobKillNoticesPerMinute: "31", MostWantedTestKills: "3", "no_such_key": "1"} {
		if _, err := Validate(key, raw); err == nil {
			t.Fatalf("%s=%q accepted", key, raw)
		}
	}
}

/*
================
TestStoredOverridesLayerOverTheEnvironment

An override replaces the environment's value and says it came from the
store; a refused or unknown stored value is reported, never dropped.
================
*/
func TestStoredOverridesLayerOverTheEnvironment(t *testing.T) {
	getenv := env(map[string]string{"SRO_JOB_KILL_NOTICES_PER_MINUTE": "9"})
	s, err := WithOverrides(getenv, map[string]string{JobKillNoticesPerMinute: "4", MallRideSpeed: "1.5"})
	if err != nil {
		t.Fatal(err)
	}
	if v := s.Value(JobKillNoticesPerMinute); v.Number != 4 || v.Source != "store" {
		t.Fatalf("override %+v", v)
	}
	if factor, on := s.Factor(MallRideSpeed); factor != 1.5 || !on {
		t.Fatalf("factor override %v %v", factor, on)
	}
	if s.Value(JobKillRepeatMinutes).Source != "default" {
		t.Fatal("an untouched key changed source")
	}
	_, err = WithOverrides(getenv, map[string]string{JobKillNoticesPerMinute: "99", "gone": "1"})
	if err == nil || !strings.Contains(err.Error(), "SRO_JOB_KILL_NOTICES_PER_MINUTE") || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("refused stored values not reported: %v", err)
	}
}

/*
================
TestApplySwapsLiveKeysOnly

A live key changes the next snapshot at once, while a snapshot taken
before keeps its value; a restart key is accepted but not swapped; a
refused value changes nothing.
================
*/
func TestApplySwapsLiveKeysOnly(t *testing.T) {
	base, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Swap(base))
	before := Current()
	if applied, err := Apply(JobKillNoticesPerMinute, "3"); err != nil || !applied {
		t.Fatalf("live apply: %v %v", applied, err)
	}
	if Current().Int(JobKillNoticesPerMinute) != 3 || before.Int(JobKillNoticesPerMinute) != 6 {
		t.Fatal("the live change did not take, or changed an action's snapshot")
	}
	if applied, err := Apply(MostWantedTestKills, "2"); err != nil || applied || Current().Int(MostWantedTestKills) != 5 {
		t.Fatalf("restart key: applied %v err %v value %d", applied, err, Current().Int(MostWantedTestKills))
	}
	snapshot := Current()
	if _, err := Apply(JobKillNoticesPerMinute, "0"); err == nil || Current() != snapshot {
		t.Fatal("a refused value changed the snapshot")
	}
}

/*
================
TestConcurrentAppliesNeverLoseAWrite

Two writers on different keys: both values survive, since each builds on
the other's result under the lock rather than on a stale base.
================
*/
func TestConcurrentAppliesNeverLoseAWrite(t *testing.T) {
	base, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Swap(base))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = Apply(JobKillNoticesPerMinute, "7") }()
		go func() { defer wg.Done(); _, _ = Apply(JobKillRepeatMinutes, "20") }()
	}
	wg.Wait()
	if Current().Int(JobKillNoticesPerMinute) != 7 || Current().Int(JobKillRepeatMinutes) != 20 {
		t.Fatalf("a concurrent write was lost: %d %d", Current().Int(JobKillNoticesPerMinute), Current().Int(JobKillRepeatMinutes))
	}
}

/*
================
TestBootOverridesReachRestartValues

A restart-class value stored earlier is the one FromEnv gives the boot,
marked as from the store. A stored value the registry now refuses (an
unknown key, a value past a narrowed bound) is left out and named, and
its key boots on the environment; the boot does not fail.
================
*/
func TestBootOverridesReachRestartValues(t *testing.T) {
	t.Cleanup(func() { _, _ = InstallBoot(nil) })
	t.Setenv("SRO_MOST_WANTED_TEST_KILLS", "5")
	if rejected, err := InstallBoot(map[string]string{MostWantedTestKills: "2", JobKillNoticesPerMinute: "4"}); err != nil ||
		len(rejected) != 0 {
		t.Fatal(rejected, err)
	}
	if v, err := FromEnv(MostWantedTestKills); err != nil || v.Number != 2 || v.Source != "store" {
		t.Fatalf("boot read %+v %v, want the stored 2", v, err)
	}
	if Current().Int(JobKillNoticesPerMinute) != 4 {
		t.Fatal("the boot snapshot does not hold the stored live value")
	}
	t.Setenv("SRO_JOB_KILL_REPEAT_MINUTES", "15")
	rejected, err := InstallBoot(map[string]string{
		MostWantedTestKills:     "3",
		"a_removed_setting":     "1",
		JobKillRepeatMinutes:    "500",
		JobKillNoticesPerMinute: "4",
	})
	if err != nil || len(rejected) != 3 || rejected[MostWantedTestKills] == nil || rejected["a_removed_setting"] == nil ||
		rejected[JobKillRepeatMinutes] == nil {
		t.Fatalf("rejected %v, err %v", rejected, err)
	}
	if v, _ := FromEnv(MostWantedTestKills); v.Number != 5 || v.Source != "env" {
		t.Fatalf("a refused stored restart value was used: %+v", v)
	}
	if Current().Int(JobKillRepeatMinutes) != 15 || Current().Int(JobKillNoticesPerMinute) != 4 {
		t.Fatal("the refused key did not fall back to the environment, or a good one was dropped")
	}
	t.Setenv("SRO_JOB_KILL_REPEAT_MINUTES", "999")
	if _, err := InstallBoot(nil); err == nil {
		t.Fatal("a refused environment value booted")
	}
}

/*
================
TestClearAndEntryClass

Clearing a live override puts the environment's value back with its own
source, not "store"; an Entry-class key is swapped at approval like a
live one (its readers take Current() at each world entry).
================
*/
func TestClearAndEntryClass(t *testing.T) {
	base, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Swap(base))
	t.Setenv("SRO_JOB_KILL_NOTICES_PER_MINUTE", "8")
	if _, err := Apply(JobKillNoticesPerMinute, "3"); err != nil || Current().Value(JobKillNoticesPerMinute).Source != "store" {
		t.Fatalf("live apply: %v %+v", err, Current().Value(JobKillNoticesPerMinute))
	}
	if _, err := Apply(JobKillNoticesPerMinute, ClearOverride); err != nil {
		t.Fatal(err)
	}
	if v := Current().Value(JobKillNoticesPerMinute); v.Number != 8 || v.Source != "env" {
		t.Fatalf("cleared value %+v, want the environment's 8 from env", v)
	}
	Registry = append(Registry, Setting{Key: "test_entry", Env: "SRO_TEST_ENTRY", Kind: Int, Default: 1, Min: 1, Max: 9,
		Class: Entry})
	t.Cleanup(func() { Registry = Registry[:len(Registry)-1] })
	withEntry, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Swap(withEntry))
	if applied, err := Apply("test_entry", "4"); err != nil || !applied || Current().Int("test_entry") != 4 {
		t.Fatalf("entry apply: %v %v", applied, err)
	}
}

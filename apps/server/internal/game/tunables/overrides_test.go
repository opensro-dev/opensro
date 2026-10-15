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
marked as from the store; a stored value the registry refuses stops the
boot and installs nothing.
================
*/
func TestBootOverridesReachRestartValues(t *testing.T) {
	t.Cleanup(func() { _ = InstallBoot(nil) })
	t.Setenv("SRO_MOST_WANTED_TEST_KILLS", "5")
	if err := InstallBoot(map[string]string{MostWantedTestKills: "2", JobKillNoticesPerMinute: "4"}); err != nil {
		t.Fatal(err)
	}
	if v, err := FromEnv(MostWantedTestKills); err != nil || v.Number != 2 || v.Source != "store" {
		t.Fatalf("boot read %+v %v, want the stored 2", v, err)
	}
	if Current().Int(JobKillNoticesPerMinute) != 4 {
		t.Fatal("the boot snapshot does not hold the stored live value")
	}
	before := Current()
	if err := InstallBoot(map[string]string{MostWantedTestKills: "3"}); err == nil || Current() != before {
		t.Fatalf("a refused stored value was installed: %v", err)
	}
}

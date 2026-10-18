/*
===========================================================================

tunables_test.go - the registry's defaults, accepted values and refusals

===========================================================================
*/
package tunables

import (
	"strconv"
	"strings"
	"testing"
)

/*
================
env

A getenv over a fixed map.
================
*/
func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

/*
================
TestUnsetEnvironmentKeepsTodaysValues

With nothing set every tunable is its default, the value the code used
before it was tunable, and the mall speed switch is off (native).
================
*/
func TestUnsetEnvironmentKeepsTodaysValues(t *testing.T) {
	s, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int64{
		MostWantedTestKills: 5, MostWantedTestBounty: 1_000_000, JobKillNoticesPerMinute: 6,
		MostWantedNoticesPerMinute: 6, JobKillRepeatMinutes: 10, SoxNoticeMinDegree: 6,
	} {
		if got := s.Int(key); got != want || s.Value(key).Source != "default" {
			t.Fatalf("%s = %d (%s), want %d (default)", key, got, s.Value(key).Source, want)
		}
	}
	if factor, on := s.Factor(MallRideSpeed); on || factor != 1 {
		t.Fatalf("mall ride speed unset: %v %v, want off", factor, on)
	}
}

/*
================
TestAcceptedValues
================
*/
func TestAcceptedValues(t *testing.T) {
	for raw, want := range map[string]struct {
		factor float64
		on     bool
	}{"on": {1.3, true}, "TRUE": {1.3, true}, "1": {1.3, true}, "off": {1, false}, "0": {1, false}, "1.5": {1.5, true}, "3": {3, true}} {
		s, err := Parse(env(map[string]string{"SRO_MALL_RIDE_SPEED": raw}))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if factor, on := s.Factor(MallRideSpeed); factor != want.factor || on != want.on {
			t.Fatalf("%q: %v %v, want %v %v", raw, factor, on, want.factor, want.on)
		}
	}
	s, err := Parse(env(map[string]string{
		"SRO_MOST_WANTED_TEST_KILLS": "2", "SRO_MOST_WANTED_TEST_BOUNTY": "0", "SRO_JOB_KILL_NOTICES_PER_MINUTE": " 30 ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Int(MostWantedTestKills) != 2 || s.Int(MostWantedTestBounty) != 0 || s.Int(JobKillNoticesPerMinute) != 30 {
		t.Fatalf("values %+v", s.values)
	}
	if s.Value(MostWantedTestKills).Source != "env" {
		t.Fatal("a set value does not say it came from the environment")
	}
}

/*
================
TestRefusedValuesNameTheVariableAndTheRange

A value out of bounds, outside an allowed list, fractional for a whole
number or not a number is refused, never clamped, and every refusal of
one boot is reported together.
================
*/
func TestRefusedValuesNameTheVariableAndTheRange(t *testing.T) {
	for name, raw := range map[string]string{
		"SRO_MALL_RIDE_SPEED":             "5",
		"SRO_MOST_WANTED_TEST_KILLS":      "3",
		"SRO_MOST_WANTED_TEST_BOUNTY":     "100000001",
		"SRO_JOB_KILL_NOTICES_PER_MINUTE": "2.5",
		"SRO_JOB_KILL_REPEAT_MINUTES":     "ten",
	} {
		_, err := Parse(env(map[string]string{name: raw}))
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "want") {
			t.Fatalf("%s=%q: %v, want a refusal naming it", name, raw, err)
		}
	}
	_, err := Parse(env(map[string]string{"SRO_MOST_WANTED_TEST_KILLS": "3", "SRO_JOB_KILL_REPEAT_MINUTES": "0"}))
	if err == nil || !strings.Contains(err.Error(), "SRO_MOST_WANTED_TEST_KILLS") ||
		!strings.Contains(err.Error(), "SRO_JOB_KILL_REPEAT_MINUTES") {
		t.Fatalf("both refusals not reported: %v", err)
	}
	if _, err := Parse(env(map[string]string{"SRO_MOST_WANTED_TEST_KILLS": "3"})); !strings.Contains(err.Error(), "one of 1, 2, 5, 10") {
		t.Fatalf("allowed list not named: %v", err)
	}
}

/*
================
TestRegistryRowsAreSound

Every default is itself accepted, and keys and variables are unique.
================
*/
func TestRegistryRowsAreSound(t *testing.T) {
	keys, envs := map[string]bool{}, map[string]bool{}
	for _, s := range Registry {
		if keys[s.Key] || envs[s.Env] || !strings.HasPrefix(s.Env, "SRO_") {
			t.Fatalf("row %+v repeats a key or variable, or is not SRO_", s)
		}
		keys[s.Key], envs[s.Env] = true, true
		if _, err := parseOne(s, strconv.FormatFloat(s.Default, 'f', -1, 64)); err != nil {
			t.Fatalf("%s: its default is refused: %v", s.Env, err)
		}
	}
}

/*
================
TestSwapReplacesAndRestoresTheSnapshot

A snapshot taken before a swap keeps its values; Current after it sees
the new one; restore brings the old one back.
================
*/
func TestSwapReplacesAndRestoresTheSnapshot(t *testing.T) {
	before := Current()
	changed, err := Parse(env(map[string]string{"SRO_JOB_KILL_NOTICES_PER_MINUTE": "12"}))
	if err != nil {
		t.Fatal(err)
	}
	restore := Swap(changed)
	if Current().Int(JobKillNoticesPerMinute) != 12 || before.Int(JobKillNoticesPerMinute) == 12 {
		t.Fatal("the swap did not take, or changed an earlier snapshot")
	}
	restore()
	if Current() != before {
		t.Fatal("restore did not bring the earlier snapshot back")
	}
}

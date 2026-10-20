/*
===========================================================================

uniquerules_test.go - the default-on unique reward switches

Both switches ship ON (owner, 2026-10-20) and only an explicit off value
restores the native rule, one switch at a time.

===========================================================================
*/
package progression

import "testing"

/*
================
TestUniqueRulesFromEnvDefaultsOn
================
*/
func TestUniqueRulesFromEnvDefaultsOn(t *testing.T) {
	t.Setenv(EnvUniqueGapExempt, "")
	t.Setenv(EnvUniquePopTicket, "")
	if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket {
		t.Fatalf("unset switches = %+v, want both on", rules)
	}
	for _, value := range []string{"on", "1", "true", "ON", "anything"} {
		t.Setenv(EnvUniqueGapExempt, value)
		t.Setenv(EnvUniquePopTicket, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket {
			t.Fatalf("switches at %q = %+v, want both on", value, rules)
		}
	}
}

/*
================
TestUniqueRulesFromEnvExplicitOffIsNative
================
*/
func TestUniqueRulesFromEnvExplicitOffIsNative(t *testing.T) {
	for _, value := range []string{"off", "0", "false", "OFF", " False "} {
		t.Setenv(EnvUniqueGapExempt, value)
		t.Setenv(EnvUniquePopTicket, "on")
		if rules := UniqueRulesFromEnv(); rules.GapExempt || !rules.PopTicket {
			t.Fatalf("gap switch at %q = %+v, want gap off and ticket on", value, rules)
		}
		t.Setenv(EnvUniqueGapExempt, "on")
		t.Setenv(EnvUniquePopTicket, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || rules.PopTicket {
			t.Fatalf("ticket switch at %q = %+v, want gap on and ticket off", value, rules)
		}
	}
}

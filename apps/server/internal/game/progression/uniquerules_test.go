/*
===========================================================================

uniquerules_test.go - native fallbacks and opt-in unique reward switches

The approved gap/ticket rules retain their default ON. The new equipment
level and seal rules need an explicit opt-in, and every rule can be disabled.

===========================================================================
*/
package progression

import "testing"

/*
================
TestUniqueRulesFromEnvDefaults
================
*/
func TestUniqueRulesFromEnvDefaults(t *testing.T) {
	t.Setenv(EnvUniqueGapExempt, "")
	t.Setenv(EnvUniquePopTicket, "")
	t.Setenv(EnvUniqueMaxLevelLoot, "")
	t.Setenv(EnvUniqueSealDrops, "")
	if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket || rules.MaxLevelLoot || rules.SealDrops {
		t.Fatalf("unset switches = %+v, want old rules on and new rules off", rules)
	}
	for _, value := range []string{"on", "1", "true", "ON", " True "} {
		t.Setenv(EnvUniqueGapExempt, value)
		t.Setenv(EnvUniquePopTicket, value)
		t.Setenv(EnvUniqueMaxLevelLoot, value)
		t.Setenv(EnvUniqueSealDrops, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket || !rules.MaxLevelLoot || !rules.SealDrops {
			t.Fatalf("switches at %q = %+v, want all on", value, rules)
		}
	}
}

/*
================
TestNewUniqueRulesRequireExplicitOptIn
================
*/
func TestNewUniqueRulesRequireExplicitOptIn(t *testing.T) {
	for _, value := range []string{"", "off", "0", "false", "anything"} {
		t.Setenv(EnvUniqueGapExempt, "anything")
		t.Setenv(EnvUniquePopTicket, "anything")
		t.Setenv(EnvUniqueMaxLevelLoot, value)
		t.Setenv(EnvUniqueSealDrops, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket || rules.MaxLevelLoot || rules.SealDrops {
			t.Fatalf("new rules at %q = %+v, want old defaults on and new rules off", value, rules)
		}
	}
}

/*
================
TestUniqueRulesFromEnvExplicitOffIsNative

Each switch turns off alone: the other three stay on when explicitly enabled.
================
*/
func TestUniqueRulesFromEnvExplicitOffIsNative(t *testing.T) {
	for _, value := range []string{"off", "0", "false", "OFF", " False "} {
		t.Setenv(EnvUniqueGapExempt, value)
		t.Setenv(EnvUniquePopTicket, "on")
		t.Setenv(EnvUniqueMaxLevelLoot, "on")
		t.Setenv(EnvUniqueSealDrops, "on")
		if rules := UniqueRulesFromEnv(); rules.GapExempt || !rules.PopTicket || !rules.MaxLevelLoot || !rules.SealDrops {
			t.Fatalf("gap switch at %q = %+v, want gap off and the rest on", value, rules)
		}
		t.Setenv(EnvUniqueGapExempt, "on")
		t.Setenv(EnvUniquePopTicket, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || rules.PopTicket || !rules.MaxLevelLoot || !rules.SealDrops {
			t.Fatalf("ticket switch at %q = %+v, want ticket off and the rest on", value, rules)
		}
		t.Setenv(EnvUniquePopTicket, "on")
		t.Setenv(EnvUniqueMaxLevelLoot, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket || rules.MaxLevelLoot || !rules.SealDrops {
			t.Fatalf("max-level switch at %q = %+v, want max-level off and the rest on", value, rules)
		}
		t.Setenv(EnvUniqueMaxLevelLoot, "on")
		t.Setenv(EnvUniqueSealDrops, value)
		if rules := UniqueRulesFromEnv(); !rules.GapExempt || !rules.PopTicket || !rules.MaxLevelLoot || rules.SealDrops {
			t.Fatalf("seal switch at %q = %+v, want seal off and the rest on", value, rules)
		}
	}
}

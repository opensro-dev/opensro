/*
===========================================================================

deepwater_test.go - the SRO_DEEP_WATER_REFUSAL switch, both settings

Off (native) accepts every destination, including the deep seabed a
character was stuck on in BR-261023-2025-B5DA; on refuses it.

===========================================================================
*/
package movement

import (
	"testing"
)

// The reported stand south of Jangan: seabed -94.7 under a surface at -80.
var strandedSeabed = moveTo(0x5EA9, 837.8, -94.7, 990.6)

/*
================
TestDeepWaterRefusalOffAcceptsSubmergedDestinations

The default validator, as the original server, accepts the synthetic
seabed, the real harbour floor and the reported seabed.
================
*/
func TestDeepWaterRefusalOffAcceptsSubmergedDestinations(t *testing.T) {
	synthetic := NewWaterValidator(syntheticWaterRoot(t))
	if refusal := synthetic.ValidateMovement(moveTo(0x6B4F, 100, 10, 100)); refusal != nil {
		t.Errorf("synthetic seabed refused with the gate off: %v", refusal)
	}
	authority := realAuthorityValidator(t)
	if refusal := authority.ValidateMovement(moveTo(0x6850, 700, -250, 100)); refusal != nil {
		t.Errorf("harbour floor refused with the gate off: %v", refusal)
	}
	if refusal := authority.ValidateMovement(strandedSeabed); refusal != nil {
		t.Errorf("reported seabed refused with the gate off: %v", refusal)
	}
}

/*
================
TestDeepWaterRefusalOnRefusesTheReportedSeabed

With the switch on, the reported stand is 14.7 units under the surface,
past MaxWadeDepth, and refused.
================
*/
func TestDeepWaterRefusalOnRefusesTheReportedSeabed(t *testing.T) {
	v := realAuthorityValidator(t)
	v.SetDeepWaterRefusal(true)
	refusal := v.ValidateMovement(strandedSeabed)
	if refusal == nil || refusal.NativeErrorCode != 0x02 {
		t.Fatalf("reported seabed with the gate on = %v, want a 0x02 refusal", refusal)
	}
}

/*
================
TestDeepWaterRefusalFromEnv
================
*/
func TestDeepWaterRefusalFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, " ON ": true, "1": true, "true": true} {
		t.Setenv(EnvDeepWaterRefusal, value)
		if got := DeepWaterRefusalFromEnv(); got != want {
			t.Errorf("%s=%q -> %v, want %v", EnvDeepWaterRefusal, value, got, want)
		}
	}
}

/*
===========================================================================

main_test.go - the compiled wire contracts have a release protocol

A change to the EnterWorld DTO or the character-list contract fails here,
at review time, until it is given a release protocol - instead of failing
release preparation, or worse, passing admission one component at a time.

===========================================================================
*/
package main

import "testing"

/*
================
TestCompiledContractsHaveReleaseProtocol
================
*/
func TestCompiledContractsHaveReleaseProtocol(t *testing.T) {
	if protocol := releaseProtocol(); protocol != 3 {
		t.Fatalf("release protocol = %d, want 3 (the item-based character list)", protocol)
	}
}

/*
================
TestReleaseProtocolsAreDistinct

Two protocol numbers naming the same contracts would make one of them
unreachable and the client declaration ambiguous.
================
*/
func TestReleaseProtocolsAreDistinct(t *testing.T) {
	seen := map[wireContracts]int{}
	for protocol, contracts := range releaseProtocols {
		if previous, ok := seen[contracts]; ok {
			t.Fatalf("release protocols %d and %d name the same contracts %+v", previous, protocol, contracts)
		}
		seen[contracts] = protocol
	}
}

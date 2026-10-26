/*
===========================================================================

main_test.go - sro-wtfixture tests: the loopback bind guard

The fixture admits anyone holding one published, reusable ticket, so the
guard is what keeps that ticket unreachable off the machine. Both
outcomes are pinned: every loopback spelling the flags may take is
accepted, and every route off the machine (a concrete external address,
the all-interfaces spellings, a malformed address) is refused.

===========================================================================
*/

package main

import "testing"

//============================================================================
// The bind guard
//============================================================================

/*
================
TestRefuseNonLoopbackAddr

Loopback spellings pass: 127.0.0.1 and the wider 127.0.0.0/8, localhost,
and ::1. Off-machine spellings fail: an external literal (192.0.2.1 is
TEST-NET-1, never assigned, so the case needs no DNS), the wildcard
binds, and an address with no port.
================
*/
func TestRefuseNonLoopbackAddr(t *testing.T) {
	accepted := []string{
		"127.0.0.1:18087",
		"127.0.0.2:18087",
		"127.255.0.9:18088",
		"localhost:18087",
		"[::1]:18088",
	}
	for _, addr := range accepted {
		if err := refuseNonLoopbackAddr(addr); err != nil {
			t.Errorf("%s: refused, want accepted: %v", addr, err)
		}
	}

	refused := []string{
		"192.0.2.1:18087",
		"0.0.0.0:18087",
		"[::]:18088",
		":18087",
		"127.0.0.1",
	}
	for _, addr := range refused {
		if err := refuseNonLoopbackAddr(addr); err == nil {
			t.Errorf("%s: accepted, want refused", addr)
		}
	}
}

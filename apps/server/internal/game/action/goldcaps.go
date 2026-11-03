/*
===========================================================================

goldcaps.go - the raised stall price and warehouse gold caps

Port-only, not native. The v1.150 client caps a stall's unit price at
1,000,000,000 (CIFStall's price editor, client 5A1D85 -> 5203B0) and a
warehouse gold deposit at 100,000,000 (client 59A430). With
SRO_RAISED_GOLD_CAPS on, this shard admits a 10,000,000,000 stall price
and a 1,000,000,000 deposit instead. Off is native: both caps and the
native u32 stall-offer wire are unchanged.

The browser learns the switch at world entry (enterworld Deps
RaisedGoldCaps), so its price editor and deposit dialog raise their own
caps in step; the server still checks every value it is sent.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvRaisedGoldCaps turns the wider stall and warehouse gold caps on.
const EnvRaisedGoldCaps = "SRO_RAISED_GOLD_CAPS"

const (
	// NativeStallPriceLimit is the v1.150 price editor's upper bound.
	NativeStallPriceLimit uint64 = 1_000_000_000
	// RaisedStallPriceLimit is the port-only bound (ten billion, eleven
	// decimal digits).
	RaisedStallPriceLimit uint64 = 10_000_000_000
	// NativeStorageGoldDepositLimit is client 59A430's warehouse deposit cap.
	NativeStorageGoldDepositLimit uint64 = 100_000_000
	// RaisedStorageGoldDepositLimit is the port-only deposit cap; it still
	// fits the native u32 amount on the wire.
	RaisedStorageGoldDepositLimit uint64 = 1_000_000_000
)

/*
================
RaisedGoldCapsFromEnv

The switch is on for "on", "1" or "true"; anything else, including unset,
is the native caps.
================
*/
func RaisedGoldCapsFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvRaisedGoldCaps))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
stallPriceLimit

The highest price an offer may carry (a unit price; a sale moves the
offer's price once, never price times quantity).
================
*/
func (rt *Runtime) stallPriceLimit() uint64 {
	if rt.RaisedGoldCaps {
		return RaisedStallPriceLimit
	}
	return NativeStallPriceLimit
}

/*
================
storageGoldDepositLimit

The highest amount one warehouse gold deposit may name. The guild
warehouse shares the personal handler (storageMutation), so it shares the
cap.
================
*/
func (rt *Runtime) storageGoldDepositLimit() uint64 {
	if rt.RaisedGoldCaps {
		return RaisedStorageGoldDepositLimit
	}
	return NativeStorageGoldDepositLimit
}

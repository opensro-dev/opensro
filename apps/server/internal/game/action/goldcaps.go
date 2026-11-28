/*
===========================================================================

goldcaps.go - the raised stall price and warehouse gold caps

Port-only, not native. The v1.150 client caps a stall's unit price at
1,000,000,000 (CIFStall's price editor, client 5A1D85 -> 5203B0) and a
warehouse gold deposit at 100,000,000 (client 59A430). With
SRO_RAISED_GOLD_CAPS on, this shard admits a 10,000,000,000 stall price
and a 10,000,000,000 deposit instead (owner, 2026-11-28; the deposit was
1,000,000,000). Off is native: both caps and the native u32 stall-offer
and gold-move wire are unchanged.

A deposit above 4,294,967,295 does not fit the native u32 gold move, so
it travels in the port's wide form (item/wire WideGoldDeposit), as the
raised stall price does.

The switch also takes a value: a bare number names the warehouse deposit
cap itself, up to MaxStorageGoldDepositLimit, without touching the stall
cap. The stall cap follows the switch alone.

The browser learns both at world entry (enterworld Deps RaisedGoldCaps
and StorageGoldDepositLimit), so its price editor and deposit dialog
raise their own caps in step; the server still checks every value it
is sent.

===========================================================================
*/
package action

import (
	"errors"
	"os"
	"strconv"
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
	// RaisedStorageGoldDepositLimit is the port-only deposit cap (owner,
	// 2026-11-28: ten billion, the raised stall price's bound).
	RaisedStorageGoldDepositLimit uint64 = 10_000_000_000
	// MaxStorageGoldDepositLimit is the most one deposit can ever name: the
	// largest integer the browser's dialog holds exactly (2^53 - 1), well
	// inside the wide gold move's u64 and the warehouse's int64 balance.
	MaxStorageGoldDepositLimit uint64 = 9_007_199_254_740_991
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
StorageGoldDepositLimitFromEnv

The warehouse deposit cap the environment selects. "on", "1" and "true"
keep the raised ten billion; a bare number names the cap itself, clamped
to MaxStorageGoldDepositLimit; unset, "off", junk and any number at or
below the native cap keep the native cap: the switch only raises (owner
2026-10-31), and a lower cap would refuse deposits the original admits.
================
*/
func StorageGoldDepositLimitFromEnv() uint64 {
	raw := strings.TrimSpace(os.Getenv(EnvRaisedGoldCaps))
	switch strings.ToLower(raw) {
	case "on", "1", "true":
		return RaisedStorageGoldDepositLimit
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if errors.Is(err, strconv.ErrRange) {
		return MaxStorageGoldDepositLimit
	}
	if err == nil && value > NativeStorageGoldDepositLimit {
		if value > MaxStorageGoldDepositLimit {
			return MaxStorageGoldDepositLimit
		}
		return value
	}
	return NativeStorageGoldDepositLimit
}

/*
================
StallPriceLimitFromEnv

The stall price cap the environment selects: the raised cap with the
switch on, the native cap otherwise. A numeric deposit cap does not
raise stall prices.
================
*/
func StallPriceLimitFromEnv() uint64 {
	if RaisedGoldCapsFromEnv() {
		return RaisedStallPriceLimit
	}
	return NativeStallPriceLimit
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
cap. A configured limit (the switch's numeric form, wired from
StorageGoldDepositLimitFromEnv) wins over the switch's two values.
================
*/
func (rt *Runtime) storageGoldDepositLimit() uint64 {
	if rt.StorageGoldDepositLimit > 0 {
		return rt.StorageGoldDepositLimit
	}
	if rt.RaisedGoldCaps {
		return RaisedStorageGoldDepositLimit
	}
	return NativeStorageGoldDepositLimit
}

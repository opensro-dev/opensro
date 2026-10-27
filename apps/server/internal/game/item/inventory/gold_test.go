package inventory

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

// The native clamp: sub_697e80 @0x00697eb5 on the way out, sub_759a30
// @0x0075a2c0 on the way in, both with the ceiling 0x5F5E100.
func TestGoldClampMatchesTheNativeCeiling(t *testing.T) {
	if wire.MaxGold != 100000000 {
		t.Fatalf("MaxGold = %d, want 100000000 (0x5F5E100)", wire.MaxGold)
	}

	cases := []struct {
		amount uint64
		want   uint32
	}{
		{0, 0},
		{1, 1},
		{uint64(wire.MaxGold) - 1, wire.MaxGold - 1},
		{uint64(wire.MaxGold), wire.MaxGold},
		{uint64(wire.MaxGold) + 1, wire.MaxGold},
		{^uint64(0), wire.MaxGold},
	}

	for _, testCase := range cases {
		if got := wire.ClampGold(testCase.amount); got != testCase.want {
			t.Fatalf("ClampGold(%d) = %d, want %d", testCase.amount, got, testCase.want)
		}
	}
}

// A balance past the ceiling must clamp rather than wrap on its way to a u32.
func TestClampGoldDoesNotTruncateLargeBalances(t *testing.T) {
	// 0x1_0000_0000 truncates to 0 in a naive uint32 conversion.
	if got := wire.ClampGold(0x100000000); got != wire.MaxGold {
		t.Fatalf("ClampGold(0x100000000) = %d, want %d", got, wire.MaxGold)
	}
}

func TestGoldHeapTierThresholds(t *testing.T) {
	cases := []struct {
		amount uint32
		want   string
	}{
		{1, GoldHeapSmall},
		{999, GoldHeapSmall},
		{1000, GoldHeapMedium},
		{9999, GoldHeapMedium},
		{10000, GoldHeapLarge},
		{wire.MaxGold, GoldHeapLarge},
	}

	for _, testCase := range cases {
		if got := GoldHeapTier(testCase.amount); got != testCase.want {
			t.Fatalf("GoldHeapTier(%d) = %s, want %s", testCase.amount, got, testCase.want)
		}
	}
}

func TestDropGoldDebitsTheBalance(t *testing.T) {
	balance, dropped, fault := DropGold(8800, 800)
	if fault != nil {
		t.Fatalf("DropGold refused: %v", fault)
	}
	if dropped != 800 {
		t.Fatalf("dropped = %d, want 800", dropped)
	}
	if balance != 8000 {
		t.Fatalf("balance = %d, want 8000", balance)
	}
}

func TestDropGoldAllowsTheWholeBalance(t *testing.T) {
	balance, dropped, fault := DropGold(8800, 8800)
	if fault != nil {
		t.Fatalf("dropping the whole balance was refused: %v", fault)
	}
	if balance != 0 || dropped != 8800 {
		t.Fatalf("balance = %d, dropped = %d; want 0 and 8800", balance, dropped)
	}
}

func TestDropGoldRefusals(t *testing.T) {
	cases := []struct {
		name      string
		balance   uint64
		requested uint32
		reason    string
	}{
		{"zero amount", 8800, 0, "invalidGoldAmount"},
		{"more than held", 8800, 8801, "insufficientGold"},
		{"nothing held", 0, 1, "insufficientGold"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			balance, dropped, fault := DropGold(testCase.balance, testCase.requested)
			if fault == nil {
				t.Fatal("DropGold was allowed, want a refusal")
			}
			if fault.Reason != testCase.reason {
				t.Fatalf("reason = %q, want %q", fault.Reason, testCase.reason)
			}
			if balance != testCase.balance {
				t.Fatalf("a refused drop changed the balance to %d, want %d", balance, testCase.balance)
			}
			if dropped != 0 {
				t.Fatalf("a refused drop reported %d dropped, want 0", dropped)
			}
		})
	}
}

// A request past the ceiling is clamped first, so it is then measured against
// the balance as the clamped amount rather than refused outright.
func TestDropGoldClampsBeforeCheckingTheBalance(t *testing.T) {
	balance, dropped, fault := DropGold(uint64(wire.MaxGold)+5000, wire.MaxGold+5000)
	if fault != nil {
		t.Fatalf("DropGold refused: %v", fault)
	}
	if dropped != wire.MaxGold {
		t.Fatalf("dropped = %d, want the clamped %d", dropped, wire.MaxGold)
	}
	if balance != 5000 {
		t.Fatalf("balance = %d, want 5000", balance)
	}
}

func TestPickupGoldCredits(t *testing.T) {
	if got := PickupGold(8000, 800); got != 8800 {
		t.Fatalf("PickupGold(8000, 800) = %d, want 8800", got)
	}
}

// The wire ceiling limits one operation, not the stored balance, so a pickup
// may take a balance past it. What it must not do is wrap.
func TestPickupGoldSaturatesInsteadOfWrapping(t *testing.T) {
	if got := PickupGold(^uint64(0), 1); got != ^uint64(0) {
		t.Fatalf("PickupGold at the uint64 ceiling = %d, want it to saturate", got)
	}
	if got := PickupGold(uint64(wire.MaxGold), wire.MaxGold); got != 2*uint64(wire.MaxGold) {
		t.Fatalf("PickupGold past the wire ceiling = %d, want %d", got, 2*uint64(wire.MaxGold))
	}
}

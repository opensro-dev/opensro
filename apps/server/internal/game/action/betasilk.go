/*
===========================================================================

betasilk.go - the closed-beta Item Mall silk allowance (port-only, not native)

Native silk is what the account's mall row holds. For the closed beta the
operator can give every account a silk allowance with one environment
variable (SRO_BETA_SILK, BUG-062) so testers can try the Item Mall: each
world entry refills it to the configured amount, and a purchase spends it
before the account's own silk.

The allowance lives only in this process. It never reaches the store, so
switching back is unsetting the variable and restarting the game world:
every silk balance is then exactly the native one. What testers bought
with the allowance stays bought: those items are ordinary inventory rows,
and the store keeps no record of which silk paid for them. Each world
entry refills the full amount, so a relog is a fresh allowance.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
)

// EnvBetaSilk is the beta silk allowance: "off" (native), "on" for
// BetaSilkDefault, or an amount.
const EnvBetaSilk = "SRO_BETA_SILK"

// BetaSilkDefault is the allowance "on" grants (BUG-062 asks for 100k).
const BetaSilkDefault = 100000

/*
================
BetaSilkFromEnv

Composition reads this once. An invalid value refuses startup instead of
silently changing the mall's currency.
================
*/
func BetaSilkFromEnv() (uint32, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaSilk)))
	switch value {
	case "", "off", "0", "false":
		return 0, nil
	case "on", "1", "true":
		return BetaSilkDefault, nil
	}
	amount, err := strconv.ParseUint(value, 10, 32)
	if err != nil || amount == 0 {
		return 0, fmt.Errorf("%s must be off, on or a silk amount", EnvBetaSilk)
	}
	return uint32(amount), nil
}

/*
================
betaSilk

A MallAuthority over the store that adds each account's allowance to its
silk. One lock serializes purchases so a spend cannot race a refill.
================
*/
type betaSilk struct {
	inner     domain.MallAuthority
	amount    uint32
	mu        sync.Mutex
	allowance map[string]uint32
}

/*
================
WithBetaSilk

The authority the mall uses: the store itself when the allowance is off,
so native mode runs exactly the native path. refill is the world-entry
hook that tops an account up; nil when off.
================
*/
func WithBetaSilk(inner domain.MallAuthority, amount uint32) (domain.MallAuthority, func(*domain.Character)) {
	if amount == 0 || inner == nil {
		return inner, nil
	}
	beta := &betaSilk{inner: inner, amount: amount, allowance: make(map[string]uint32)}
	return beta, beta.refill
}

/*
================
refill

World entry tops the account's allowance up to the configured amount.
================
*/
func (b *betaSilk) refill(character *domain.Character) {
	if character == nil || character.AccountID == "" {
		return
	}
	b.mu.Lock()
	b.allowance[character.AccountID] = b.amount
	b.mu.Unlock()
}

/*
================
withAllowance
================
*/
func withAllowance(balance domain.MallBalance, allowance uint32) domain.MallBalance {
	balance.Silk = uint32(min(uint64(balance.Silk)+uint64(allowance), math.MaxUint32))
	return balance
}

/*
================
MallBalance
================
*/
func (b *betaSilk) MallBalance(character *domain.Character) (domain.MallBalance, error) {
	balance, err := b.inner.MallBalance(character)
	if err != nil || character == nil {
		return balance, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return withAllowance(balance, b.allowance[character.AccountID]), nil
}

/*
================
PurchaseMall

The allowance pays first; the store debits only the rest, so a failed
purchase leaves both untouched.
================
*/
func (b *betaSilk) PurchaseMall(character *domain.Character, cost domain.MallBalance, grant func([]domain.InventoryRow) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	if character == nil {
		return b.inner.PurchaseMall(character, cost, grant)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	allowance := b.allowance[character.AccountID]
	spent := min(cost.Silk, allowance)
	rest := cost
	rest.Silk -= spent
	balance, err := b.inner.PurchaseMall(character, rest, grant)
	if err != nil {
		return withAllowance(balance, allowance), err
	}
	allowance -= spent
	b.allowance[character.AccountID] = allowance
	return withAllowance(balance, allowance), nil
}

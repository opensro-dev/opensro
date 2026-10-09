/*
===========================================================================

betasilk.go - the closed beta's earned Item Mall silk (port-only, not native)

Native silk is what the account's mall wallet holds. For the closed beta
(SRO_BETA_SILK) testers EARN silk by playing: every full hour a character
spends in the world credits the hourly rate to the account's real wallet
(mall_accounts.silk), until the wallet holds betaSilkBankCap. A first world
entry also creates the wallet with betaSilkStarter, once per account (the
wallet row is the persisted marker).

Credited silk is REAL: it survives restarts and deploys, and switching the
beta off stops new credits but does not take earned silk back. A launch must
wipe beta accounts or zero their silk (an owner decision).

The hour clock counts world-ready time only (sessions in the world tick, not
the title screen or character select) and lives in this process, so a
restart loses uncredited progress, including a failed credit awaiting retry;
it never credits the same completed payment twice. A credit pushes the new balance to the player's
session (MALL_BALANCE_CONTROL), so an open mall shows it at once.

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvBetaSilk is the beta silk earn rate: "off" (native), "on" for
// BetaSilkHourlyDefault, or silk per in-world hour.
const EnvBetaSilk = "SRO_BETA_SILK"

// BetaSilkHourlyDefault is the silk one in-world hour earns.
const BetaSilkHourlyDefault = 50

// betaSilkBankCap pauses earning once the wallet holds this much silk.
const betaSilkBankCap = 1500

// betaSilkStarter is the silk a new account's wallet starts with.
const betaSilkStarter = 300

// betaSilkHourMs is the in-world time one credit takes.
const betaSilkHourMs = 60 * 60 * 1000

// betaSilkMaxStepMs bounds the time one tick can add, so a stalled tick or a
// clock jump never credits idle time at once.
const betaSilkMaxStepMs = 60 * 1000

// opMallBalance pushes an account's mall balance to its session; the client
// updates an open mall (commerce-controls.ts MALL_BALANCE_CONTROL).
const opMallBalance uint16 = 17

// maxBetaSilkRate bounds an operator override.
const maxBetaSilkRate = 100000

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
	case "on", "true":
		return BetaSilkHourlyDefault, nil
	}
	rate, err := strconv.ParseUint(value, 10, 32)
	if err != nil || rate == 0 || rate > maxBetaSilkRate {
		return 0, fmt.Errorf("%s must be off, on or a silk amount per in-world hour (1-%d)", EnvBetaSilk, maxBetaSilkRate)
	}
	return uint32(rate), nil
}

/*
================
BetaSilkWallet

The persisted wallet the credits land in (store/betasilk.go).
================
*/
type BetaSilkWallet interface {
	domain.MallAuthority
	GrantBetaSilkStarter(accountID string, starter uint32) (bool, error)
	CreditBetaSilk(accountID string, amount, bankCap uint32) (domain.MallBalance, bool, error)
}

/*
================
betaSilkClock

One account's in-world time toward its next credit. generation is the last
tick the account was in the world, so time away is never counted.
================
*/
type betaSilkClock struct {
	earnedMs   int64
	lastMs     int64
	generation uint64
	capped     bool
	ready      bool
	retryAtMs  int64
}

/*
================
BetaSilk

The beta silk owner: the starter at world entry and the hourly credits.
================
*/
type BetaSilk struct {
	wallet     BetaSilkWallet
	rate       uint32
	mu         sync.Mutex
	generation uint64
	clocks     map[string]*betaSilkClock
}

/*
================
NewBetaSilk

nil when the rate is 0: native mode installs no hook at all.
================
*/
func NewBetaSilk(wallet BetaSilkWallet, rate uint32) *BetaSilk {
	if rate == 0 || wallet == nil {
		return nil
	}
	return &BetaSilk{wallet: wallet, rate: rate, clocks: make(map[string]*betaSilkClock)}
}

/*
================
Starter

World entry: create the account's wallet with the starter silk if it has
none. A failure is logged, never fatal to the entry.
================
*/
func (b *BetaSilk) Starter(character *domain.Character) {
	if b == nil || character == nil || character.AccountID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarter); err != nil {
		log.WithError(err).WithField("character", character.Name).Warn("beta silk: starter grant failed")
	}
}

/*
================
observeBalance

The mall wrapper observes every successful read and purchase. Capped time
never banks toward a later payment; spending resumes on the next tick.
The caller holds mu, before taking any wallet lock.
================
*/
func (b *BetaSilk) observeBalance(account string, balance domain.MallBalance) *betaSilkClock {
	clock := b.clocks[account]
	if clock == nil {
		clock = &betaSilkClock{}
		b.clocks[account] = clock
	}
	capped := balance.Silk >= betaSilkBankCap
	if clock.capped && !capped {
		clock.generation = 0
	}
	clock.ready = true
	clock.capped = capped
	if capped {
		clock.earnedMs = 0
	}
	return clock
}

/*
================
MallBalance

Retry an entry grant that failed before another mall operation can create
an hourly-only or purchase-only wallet and suppress the starter forever.
================
*/
func (b *BetaSilk) MallBalance(character *domain.Character) (domain.MallBalance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if character == nil {
		return domain.MallBalance{}, fmt.Errorf("beta silk: missing character")
	}
	if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarter); err != nil {
		return domain.MallBalance{}, err
	}
	balance, err := b.wallet.MallBalance(character)
	if err == nil {
		b.observeBalance(character.AccountID, balance)
	}
	return balance, err
}

/*
================
PurchaseMall

The real wallet still owns the atomic debit and grant. Observing the
committed balance resumes capped earnings without polling SQL per tick.
================
*/
func (b *BetaSilk) PurchaseMall(character *domain.Character, cost domain.MallBalance, grant func([]domain.InventoryRow) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if character == nil {
		return domain.MallBalance{}, fmt.Errorf("beta silk: missing character")
	}
	if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarter); err != nil {
		return domain.MallBalance{}, err
	}
	balance, err := b.wallet.PurchaseMall(character, cost, grant)
	if err == nil {
		b.observeBalance(character.AccountID, balance)
	}
	return balance, err
}

/*
================
Tick

Advance every in-world account's clock and credit each full hour. lookup
resolves a session's character. Returns the balance pushes for the
sessions whose wallet changed.
================
*/
func (b *BetaSilk) Tick(nowMs int64, sessions []simulation.SessionSnapshot, lookup func(division string, id int64) *domain.Character) []simulation.DivisionFrames {
	if b == nil || lookup == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.generation++
	accounts := make([]string, len(sessions))
	updates := make(map[string][]byte)
	for i, session := range sessions {
		character := lookup(session.DivisionID, session.CharacterID)
		if character == nil || character.AccountID == "" {
			continue
		}
		accounts[i] = character.AccountID
		clock := b.clocks[character.AccountID]
		if clock == nil {
			clock = &betaSilkClock{}
			b.clocks[character.AccountID] = clock
		}
		if !clock.ready {
			if nowMs < clock.retryAtMs {
				continue
			}
			if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarter); err != nil {
				clock.retryAtMs = nowMs + betaSilkMaxStepMs
				log.WithError(err).Warn("beta silk: starter retry failed")
				continue
			}
			balance, err := b.wallet.MallBalance(character)
			if err != nil {
				clock.retryAtMs = nowMs + betaSilkMaxStepMs
				log.WithError(err).Warn("beta silk: initial balance failed")
				continue
			}
			clock = b.observeBalance(character.AccountID, balance)
		}
		if clock.generation == b.generation {
			continue // a second session of one account counts once
		}
		if !clock.capped && clock.generation != 0 && clock.generation == b.generation-1 && nowMs > clock.lastMs {
			clock.earnedMs += min(nowMs-clock.lastMs, betaSilkMaxStepMs)
		}
		clock.lastMs, clock.generation = nowMs, b.generation
		if clock.capped || clock.earnedMs < betaSilkHourMs || nowMs < clock.retryAtMs {
			continue
		}
		balance, credited, err := b.wallet.CreditBetaSilk(character.AccountID, b.rate, betaSilkBankCap)
		if err != nil {
			clock.retryAtMs = nowMs + betaSilkMaxStepMs
			log.WithError(err).WithField("character", character.Name).Warn("beta silk: credit failed")
			continue
		}
		clock.retryAtMs = 0
		clock.earnedMs -= betaSilkHourMs
		b.observeBalance(character.AccountID, balance)
		if !credited {
			continue
		}
		payload, err := json.Marshal(balance)
		if err != nil {
			continue
		}
		updates[character.AccountID] = payload
	}
	var out []simulation.DivisionFrames
	for i, session := range sessions {
		if payload, ok := updates[accounts[i]]; ok {
			out = append(out, simulation.DivisionFrames{DivisionID: session.DivisionID, OnlyCharacterID: session.CharacterID,
				Frames: simFrames([]wire.Frame{{Opcode: opMallBalance, Payload: payload}})})
		}
	}
	return out
}

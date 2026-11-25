/*
===========================================================================

betasilk.go - the closed beta's earned Item Mall silk (port-only, not native)

Native silk is what the account's mall wallet holds. For the closed beta
(SRO_BETA_SILK) testers EARN silk by playing: every full hour a character
spends in the world credits the hourly rate to the account's earned
balance (mall_earned_silk, #4133), until the wallet's whole silk -
earned and granted together - holds betaSilkBankCap. A first world
entry also creates the wallet with the BetaSilkStarter tunable (700 by default), once per account (the
wallet row is the persisted marker).

Credited silk is REAL: it survives restarts and deploys, and switching the
beta off stops new credits but does not take earned silk back. The official
launch runs on a separate shard, so beta silk never reaches it (owner,
2026-10-10).

The hour clock counts world-ready time only (sessions in the world tick, not
the title screen or character select). Its uncredited time is persisted per
account (store beta_silk_progress, #4429): every GameWorld restart used to
drop each player's unfinished hour, and on a day of several deploys nobody
completed one. The time is saved every betaSilkSaveStepMs of accrual, so a
restart loses at most that, and a payment writes the hour's remainder in the
credit's own transaction, so it never credits the same completed payment
twice. A credit pushes the new
balance to the player's session (MALL_BALANCE_CONTROL), so an open mall
shows it at once. That control is a browser frame the client's world gate
must know, so it ships only in matching server and client releases (#529).

Earning is per GameWorld process, keyed by account: an account online on
two shards at once would earn on both. Harmless while the beta runs one
shard; a second beta shard needs a shard-wide owner first (#529).

The starter is once per wallet row, so an account that bought anything
before the beta silk shipped (#505) already has a row and gets no starter.
Accepted for the beta and announced; recorded so it is not rediscovered.

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
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvBetaSilk is the beta silk earn rate: "off" (native), "on" for
// BetaSilkHourlyDefault, or silk per in-world hour.
const EnvBetaSilk = "SRO_BETA_SILK"

// EnvBetaSilkActiveOnly restricts earning to active play (port-only, not
// native): with it on, an account's hour accrues only while its character
// has acted within the last betaSilkActivityWindowMs. Off (or unset) is the
// shipped rule: world-ready time accrues.
const EnvBetaSilkActiveOnly = "SRO_BETA_SILK_ACTIVE_ONLY"

// EnvEarnedSilkTradable is the mall purchase rule (port-only, not
// native, #4133): a purchase paid wholly from earned silk delivers a
// tradable item; anything else stays account-bound, as native. On (or
// unset) is the shipped rule, the owner's 2026-11-23 default; off makes
// every mall purchase bound, the native reading, whatever paid for it.
const EnvEarnedSilkTradable = "SRO_EARNED_SILK_TRADABLE"

// BetaSilkHourlyDefault is the silk one in-world hour earns.
const BetaSilkHourlyDefault = 50

// betaSilkBankCap pauses earning once the wallet holds this much silk.
const betaSilkBankCap = 1500

/*
================
betaSilkStarterSilk

The silk a new account's wallet starts with: the BetaSilkStarter tunable
read now, so a staff change reaches the next wallet created without a
restart. It is only ever an argument to GrantBetaSilkStarter, which grants
once, when the account has no wallet row; an existing wallet is never
topped up whatever this returns.
================
*/
func betaSilkStarterSilk() uint32 {
	return uint32(tunables.Current().Int(tunables.BetaSilkStarter))
}

// betaSilkHourMs is the in-world time one credit takes.
const betaSilkHourMs = 60 * 60 * 1000

// betaSilkForgetMs is how long an absent account keeps its partial hour.
const betaSilkForgetMs = 24 * 60 * 60 * 1000

// betaSilkMaxStepMs bounds the time one tick can add, so a stalled tick or a
// clock jump never credits idle time at once.
const betaSilkMaxStepMs = 60 * 1000

// betaSilkSaveStepMs is the accrued time between progress saves: the most
// a restart can lose, against one store write per account per step.
const betaSilkSaveStepMs = 60 * 1000

// betaSilkActivityWindowMs is how long one player action keeps an account's
// silk clock accruing under EnvBetaSilkActiveOnly.
const betaSilkActivityWindowMs = 10 * 60 * 1000

// opMallBalance pushes an account's mall balance to its session; the client
// updates an open mall (commerce-controls.ts MALL_BALANCE_CONTROL).
const opMallBalance uint16 = 17

/*
================
mallBalancePush

The push body: silk, earnedSilk, giftSilk and points. The hourly credit
lands in earnedSilk (#4133), so a push without it left the open mall's
Earned figure stale and the visible Silk unchanged - players read that
as no credit at all (#4429). Every tab the release gate admits decodes
the four-key body (mall.ts mallBalance accepts it since #4217, and the
protocol has moved past that release since).
================
*/
type mallBalancePush struct {
	Silk       uint32 `json:"silk"`
	EarnedSilk uint32 `json:"earnedSilk"`
	GiftSilk   uint32 `json:"giftSilk"`
	Points     uint32 `json:"points"`
}

/*
================
mallBalancePushPayload
================
*/
func mallBalancePushPayload(balance domain.MallBalance) ([]byte, error) {
	return json.Marshal(mallBalancePush{
		Silk:       balance.Silk,
		EarnedSilk: balance.EarnedSilk,
		GiftSilk:   balance.GiftSilk,
		Points:     balance.Points,
	})
}

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
	return ParseBetaSilk(os.Getenv(EnvBetaSilk))
}

/*
================
ParseBetaSilk

The runtime and deployer share the same earn-rate rule without changing
the process environment. Empty or off keeps the native wallet behaviour.
================
*/
func ParseBetaSilk(raw string) (uint32, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
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
BetaSilkActiveOnlyFromEnv

Composition reads this once, beside BetaSilkFromEnv.
================
*/
func BetaSilkActiveOnlyFromEnv() (bool, error) {
	return ParseBetaSilkActiveOnly(os.Getenv(EnvBetaSilkActiveOnly))
}

/*
================
ParseBetaSilkActiveOnly

Only an explicit on spelling turns the rule on; empty and the off spellings
are the shipped rule. Anything else is refused at startup, as a mistyped
rate is.
================
*/
func ParseBetaSilkActiveOnly(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "0", "false":
		return false, nil
	case "on", "1", "true":
		return true, nil
	}
	return false, fmt.Errorf("%s must be off or on", EnvBetaSilkActiveOnly)
}

/*
================
EarnedSilkTradableFromEnv

Composition reads this once, beside BetaSilkFromEnv.
================
*/
func EarnedSilkTradableFromEnv() (bool, error) {
	return ParseEarnedSilkTradable(os.Getenv(EnvEarnedSilkTradable))
}

/*
================
ParseEarnedSilkTradable

On is the shipped default (the owner's 2026-11-23 rule), so empty reads
on; off is the native reading, every mall item bound. Anything else is
refused at startup, as a mistyped rate is.
================
*/
func ParseEarnedSilkTradable(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "on", "1", "true":
		return true, nil
	case "off", "0", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s must be off or on", EnvEarnedSilkTradable)
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
	CreditBetaSilk(accountID string, amount, bankCap uint32, progressMs int64) (domain.MallBalance, bool, error)
	BetaSilkProgress(accountID string) (int64, error)
	SaveBetaSilkProgress(accountID string, earnedMs int64) error
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
	// activeMs is the tick that last saw a noted action (ActiveOnly);
	// acted says activeMs is set, since 0 is a valid clock reading.
	activeMs int64
	acted    bool
	// loaded says earnedMs resumed the persisted progress; savedMs is
	// what the store holds now.
	loaded  bool
	savedMs int64
}

/*
================
BetaSilk

The beta silk owner: the starter at world entry and the hourly credits.
ActiveOnly (EnvBetaSilkActiveOnly) gates the hourly accrual on recent player
action (NoteActivity); the starter and the credits themselves are unchanged.
================
*/
type BetaSilk struct {
	wallet BetaSilkWallet
	rate   uint32
	// ActiveOnly accrues the hour only for recently active accounts. Memory
	// only: a restart forgets activity (the next action resumes it), while
	// the uncredited time itself is persisted.
	ActiveOnly bool
	mu         sync.Mutex
	generation uint64
	clocks     map[string]*betaSilkClock
	// noted holds accounts that acted since the last Tick. The hub has no
	// world clock, so Tick stamps each note with its own nowMs: one clock.
	noted map[string]bool
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
	if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarterSilk()); err != nil {
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
	capped := uint64(balance.Silk)+uint64(balance.EarnedSilk) >= betaSilkBankCap
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
	if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarterSilk()); err != nil {
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
func (b *BetaSilk) PurchaseMall(character *domain.Character, cost domain.MallBalance, grant func(rows []domain.InventoryRow, earnedOnly bool) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if character == nil {
		return domain.MallBalance{}, fmt.Errorf("beta silk: missing character")
	}
	if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarterSilk()); err != nil {
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
SilkHistory

The wallet owns the recorded rows; the beta layer adds nothing.
================
*/
func (b *BetaSilk) SilkHistory(character *domain.Character, periodDays int, limit int) ([]domain.SilkHistoryRow, uint32, error) {
	return b.wallet.SilkHistory(character, periodDays, limit)
}

/*
================
Tick

Advance every in-world account's clock and credit each full hour. lookup
resolves a session's character. Returns the balance pushes for the
sessions whose wallet changed.
================
*/
func (b *BetaSilk) Tick(nowMs int64, sessions []simulation.SessionView, lookup func(division string, id int64) *domain.Character) []simulation.DivisionFrames {
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
			if _, err := b.wallet.GrantBetaSilkStarter(character.AccountID, betaSilkStarterSilk()); err != nil {
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
		if !clock.loaded {
			if nowMs < clock.retryAtMs {
				continue
			}
			progress, err := b.wallet.BetaSilkProgress(character.AccountID)
			if err != nil {
				clock.retryAtMs = nowMs + betaSilkMaxStepMs
				log.WithError(err).Warn("beta silk: progress read failed")
				continue
			}
			clock.loaded, clock.savedMs = true, progress
			// Capped time never banks; the save below then clears it.
			if !clock.capped {
				clock.earnedMs = max(clock.earnedMs, progress)
			}
		}
		if clock.generation == b.generation {
			continue // a second session of one account counts once
		}
		if b.noted[character.AccountID] {
			clock.activeMs, clock.acted = nowMs, true
			delete(b.noted, character.AccountID)
		}
		// Under ActiveOnly the step accrues only within the window after a
		// noted action; otherwise the hour stands still, as absence pauses it.
		active := !b.ActiveOnly || clock.acted && nowMs-clock.activeMs <= betaSilkActivityWindowMs
		if active && !clock.capped && clock.generation != 0 && clock.generation == b.generation-1 && nowMs > clock.lastMs {
			clock.earnedMs += min(nowMs-clock.lastMs, betaSilkMaxStepMs)
		}
		clock.lastMs, clock.generation = nowMs, b.generation
		if clock.capped || clock.earnedMs < betaSilkHourMs || nowMs < clock.retryAtMs {
			b.saveProgress(character.AccountID, clock)
			continue
		}
		balance, credited, err := b.wallet.CreditBetaSilk(character.AccountID, b.rate, betaSilkBankCap, clock.earnedMs-betaSilkHourMs)
		if err != nil {
			clock.retryAtMs = nowMs + betaSilkMaxStepMs
			log.WithError(err).WithField("character", character.Name).Warn("beta silk: credit failed")
			continue
		}
		clock.retryAtMs = 0
		clock.earnedMs -= betaSilkHourMs
		if credited {
			clock.savedMs = clock.earnedMs
		}
		b.observeBalance(character.AccountID, balance)
		b.saveProgress(character.AccountID, clock)
		if !credited {
			continue
		}
		payload, err := mallBalancePushPayload(balance)
		if err != nil {
			continue
		}
		updates[character.AccountID] = payload
	}
	// Absence pauses an account's hour without losing it, but an account
	// gone for betaSilkForgetMs is dropped rather than kept for every account
	// the process has ever seen (#529). On return it resumes the persisted
	// progress, with a fresh starter check and balance read.
	for account, clock := range b.clocks {
		if clock.generation != b.generation && nowMs-clock.lastMs > betaSilkForgetMs {
			delete(b.clocks, account)
		}
	}
	// A note from an account this tick did not see (left before the tick)
	// is stale by the next one.
	clear(b.noted)
	var out []simulation.DivisionFrames
	for i, session := range sessions {
		if payload, ok := updates[accounts[i]]; ok {
			out = append(out, simulation.DivisionFrames{DivisionID: session.DivisionID, OnlyCharacterID: session.CharacterID,
				Frames: simFrames([]wire.Frame{{Opcode: opMallBalance, Payload: payload}})})
		}
	}
	return out
}

/*
================
saveProgress

Persist the clock's uncredited time once it has grown a save step past
what the store holds, or at once when it fell (a cap cleared it). A
failed save is logged and retried a step later, never every tick. The
caller holds mu.
================
*/
func (b *BetaSilk) saveProgress(account string, clock *betaSilkClock) {
	if !clock.loaded || clock.earnedMs == clock.savedMs {
		return
	}
	if clock.earnedMs > clock.savedMs && clock.earnedMs-clock.savedMs < betaSilkSaveStepMs {
		return
	}
	if err := b.wallet.SaveBetaSilkProgress(account, clock.earnedMs); err != nil {
		log.WithError(err).Warn("beta silk: progress save failed")
	}
	clock.savedMs = clock.earnedMs
}

/*
================
NoteActivity

Records that an account acted. The transport hub calls it for every
dispatched game frame, so any player-sent command counts (#1604 P5) and a
heartbeat never does; inert unless ActiveOnly is on.
================
*/
func (b *BetaSilk) NoteActivity(accountID string) {
	if b == nil || !b.ActiveOnly || accountID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.noted == nil {
		b.noted = make(map[string]bool)
	}
	b.noted[accountID] = true
}

/*
===========================================================================

referrals.go - referral codes and invited accounts (#3518)

Port-only, not native (SRO_REFERRALS): the original has no referrals. Two
tables an older binary ignores, so they are created at open with no schema
stamp, as the economy buckets are (ensureReferrals):

  - referral_codes: one code per account, made the first time it is asked
    for and never changed afterwards;
  - referrals: one row per invited account, written once at sign-up, and
    marked once when it is rewarded.

A reward is one transaction: the row is marked qualified and both wallets
are credited with their silk_history rows, so a referral is paid exactly
once or not at all. The rows are the audit record: who invited whom, when,
when it qualified and what each side received.

Abuse costs nothing (owner rule): a referral pays only from a character's
real level, a code cannot name its own account, a link is refused once the
invited account has a character, and a held referral pays nothing until
staff approve it.

===========================================================================
*/
package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"opensro.online/server/internal/domain"
)

const referralSchema = `
CREATE TABLE IF NOT EXISTS referral_codes (
  account_id TEXT PRIMARY KEY,
  code       TEXT NOT NULL UNIQUE
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS referrals (
  referee_account  TEXT PRIMARY KEY,
  referrer_account TEXT    NOT NULL,
  linked_ms        INTEGER NOT NULL,
  held             INTEGER NOT NULL,
  qualified_ms     INTEGER NOT NULL,
  referrer_silk    INTEGER NOT NULL,
  referee_silk     INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS referrals_referrer ON referrals (referrer_account);
CREATE INDEX IF NOT EXISTS referrals_pending ON referrals (qualified_ms, held);
`

const (
	// referralCodeLength and referralCodeAlphabet: eight characters of
	// Crockford's base32 (no I, L, O, U), 40 bits; easy to read aloud and
	// type, and far too many to guess one that exists.
	referralCodeLength   = 8
	referralCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// referralCodeTries bounds the retries on a code collision.
	referralCodeTries = 5
)

// The refusals live in domain so the website routes can map them.
var (
	ErrReferralUnknownCode = domain.ErrReferralUnknownCode
	ErrReferralSelf        = domain.ErrReferralSelf
	ErrReferralLinked      = domain.ErrReferralLinked
	ErrReferralTooLate     = domain.ErrReferralTooLate
	ErrReferralNotFound    = domain.ErrReferralNotFound
)

/*
================
ensureReferrals

Creates the referral tables when the store opens: additive, so no stamp
moves and an older binary on the same file never notices them.
================
*/
func ensureReferrals(db *sql.DB) error {
	if _, err := db.Exec(referralSchema); err != nil {
		return fmt.Errorf("referrals: %w", err)
	}
	return nil
}

/*
================
NormalizeReferralCode

The code as stored: upper case, spaces and dashes dropped, and the letters
Crockford reads as digits (O, I, L) read as those digits. Empty when what
is left is not a code.
================
*/
func NormalizeReferralCode(raw string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "O", "0", "I", "1", "L", "1")
	code := replacer.Replace(strings.ToUpper(strings.TrimSpace(raw)))
	if len(code) != referralCodeLength {
		return ""
	}
	for _, r := range code {
		if !strings.ContainsRune(referralCodeAlphabet, r) {
			return ""
		}
	}
	return code
}

/*
================
newReferralCode

32 symbols divide 256 evenly, so a byte modulo the alphabet is uniform.
================
*/
func newReferralCode() (string, error) {
	raw := make([]byte, referralCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := make([]byte, referralCodeLength)
	for i, b := range raw {
		code[i] = referralCodeAlphabet[int(b)%len(referralCodeAlphabet)]
	}
	return string(code), nil
}

/*
================
ReferralCode

The account's code, made and kept the first time it is asked for.
================
*/
func (s *Store) ReferralCode(accountID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accountID == "" || s.db == nil {
		return "", fmt.Errorf("referral code: account unavailable")
	}
	db := s.syncDBLocked()
	var code string
	err := db.QueryRow("SELECT code FROM referral_codes WHERE account_id = ?", accountID).Scan(&code)
	if err == nil {
		return code, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	for range referralCodeTries {
		code, err = newReferralCode()
		if err != nil {
			return "", err
		}
		result, err := db.Exec("INSERT OR IGNORE INTO referral_codes (account_id, code) VALUES (?, ?)", accountID, code)
		if err != nil {
			return "", err
		}
		if inserted, _ := result.RowsAffected(); inserted == 1 {
			return code, nil
		}
	}
	return "", fmt.Errorf("referral code: no free code after %d tries", referralCodeTries)
}

/*
================
ReferralCodeOwner

The account that owns a code; ErrReferralUnknownCode when none does.
================
*/
func (s *Store) ReferralCodeOwner(code string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.referralCodeOwnerLocked(code)
}

/*
================
referralCodeOwnerLocked
================
*/
func (s *Store) referralCodeOwnerLocked(code string) (string, error) {
	code = NormalizeReferralCode(code)
	if code == "" || s.db == nil {
		return "", ErrReferralUnknownCode
	}
	var owner string
	err := s.syncDBLocked().QueryRow("SELECT account_id FROM referral_codes WHERE code = ?", code).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrReferralUnknownCode
	}
	return owner, err
}

/*
================
AccountMaxLevel

The highest level any live character of the account has reached (its
MaxLevel watermark), 0 when it has none. A level lost to death does not
undo a referral's progress.
================
*/
func (s *Store) AccountMaxLevel(accountID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accountMaxLevelLocked(accountID)
}

/*
================
accountMaxLevelLocked
================
*/
func (s *Store) accountMaxLevelLocked(accountID string) int64 {
	best := int64(0)
	for _, roster := range s.characters {
		for _, c := range roster {
			if c == nil || c.DeletePending || c.AccountID != accountID {
				continue
			}
			if c.Level != nil && *c.Level > best {
				best = *c.Level
			}
			if c.MaxLevel != nil && *c.MaxLevel > best {
				best = *c.MaxLevel
			}
		}
	}
	return best
}

/*
================
accountHasCharacterLocked

Any character at all, one pending deletion too: an account that ever
played is past sign-up.
================
*/
func (s *Store) accountHasCharacterLocked(accountID string) bool {
	for _, roster := range s.characters {
		for _, c := range roster {
			if c != nil && c.AccountID == accountID {
				return true
			}
		}
	}
	return false
}

/*
================
LinkReferral

Records that referee signed up with code. held marks it for staff approval
before it can pay. Refused for an unknown code, the account's own code, an
account that already names a referrer, and an account that already has a
character.
================
*/
func (s *Store) LinkReferral(referee, code string, held bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if referee == "" || s.db == nil {
		return fmt.Errorf("referral link: account unavailable")
	}
	referrer, err := s.referralCodeOwnerLocked(code)
	if err != nil {
		return err
	}
	if referrer == referee {
		return ErrReferralSelf
	}
	if s.accountHasCharacterLocked(referee) {
		return ErrReferralTooLate
	}
	result, err := s.syncDBLocked().Exec(`INSERT OR IGNORE INTO referrals
(referee_account, referrer_account, linked_ms, held, qualified_ms, referrer_silk, referee_silk)
VALUES (?, ?, ?, ?, 0, 0, 0)`, referee, referrer, s.now().UnixMilli(), held)
	if err != nil {
		return err
	}
	if inserted, _ := result.RowsAffected(); inserted == 0 {
		return ErrReferralLinked
	}
	return nil
}

const referralColumns = `referee_account, referrer_account, linked_ms, held, qualified_ms, referrer_silk, referee_silk`

/*
================
queryReferralsLocked
================
*/
func (s *Store) queryReferralsLocked(where string, args ...any) ([]domain.Referral, error) {
	if s.db == nil {
		return nil, fmt.Errorf("referrals: store unavailable")
	}
	rows, err := s.syncDBLocked().Query(`SELECT `+referralColumns+` FROM referrals `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Referral{}
	for rows.Next() {
		var r domain.Referral
		if err := rows.Scan(&r.Referee, &r.Referrer, &r.LinkedMs, &r.Held, &r.QualifiedMs, &r.ReferrerSilk, &r.RefereeSilk); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

/*
================
ReferralsBy

The accounts referrer invited, oldest first.
================
*/
func (s *Store) ReferralsBy(referrer string) ([]domain.Referral, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryReferralsLocked("WHERE referrer_account = ? ORDER BY linked_ms, referee_account", referrer)
}

/*
================
ReferralOf

The referral that invited referee; ErrReferralNotFound when none did.
================
*/
func (s *Store) ReferralOf(referee string) (domain.Referral, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found, err := s.queryReferralsLocked("WHERE referee_account = ?", referee)
	if err != nil {
		return domain.Referral{}, err
	}
	if len(found) == 0 {
		return domain.Referral{}, ErrReferralNotFound
	}
	return found[0], nil
}

/*
================
PendingReferrals

Up to limit referrals that can still pay (not rewarded, not held) whose
invited account sorts after after, in account order: a caller pages
through them all with the last one it saw, so a long queue of players who
never reach the level cannot hide the ones behind it.
================
*/
func (s *Store) PendingReferrals(after string, limit int) ([]domain.Referral, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryReferralsLocked(`WHERE qualified_ms = 0 AND held = 0 AND referee_account > ?
ORDER BY referee_account LIMIT ?`, after, limit)
}

/*
================
HeldReferrals

Up to limit referrals waiting for staff, oldest first.
================
*/
func (s *Store) HeldReferrals(limit int) ([]domain.Referral, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryReferralsLocked("WHERE qualified_ms = 0 AND held = 1 ORDER BY linked_ms LIMIT ?", limit)
}

/*
================
ApproveReferral

Staff clear a held referral; it pays like any other from then on.
================
*/
func (s *Store) ApproveReferral(referee string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return fmt.Errorf("referrals: store unavailable")
	}
	result, err := s.syncDBLocked().Exec("UPDATE referrals SET held = 0 WHERE referee_account = ?", referee)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return ErrReferralNotFound
	}
	return nil
}

/*
================
QualifyReferral

Pays referee's referral once, if it is pending and one of its characters
has reached level: the row is marked and both wallets credited in one
transaction. The referrer is paid only for its first rewards.RewardCap
paid invitations; the invited account's gift is always paid. False with
no error when there is nothing to pay.
================
*/
func (s *Store) QualifyReferral(referee string, level int64, rewards domain.ReferralRewards) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return false, fmt.Errorf("referrals: store unavailable")
	}
	if s.accountMaxLevelLocked(referee) < level {
		return false, nil
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var referrer string
	err = tx.QueryRow(`SELECT referrer_account FROM referrals
WHERE referee_account = ? AND qualified_ms = 0 AND held = 0`, referee).Scan(&referrer)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var paid int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM referrals
WHERE referrer_account = ? AND referrer_silk > 0`, referrer).Scan(&paid); err != nil {
		return false, err
	}
	referrerSilk := rewards.ReferrerSilk
	if paid >= rewards.RewardCap {
		referrerSilk = 0
	}
	atMs := s.now().UnixMilli()
	if _, err := tx.Exec(`UPDATE referrals SET qualified_ms = ?, referrer_silk = ?, referee_silk = ?
WHERE referee_account = ?`, atMs, referrerSilk, rewards.RefereeSilk, referee); err != nil {
		return false, err
	}
	if err := creditGiftSilkTx(tx, referrer, referrerSilk, atMs); err != nil {
		return false, err
	}
	if err := creditGiftSilkTx(tx, referee, rewards.RefereeSilk, atMs); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

/*
================
creditGiftSilkTx

Adds amount to the account's gift silk inside tx, creating the wallet
when it has none, and records the charge in silk_history. Referral
rewards are minted, not bought, so they land as gift silk: the Item Mall
prices every offer in gift silk too (refpricepolicyofitem currency 4, as
many rows as purchased silk), but the market never takes or pays it, so
a levelled alt cannot turn its reward into gold (opensro#3686). Zero
moves nothing and writes nothing; a total past the wallet's uint32 is
refused whole rather than clamped.
================
*/
func creditGiftSilkTx(tx *sql.Tx, accountID string, amount uint32, atMs int64) error {
	if amount == 0 {
		return nil
	}
	var balance domain.MallBalance
	err := tx.QueryRow("SELECT silk, gift_silk, points FROM mall_accounts WHERE account_id = ?", accountID).
		Scan(&balance.Silk, &balance.GiftSilk, &balance.Points)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if uint64(balance.GiftSilk)+uint64(amount) > math.MaxUint32 {
		return fmt.Errorf("gift silk credit: wallet would overflow")
	}
	balance.GiftSilk += amount
	_, err = tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET gift_silk = excluded.gift_silk`, accountID, balance.Silk, balance.GiftSilk, balance.Points)
	if err != nil {
		return err
	}
	return insertSilkHistoryTx(tx, accountID, atMs, silkHistoryCharge, domain.MallBalance{GiftSilk: amount})
}

/*
===========================================================================

betasilk_recovery_test.go - starter recovery through the real beta wallet

An unavailable starter write must not let a later credit or purchase create
a wallet that permanently suppresses the grant. Exercise the production
action owner against SQLite, including reopen and durable item delivery.

===========================================================================
*/
package store

import (
	"encoding/json"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/simulation"
)

const betaSilkRecoveryAccount = "beta-recovery"
const betaSilkRecoveryFailure = "starter temporarily unavailable"

/*
================
blockBetaSilkStarter

The real SQL write fails, rather than a fake wallet merely returning an
error. Remove this trigger to recover without replacing the action owner.
================
*/
func blockBetaSilkStarter(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec(`CREATE TRIGGER reject_beta_starter BEFORE INSERT ON mall_accounts
BEGIN SELECT RAISE(ABORT, 'starter temporarily unavailable'); END`); err != nil {
		t.Fatal(err)
	}
}

/*
================
unblockBetaSilkStarter
================
*/
func unblockBetaSilkStarter(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec("DROP TRIGGER reject_beta_starter"); err != nil {
		t.Fatal(err)
	}
}

/*
================
assertBetaSilkWallet

Check existence separately: a missing row and a zero wallet both read as
zero through the mall API, but only the latter suppresses another starter.
================
*/
func assertBetaSilkWallet(t *testing.T, s *Store, want uint32, exists bool) {
	t.Helper()
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM mall_accounts WHERE account_id = ?", betaSilkRecoveryAccount).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if (count == 1) != exists {
		t.Fatalf("wallet rows=%d, want exists=%v", count, exists)
	}
	balance, err := readMallBalance(s.db, betaSilkRecoveryAccount)
	if err != nil || balance != (domain.MallBalance{Silk: want}) {
		t.Fatalf("persisted balance=%+v err=%v, want silk=%d with no gift silk or points", balance, err, want)
	}
}

/*
================
TestBetaSilkStarterFailureRecoversBeforeHourlyCredit
================
*/
func TestBetaSilkStarterFailureRecoversBeforeHourlyCredit(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	character := seededCharacter()
	if err := s.CreateCharacter(testDivision, betaSilkRecoveryAccount, character); err != nil {
		t.Fatal(err)
	}
	beta := action.NewBetaSilk(s, 50)
	if beta == nil {
		t.Fatal("enabled beta owner missing")
	}
	lookup := func(division string, id int64) *domain.Character {
		if division == testDivision && id == character.ID {
			return character
		}
		return nil
	}
	online := []simulation.SessionView{{DivisionID: testDivision, CharacterID: character.ID}}
	const minuteMs int64 = 60 * 1000
	const startedMs int64 = 1_000_000
	blockBetaSilkStarter(t, s)
	beta.Starter(character)
	for _, now := range []int64{startedMs, startedMs + minuteMs} {
		if frames := beta.Tick(now, online, lookup); len(frames) != 0 {
			t.Fatalf("failed starter pushed frames: %+v", frames)
		}
		assertBetaSilkWallet(t, s, 0, false)
	}
	unblockBetaSilkStarter(t, s)
	const recoveredMs = startedMs + 2*minuteMs
	beta.Tick(recoveredMs, online, lookup)
	assertBetaSilkWallet(t, s, 300, true)
	for minute := int64(1); minute < 60; minute++ {
		if frames := beta.Tick(recoveredMs+minute*minuteMs, online, lookup); len(frames) != 0 {
			t.Fatalf("credited before a full recovered hour at minute %d: %+v", minute, frames)
		}
	}
	assertBetaSilkWallet(t, s, 300, true)
	frames := beta.Tick(recoveredMs+60*minuteMs, online, lookup)
	assertBetaSilkWallet(t, s, 350, true)
	if len(frames) != 1 || frames[0].DivisionID != testDivision || frames[0].OnlyCharacterID != character.ID || len(frames[0].Frames) != 1 {
		t.Fatalf("hourly balance push=%+v", frames)
	}
	var pushed domain.MallBalance
	frame := frames[0].Frames[0]
	if err := json.Unmarshal(frame.Payload, &pushed); err != nil || frame.Opcode != 17 || pushed != (domain.MallBalance{Silk: 350}) {
		t.Fatalf("hourly frame=%+v balance=%+v err=%v", frame, pushed, err)
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	characters := reopened.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 {
		t.Fatalf("reopened characters=%d, want 1", len(characters))
	}
	resumed := action.NewBetaSilk(reopened, 50)
	resumed.Starter(characters[0])
	resumed.Starter(characters[0])
	assertBetaSilkWallet(t, reopened, 350, true)
}

/*
================
TestBetaSilkStarterRecoveryBeforeMallUse

Neither opening the mall nor purchasing before the first world tick may
bypass a failed starter. A recovered purchase spends the starter once and
commits its inventory through the real store transaction.
================
*/
func TestBetaSilkStarterRecoveryBeforeMallUse(t *testing.T) {
	for _, firstUse := range []string{"balance", "purchase"} {
		t.Run(firstUse, func(t *testing.T) {
			dir := t.TempDir()
			s := openTest(t, dir, newTestClock())
			character := seededCharacter()
			if err := s.CreateCharacter(testDivision, betaSilkRecoveryAccount, character); err != nil {
				t.Fatal(err)
			}
			beta := action.NewBetaSilk(s, 50)
			if beta == nil {
				t.Fatal("enabled beta owner missing")
			}
			grants := 0
			grant := func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) {
				grants++
				rows[0].StackCount++
				return rows, nil
			}
			cost := domain.MallBalance{Silk: 25}
			blockBetaSilkStarter(t, s)
			beta.Starter(character)
			var err error
			if firstUse == "balance" {
				_, err = beta.MallBalance(character)
			} else {
				_, err = beta.PurchaseMall(character, cost, grant)
			}
			if err == nil || !strings.Contains(err.Error(), betaSilkRecoveryFailure) {
				t.Fatalf("mall use error=%v, want starter SQL failure", err)
			}
			if grants != 0 || character.MissionInventory[0].StackCount != 1 {
				t.Fatal("failed starter reached delivery or changed inventory")
			}
			assertBetaSilkWallet(t, s, 0, false)
			unblockBetaSilkStarter(t, s)
			if firstUse == "balance" {
				balance, err := beta.MallBalance(character)
				if err != nil || balance != (domain.MallBalance{Silk: 300}) {
					t.Fatalf("recovered mall balance=%+v err=%v", balance, err)
				}
			}
			balance, err := beta.PurchaseMall(character, cost, grant)
			if err != nil || balance != (domain.MallBalance{Silk: 275}) || grants != 1 || character.MissionInventory[0].StackCount != 2 {
				t.Fatalf("recovered purchase balance=%+v err=%v grants=%d inventory=%+v", balance, err, grants, character.MissionInventory)
			}
			balance, err = beta.MallBalance(character)
			if err != nil || balance != (domain.MallBalance{Silk: 275}) {
				t.Fatalf("repeat mall read regranted starter: balance=%+v err=%v", balance, err)
			}
			assertBetaSilkWallet(t, s, 275, true)
			s.Close()
			reopened := openTest(t, dir, newTestClock())
			characters := reopened.Characters().CharactersForDivision(testDivision)
			if len(characters) != 1 || characters[0].MissionInventory[0].StackCount != 2 {
				t.Fatalf("purchase inventory not durable: %+v", characters)
			}
			action.NewBetaSilk(reopened, 50).Starter(characters[0])
			assertBetaSilkWallet(t, reopened, 275, true)
		})
	}
}

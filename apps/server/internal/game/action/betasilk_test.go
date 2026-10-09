/*
===========================================================================

betasilk_test.go - the beta's earned Item Mall silk and its native mode

In-world time earns the hourly rate into the account's wallet, which stops
at the bank cap; a credit pushes the new balance to the player's session;
time away never counts; off, nothing is installed.

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
fakeSilkWallet

One account's silk, capped as the store caps it, with every starter grant.
================
*/
type fakeSilkWallet struct {
	silk     uint32
	starters int
	exists   bool
}

/*
================
GrantBetaSilkStarter
================
*/
func (w *fakeSilkWallet) GrantBetaSilkStarter(_ string, starter uint32) (bool, error) {
	if w.exists {
		return false, nil
	}
	w.exists, w.silk = true, starter
	w.starters++
	return true, nil
}

/*
================
CreditBetaSilk
================
*/
func (w *fakeSilkWallet) CreditBetaSilk(_ string, amount, bankCap uint32) (domain.MallBalance, bool, error) {
	if w.silk >= bankCap {
		return domain.MallBalance{Silk: w.silk}, false, nil
	}
	w.silk = min(w.silk+amount, bankCap)
	w.exists = true
	return domain.MallBalance{Silk: w.silk}, true, nil
}

/*
================
TestBetaSilkFromEnv
================
*/
func TestBetaSilkFromEnv(t *testing.T) {
	for _, value := range []struct {
		text string
		rate uint32
		bad  bool
	}{{"", 0, false}, {"off", 0, false}, {"on", BetaSilkHourlyDefault, false}, {"75", 75, false},
		{"-1", 0, true}, {"x", 0, true}, {"100001", 0, true}} {
		t.Setenv(EnvBetaSilk, value.text)
		rate, err := BetaSilkFromEnv()
		if (err != nil) != value.bad || rate != value.rate {
			t.Fatalf("%q: rate=%d err=%v", value.text, rate, err)
		}
	}
	if NewBetaSilk(&fakeSilkWallet{}, 0) != nil {
		t.Fatal("rate 0 must install nothing (native)")
	}
}

/*
================
TestBetaSilkEarnsPerInWorldHourUpToTheCap
================
*/
func TestBetaSilkEarnsPerInWorldHourUpToTheCap(t *testing.T) {
	wallet := &fakeSilkWallet{}
	beta := NewBetaSilk(wallet, 50)
	character := &domain.Character{ID: 7, Name: "Tester", AccountID: "acct"}
	lookup := func(_ string, id int64) *domain.Character {
		if id == character.ID {
			return character
		}
		return nil
	}
	beta.Starter(character)
	beta.Starter(character)
	if wallet.starters != 1 || wallet.silk != betaSilkStarter {
		t.Fatalf("starter: %d grants, silk %d", wallet.starters, wallet.silk)
	}
	online := []simulation.SessionSnapshot{{DivisionID: "d", CharacterID: character.ID}}
	now := int64(0)
	tick := func( /* one minute */ ) []simulation.DivisionFrames {
		now += 60 * 1000
		return beta.Tick(now, online, lookup)
	}
	beta.Tick(now, online, lookup)
	var pushed []simulation.DivisionFrames
	for range 59 {
		pushed = append(pushed, tick()...)
	}
	if wallet.silk != betaSilkStarter || len(pushed) != 0 {
		t.Fatalf("59 minutes credited: silk %d, %d pushes", wallet.silk, len(pushed))
	}
	pushed = tick()
	if wallet.silk != betaSilkStarter+50 || len(pushed) != 1 || pushed[0].OnlyCharacterID != character.ID {
		t.Fatalf("hour 1: silk %d, pushes %+v", wallet.silk, pushed)
	}
	var balance domain.MallBalance
	if frame := pushed[0].Frames[0]; frame.Opcode != opMallBalance || json.Unmarshal(frame.Payload, &balance) != nil || balance.Silk != wallet.silk {
		t.Fatalf("push: %+v", pushed[0].Frames[0])
	}

	// Time away (no session in the tick) never counts.
	now += 5 * betaSilkHourMs
	beta.Tick(now, nil, lookup)
	beta.Tick(now, online, lookup)
	if wallet.silk != betaSilkStarter+50 {
		t.Fatalf("away time credited: silk %d", wallet.silk)
	}

	// Earning stops at the bank cap: no credit and no push there.
	wallet.silk = betaSilkBankCap - 20
	for range 60 {
		tick()
	}
	if wallet.silk != betaSilkBankCap {
		t.Fatalf("cap clamp: silk %d", wallet.silk)
	}
	for i := 0; i < 60; i++ {
		if frames := tick(); len(frames) != 0 {
			t.Fatalf("a full wallet pushed %+v", frames)
		}
	}
	if wallet.silk != betaSilkBankCap {
		t.Fatalf("past the cap: silk %d", wallet.silk)
	}
}

/*
================
TestBetaSilkStallNeverCreditsIdleTime

A tick an hour after the last one adds at most one minute.
================
*/
func TestBetaSilkStallNeverCreditsIdleTime(t *testing.T) {
	wallet := &fakeSilkWallet{exists: true}
	beta := NewBetaSilk(wallet, 50)
	character := &domain.Character{ID: 1, AccountID: "acct"}
	online := []simulation.SessionSnapshot{{DivisionID: "d", CharacterID: 1}}
	lookup := func(string, int64) *domain.Character { return character }
	beta.Tick(0, online, lookup)
	beta.Tick(betaSilkHourMs, online, lookup)
	if wallet.silk != 0 {
		t.Fatalf("a stalled hour credited: silk %d", wallet.silk)
	}
}

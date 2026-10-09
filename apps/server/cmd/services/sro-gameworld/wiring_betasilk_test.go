/*
===========================================================================

wiring_betasilk_test.go - earned silk publication ordering and exact recipients

The real action publication gate and beta clock run against a locked wallet,
a detached session source and a barrier-controlled enqueue adapter.

===========================================================================
*/
package main

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
)

const (
	betaWiringDivision      = "beta-wiring"
	betaWiringMinute        = int64(time.Minute / time.Millisecond)
	betaWiringRate          = uint32(50)
	betaWiringInitial       = uint32(300)
	betaWiringCreditOpcode  = uint16(17)
	betaWiringCatalogOpcode = uint16(15)
	betaWiringTimeout       = 5 * time.Second
	betaWiringBlockedWindow = 25 * time.Millisecond
)

/*
================
betaWiringWallet
================
*/
type betaWiringWallet struct {
	mu      sync.Mutex
	silk    uint32
	credits int
}

/*
================
GrantBetaSilkStarter
================
*/
func (w *betaWiringWallet) GrantBetaSilkStarter(string, uint32) (bool, error) {
	return false, nil
}

/*
================
MallBalance
================
*/
func (w *betaWiringWallet) MallBalance(*domain.Character) (domain.MallBalance, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return domain.MallBalance{Silk: w.silk}, nil
}

/*
================
CreditBetaSilk
================
*/
func (w *betaWiringWallet) CreditBetaSilk(_ string, amount, cap uint32) (domain.MallBalance, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.silk >= cap {
		return domain.MallBalance{Silk: w.silk}, false, nil
	}
	w.silk = min(w.silk+amount, cap)
	w.credits++
	return domain.MallBalance{Silk: w.silk}, true, nil
}

/*
================
PurchaseMall
================
*/
func (w *betaWiringWallet) PurchaseMall(c *domain.Character, cost domain.MallBalance, grant func([]domain.InventoryRow) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cost.Silk > w.silk {
		return domain.MallBalance{}, domain.MallInsufficientCurrency{}
	}
	rows, err := grant(c.MissionInventory)
	if err != nil {
		return domain.MallBalance{}, err
	}
	c.MissionInventory = rows
	w.silk -= cost.Silk
	return domain.MallBalance{Silk: w.silk}, nil
}

/*
================
betaWiringSource
================
*/
type betaWiringSource struct {
	mu       sync.Mutex
	sessions []simulation.SessionSnapshot
	calls    atomic.Int32
}

/*
================
SnapshotSessions
================
*/
func (s *betaWiringSource) SnapshotSessions() []simulation.SessionSnapshot {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]simulation.SessionSnapshot(nil), s.sessions...)
}

/*
================
set
================
*/
func (s *betaWiringSource) set(sessions ...simulation.SessionSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = sessions
}

/*
================
betaWiringDelivery
================
*/
type betaWiringDelivery struct {
	session string
	frames  []simulation.Frame
}

/*
================
betaWiringPusher
================
*/
type betaWiringPusher struct {
	deliveries chan betaWiringDelivery
	beforePush func()
	once       sync.Once
}

/*
================
PushToSession
================
*/
func (p *betaWiringPusher) PushToSession(session string, frames []simulation.Frame) {
	if p.beforePush != nil {
		p.once.Do(p.beforePush)
	}
	p.deliveries <- betaWiringDelivery{session: session, frames: frames}
}

/*
================
PushToDivision
================
*/
func (p *betaWiringPusher) PushToDivision(division string, frames []simulation.Frame, _ string) {
	p.deliveries <- betaWiringDelivery{session: "unexpected division:" + division, frames: frames}
}

/*
================
betaWiringFixture
================
*/
func betaWiringFixture() (*gameplayPlane, *betaWiringWallet, *domain.Character) {
	character := &domain.Character{ID: 1, Name: "Tester", AccountID: "account"}
	foreign := &domain.Character{ID: 2, Name: "Foreign", AccountID: "foreign"}
	deps := &enterworld.Deps{Characters: enterworld.StaticCharacterSource{
		betaWiringDivision: {character}, "foreign": {foreign},
	}}
	wallet := &betaWiringWallet{silk: betaWiringInitial}
	return &gameplayPlane{
		divisionID: betaWiringDivision, deps: deps, items: action.NewRuntime(deps, nil),
		movement: &movement.Runtime{}, betaSilk: action.NewBetaSilk(wallet, betaWiringRate),
	}, wallet, character
}

/*
================
TestBetaSilkWiringInstallsOnlyWhenEnabled
================
*/
func TestBetaSilkWiringInstallsOnlyWhenEnabled(t *testing.T) {
	game, _, _ := betaWiringFixture()
	enabled := game.newMissionTicker(&action.PeerReferenceCatalog{})
	game.betaSilk = nil
	disabled := game.newMissionTicker(&action.PeerReferenceCatalog{})
	if len(enabled.Hooks) != len(disabled.Hooks)+1 {
		t.Fatalf("enabled hooks %d, disabled hooks %d: off must install no beta hook", len(enabled.Hooks), len(disabled.Hooks))
	}
}

/*
================
TestBetaSilkWiringPublicationBarrier
================
*/
func TestBetaSilkWiringPublicationBarrier(t *testing.T) {
	for _, purchase := range []bool{false, true} {
		name := "catalog"
		if purchase {
			name = "purchase"
		}
		t.Run(name, func(t *testing.T) {
			game, wallet, character := betaWiringFixture()
			source := &betaWiringSource{}
			source.set(simulation.SessionSnapshot{SessionID: "1:1", DivisionID: betaWiringDivision, CharacterID: character.ID})
			push := &betaWiringPusher{deliveries: make(chan betaWiringDelivery, 4)}
			ticker := game.newMissionTicker(&action.PeerReferenceCatalog{})
			ticker.Source, ticker.Push = source, push
			// The transport timing hook remains last; exercise the installed hook.
			hook := ticker.Hooks[len(ticker.Hooks)-2]
			for minute := int64(0); minute < 60; minute++ {
				if out := hook(minute * betaWiringMinute); out != nil {
					t.Fatal("beta hook leaked generic routing")
				}
			}
			unlock := game.items.LockPublication(betaWiringDivision)
			var releaseGate sync.Once
			t.Cleanup(func() { releaseGate.Do(unlock) })
			entered, finishPush := make(chan struct{}), make(chan struct{})
			var releasePush sync.Once
			t.Cleanup(func() { releasePush.Do(func() { close(finishPush) }) })
			push.beforePush = func() {
				// Reenter the beta/wallet read door: enqueue must hold neither.
				_, _ = game.betaSilk.MallBalance(character)
				close(entered)
				<-finishPush
			}
			started, done := make(chan struct{}), make(chan []simulation.DivisionFrames, 1)
			beforeSnapshots := source.calls.Load()
			go func() { close(started); done <- hook(60 * betaWiringMinute) }()
			<-started
			wait.Consistently(t, betaWiringBlockedWindow, "publication gate blocks the beta snapshot", func() bool {
				return source.calls.Load() == beforeSnapshots
			})
			source.set(
				simulation.SessionSnapshot{SessionID: "1:2", DivisionID: betaWiringDivision, CharacterID: character.ID},
				simulation.SessionSnapshot{SessionID: "9:4", DivisionID: "foreign", CharacterID: 2},
			)
			releaseGate.Do(unlock)
			select {
			case <-entered:
			case <-time.After(betaWiringTimeout):
				t.Fatal("credit did not reach enqueue with wallet locks released")
			}
			competingStarted, competingDone := make(chan struct{}), make(chan error, 1)
			var admitted atomic.Bool
			go func() {
				close(competingStarted)
				release := game.items.LockPublication(betaWiringDivision)
				defer release()
				admitted.Store(true)
				var balance domain.MallBalance
				var err error
				if purchase {
					balance, err = game.betaSilk.PurchaseMall(character, domain.MallBalance{Silk: 30}, func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) { return rows, nil })
				} else {
					balance, err = game.betaSilk.MallBalance(character)
				}
				if err == nil {
					payload, marshalErr := json.Marshal(balance)
					err = marshalErr
					push.PushToSession("1:2", []simulation.Frame{{Opcode: betaWiringCatalogOpcode, Payload: payload}})
				}
				competingDone <- err
			}()
			<-competingStarted
			wait.Consistently(t, betaWiringBlockedWindow, "credit enqueue precedes competing mall admission", func() bool { return !admitted.Load() })
			releasePush.Do(func() { close(finishPush) })
			select {
			case out := <-done:
				if out != nil {
					t.Fatal("credit escaped into generic routing")
				}
			case <-time.After(betaWiringTimeout):
				t.Fatal("credit hook did not finish")
			}
			select {
			case err := <-competingDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(betaWiringTimeout):
				t.Fatal("competing publication did not finish")
			}
			wantFinal := betaWiringInitial + betaWiringRate
			if purchase {
				wantFinal -= 30
			}
			for index, want := range []uint32{betaWiringInitial + betaWiringRate, wantFinal} {
				delivery := <-push.deliveries
				var balance domain.MallBalance
				wantOpcode := betaWiringCreditOpcode
				if index == 1 {
					wantOpcode = betaWiringCatalogOpcode
				}
				if delivery.session != "1:2" || len(delivery.frames) != 1 || delivery.frames[0].Opcode != wantOpcode || json.Unmarshal(delivery.frames[0].Payload, &balance) != nil || balance.Silk != want {
					t.Fatalf("delivery %d = %+v balance %+v, want fresh session and silk %d", index, delivery, balance, want)
				}
			}
			if len(push.deliveries) != 0 || source.calls.Load() != beforeSnapshots+1 {
				t.Fatal("hook broadcast, duplicated delivery, or resnapshotted after credit")
			}
			balance, _ := wallet.MallBalance(character)
			if balance.Silk != wantFinal {
				t.Fatalf("final wallet %+v, want %d", balance, wantFinal)
			}
		})
	}
}

/*
================
TestBetaSilkWiringEmptyTickBreaksContinuityAndFiltersForeignDivision
================
*/
func TestBetaSilkWiringEmptyTickBreaksContinuityAndFiltersForeignDivision(t *testing.T) {
	game, wallet, character := betaWiringFixture()
	source := &betaWiringSource{}
	push := &betaWiringPusher{deliveries: make(chan betaWiringDelivery, 4)}
	ticker := &simulation.Ticker{Source: source, Push: push}
	own := simulation.SessionSnapshot{SessionID: "1:3", DivisionID: betaWiringDivision, CharacterID: character.ID}
	foreign := simulation.SessionSnapshot{SessionID: "9:4", DivisionID: "foreign", CharacterID: 2}
	source.set(own, foreign)
	for minute := int64(0); minute < 60; minute++ {
		game.betaSilkTick(ticker, minute*betaWiringMinute)
	}
	source.set(foreign)
	game.betaSilkTick(ticker, 60*betaWiringMinute)
	source.set(own, foreign)
	game.betaSilkTick(ticker, 61*betaWiringMinute)
	if wallet.credits != 0 || len(push.deliveries) != 0 {
		t.Fatal("foreign division or absent tick earned silk")
	}
	game.betaSilkTick(ticker, 62*betaWiringMinute)
	if wallet.credits != 1 || len(push.deliveries) != 1 {
		t.Fatalf("credits=%d pushes=%d, want one logical clock credit", wallet.credits, len(push.deliveries))
	}
	if got := <-push.deliveries; got.session != own.SessionID {
		t.Fatalf("wrong recipient %q", got.session)
	}
}

package bot_test

import (
	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

type missingPotion struct{ *fakeActions }

func (a missingPotion) UsePotion() error { return bot.ErrRecoveryUnavailable }

func recoveryScenario() (*bot.Brain, *fakeActions, fakeWorld) {
	return newBrain(), &fakeActions{}, fakeWorld{
		self:     bot.Self{HP: 10, MaxHP: 100, Region: 1},
		monsters: []bot.Actor{{GID: 1, Region: 1, X: 10}},
	}
}

func TestBrainHuntsWithoutARecoveryItem(t *testing.T) {
	brain, actions, world := recoveryScenario()
	for i := 0; i < 30; i++ {
		if err := brain.Tick(world, missingPotion{actions}); err != nil {
			t.Fatal(err)
		}
	}
	if len(actions.attacks) != 30 || brain.RecoveryReason() != "potion-unavailable" {
		t.Fatalf("attacks=%d reason=%s", len(actions.attacks), brain.RecoveryReason())
	}
}

func TestBrainBoundsRecoveryWithoutHPProgress(t *testing.T) {
	brain, actions, world := recoveryScenario()
	for i := 0; i < 30; i++ {
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	if actions.potions != 2 || len(actions.attacks) != 10 || brain.RecoveryReason() != "recovery-no-progress" {
		t.Fatalf("potions=%d attacks=%d reason=%s", actions.potions, len(actions.attacks), brain.RecoveryReason())
	}
	world.self.HP = 20
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if actions.potions != 3 || brain.State() != bot.StateRecover || brain.RecoveryReason() != "" {
		t.Fatalf("progress did not reset recovery: potions=%d state=%v reason=%s", actions.potions, brain.State(), brain.RecoveryReason())
	}
}

func TestRebirthChoiceRespectsLevelGate(t *testing.T) {
	for _, level := range []int{0, 1, 10, 11, 140} {
		want := wire.RebirthAtSpecifiedPoint
		if level >= 1 && level <= 10 {
			want = wire.RebirthAtPresentPoint
		}
		payload := bot.EncodeRebirth(level)
		if len(payload) != 1 || payload[0] != want {
			t.Fatalf("level %d: %v want %d", level, payload, want)
		}
	}
}

func TestSceneResetDropsOldHuntTarget(t *testing.T) {
	brain, actions, world := recoveryScenario()
	world.self.HP = 100
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil {
		t.Fatal("missing initial target")
	}
	brain.ResetScene()
	if brain.Target() != nil || brain.State() != bot.StateIdle {
		t.Fatal("stale hunt survived scene reset")
	}
}

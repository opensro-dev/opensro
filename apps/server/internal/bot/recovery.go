package bot

import (
	"errors"
	"opensro.online/server/internal/game/item/wire"
)

// ErrRecoveryUnavailable reports that the client has no configured recovery item.
var ErrRecoveryUnavailable = errors.New("bot: recovery item unavailable")

// RecoveryReason is the recovery fallback recorded with each decision.
func (b *Brain) RecoveryReason() string { return b.recoveryReason }

// recover bounds a low-HP hold. HP progress opens a new recovery window.
// Without progress, two requests ten ticks apart precede a hunt fallback.
func (b *Brain) recover(self Self, actions Actions) (bool, error) {
	if self.MaxHP <= 0 || float64(self.HP) >= b.cfg.PotionHPBelow*float64(self.MaxHP) {
		b.recoverySeen = false
		b.recoveryReason = ""
		return false, nil
	}
	if !b.recoverySeen || self.HP > b.recoveryHP {
		b.recoverySeen = true
		b.recoveryTicks = 0
		b.recoveryReason = ""
	}
	b.recoveryHP = self.HP
	if b.recoveryReason != "" {
		return false, nil
	}
	if b.recoveryTicks >= 20 {
		b.recoveryReason = "recovery-no-progress"
		return false, nil
	}
	attempt := b.recoveryTicks%10 == 0
	b.recoveryTicks++
	if !attempt {
		return true, nil
	}
	err := actions.UsePotion()
	if errors.Is(err, ErrRecoveryUnavailable) {
		b.recoveryReason = "potion-unavailable"
		return false, nil
	}
	return true, err
}

// ResetScene removes targets and movement holds after a server scene reset.
// The configured hunt destination and authored route remain available.
func (b *Brain) ResetScene() { *b = *NewBrain(b.cfg) }

// EncodeRebirth selects the allowed native rebirth choice. Unknown levels use town.
func EncodeRebirth(level int) []byte {
	if level >= 1 && level <= 10 {
		return EncodeLocalRebirth()
	}
	return []byte{wire.RebirthAtSpecifiedPoint}
}

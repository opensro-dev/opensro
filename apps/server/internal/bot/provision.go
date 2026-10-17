/*
===========================================================================

provision.go - create a bot account and a bot character

The provisioning path creates the account through the account authority and
the character through the character store, so the bot is an ordinary
account with an ordinary character. Every call refuses while the operator
switch (gate.go) is off.

Phase 0 of issue #74 delivers this path and its tests. The headless bot
service (phase 1) and the host tool (phase 3) call it.

===========================================================================
*/
package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/security/auth"
)

// createdAtFormat matches the client character-create timestamp
// (agentapi.createdAtTimeFormatISO), so a bot record is byte-compatible with
// a player record.
const createdAtFormat = "2006-01-02T15:04:05.000Z"

// DefaultModelCodename is the appearance of a bot. It is a normal player
// model, so the bot looks like a player in the world.
const DefaultModelCodename = "CHAR_CH_MAN_ADVENTURER"

// AccountAuthority creates a game account. *auth.Accounts satisfies it.
type AccountAuthority interface {
	Create(ctx context.Context, accountID, password string) (auth.AccountInfo, error)
}

// CharacterStore installs a character record. *store.Store satisfies it.
type CharacterStore interface {
	CreateCharacter(divisionID, accountID string, c *domain.Character) error
}

// Identity is one provisioned bot account with its character.
type Identity struct {
	Index         int
	AccountID     string
	CharacterName string
	DivisionID    string
	Password      string
}

// Provisioner creates bot accounts and characters through the ordinary
// authorities. Every call refuses while the operator switch is off.
type Provisioner struct {
	Accounts   AccountAuthority
	Characters CharacterStore
	DivisionID string
	// NewPassword mints an account password. Nil uses a 32-byte random
	// password. A test injects a constant.
	NewPassword func() (string, error)
	// Now is the creation clock. Nil uses the wall clock.
	Now func() time.Time
}

// Provision creates the account and the character of bot number index.
func (p Provisioner) Provision(ctx context.Context, index int) (Identity, error) {
	if err := RequireEnabled(); err != nil {
		return Identity{}, err
	}
	if index < 0 || index >= MaxCount {
		return Identity{}, fmt.Errorf("bot: index %d outside 0..%d", index, MaxCount-1)
	}
	if p.Accounts == nil || p.Characters == nil {
		return Identity{}, errors.New("bot: account authority and character store are required")
	}
	division := p.DivisionID
	if division == "" {
		division = domain.DefaultDivisionID
	}
	account := AccountID(index)
	name := CharacterName(index)
	if !domain.AccountIDValid(account) {
		return Identity{}, fmt.Errorf("bot: invalid account id %q", account)
	}
	newPassword := p.NewPassword
	if newPassword == nil {
		newPassword = RandomPassword
	}
	password, err := newPassword()
	if err != nil {
		return Identity{}, fmt.Errorf("bot: password for %s: %w", account, err)
	}
	if _, err := p.Accounts.Create(ctx, account, password); err != nil {
		return Identity{}, fmt.Errorf("bot: create account %s: %w", account, err)
	}
	character := NewCharacter(name, p.now())
	if err := p.Characters.CreateCharacter(division, account, character); err != nil {
		return Identity{}, fmt.Errorf("bot: create character %s: %w", name, err)
	}
	return Identity{
		Index:         index,
		AccountID:     account,
		CharacterName: name,
		DivisionID:    division,
		Password:      password,
	}, nil
}

func (p Provisioner) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// RandomPassword returns a 32-byte random account password in hex. The bot
// account is a login, so the password is never weak.
func RandomPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// NewCharacter builds the persisted record of a bot. The fields copy the
// client character-create defaults (agentapi.characterFromCreate), so the
// store accepts a bot like any player character.
func NewCharacter(name string, at time.Time) *domain.Character {
	level := int64(1)
	gold := int64(0)
	visualFlags := int64(domain.VisualFlagBeginner)
	height := int64(0)
	volume := int64(0)
	weapon := int64(0)
	protector := int64(0)
	bodyShape := (volume << 4) | (height & 0x0f)
	c := &domain.Character{
		Name:           name,
		ModelCodename:  DefaultModelCodename,
		HeightIndex:    &height,
		VolumeIndex:    &volume,
		WeaponIndex:    &weapon,
		ProtectorIndex: &protector,
		BodyShapeByte:  &bodyShape,
		Level:          &level,
		Gold:           &gold,
		VisualFlags:    &visualFlags,
		CreatedAt:      at.UTC().Format(createdAtFormat),
	}
	race := domain.ResolveCharacterRaceIndex(c)
	gender := domain.ResolveCharacterGenderIndex(c)
	c.RaceIndex = &race
	c.Gender = &gender
	return c
}

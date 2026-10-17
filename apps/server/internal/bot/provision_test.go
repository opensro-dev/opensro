package bot_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/security/auth"
)

type fakeAccounts struct {
	created []string
	err     error
}

func (f *fakeAccounts) Create(_ context.Context, accountID, password string) (auth.AccountInfo, error) {
	if f.err != nil {
		return auth.AccountInfo{}, f.err
	}
	f.created = append(f.created, accountID+"="+password)
	return auth.AccountInfo{ID: accountID}, nil
}

type fakeCharacters struct {
	division string
	account  string
	created  []*domain.Character
	err      error
}

func (f *fakeCharacters) CreateCharacter(divisionID, accountID string, c *domain.Character) error {
	if f.err != nil {
		return f.err
	}
	f.division, f.account = divisionID, accountID
	f.created = append(f.created, c)
	return nil
}

func newProvisioner(accounts bot.AccountAuthority, characters bot.CharacterStore) bot.Provisioner {
	return bot.Provisioner{
		Accounts:    accounts,
		Characters:  characters,
		DivisionID:  domain.DefaultDivisionID,
		NewPassword: func() (string, error) { return "test-password", nil },
		Now:         func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func TestProvisionRefusesWhenSwitchIsOff(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "")
	accounts := &fakeAccounts{}
	characters := &fakeCharacters{}
	_, err := newProvisioner(accounts, characters).Provision(context.Background(), 1)
	if !errors.Is(err, bot.ErrDisabled) {
		t.Fatalf("Provision off = %v, want ErrDisabled", err)
	}
	if len(accounts.created) != 0 || len(characters.created) != 0 {
		t.Fatal("a refused provisioning touched an authority")
	}
}

func TestProvisionCreatesAccountAndCharacter(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "1")
	accounts := &fakeAccounts{}
	characters := &fakeCharacters{}
	identity, err := newProvisioner(accounts, characters).Provision(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if identity.AccountID != bot.AccountID(7) || identity.CharacterName != bot.CharacterName(7) {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.DivisionID != domain.DefaultDivisionID || identity.Password != "test-password" {
		t.Fatalf("identity = %+v", identity)
	}
	if len(accounts.created) != 1 || accounts.created[0] != bot.AccountID(7)+"=test-password" {
		t.Fatalf("accounts created = %v", accounts.created)
	}
	if characters.division != domain.DefaultDivisionID || characters.account != bot.AccountID(7) {
		t.Fatalf("character owner = %s/%s", characters.division, characters.account)
	}
	if len(characters.created) != 1 || characters.created[0].Name != bot.CharacterName(7) {
		t.Fatalf("characters created = %+v", characters.created)
	}
}

func TestProvisionRefusesAnOutOfRangeIndex(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "1")
	accounts := &fakeAccounts{}
	characters := &fakeCharacters{}
	provisioner := newProvisioner(accounts, characters)
	for _, index := range []int{-1, bot.MaxCount} {
		if _, err := provisioner.Provision(context.Background(), index); err == nil {
			t.Fatalf("index %d was accepted", index)
		}
	}
	if len(accounts.created) != 0 || len(characters.created) != 0 {
		t.Fatal("an out-of-range index touched an authority")
	}
}

func TestProvisionReportsAnAccountFailure(t *testing.T) {
	t.Setenv(bot.EnvBotPlane, "1")
	accounts := &fakeAccounts{err: errors.New("db down")}
	characters := &fakeCharacters{}
	if _, err := newProvisioner(accounts, characters).Provision(context.Background(), 1); err == nil {
		t.Fatal("an account failure was ignored")
	}
	if len(characters.created) != 0 {
		t.Fatal("a failed account still created a character")
	}
}

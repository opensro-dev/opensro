package bot_test

import (
	"strings"
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
)

func TestBotNamesCarryTheConstantPrefixAndStayLegal(t *testing.T) {
	for _, index := range []int{0, 1, 42, bot.MaxCount - 1} {
		account := bot.AccountID(index)
		name := bot.CharacterName(index)
		if !strings.HasPrefix(account, bot.NamePrefix) || !strings.HasPrefix(name, bot.NamePrefix) {
			t.Fatalf("index %d: names %q/%q miss the prefix %q", index, account, name, bot.NamePrefix)
		}
		if !bot.IsBot(account) || !bot.IsBot(name) {
			t.Fatalf("index %d: IsBot false for %q/%q", index, account, name)
		}
		if !domain.AccountIDValid(account) {
			t.Fatalf("index %d: account id %q is not valid", index, account)
		}
		if !store.CharacterNameShapeValid(name) {
			t.Fatalf("index %d: character name %q violates the native shape", index, name)
		}
	}
}

func TestIsBotRejectsACommonName(t *testing.T) {
	if bot.IsBot("asd2") {
		t.Fatal("a common player name was reported as a bot")
	}
}

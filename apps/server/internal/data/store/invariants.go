package store

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Persisted authority invariants

The store owns these checks because every caller and every reboot must see the
same identity and wire-width rules. Protocol handlers may pre-check for better
client errors, but they are not the authority boundary.
================================================================================
*/

// Retail CICUser name presentation (858810) recognizes an actual [GM] prefix.
// Reserved operator names are persisted identities, never player-create input.
func persistedCharacterNameValid(name string) bool {
	return CharacterNameShapeValid(name) || strings.HasPrefix(name, "[GM]") &&
		len(name) <= domain.CharacterNameMaxBytes && CharacterNameShapeValid(name[4:])
}

func validateCharacterIdentity(character *domain.Character) error {
	if character == nil {
		return fmt.Errorf("nil character")
	}
	if character.ID < 1 || character.ID > domain.MaxCharacterID {
		return fmt.Errorf("character id %d is outside 1..%d", character.ID, domain.MaxCharacterID)
	}
	if !persistedCharacterNameValid(character.Name) {
		return fmt.Errorf("character name %q violates the native 2..12 ASCII shape", character.Name)
	}
	if !domain.AccountIDValid(character.AccountID) {
		return fmt.Errorf("character %q has an invalid account id", character.Name)
	}
	if !character.InventoryCapacityValid() {
		return fmt.Errorf("character %q inventory capacity %d (+%d waiting) is outside %d..%d",
			character.Name, character.InventorySize, character.InventoryExpansion,
			domain.DefaultInventorySize, domain.MaxInventorySize)
	}
	return nil
}

func validateGuildRecord(guild domain.GuildRecord) error {
	if guild.ID < 1 || guild.ID > domain.MaxGuildID {
		return fmt.Errorf("guild id %d is outside 1..%d", guild.ID, domain.MaxGuildID)
	}
	if len(guild.Name) == 0 || len(guild.Name) > domain.GuildNameMaxBytes {
		return fmt.Errorf("guild %d name length %d is outside 1..%d bytes", guild.ID, len(guild.Name), domain.GuildNameMaxBytes)
	}
	if len(guild.NoticeSubject) > domain.GuildNoticeSubjectBytes {
		return fmt.Errorf("guild %d notice subject exceeds %d bytes", guild.ID, domain.GuildNoticeSubjectBytes)
	}
	if len(guild.NoticeContents) > domain.GuildNoticeBodyBytes {
		return fmt.Errorf("guild %d notice contents exceed %d bytes", guild.ID, domain.GuildNoticeBodyBytes)
	}
	return nil
}

func validateLetterRecord(letter domain.LetterRecord) error {
	if !persistedCharacterNameValid(letter.Sender) {
		return fmt.Errorf("letter sender %q violates the native character-name shape", letter.Sender)
	}
	if len(letter.Body) == 0 || len(letter.Body) > domain.LetterBodyMaxBytes {
		return fmt.Errorf("letter body length %d is outside 1..%d bytes", len(letter.Body), domain.LetterBodyMaxBytes)
	}
	if letter.ReadFlag > 1 {
		return fmt.Errorf("letter read flag %d is not 0 or 1", letter.ReadFlag)
	}
	return nil
}

func validateGroundItemRecord(item domain.GroundItemRecord, counter uint32) error {
	if item.Gid <= domain.GroundItemGIDBase {
		return fmt.Errorf("gid %d is outside the ground entity band", item.Gid)
	}
	if item.Gid-domain.GroundItemGIDBase > counter {
		return fmt.Errorf("gid %d exceeds allocation counter %d", item.Gid, counter)
	}
	if item.RefObjID == 0 {
		return fmt.Errorf("refObjId is zero")
	}
	if len(item.Codename) > domain.GroundCodenameMaxBytes {
		return fmt.Errorf("codename length %d exceeds %d bytes", len(item.Codename), domain.GroundCodenameMaxBytes)
	}
	if item.GoldAmount > domain.GroundGoldAmountMax {
		return fmt.Errorf("gold amount %d exceeds %d", item.GoldAmount, domain.GroundGoldAmountMax)
	}
	if item.VarianceBits != "" {
		if _, err := strconv.ParseUint(item.VarianceBits, 10, 64); err != nil {
			return fmt.Errorf("varianceBits is not a u64 decimal: %w", err)
		}
	}
	for axis, value := range []float32{item.X, item.Y, item.Z} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("coordinate axis %d is not finite", axis)
		}
	}
	if item.DroppedBy != "" && !persistedCharacterNameValid(item.DroppedBy) {
		return fmt.Errorf("droppedBy %q violates the native character-name shape", item.DroppedBy)
	}
	return nil
}

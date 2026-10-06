/*
===========================================================================

shards.go - validate persisted shard ownership before serving the authority

===========================================================================
*/
package store

import (
	"fmt"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
)

// ValidateShardState proves that every persisted world belongs to the process
// catalog and every account roster satisfies the retail slot invariant.
//
// This is a boot gate, never a repair. Unknown shards and overflow characters
// require an explicit operator migration; silently merging, deleting, or
// reassigning authoritative state would destroy shard isolation.
/*
================
ValidateShardState
================
*/
func (s *Store) ValidateShardState(configuredShardIDs []string) error {
	allowed := make(map[string]bool, len(configuredShardIDs))
	for _, shardID := range configuredShardIDs {
		if shardID == "" || strings.TrimSpace(shardID) != shardID {
			return fmt.Errorf("configured shard id %q is empty or padded", shardID)
		}
		if allowed[shardID] {
			return fmt.Errorf("configured shard id %q is duplicated", shardID)
		}
		allowed[shardID] = true
	}
	if len(allowed) == 0 {
		return fmt.Errorf("no configured shards")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	persisted := make(map[string]bool)
	collectKeys(persisted, s.characters)
	collectKeys(persisted, s.deleted)
	collectKeys(persisted, s.ground.loadedRecords)
	collectKeys(persisted, s.mailboxes)
	collectKeys(persisted, s.guilds)
	collectKeys(persisted, s.guildMembers)
	collectKeys(persisted, s.camps)
	collectKeys(persisted, s.campMembers)
	collectKeys(persisted, s.meta.NextCharID)
	collectKeys(persisted, s.meta.NextGuildID)
	collectKeys(persisted, s.meta.TradeRewards)

	var unknown []string
	for shardID := range persisted {
		if !allowed[shardID] {
			unknown = append(unknown, shardID)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		return fmt.Errorf(
			"persisted state references shards absent from the catalog: %s",
			strings.Join(unknown, ", "),
		)
	}

	type rosterKey struct {
		shardID   string
		accountID string
	}
	rosters := make(map[rosterKey][]string)
	for shardID, characters := range s.characters {
		for _, character := range characters {
			if character == nil {
				continue
			}
			key := rosterKey{shardID: shardID, accountID: character.AccountID}
			rosters[key] = append(rosters[key], character.Name)
		}
	}
	var overflow []string
	for key, names := range rosters {
		if len(names) <= domain.MaxCharactersPerShardAccount {
			continue
		}
		sort.Strings(names)
		overflow = append(overflow, fmt.Sprintf(
			"%s/%s has %d (%s)",
			key.shardID,
			key.accountID,
			len(names),
			strings.Join(names, ", "),
		))
	}
	sort.Strings(overflow)
	if len(overflow) > 0 {
		return fmt.Errorf(
			"character rosters exceed the per-shard account maximum %d: %s",
			domain.MaxCharactersPerShardAccount,
			strings.Join(overflow, "; "),
		)
	}
	return nil
}

/*
================
collectKeys
================
*/
func collectKeys[T any](destination map[string]bool, source map[string]T) {
	for key := range source {
		destination[key] = true
	}
}

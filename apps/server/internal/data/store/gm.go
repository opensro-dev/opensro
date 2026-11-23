package store

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

// EnvGMCharacters is the operator's GM allowlist: a comma-separated list
// of division:character identities (character names are case-insensitive).
// It is the ONLY
// path that grants or keeps the persisted GMPrivilege flag - the bit the
// entered 0x32B3 stream emits into CICPlayer+0x1890 bit 0 (see the
// domain.Character field doc for the native evidence).
//
// Why an env allowlist and nothing else: privilege administration belongs
// to the host trust boundary, never a gameplay or agent endpoint. This
// remains true even though normal startup requires strict accounts and
// character-bound EnterWorld tickets: compromise of an ordinary account
// must not become a route to GM. The environment is host-access-only by
// construction - the same boundary that guards the store directory itself.
const EnvGMCharacters = "SRO_GM_CHARACTERS"

// GMIdentity is the complete privilege subject. Character names alone are
// insufficient because equal names may exist on different divisions.
// Capabilities are the staff.Capability bits the GM holds: every bit for a
// host allowlist entry, the book's set for a console grant (package staff).
type GMIdentity struct {
	DivisionID    string
	CharacterName string
	Capabilities  uint32
}

func (identity GMIdentity) key() string {
	return identity.DivisionID + "\x00" + strings.ToLower(identity.CharacterName)
}

func (identity GMIdentity) String() string {
	return identity.DivisionID + ":" + identity.CharacterName
}

// GMCharactersFromEnv parses the allowlist. Unset or empty means NO GMs:
// the reconcile demotes every persisted flag, so privilege can never
// outlive the operator's explicit grant (a store file copied from
// elsewhere cannot smuggle a GM in).
func GMCharactersFromEnv() ([]GMIdentity, error) {
	return ParseGMCharacters(os.Getenv(EnvGMCharacters))
}

// ParseGMCharacters validates and parses one operator-owned allowlist value.
// Deployment tooling uses the same parser as GameWorld startup so an invalid
// division-qualified identity is rejected before a job is submitted.
func ParseGMCharacters(value string) ([]GMIdentity, error) {
	var identities []GMIdentity
	for _, raw := range strings.Split(value, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		divisionID, characterName, found := strings.Cut(entry, ":")
		divisionID = strings.TrimSpace(divisionID)
		characterName = strings.TrimSpace(characterName)
		if !found || divisionID == "" || characterName == "" || strings.Contains(characterName, ":") {
			return nil, fmt.Errorf("%s entry %q must be division:character", EnvGMCharacters, raw)
		}
		identities = append(identities, GMIdentity{
			DivisionID:    divisionID,
			CharacterName: characterName,
		})
	}
	return identities, nil
}

// ReconcileGMPrivilege makes the staff sources (the host allowlist and the
// console's staff book, merged by the caller) the single authority over
// the persisted GMPrivilege flags and GMCapabilities bits: listed
// characters promote with their bits, every other record demotes. Runs at boot (wiring.go, right after the matured-
// deletion reap); one commit carries every changed record. Returns the
// promoted and demoted names so the boot log states the resulting GM set
// explicitly. A listed name matching no character warns loudly - a typo
// here would otherwise read as a silently missing GM.
func (s *Store) ReconcileGMPrivilege(allowed []GMIdentity) (promoted, demoted []string) {
	want := make(map[string]bool, len(allowed))
	capabilities := make(map[string]uint32, len(allowed))
	for _, identity := range allowed {
		want[identity.key()] = true
		capabilities[identity.key()] |= identity.Capabilities
	}

	s.mu.Lock()
	matched := map[string]bool{}
	var changed []*domain.Character
	for divisionID, records := range s.characters {
		for _, c := range records {
			identity := GMIdentity{DivisionID: divisionID, CharacterName: c.Name}
			key := identity.key()
			if want[key] {
				matched[key] = true
			}
			target := want[key]
			if c.GMPrivilege == target && c.GMCapabilities == capabilities[key] {
				continue
			}
			s.beginDoorLocked(c)
			flipped := c.GMPrivilege != target
			c.GMPrivilege, c.GMCapabilities = target, capabilities[key]
			changed = append(changed, c)
			if !flipped {
				continue
			}
			if target {
				promoted = append(promoted, identity.String())
			} else {
				demoted = append(demoted, identity.String())
			}
		}
	}
	if len(changed) > 0 {
		s.finishDoorLocked("gm-privilege reconcile", false, changed)
	}
	s.mu.Unlock()

	for _, identity := range allowed {
		if !matched[identity.key()] {
			log.Warnf("store: %s names %q but no such character identity exists; no privilege granted", EnvGMCharacters, identity)
		}
	}
	for _, identity := range promoted {
		log.Infof("store: GM privilege GRANTED to %q (%s)", identity, EnvGMCharacters)
	}
	for _, identity := range demoted {
		log.Infof("store: GM privilege revoked from %q (no longer in %s)", identity, EnvGMCharacters)
	}
	return promoted, demoted
}

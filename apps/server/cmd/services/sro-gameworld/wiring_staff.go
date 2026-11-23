/*
===========================================================================

wiring_staff.go - the staff book at boot, in the world and on the console

Port-only, not native; the default by owner decision 2026-11-23. One owner
per piece: the staff book (package staff) owns grants and roles; the
store's reconcile projects them onto characters at boot; the action
runtime projects each later change through the character door and tells
the clients (action/staff.go); the console reaches all of it through the
operator endpoint. The host allowlist (SRO_GM_CHARACTERS) stays the
host's: its GMs hold every capability, wear the gm role, and the console
can neither change nor revoke them.

===========================================================================
*/
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/staff"
)

/*
================
staffSources

The two sources of staff a shard has: the console's book and the host
allowlist.
================
*/
type staffSources struct {
	book      *staff.Book
	allowlist []store.GMIdentity
}

/*
================
openStaffSources

Opens the book (a corrupt one refuses the boot) and reads the allowlist,
then projects both onto the stored characters before anyone can enter.
================
*/
func openStaffSources(authority *store.Store, shard string) (staffSources, error) {
	allowlist, err := store.GMCharactersFromEnv()
	if err != nil {
		return staffSources{}, fmt.Errorf("GM allowlist: %w", err)
	}
	for i := range allowlist {
		allowlist[i].Capabilities = uint32(staff.All)
	}
	book, err := staff.OpenBook(filepath.Join(store.DirForShardFromEnv(shard), staff.FileName), time.Now, store.WriteFileAtomic)
	if err != nil {
		return staffSources{}, err
	}
	sources := staffSources{book: book, allowlist: allowlist}
	authority.ReconcileGMPrivilege(sources.identities(shard))
	return sources, nil
}

/*
================
staffSources.identities

The allowlist and the book's grants as one reconcile set. A character in
both keeps the allowlist's full set (the reconcile ORs the bits).
================
*/
func (sources staffSources) identities(shard string) []store.GMIdentity {
	identities := append([]store.GMIdentity(nil), sources.allowlist...)
	for _, grant := range sources.book.Grants(shard) {
		mask, err := staff.Mask(grant.Capabilities)
		if err != nil {
			continue // OpenBook refused unknown ids already
		}
		identities = append(identities, store.GMIdentity{DivisionID: grant.Division, CharacterName: grant.Character, Capabilities: uint32(mask)})
	}
	return identities
}

/*
================
staffSources.allowlisted

The allowlist's character names in the division.
================
*/
func (sources staffSources) allowlisted(division string) []string {
	var names []string
	for _, identity := range sources.allowlist {
		if identity.DivisionID == division {
			names = append(names, identity.CharacterName)
		}
	}
	return names
}

func (sources staffSources) isAllowlisted(division, name string) bool {
	for _, allowed := range sources.allowlisted(division) {
		if strings.EqualFold(allowed, name) {
			return true
		}
	}
	return false
}

/*
================
wireStaff

Gives the action runtime the roster and adds the staff frames to every
world entry, after the other entry frames.
================
*/
func (game *gameplayPlane) wireStaff(sources staffSources) {
	game.items.StaffRoster = func(division string) []staff.RosterEntry {
		return sources.book.Roster(division, sources.allowlisted(division))
	}
	previous := game.deps.EntryEventFrames
	game.deps.EntryEventFrames = func(division string, character *enterworld.Character) []enterworld.Packet {
		var packets []enterworld.Packet
		if previous != nil {
			packets = previous(division, character)
		}
		for _, frame := range game.items.StaffEntryFrames(division, character) {
			packets = append(packets, enterworld.NewPacket(frame.Opcode, frame.Payload))
		}
		return packets
	}
}

/*
================
staffRequest

The staff book's request for a console staff-grant: the one place a
console role becomes the approver authority the book checks.
================
*/
func staffRequest(request agentapi.PlayerOperation, shard, name string) staff.Request {
	return staff.Request{Division: shard, Character: name, Role: request.Role, Capabilities: request.Capabilities,
		Operator: request.Operator, Approver: request.ConsoleRole == "approver"}
}

/*
================
staffOperations

The console's staff hooks. A grant or revoke resolves the character first,
so the book never records one the world cannot project; an allowlist GM is
refused (the host owns it).
================
*/
func staffOperations(game *gameplayPlane, sources staffSources, shard string) *agentapi.StaffOperations {
	read := func() (any, error) {
		return map[string]any{"shard": shard, "roles": sources.book.Roles(), "capabilities": staff.Rows,
			"grants": sources.book.Grants(shard), "allowlist": sources.allowlisted(shard)}, nil
	}
	target := func(name string) (string, error) {
		resolved, err := game.items.OperatorStaffTarget(shard, name)
		if err != nil {
			return "", err
		}
		if sources.isAllowlisted(shard, resolved) {
			return "", fmt.Errorf("%s is a host allowlist GM (SRO_GM_CHARACTERS); change it on the host", resolved)
		}
		return resolved, nil
	}
	return &agentapi.StaffOperations{
		Read: read,
		Grant: func(request agentapi.PlayerOperation) (any, error) {
			name, err := target(request.Character)
			if err != nil {
				return nil, err
			}
			grant, mask, err := sources.book.Grant(staffRequest(request, shard, name))
			if errors.Is(err, staff.ErrElevated) {
				return nil, fmt.Errorf("%w: %w", agentapi.ErrForbidden, err)
			}
			if err != nil {
				return nil, err
			}
			if err := game.items.OperatorSetStaff(shard, name, true, mask); err != nil {
				return nil, fmt.Errorf("grant saved but not applied (it applies at the next boot): %w", err)
			}
			return grant, nil
		},
		Revoke: func(request agentapi.PlayerOperation) (any, error) {
			name, err := target(request.Character)
			if err != nil {
				return nil, err
			}
			grant, err := sources.book.Revoke(shard, name)
			if err != nil {
				return nil, err
			}
			if err := game.items.OperatorSetStaff(shard, name, false, 0); err != nil {
				return nil, fmt.Errorf("revoke saved but not applied (it applies at the next boot): %w", err)
			}
			return grant, nil
		},
		SetRole: func(request agentapi.PlayerOperation) (any, error) {
			var edit staff.Role
			decoder := json.NewDecoder(bytes.NewReader(request.StaffRole))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&edit); err != nil {
				return nil, fmt.Errorf("staff role: %w", err)
			}
			role, err := sources.book.SetRole(edit)
			if err != nil {
				return nil, err
			}
			log.WithFields(log.Fields{"role": role.ID, "tag": role.Tag, "operator": request.Operator}).Info("staff: role saved")
			game.items.PushStaffRoster(shard)
			return role, nil
		},
	}
}

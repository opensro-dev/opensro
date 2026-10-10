/*
===========================================================================

player_operations.go - console composition for the running shard authority

An optional private credential enables this surface. Recovery first evicts
the selected character and acquires the existing binding-control lease;
other players and the GameWorld process continue running.

===========================================================================
*/
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/transport"
)

const playerOperationWait = 5 * time.Second
const playerOperationPoll = 50 * time.Millisecond

/*
================
playerOperationControl

The binding lease excludes reconnects while a disconnected recovery commits.
================
*/
type playerOperationControl struct {
	hub       *transport.Hub
	authority *store.Store
	shard     string
}

/*
================
run

AcquireBindingControl evicts a bound session and waits for its final teardown.
Storage can fail during teardown as well as during the requested mutation.
The store retains dirty in-memory changes on failure; never report them as a
durable success or encourage blindly replaying an audited operation.
================
*/
func (control playerOperationControl) run(name, label string, apply func() error) error {
	if control.authority.Health().LastError != "" {
		return fmt.Errorf("storage is unhealthy; %s refused", label)
	}
	key := control.shard + ":" + strings.ToLower(name)
	timeout := time.NewTimer(playerOperationWait)
	defer timeout.Stop()
	poll := time.NewTicker(playerOperationPoll)
	defer poll.Stop()
	for {
		lease, acquired := control.hub.AcquireBindingControl(key)
		if acquired {
			defer lease.Release()
			break
		}
		select {
		case <-timeout.C:
			return fmt.Errorf("character session is still closing; inspect before retrying")
		case <-poll.C:
		}
	}
	if control.authority.Health().LastError != "" {
		return fmt.Errorf("storage is unhealthy after session teardown; %s refused", label)
	}
	if err := apply(); err != nil {
		return err
	}
	if control.authority.Health().LastError != "" {
		return fmt.Errorf("%s persistence failed; in-memory changes may remain; inspect storage before retrying", label)
	}
	return nil
}

/*
================
installPlayerOperations
================
*/
func installPlayerOperations(api *agentapi.API, game *gameplayPlane, hub *transport.Hub, authority *store.Store, shard string) error {
	state := store.DirForShardFromEnv(shard)
	bytes, err := os.ReadFile(filepath.Join(state, "operator-token"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	read := func(name string) (any, error) {
		if name == "" {
			return map[string]any{"towns": game.items.OperatorTowns()}, nil
		}
		player, err := game.items.OperatorCharacter(shard, name)
		if err != nil {
			return nil, err
		}
		session, bound := hub.BoundSession(shard + ":" + strings.ToLower(name))
		player["bound"] = bound
		if bound {
			player["session"] = map[string]any{"id": session.ID, "ready": session.WorldReady(), "evicted": session.Evicted()}
		}
		return map[string]any{"shard": shard, "capturedAt": time.Now().UTC().Format(time.RFC3339Nano), "player": player,
			"towns": game.items.OperatorTowns(), "storage": authority.Health()}, nil
	}
	control := playerOperationControl{hub: hub, authority: authority, shard: shard}
	return api.InstallPlayerOperations(agentapi.PlayerOperations{
		Token: strings.TrimSpace(string(bytes)), AuditPath: filepath.Join(state, "operator-audit.jsonl"), Read: read,
		ClearPK: func(request agentapi.PlayerOperation) (any, error) {
			if err := control.run(request.Character, "PK clear", func() error {
				return game.items.OperatorClearPK(shard, request.Character)
			}); err != nil {
				return nil, err
			}
			return read(request.Character)
		},
		ResetStats: func(request agentapi.PlayerOperation) (any, error) {
			if err := control.run(request.Character, "stat reset", func() error {
				return game.items.OperatorResetStats(shard, request.Character)
			}); err != nil {
				return nil, err
			}
			return read(request.Character)
		},
		GrantItems: func(request agentapi.PlayerOperation) (any, error) {
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("storage is unhealthy; item grant refused")
			}
			grants := make([]inventory.ItemAmount, len(request.Items))
			for i, item := range request.Items {
				grants[i] = inventory.ItemAmount{Codename: item.Codename, Count: item.Count}
			}
			if err := game.items.OperatorGrantItems(shard, request.Character, grants); err != nil {
				return nil, err
			}
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("item grant persistence failed; inspect storage before retrying")
			}
			return read(request.Character)
		},
		GrantSilk: func(request agentapi.PlayerOperation) (any, error) {
			if authority.Health().LastError != "" {
				return nil, fmt.Errorf("storage is unhealthy; silk grant refused")
			}
			balance, err := game.items.GrantSilk(shard, request.Operator, "operator", request.Character, request.Silk)
			if err != nil {
				return nil, err
			}
			result, err := read(request.Character)
			if err != nil {
				return nil, err
			}
			// The dashboard reports the wallet the grant left.
			result.(map[string]any)["silkBalance"] = balance.Silk
			return result, nil
		},
		Rescue: func(request agentapi.PlayerOperation) (any, error) {
			valid := false
			for _, town := range game.items.OperatorTowns() {
				if town.ID == request.Town {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("unknown rescue town")
			}
			if err := control.run(request.Character, "rescue", func() error {
				return game.items.OperatorRescue(shard, request.Character, request.Town)
			}); err != nil {
				return nil, err
			}
			return read(request.Character)
		},
	})
}
